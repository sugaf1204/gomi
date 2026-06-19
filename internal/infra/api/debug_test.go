package api_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestDebugPprofAllowsUnauthenticatedAccess(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/debug/pprof/", nil, "")
	requireStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), "/debug/pprof/") {
		t.Fatalf("expected pprof index, got: %s", rec.Body.String())
	}
}

func TestDebugPprofIndex(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/debug/pprof/", nil, "")
	requireStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), "/debug/pprof/") {
		t.Fatalf("expected pprof index, got: %s", rec.Body.String())
	}
}

func TestDebugPprofNamedProfile(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/debug/pprof/goroutine?debug=1", nil, "")
	requireStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), "goroutine profile:") {
		t.Fatalf("expected goroutine profile, got: %s", rec.Body.String())
	}
}

func TestDebugPprofPackageRoute(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/debug/pprof/cmdline", nil, "")
	requireStatus(t, rec, http.StatusOK)
	if rec.Body.Len() == 0 {
		t.Fatal("expected cmdline profile response")
	}
}

func TestDebugFgprofRoute(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/debug/fgprof?seconds=0", nil, "")
	requireStatus(t, rec, http.StatusBadRequest)
	if !strings.Contains(rec.Body.String(), "bad seconds") {
		t.Fatalf("expected fgprof handler response, got: %s", rec.Body.String())
	}
}
