package api_test

import (
	"context"
	"github.com/sugaf1204/gomi/internal/auth"
	"net/http"
	"testing"
)

func TestCreateUser(t *testing.T) {
	env := setupTestEnv(t)

	userBody := map[string]any{
		"username": "newuser",
		"password": "secret123",
		"role":     "viewer",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/users", userBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// New user can login
	loginBody := map[string]any{
		"username": "newuser",
		"password": "secret123",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/auth/login", loginBody, "")
	requireStatus(t, rec, http.StatusOK)
	loginResp := parseBody(t, rec)
	token, _ := loginResp["token"].(string)
	if token == "" {
		t.Fatalf("expected login token, got %v", loginResp)
	}

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/me", nil, token)
	requireStatus(t, rec, http.StatusOK)
	meResp := parseBody(t, rec)
	if meResp["role"] != "viewer" {
		t.Fatalf("expected role viewer, got %v", meResp["role"])
	}
}

func TestCreateUserSupportsOperatorAndAdminRoles(t *testing.T) {
	env := setupTestEnv(t)

	for _, role := range []string{"operator", "admin"} {
		userBody := map[string]any{
			"username": "new-" + role,
			"password": "secret123",
			"role":     role,
		}
		rec := doRequest(env.echo, http.MethodPost, "/api/v1/users", userBody, env.token)
		requireStatus(t, rec, http.StatusCreated)

		user, err := env.authStore.GetUser(context.Background(), "new-"+role)
		if err != nil {
			t.Fatalf("get user for role %s: %v", role, err)
		}
		if string(user.Role) != role {
			t.Fatalf("expected role %s, got %s", role, user.Role)
		}
	}
}

func TestCreateUserRejectsInvalidRole(t *testing.T) {
	env := setupTestEnv(t)

	userBody := map[string]any{
		"username": "bad-role",
		"password": "secret123",
		"role":     "owner",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/users", userBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)
}

// ---------------------------------------------------------------------------
// RBAC Tests: viewer can read, but cannot create/delete sensitive resources
// ---------------------------------------------------------------------------

func TestRBAC_ViewerCanRead(t *testing.T) {
	env := setupTestEnv(t)

	// Create viewer user and session.
	createUser(t, env.authStore, "viewer-user", "viewerpass", auth.RoleViewer)
	viewerToken := createSession(t, env.authStore, "viewer-user")

	// Viewer can GET hypervisors (read-only).
	rec := doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors", nil, viewerToken)
	requireStatus(t, rec, http.StatusOK)

	// Viewer can GET virtual-machines (read-only).
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines", nil, viewerToken)
	requireStatus(t, rec, http.StatusOK)

	// Viewer can GET ssh-keys (read-only, sanitized).
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/ssh-keys", nil, viewerToken)
	requireStatus(t, rec, http.StatusOK)

}

func TestRBAC_ViewerCannotWrite(t *testing.T) {
	env := setupTestEnv(t)

	// Create viewer user and session.
	createUser(t, env.authStore, "viewer-user", "viewerpass", auth.RoleViewer)
	viewerToken := createSession(t, env.authStore, "viewer-user")

	// Viewer cannot create SSH key (admin only -- handles secrets).
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/ssh-keys", map[string]any{
		"name": "test-key", "publicKey": "ssh-ed25519 AAAA...",
	}, viewerToken)
	requireStatus(t, rec, http.StatusForbidden)

	// Viewer cannot create hypervisor (admin only).
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", map[string]any{
		"name": "hv-test", "connection": map[string]any{"type": "tcp", "host": "127.0.0.1", "port": 16509},
	}, viewerToken)
	requireStatus(t, rec, http.StatusForbidden)

	// Viewer cannot create virtual machine (operator+ only).
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", map[string]any{
		"name": "vm-test", "hypervisorRef": "hv-1", "resources": map[string]any{"cpuCores": 1, "memoryMB": 1024, "diskGB": 10},
	}, viewerToken)
	requireStatus(t, rec, http.StatusForbidden)

	// Viewer cannot create user (admin only).
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "hacker", "password": "test", "role": "admin",
	}, viewerToken)
	requireStatus(t, rec, http.StatusForbidden)
}

func TestRBAC_OperatorCanWriteButNotAdmin(t *testing.T) {
	env := setupTestEnv(t)

	// Create operator user and session.
	createUser(t, env.authStore, "operator-user", "operatorpass", auth.RoleOperator)
	operatorToken := createSession(t, env.authStore, "operator-user")

	// Operator can create cloud-init templates (operator+).
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", map[string]any{
		"name": "ci-op-test", "userData": "#cloud-config\npackages:\n  - curl",
	}, operatorToken)
	requireStatus(t, rec, http.StatusCreated)

	// Operator cannot create SSH keys (admin only).
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/ssh-keys", map[string]any{
		"name": "op-key", "publicKey": "ssh-ed25519 AAAA...",
	}, operatorToken)
	requireStatus(t, rec, http.StatusForbidden)

	// Operator cannot create users (admin only).
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "escalate", "password": "test", "role": "admin",
	}, operatorToken)
	requireStatus(t, rec, http.StatusForbidden)

	// Operator cannot create hypervisors (admin only).
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", map[string]any{
		"name": "hv-op", "connection": map[string]any{"type": "tcp", "host": "127.0.0.1", "port": 16509},
	}, operatorToken)
	requireStatus(t, rec, http.StatusForbidden)
}

// ---------------------------------------------------------------------------
// Machine OS Preset auto-resolution from OS Image
// ---------------------------------------------------------------------------
