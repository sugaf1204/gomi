package api_test

import (
	"net/http"
	"testing"
)

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
