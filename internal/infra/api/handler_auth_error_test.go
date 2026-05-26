package api_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuth_Unauthenticated(t *testing.T) {
	env := setupTestEnv(t)

	// Unauthenticated request to GET /api/v1/hypervisors - should 401
	rec := doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors", nil, "")
	requireStatus(t, rec, http.StatusUnauthorized)
}

func TestAuth_InvalidToken(t *testing.T) {
	env := setupTestEnv(t)

	// Request with invalid token
	rec := doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors", nil, "invalid-token-abc")
	requireStatus(t, rec, http.StatusUnauthorized)
}

// ---------------------------------------------------------------------------
// Error Case Tests
// ---------------------------------------------------------------------------

func TestError_GetNonexistentVM(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines/nonexistent", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestError_GetNonexistentHypervisor(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors/nonexistent", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestError_GetNonexistentCloudInit(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/cloud-init-templates/nonexistent", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestError_GetNonexistentOSImage(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/os-images/nonexistent", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestError_CreateHypervisorInvalidBody(t *testing.T) {
	env := setupTestEnv(t)

	// Send completely invalid JSON
	req := httptest.NewRequest(http.MethodPost, "/api/v1/hypervisors", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.token)
	rec := httptest.NewRecorder()
	env.echo.ServeHTTP(rec, req)
	requireStatus(t, rec, http.StatusBadRequest)
}

func TestError_CreateHypervisorMissingName(t *testing.T) {
	env := setupTestEnv(t)

	// Missing name (only has connection)
	hvBody := map[string]any{
		"connection": map[string]any{
			"host": "127.0.0.1",
		},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", hvBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)
}

func TestError_CreateHypervisorMissingHost(t *testing.T) {
	env := setupTestEnv(t)

	// Missing connection host
	hvBody := map[string]any{
		"name":       "hv-no-host",
		"connection": map[string]any{},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", hvBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)
}

func TestError_CreateVMAutoPlacementNoHypervisors(t *testing.T) {
	env := setupTestEnv(t)

	// No hypervisorRef and no hypervisors exist -> auto-placement fails.
	vmBody := map[string]any{
		"name": "vm-no-ref",
		"resources": map[string]any{
			"cpuCores": 1,
			"memoryMB": 1024,
			"diskGB":   10,
		},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)
	body := parseBody(t, rec)
	errMsg, _ := body["error"].(string)
	if errMsg == "" {
		t.Fatal("expected error message for auto-placement with no hypervisors")
	}
}

func TestError_CreateCloudInitMissingUserData(t *testing.T) {
	env := setupTestEnv(t)

	ciBody := map[string]any{
		"name":        "ci-no-data",
		"description": "no userData",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", ciBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)
}

func TestError_CreateOSImageMissingFields(t *testing.T) {
	env := setupTestEnv(t)

	// Missing osFamily and osVersion
	imgBody := map[string]any{
		"name": "img-bad",
		"arch": "amd64",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", imgBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)
}

func TestError_DeleteNonexistentHypervisor(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/hypervisors/nonexistent", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestError_DeleteNonexistentVM(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/virtual-machines/nonexistent", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestError_DeleteNonexistentCloudInit(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/cloud-init-templates/nonexistent", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestError_DeleteNonexistentOSImage(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/os-images/nonexistent", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// Auth Login/Logout Tests
// ---------------------------------------------------------------------------

func TestAuthLoginLogout(t *testing.T) {
	env := setupTestEnv(t)

	// Login with valid credentials
	loginBody := map[string]any{
		"username": "admin",
		"password": "adminpass",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/auth/login", loginBody, "")
	requireStatus(t, rec, http.StatusOK)
	loginResp := parseBody(t, rec)
	loginToken, ok := loginResp["token"].(string)
	if !ok || loginToken == "" {
		t.Fatalf("expected non-empty token from login, got %v", loginResp)
	}

	// Use the token to access a protected endpoint
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/me", nil, loginToken)
	requireStatus(t, rec, http.StatusOK)
	meResp := parseBody(t, rec)
	if meResp["username"] != "admin" {
		t.Fatalf("expected username admin from /me, got %v", meResp["username"])
	}

	// Logout
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/auth/logout", nil, loginToken)
	requireStatus(t, rec, http.StatusNoContent)

	// Token should now be invalid
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/me", nil, loginToken)
	requireStatus(t, rec, http.StatusUnauthorized)
}

func TestAuthLoginInvalidCredentials(t *testing.T) {
	env := setupTestEnv(t)

	loginBody := map[string]any{
		"username": "admin",
		"password": "wrongpassword",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/auth/login", loginBody, "")
	requireStatus(t, rec, http.StatusUnauthorized)
}

func TestAuthLoginNonexistentUser(t *testing.T) {
	env := setupTestEnv(t)

	loginBody := map[string]any{
		"username": "nobody",
		"password": "nopass",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/auth/login", loginBody, "")
	requireStatus(t, rec, http.StatusUnauthorized)
}

func TestSetupStatusRequiresFirstAdminWhenNoUsersExist(t *testing.T) {
	env := setupFirstRunTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/setup/status", nil, "")
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	if body["required"] != true {
		t.Fatalf("expected setup required, got %v", body["required"])
	}
}

func TestSetupAdminCreatesFirstAdmin(t *testing.T) {
	env := setupFirstRunTestEnv(t)

	setupBody := map[string]any{
		"username": "owner",
		"password": "secret123",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/setup/admin", setupBody, "")
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/setup/status", nil, "")
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	if body["required"] != false {
		t.Fatalf("expected setup not required after admin creation, got %v", body["required"])
	}

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/auth/login", setupBody, "")
	requireStatus(t, rec, http.StatusOK)
}

func TestSetupAdminRejectedAfterUserExists(t *testing.T) {
	env := setupTestEnv(t)

	setupBody := map[string]any{
		"username": "owner",
		"password": "secret123",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/setup/admin", setupBody, "")
	requireStatus(t, rec, http.StatusConflict)
}

// ---------------------------------------------------------------------------
// Health Check Tests
// ---------------------------------------------------------------------------
