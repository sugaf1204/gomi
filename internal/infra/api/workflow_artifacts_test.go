package api_test

import (
	"net/http"
	"testing"
)

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
