package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestCloudInitTemplateCRUD(t *testing.T) {
	env := setupTestEnv(t)

	// POST /api/v1/cloud-init-templates - Create
	ciBody := map[string]any{
		"name":        "ci-basic",
		"userData":    "#cloud-config\npackages:\n  - vim\n",
		"description": "basic cloud-init template",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", ciBody, env.token)
	requireStatus(t, rec, http.StatusCreated)
	created := parseBody(t, rec)
	if created["name"] != "cloudInitTemplates/ci-basic" {
		t.Fatalf("expected name ci-basic, got %v", created["name"])
	}

	// GET /api/v1/cloud-init-templates - List
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/cloud-init-templates", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	items := listValues(t, body)
	if len(items) != 1 {
		t.Fatalf("expected 1 cloud-init template, got %v", body)
	}

	// GET /api/v1/cloud-init-templates/ci-basic - Get
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/cloud-init-templates/ci-basic", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	got := parseBody(t, rec)
	if got["userData"] != "#cloud-config\npackages:\n  - vim\n" {
		t.Fatalf("unexpected userData: %v", got["userData"])
	}

	// PUT /api/v1/cloud-init-templates/ci-basic - Update
	updateBody := map[string]any{
		"userData":    "#cloud-config\npackages:\n  - vim\n  - curl\n",
		"description": "updated cloud-init template",
	}
	rec = doRequest(env.echo, http.MethodPut, "/api/v1/cloud-init-templates/ci-basic", updateBody, env.token)
	requireStatus(t, rec, http.StatusOK)
	updated := parseBody(t, rec)
	if updated["userData"] != "#cloud-config\npackages:\n  - vim\n  - curl\n" {
		t.Fatalf("unexpected updated userData: %v", updated["userData"])
	}

	// DELETE /api/v1/cloud-init-templates/ci-basic - Delete
	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/cloud-init-templates/ci-basic", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)

	// Verify deleted
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/cloud-init-templates/ci-basic", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// OSImage CRUD Tests
// ---------------------------------------------------------------------------

func TestOSImageCRUD(t *testing.T) {
	env := setupTestEnv(t)

	// POST /api/v1/os-images - Create
	imgBody := map[string]any{
		"name":      "ubuntu-22.04",
		"osFamily":  "ubuntu",
		"osVersion": "22.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", imgBody, env.token)
	requireStatus(t, rec, http.StatusCreated)
	created := parseBody(t, rec)
	if created["name"] != "osImages/ubuntu-22.04" {
		t.Fatalf("expected name ubuntu-22.04, got %v", created["name"])
	}

	// GET /api/v1/os-images - List
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/os-images", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	items := listValues(t, body)
	if len(items) != 1 {
		t.Fatalf("expected 1 os-image, got %v", body)
	}

	// GET /api/v1/os-images/ubuntu-22.04 - Get
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/os-images/ubuntu-22.04", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	got := parseBody(t, rec)
	if got["osFamily"] != "ubuntu" {
		t.Fatalf("expected osFamily ubuntu, got %v", got["osFamily"])
	}

	// DELETE /api/v1/os-images/ubuntu-22.04 - Delete
	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/os-images/ubuntu-22.04", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)

	// Verify deleted
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/os-images/ubuntu-22.04", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestCreateURLOSImageDownloadsToServerStorage(t *testing.T) {
	env := setupTestEnv(t)
	content := []byte("fake image bytes")
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/debian.qcow2" {
			http.NotFound(w, r)
			return
		}
		w.Write(content)
	}))
	defer srv.Close()

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", map[string]any{
		"name":      "debian-url",
		"osFamily":  "debian",
		"osVersion": "13",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "url",
		"url":       srv.URL + "/images/debian.qcow2",
		"checksum":  "sha256:" + checksum,
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)
	created := parseBody(t, rec)
	if ready, _ := created["ready"].(bool); !ready {
		t.Fatalf("expected ready URL image, got %v", created)
	}
	if _, leaked := created["localPath"]; leaked {
		t.Fatalf("localPath must not be exposed in API response: %v", created)
	}
	stored, err := env.osimages.Get(context.Background(), "debian-url")
	if err != nil {
		t.Fatalf("get stored os image: %v", err)
	}
	localPath := stored.LocalPath
	if strings.TrimSpace(localPath) == "" {
		t.Fatalf("expected stored localPath, got %#v", stored)
	}
	data, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read downloaded image: %v", err)
	}
	if string(data) != string(content) {
		t.Fatalf("unexpected downloaded content: %q", data)
	}
}

// ---------------------------------------------------------------------------
// Auth Tests: Unauthenticated and Invalid Token (still enforced)
// ---------------------------------------------------------------------------
