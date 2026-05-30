package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

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
	if vm1["hypervisorRef"] != "hypervisors/hv-x1" {
		t.Fatalf("expected hypervisorRef hv-x1, got %v", vm1["hypervisorRef"])
	}

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines/vm-x2", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	vm2 := parseBody(t, rec)
	if vm2["hypervisorRef"] != "hypervisors/hv-x2" {
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
	if _, ok := listRaw["virtualMachines"]; !ok {
		t.Fatal("list response missing 'virtualMachines' key")
	}

	// Cleanup.
	doRequest(env.echo, http.MethodDelete, "/api/v1/virtual-machines/vm-json", nil, env.token)
	doRequest(env.echo, http.MethodDelete, "/api/v1/hypervisors/hv-json", nil, env.token)
}
