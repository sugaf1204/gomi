package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// listItems is a test helper that lists a resource endpoint and returns the items array.
func listItems(t *testing.T, env testEnv, path, token string) []any {
	t.Helper()
	rec := doRequest(env.echo, http.MethodGet, path, nil, token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	items, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("expected items array from %s, got %v", path, body["items"])
	}
	return items
}

// requireItemCount is a test helper that asserts the number of items in a list response.
func requireItemCount(t *testing.T, env testEnv, path, token string, expected int) {
	t.Helper()
	items := listItems(t, env, path, token)
	if len(items) != expected {
		t.Fatalf("[%s] expected %d items, got %d", path, expected, len(items))
	}
}

// ---------------------------------------------------------------------------
// Workflow 1: Full Hypervisor Registration & VM Lifecycle
// ---------------------------------------------------------------------------

func TestWorkflow_HypervisorRegistrationAndVMLifecycle(t *testing.T) {
	env := setupTestEnv(t)

	// Step 1: Create registration token.
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/registration-tokens", nil, env.token)
	requireStatus(t, rec, http.StatusCreated)
	tokenBody := parseBody(t, rec)
	regToken, ok := tokenBody["token"].(string)
	if !ok || regToken == "" {
		t.Fatalf("expected non-empty registration token, got %v", tokenBody)
	}

	// Step 2: Hypervisor self-registers with the token (unauthenticated).
	regReq := map[string]any{
		"token":    regToken,
		"hostname": "hv-wf1",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
			"port": 16509,
		},
		"capacity": map[string]any{
			"cpuCores": 16,
			"memoryMB": 32768,
		},
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/register", regReq, "")
	requireStatus(t, rec, http.StatusCreated)
	registered := parseBody(t, rec)
	regHV, _ := registered["hypervisor"].(map[string]any)
	if regHV["name"] != "hv-wf1" {
		t.Fatalf("expected registered name hv-wf1, got %v", regHV["name"])
	}

	// Step 3: Verify hypervisor appears in list.
	items := listItems(t, env, "/api/v1/hypervisors", env.token)
	if len(items) != 1 {
		t.Fatalf("expected 1 hypervisor in list, got %d", len(items))
	}

	// Step 4: Verify hypervisor details are correct.
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors/hv-wf1", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	hvDetail := parseBody(t, rec)
	conn, _ := hvDetail["connection"].(map[string]any)
	if conn["host"] != "127.0.0.1" {
		t.Fatalf("expected host 127.0.0.1, got %v", conn["host"])
	}

	// Step 5: Create a cloud-init template.
	ciBody := map[string]any{
		"name":        "ci-wf1",
		"userData":    "#cloud-config\npackages:\n  - nginx\n",
		"description": "workflow 1 cloud-init",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", ciBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// Step 6: Create an OS image.
	imgBody := map[string]any{
		"name":      "ubuntu-wf1",
		"osFamily":  "ubuntu",
		"osVersion": "22.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/os-images", imgBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// Step 7: Create a VM referencing the hypervisor + OS image + cloud-init.
	vmBody := map[string]any{
		"name":          "vm-wf1",
		"hypervisorRef": "hv-wf1",
		"resources": map[string]any{
			"cpuCores": 4,
			"memoryMB": 8192,
			"diskGB":   80,
		},
		"osImageRef":   "ubuntu-wf1",
		"cloudInitRef": "ci-wf1",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// Step 8: Verify VM appears in list.
	requireItemCount(t, env, "/api/v1/virtual-machines", env.token, 1)

	// Step 9: Verify VM has correct hypervisorRef.
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines/vm-wf1", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	vmDetail := parseBody(t, rec)
	if vmDetail["hypervisorRef"] != "hv-wf1" {
		t.Fatalf("expected hypervisorRef hv-wf1, got %v", vmDetail["hypervisorRef"])
	}
	cloudInitRefs, _ := vmDetail["cloudInitRefs"].([]any)
	if len(cloudInitRefs) == 0 || cloudInitRefs[0] != "ci-wf1" {
		t.Fatalf("expected legacy cloudInitRef to map to cloudInitRefs[0], got %v", cloudInitRefs)
	}

	// Step 10: Attempt power-on (will get libvirt error but should not crash).
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines/vm-wf1/actions/power-on", nil, env.token)
	// Accept 500 (expected libvirt/SSH failure) but NOT 401/403 (auth issues).
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
		t.Fatalf("authenticated user should be allowed for power-on, got status %d", rec.Code)
	}

	// Step 11: Verify VM status is updated (expect Error phase due to no real libvirt).
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines/vm-wf1", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	vmAfterPower := parseBody(t, rec)
	phase, _ := vmAfterPower["phase"].(string)
	if phase != "Error" && phase != "Pending" && phase != "Running" {
		t.Fatalf("expected VM phase to be Error, Pending, or Running after power-on attempt, got %q", phase)
	}

	// Step 12: Delete VM.
	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/virtual-machines/vm-wf1", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)

	// Step 13: Verify VM no longer in list.
	requireItemCount(t, env, "/api/v1/virtual-machines", env.token, 0)

	// Step 14: Delete hypervisor.
	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/hypervisors/hv-wf1", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)

	// Step 15: Verify hypervisor no longer in list.
	requireItemCount(t, env, "/api/v1/hypervisors", env.token, 0)
}

// ---------------------------------------------------------------------------
// Workflow 2: Artifact Management (CloudInit + OSImage)
// ---------------------------------------------------------------------------

func TestWorkflow_ArtifactManagement(t *testing.T) {
	env := setupTestEnv(t)

	// Step 1: Create OS image.
	imgBody := map[string]any{
		"name":      "debian-wf2",
		"osFamily":  "debian",
		"osVersion": "13",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", imgBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// Step 2: Create cloud-init template.
	ciBody := map[string]any{
		"name":        "ci-wf2",
		"userData":    "#cloud-config\npackages:\n  - htop\n",
		"description": "workflow 2 cloud-init",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", ciBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// Step 3: Verify both appear in their respective lists.
	requireItemCount(t, env, "/api/v1/os-images", env.token, 1)
	requireItemCount(t, env, "/api/v1/cloud-init-templates", env.token, 1)

	// Step 4: Update cloud-init template (PUT).
	updateBody := map[string]any{
		"userData":    "#cloud-config\npackages:\n  - htop\n  - vim\n",
		"description": "updated workflow 2 cloud-init",
	}
	rec = doRequest(env.echo, http.MethodPut, "/api/v1/cloud-init-templates/ci-wf2", updateBody, env.token)
	requireStatus(t, rec, http.StatusOK)

	// Step 5: Verify update persisted.
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/cloud-init-templates/ci-wf2", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	ciGot := parseBody(t, rec)
	if ciGot["userData"] != "#cloud-config\npackages:\n  - htop\n  - vim\n" {
		t.Fatalf("expected updated userData, got %v", ciGot["userData"])
	}

	// Step 6: Delete in reverse order: cloud-init -> OS image.
	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/cloud-init-templates/ci-wf2", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)

	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/os-images/debian-wf2", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)

	// Step 7: Verify all are gone.
	requireItemCount(t, env, "/api/v1/cloud-init-templates", env.token, 0)
	requireItemCount(t, env, "/api/v1/os-images", env.token, 0)
}

// ---------------------------------------------------------------------------
// Workflow 4: VM Creation Validation
// ---------------------------------------------------------------------------

func TestWorkflow_VMCreationValidation(t *testing.T) {
	env := setupTestEnv(t)

	// Step 1: Try to create VM without creating hypervisor first -> should fail (400).
	vmBody := map[string]any{
		"name":          "vm-no-hv",
		"hypervisorRef": "nonexistent-hv",
		"resources":     map[string]any{"cpuCores": 2, "memoryMB": 4096, "diskGB": 40},
		"osImageRef":    "ubuntu-22.04",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)

	// Step 2: Create hypervisor.
	hvBody := map[string]any{
		"name":       "hv-val-wf4",
		"connection": map[string]any{"host": "127.0.0.1"},
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", hvBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// Step 2.5: Create OS image prerequisite for PXE install type mapping.
	osImageBody := map[string]any{
		"name":      "ubuntu-22.04",
		"osFamily":  "ubuntu",
		"osVersion": "22.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/os-images", osImageBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// Step 3: Try to create VM with empty name -> should fail (400).
	vmNoName := map[string]any{
		"name":          "",
		"hypervisorRef": "hv-val-wf4",
		"resources":     map[string]any{"cpuCores": 2, "memoryMB": 4096, "diskGB": 40},
		"osImageRef":    "ubuntu-22.04",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmNoName, env.token)
	requireStatus(t, rec, http.StatusBadRequest)

	// Step 4: Try to create VM with 0 resources -> should fail (400).
	vmZeroRes := map[string]any{
		"name":          "vm-zero-res",
		"hypervisorRef": "hv-val-wf4",
		"resources":     map[string]any{"cpuCores": 0, "memoryMB": 0, "diskGB": 0},
		"osImageRef":    "ubuntu-22.04",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmZeroRes, env.token)
	requireStatus(t, rec, http.StatusBadRequest)

	// Step 5: Create valid VM -> success.
	vmValid1 := map[string]any{
		"name":          "vm-valid-1",
		"hypervisorRef": "hv-val-wf4",
		"resources":     map[string]any{"cpuCores": 2, "memoryMB": 4096, "diskGB": 40},
		"osImageRef":    "ubuntu-22.04",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmValid1, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// Step 6: Create second VM on same hypervisor -> success.
	vmValid2 := map[string]any{
		"name":          "vm-valid-2",
		"hypervisorRef": "hv-val-wf4",
		"resources":     map[string]any{"cpuCores": 1, "memoryMB": 2048, "diskGB": 20},
		"osImageRef":    "ubuntu-22.04",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmValid2, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// Step 7: Verify list returns both VMs.
	requireItemCount(t, env, "/api/v1/virtual-machines", env.token, 2)

	// Step 8: Delete both VMs, then hypervisor.
	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/virtual-machines/vm-valid-1", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)
	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/virtual-machines/vm-valid-2", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)
	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/hypervisors/hv-val-wf4", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)

	// Verify all clean.
	requireItemCount(t, env, "/api/v1/virtual-machines", env.token, 0)
	requireItemCount(t, env, "/api/v1/hypervisors", env.token, 0)
}

// ---------------------------------------------------------------------------
// Workflow 6: Registration Token Security
// ---------------------------------------------------------------------------

func TestWorkflow_RegistrationTokenSecurity(t *testing.T) {
	env := setupTestEnv(t)

	// Step 1: Create token.
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/registration-tokens", nil, env.token)
	requireStatus(t, rec, http.StatusCreated)
	tokenBody := parseBody(t, rec)
	token1, ok := tokenBody["token"].(string)
	if !ok || token1 == "" {
		t.Fatalf("expected non-empty token, got %v", tokenBody)
	}

	// Step 2: Register hypervisor with token -> success.
	regReq := map[string]any{
		"token":    token1,
		"hostname": "hv-tok-1",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
			"port": 16509,
		},
		"capacity": map[string]any{
			"cpuCores": 4,
			"memoryMB": 8192,
		},
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/register", regReq, "")
	requireStatus(t, rec, http.StatusCreated)

	// Step 3: Try to register again with same token -> fail (used).
	regReq2 := map[string]any{
		"token":    token1,
		"hostname": "hv-tok-2",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
			"port": 16509,
		},
		"capacity": map[string]any{
			"cpuCores": 4,
			"memoryMB": 8192,
		},
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/register", regReq2, "")
	requireStatus(t, rec, http.StatusBadRequest)
	errBody := parseBody(t, rec)
	errMsg, _ := errBody["error"].(string)
	if errMsg == "" {
		t.Fatal("expected error message for used token")
	}

	// Step 4: Create another token.
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/registration-tokens", nil, env.token)
	requireStatus(t, rec, http.StatusCreated)
	tokenBody2 := parseBody(t, rec)
	token2, ok := tokenBody2["token"].(string)
	if !ok || token2 == "" {
		t.Fatalf("expected non-empty second token, got %v", tokenBody2)
	}

	// Step 5: Verify the token value differs from the first.
	if token1 == token2 {
		t.Fatal("expected different tokens on separate creation")
	}

	// Step 6: Try to register with random/fake token -> fail.
	fakeReq := map[string]any{
		"token":    "totally-fake-token-12345",
		"hostname": "hv-fake",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
			"port": 16509,
		},
		"capacity": map[string]any{
			"cpuCores": 2,
			"memoryMB": 4096,
		},
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/register", fakeReq, "")
	requireStatus(t, rec, http.StatusBadRequest)

	// Step 7: Try to register with empty token -> fail.
	emptyTokenReq := map[string]any{
		"token":    "",
		"hostname": "hv-empty",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
			"port": 16509,
		},
		"capacity": map[string]any{
			"cpuCores": 2,
			"memoryMB": 4096,
		},
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/register", emptyTokenReq, "")
	requireStatus(t, rec, http.StatusBadRequest)

	// Step 8: Verify that the second token can still be used (proves token isolation).
	regReq3 := map[string]any{
		"token":    token2,
		"hostname": "hv-tok-3",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
			"port": 16509,
		},
		"capacity": map[string]any{
			"cpuCores": 8,
			"memoryMB": 16384,
		},
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/register", regReq3, "")
	requireStatus(t, rec, http.StatusCreated)

	// Step 9: Verify second token is also now used.
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/register", regReq3, "")
	requireStatus(t, rec, http.StatusBadRequest)
}

// ---------------------------------------------------------------------------
// Workflow 7: Cross-resource Integrity (VM references valid hypervisor)
// ---------------------------------------------------------------------------

func TestWorkflow_CrossResourceIntegrity(t *testing.T) {
	env := setupTestEnv(t)

	// Create two hypervisors.
	for _, name := range []string{"hv-x1", "hv-x2"} {
		hv := map[string]any{
			"name":       name,
			"connection": map[string]any{"host": "127.0.0.1"},
		}
		rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", hv, env.token)
		requireStatus(t, rec, http.StatusCreated)
	}

	for _, image := range []struct {
		name     string
		osFamily string
		osVer    string
	}{
		{name: "ubuntu", osFamily: "ubuntu", osVer: "24.04"},
		{name: "debian", osFamily: "debian", osVer: "12"},
	} {
		img := map[string]any{
			"name":      image.name,
			"osFamily":  image.osFamily,
			"osVersion": image.osVer,
			"arch":      "amd64",
			"format":    "qcow2",
			"source":    "upload",
		}
		rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", img, env.token)
		requireStatus(t, rec, http.StatusCreated)
	}

	// Create VM on hv-x1.
	vmBody := map[string]any{
		"name":          "vm-x1",
		"hypervisorRef": "hv-x1",
		"resources":     map[string]any{"cpuCores": 1, "memoryMB": 1024, "diskGB": 10},
		"osImageRef":    "ubuntu",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// Create VM on hv-x2.
	vmBody2 := map[string]any{
		"name":          "vm-x2",
		"hypervisorRef": "hv-x2",
		"resources":     map[string]any{"cpuCores": 2, "memoryMB": 2048, "diskGB": 20},
		"osImageRef":    "debian",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmBody2, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// Verify VM details show correct hypervisorRef.
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines/vm-x1", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	vm1 := parseBody(t, rec)
	if vm1["hypervisorRef"] != "hv-x1" {
		t.Fatalf("expected hypervisorRef hv-x1, got %v", vm1["hypervisorRef"])
	}

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines/vm-x2", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	vm2 := parseBody(t, rec)
	if vm2["hypervisorRef"] != "hv-x2" {
		t.Fatalf("expected hypervisorRef hv-x2, got %v", vm2["hypervisorRef"])
	}

	// Verify list has exactly 2 VMs.
	requireItemCount(t, env, "/api/v1/virtual-machines", env.token, 2)

	// Cleanup.
	for _, name := range []string{"vm-x1", "vm-x2"} {
		rec = doRequest(env.echo, http.MethodDelete, "/api/v1/virtual-machines/"+name, nil, env.token)
		requireStatus(t, rec, http.StatusNoContent)
	}
	for _, name := range []string{"hv-x1", "hv-x2"} {
		rec = doRequest(env.echo, http.MethodDelete, "/api/v1/hypervisors/"+name, nil, env.token)
		requireStatus(t, rec, http.StatusNoContent)
	}
}

// ---------------------------------------------------------------------------
// Workflow 8: Full Resource JSON Response Verification
// ---------------------------------------------------------------------------

func TestWorkflow_ResponseJSONStructure(t *testing.T) {
	env := setupTestEnv(t)

	// Create a hypervisor and verify the response JSON has expected flat structure.
	hvBody := map[string]any{
		"name":       "hv-json",
		"connection": map[string]any{"type": "tcp", "host": "127.0.0.1", "port": 16509},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", hvBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	imgBody := map[string]any{
		"name":      "ubuntu",
		"osFamily":  "ubuntu",
		"osVersion": "24.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/os-images", imgBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// Verify flat structure by re-parsing with json.Unmarshal to a raw map.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to parse OS image JSON: %v", err)
	}

	// Flat structure: name, osFamily, etc. are top-level keys (no metadata/spec/status nesting).
	expectedKeys := []string{"name", "osFamily", "osVersion", "arch", "format", "source", "createdAt", "updatedAt"}
	for _, key := range expectedKeys {
		if _, ok := raw[key]; !ok {
			t.Fatalf("OS image response missing expected key %q", key)
		}
	}

	// Create a VM and verify its JSON structure.
	vmBody := map[string]any{
		"name":          "vm-json",
		"hypervisorRef": "hv-json",
		"resources":     map[string]any{"cpuCores": 1, "memoryMB": 1024, "diskGB": 10},
		"osImageRef":    "ubuntu",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	var vmRaw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &vmRaw); err != nil {
		t.Fatalf("failed to parse VM JSON: %v", err)
	}

	vmExpectedKeys := []string{"name", "hypervisorRef", "resources", "phase", "createdAt", "updatedAt"}
	for _, key := range vmExpectedKeys {
		if _, ok := vmRaw[key]; !ok {
			t.Fatalf("VM response missing expected key %q", key)
		}
	}

	// Verify list response structure.
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	var listRaw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &listRaw); err != nil {
		t.Fatalf("failed to parse list JSON: %v", err)
	}
	if _, ok := listRaw["items"]; !ok {
		t.Fatal("list response missing 'items' key")
	}

	// Cleanup.
	doRequest(env.echo, http.MethodDelete, "/api/v1/virtual-machines/vm-json", nil, env.token)
	doRequest(env.echo, http.MethodDelete, "/api/v1/hypervisors/hv-json", nil, env.token)
}
