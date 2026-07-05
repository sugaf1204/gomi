package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/sugaf1204/gomi/internal/auth"
)

func TestServiceAccountTokenLifecycle(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/service-accounts", map[string]any{
		"name": "automation",
		"role": "operator",
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)
	body := parseBody(t, rec)
	token, _ := body["token"].(string)
	if !strings.HasPrefix(token, "gomi_sa_") {
		t.Fatalf("expected generated service account token, got %q", token)
	}

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/me", nil, token)
	requireStatus(t, rec, http.StatusOK)
	me := parseBody(t, rec)
	if me["username"] != "serviceaccount:automation" || me["role"] != "operator" {
		t.Fatalf("unexpected /me response for service account: %v", me)
	}

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", map[string]any{
		"name":     "ci-service-account",
		"userData": "#cloud-config\npackages:\n  - curl",
	}, token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/service-accounts/automation", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/me", nil, token)
	requireStatus(t, rec, http.StatusUnauthorized)
}

func TestServiceAccountViewerCannotWrite(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/service-accounts", map[string]any{
		"name": "viewer-bot",
		"role": "viewer",
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)
	token, _ := parseBody(t, rec)["token"].(string)

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors", nil, token)
	requireStatus(t, rec, http.StatusOK)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", map[string]any{
		"name":     "ci-forbidden",
		"userData": "#cloud-config\n",
	}, token)
	requireStatus(t, rec, http.StatusForbidden)
}

func TestServiceAccountRequiresAdmin(t *testing.T) {
	env := setupTestEnv(t)
	createUser(t, env.authStore, "operator-user", "operatorpass", auth.RoleOperator)
	operatorToken := createSession(t, env.authStore, "operator-user")

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/service-accounts", map[string]any{
		"name": "not-allowed",
		"role": "viewer",
	}, operatorToken)
	requireStatus(t, rec, http.StatusForbidden)
}
