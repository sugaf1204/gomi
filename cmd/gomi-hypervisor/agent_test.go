package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSyncOnce_DownloadsReadyImages(t *testing.T) {
	imageContent := []byte("fake-qcow2-data")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/os-images":
			resp := map[string]any{
				"osImages": []OSImage{
					{Name: "osImages/ubuntu-22.04", OSImageID: "ubuntu-22.04", Format: "qcow2", Ready: true},
					{Name: "osImages/not-ready", OSImageID: "not-ready", Format: "iso", Ready: false},
				},
			}
			json.NewEncoder(w).Encode(resp)
		case "/api/v1/os-images/ubuntu-22.04/download":
			w.Write(imageContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := Config{
		ServerURL: srv.URL,
		Interval:  time.Minute,
		ImageDir:  dir,
	}
	client := newAPIClient(srv.URL, "")

	if err := syncOnce(context.Background(), cfg, client); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "ubuntu-22.04.qcow2"))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != string(imageContent) {
		t.Fatalf("unexpected content: %q", data)
	}

	if _, err := os.Stat(filepath.Join(dir, "not-ready.iso")); !os.IsNotExist(err) {
		t.Fatal("not-ready image should not be downloaded")
	}
}

func TestSyncOnce_DownloadsArtifactRoot(t *testing.T) {
	imageContent := []byte("qcow2-root-image")
	sum := sha256.Sum256(imageContent)
	checksum := hex.EncodeToString(sum[:])
	legacyDownloadCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/os-images":
			resp := map[string]any{
				"osImages": []OSImage{
					{
						Name:   "ubuntu-artifact",
						Format: "qcow2",
						Ready:  true,
						Manifest: &OSImageManifest{Root: OSImageRootArtifact{
							Format: "qcow2",
							Path:   "root.qcow2",
							SHA256: checksum,
						}},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		case "/pxe/artifacts/os-images/ubuntu-artifact/root.qcow2":
			w.Write(imageContent)
		case "/api/v1/os-images/ubuntu-artifact/download":
			legacyDownloadCalled = true
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := Config{ServerURL: srv.URL, Interval: time.Minute, ImageDir: dir}
	client := newAPIClient(srv.URL, "")

	if err := syncOnce(context.Background(), cfg, client); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}
	if legacyDownloadCalled {
		t.Fatal("artifact image should be downloaded from the PXE artifact route")
	}
	data, err := os.ReadFile(filepath.Join(dir, "ubuntu-artifact.qcow2"))
	if err != nil {
		t.Fatalf("read artifact root: %v", err)
	}
	if string(data) != string(imageContent) {
		t.Fatalf("unexpected artifact content: %q", data)
	}
}

func TestSyncOnce_DownloadsXZCompressedArtifactRoot(t *testing.T) {
	if _, err := exec.LookPath("xz"); err != nil {
		t.Skip("xz command is not installed")
	}
	imageContent := []byte("squashfs-root-image")
	cmd := exec.Command("xz", "-c")
	cmd.Stdin = strings.NewReader(string(imageContent))
	compressed, err := cmd.Output()
	if err != nil {
		t.Fatalf("compress fixture with xz: %v", err)
	}
	sum := sha256.Sum256(imageContent)
	checksum := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/os-images":
			resp := map[string]any{
				"osImages": []OSImage{
					{
						Name:   "debian-artifact",
						Format: "squashfs",
						Ready:  true,
						Manifest: &OSImageManifest{Root: OSImageRootArtifact{
							Format:      "squashfs",
							Compression: "xz",
							Path:        "rootfs.squashfs.xz",
							SHA256:      checksum,
						}},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		case "/pxe/artifacts/os-images/debian-artifact/rootfs.squashfs.xz":
			w.Write(compressed)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := Config{ServerURL: srv.URL, Interval: time.Minute, ImageDir: dir}
	client := newAPIClient(srv.URL, "")

	if err := syncOnce(context.Background(), cfg, client); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "debian-artifact.squashfs"))
	if err != nil {
		t.Fatalf("read artifact root: %v", err)
	}
	if string(data) != string(imageContent) {
		t.Fatalf("unexpected artifact content: %q", data)
	}
}

func TestSyncOnce_DownloadsSquashFSWithInternalXZCompression(t *testing.T) {
	imageContent := []byte("squashfs-bytes")
	sum := sha256.Sum256(imageContent)
	checksum := hex.EncodeToString(sum[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/os-images":
			resp := map[string]any{
				"osImages": []OSImage{
					{
						Name:   "fedora-artifact",
						Format: "squashfs",
						Ready:  true,
						Manifest: &OSImageManifest{Root: OSImageRootArtifact{
							Format:      "squashfs",
							Compression: "xz",
							Path:        "rootfs.squashfs",
							SHA256:      checksum,
						}},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		case "/pxe/artifacts/os-images/fedora-artifact/rootfs.squashfs":
			w.Write(imageContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := Config{ServerURL: srv.URL, Interval: time.Minute, ImageDir: dir}
	client := newAPIClient(srv.URL, "")

	if err := syncOnce(context.Background(), cfg, client); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "fedora-artifact.squashfs"))
	if err != nil {
		t.Fatalf("read artifact root: %v", err)
	}
	if string(data) != string(imageContent) {
		t.Fatalf("unexpected artifact content: %q", data)
	}
}

func TestSyncOnce_CleansUpStaleFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/os-images":
			resp := map[string]any{
				"osImages": []OSImage{
					{Name: "keep-me", Format: "qcow2", Ready: true},
				},
			}
			json.NewEncoder(w).Encode(resp)
		case "/api/v1/os-images/keep-me/download":
			w.Write([]byte("data"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "old-image.qcow2"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(dir, "old-image.qcow2"+managedMarkerSuffix), []byte("gomi-hypervisor\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("keep"), 0o644)

	cfg := Config{
		ServerURL: srv.URL,
		Interval:  time.Minute,
		ImageDir:  dir,
	}
	client := newAPIClient(srv.URL, "")

	if err := syncOnce(context.Background(), cfg, client); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "old-image.qcow2")); !os.IsNotExist(err) {
		t.Fatal("stale managed file should be removed")
	}
	if _, err := os.Stat(filepath.Join(dir, "readme.txt")); err != nil {
		t.Fatal("non-managed file should be kept")
	}
	if _, err := os.Stat(filepath.Join(dir, "keep-me.qcow2")); err != nil {
		t.Fatal("current image should exist")
	}
}

func TestSyncOnce_SkipsExistingFileWithNoChecksum(t *testing.T) {
	downloadCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/os-images":
			resp := map[string]any{
				"osImages": []OSImage{
					{Name: "existing", Format: "qcow2", Ready: true},
				},
			}
			json.NewEncoder(w).Encode(resp)
		case "/api/v1/os-images/existing/download":
			downloadCalled = true
			w.Write([]byte("new-data"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "existing.qcow2"), []byte("already-here"), 0o644)

	cfg := Config{
		ServerURL: srv.URL,
		Interval:  time.Minute,
		ImageDir:  dir,
	}
	client := newAPIClient(srv.URL, "")

	if err := syncOnce(context.Background(), cfg, client); err != nil {
		t.Fatalf("syncOnce: %v", err)
	}

	if downloadCalled {
		t.Fatal("should not download when file exists and no checksum specified")
	}
}
