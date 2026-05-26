package api_test

import (
	"net/http"
	"testing"
)

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
