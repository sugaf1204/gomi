package api_test

import (
	"net/http"
	"testing"
)

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
