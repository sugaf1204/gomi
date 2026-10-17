package api_test

import (
	"bytes"
	"context"
	"github.com/sugaf1204/gomi/internal/osimage"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWindowsImageUploadReadinessAndArtifactAllowlist(t *testing.T) {
	env := setupTestEnv(t)
	img := osimage.OSImage{Name: "win-test", OSFamily: "windows", OSVersion: "11", Arch: "amd64", Format: osimage.FormatWIM, Source: osimage.SourceUpload, Manifest: &osimage.Manifest{BootModes: []string{"uefi"}, Root: osimage.RootArtifact{Format: osimage.FormatWIM, Path: "install.wim"}, Windows: &osimage.WindowsImage{ImageIndex: 1, WinPE: osimage.WindowsPE{Wimboot: "wimboot", BCD: "BCD", BootSDI: "boot.sdi", BootWIM: "boot.wim"}}}}
	requireStatus(t, doRequest(env.echo, http.MethodPost, "/api/v1/os-images", img, env.token), http.StatusCreated)
	upload := func(endpoint, path, contents string) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if path != "" {
			if err := writer.WriteField("path", path); err != nil {
				t.Fatal(err)
			}
		}
		file, err := writer.CreateFormFile("file", "asset")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, endpoint, &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+env.token)
		rec := httptest.NewRecorder()
		env.echo.ServeHTTP(rec, req)
		return rec
	}
	root := "/api/v1/os-images/win-test"
	requireStatus(t, upload(root+"/upload", "", "install payload"), http.StatusOK)
	stored, err := env.osimages.Get(context.Background(), "win-test")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Ready {
		t.Fatal("root-only Windows image ready")
	}
	requireStatus(t, upload(root+"/upload-artifact", "../escape", "bad"), http.StatusBadRequest)
	requireStatus(t, upload(root+"/upload-artifact", "wimboot", ""), http.StatusBadRequest)
	for _, asset := range img.Manifest.WindowsArtifacts()[1:] {
		requireStatus(t, upload(root+"/upload-artifact", asset, "asset payload"), http.StatusOK)
	}
	stored, err = env.osimages.Get(context.Background(), "win-test")
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Ready {
		t.Fatal("complete Windows image not ready")
	}
	requireStatus(t, doRequest(env.echo, http.MethodGet, "/pxe/artifacts/os-images/win-test/boot.wim", nil, ""), http.StatusOK)
}

func TestCreateOSImageIgnoresClientStatus(t *testing.T) {
	env := setupTestEnv(t)
	body := map[string]any{"name": "fake-ready", "osFamily": "fedora", "osVersion": "43", "format": "qcow2", "ready": true, "localPath": "/private/somewhere", "error": "client-status"}
	requireStatus(t, doRequest(env.echo, http.MethodPost, "/api/v1/os-images", body, env.token), http.StatusCreated)
	img, err := env.osimages.Get(context.Background(), "fake-ready")
	if err != nil {
		t.Fatal(err)
	}
	if img.Ready || img.LocalPath != "" || img.Error != "" {
		t.Fatalf("client status accepted: %+v", img)
	}
}
