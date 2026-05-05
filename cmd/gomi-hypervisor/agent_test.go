package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
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
				"items": []OSImage{
					{Name: "ubuntu-22.04", Format: "qcow2", Ready: true},
					{Name: "not-ready", Format: "iso", Ready: false},
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
	imageContent := []byte("raw-root-image")
	sum := sha256.Sum256(imageContent)
	checksum := hex.EncodeToString(sum[:])
	legacyDownloadCalled := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/os-images":
			resp := map[string]any{
				"items": []OSImage{
					{
						Name:   "ubuntu-artifact",
						Format: "raw",
						Ready:  true,
						Manifest: &OSImageManifest{Root: OSImageRootArtifact{
							Format: "raw",
							Path:   "root.raw",
							SHA256: checksum,
						}},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		case "/pxe/artifacts/os-images/ubuntu-artifact/root.raw":
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
	data, err := os.ReadFile(filepath.Join(dir, "ubuntu-artifact.raw"))
	if err != nil {
		t.Fatalf("read artifact root: %v", err)
	}
	if string(data) != string(imageContent) {
		t.Fatalf("unexpected artifact content: %q", data)
	}
}

func TestSyncOnce_DownloadsCompressedArtifactRoot(t *testing.T) {
	fakeBin := t.TempDir()
	zstdPath := filepath.Join(fakeBin, "zstd")
	if err := os.WriteFile(zstdPath, []byte("#!/bin/sh\nif [ \"$1\" = \"-dc\" ]; then cat \"$2\"; else exit 2; fi\n"), 0o755); err != nil {
		t.Fatalf("write fake zstd: %v", err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	compressedContent := []byte("pretend-zstd-root")
	sum := sha256.Sum256(compressedContent)
	checksum := hex.EncodeToString(sum[:])
	downloadCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/os-images":
			resp := map[string]any{
				"items": []OSImage{
					{
						Name:   "ubuntu-zst",
						Format: "raw",
						Ready:  true,
						Manifest: &OSImageManifest{Root: OSImageRootArtifact{
							Format:      "raw",
							Compression: "zst",
							Path:        "root.raw.zst",
							SHA256:      checksum,
						}},
					},
				},
			}
			json.NewEncoder(w).Encode(resp)
		case "/pxe/artifacts/os-images/ubuntu-zst/root.raw.zst":
			downloadCount++
			w.Write(compressedContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	cfg := Config{ServerURL: srv.URL, Interval: time.Minute, ImageDir: dir}
	client := newAPIClient(srv.URL, "")

	if err := syncOnce(context.Background(), cfg, client); err != nil {
		t.Fatalf("first syncOnce: %v", err)
	}
	if err := syncOnce(context.Background(), cfg, client); err != nil {
		t.Fatalf("second syncOnce: %v", err)
	}
	if downloadCount != 1 {
		t.Fatalf("expected artifact to be downloaded once, got %d", downloadCount)
	}
	data, err := os.ReadFile(filepath.Join(dir, "ubuntu-zst.raw"))
	if err != nil {
		t.Fatalf("read decompressed artifact root: %v", err)
	}
	if string(data) != string(compressedContent) {
		t.Fatalf("unexpected decompressed artifact content: %q", data)
	}
	sidecar, err := os.ReadFile(filepath.Join(dir, "ubuntu-zst.raw"+sourceChecksumSuffix))
	if err != nil {
		t.Fatalf("read source checksum sidecar: %v", err)
	}
	if strings.TrimSpace(string(sidecar)) != checksum {
		t.Fatalf("unexpected source checksum sidecar: %q", sidecar)
	}
}

func TestSyncOnce_CleansUpStaleFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/os-images":
			resp := map[string]any{
				"items": []OSImage{
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
				"items": []OSImage{
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

func TestAtomicWriteFromReader(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "test.bin")

	r := strings.NewReader("hello world")
	if err := atomicWriteFromReader(dest, r); err != nil {
		t.Fatalf("atomicWriteFromReader: %v", err)
	}

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("unexpected content: %q", data)
	}

	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file should not exist after successful write")
	}
}

func TestVerifyChecksum_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	content := []byte("test data for checksum")
	os.WriteFile(path, content, 0o644)

	h := sha256.Sum256(content)
	expected := hex.EncodeToString(h[:])

	if err := verifyChecksum(path, expected); err != nil {
		t.Fatalf("verifyChecksum should pass: %v", err)
	}
}

func TestVerifyChecksum_WithPrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	content := []byte("test data")
	os.WriteFile(path, content, 0o644)

	h := sha256.Sum256(content)
	expected := "sha256:" + hex.EncodeToString(h[:])

	if err := verifyChecksum(path, expected); err != nil {
		t.Fatalf("verifyChecksum with sha256: prefix should pass: %v", err)
	}
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.bin")
	os.WriteFile(path, []byte("some data"), 0o644)

	if err := verifyChecksum(path, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Fatal("verifyChecksum should fail for wrong checksum")
	}
}

func TestNeedsDownload_NoFile(t *testing.T) {
	if !needsDownload("/nonexistent/path", "") {
		t.Fatal("should need download when file does not exist")
	}
}

func TestNeedsDownload_ExistingNoChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.qcow2")
	os.WriteFile(path, []byte("data"), 0o644)

	if needsDownload(path, "") {
		t.Fatal("should not need download when file exists and no checksum")
	}
}

func TestNeedsDownload_ChecksumMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.qcow2")
	content := []byte("exact content")
	os.WriteFile(path, content, 0o644)

	h := sha256.Sum256(content)
	checksum := hex.EncodeToString(h[:])

	if needsDownload(path, checksum) {
		t.Fatal("should not need download when checksum matches")
	}
}

func TestNeedsDownload_ChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.qcow2")
	os.WriteFile(path, []byte("old content"), 0o644)

	if !needsDownload(path, "0000000000000000000000000000000000000000000000000000000000000000") {
		t.Fatal("should need download when checksum mismatches")
	}
}

func TestIsManagedFile(t *testing.T) {
	tests := []struct {
		filename string
		want     bool
	}{
		{"ubuntu.qcow2", true},
		{"debian.raw", true},
		{"server.iso", true},
		{"disk.img", true},
		{"UPPER.QCOW2", true},
		{"readme.txt", false},
		{"script.sh", false},
		{"config.yaml", false},
		{"noext", false},
	}
	for _, tt := range tests {
		if got := isManagedFile(tt.filename); got != tt.want {
			t.Errorf("isManagedFile(%q) = %v, want %v", tt.filename, got, tt.want)
		}
	}
}

func TestCleanupStaleFiles(t *testing.T) {
	dir := t.TempDir()

	files := map[string]string{
		"keep.qcow2":    "keep",
		"stale.qcow2":   "remove",
		"old.iso":       "remove",
		"manual.txt":    "keep (not managed)",
		"another.raw":   "remove",
		"keep-too.img":  "keep",
		"vm-disk.qcow2": "keep (unmarked VM disk)",
	}
	for name, content := range files {
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	}
	for _, name := range []string{"stale.qcow2", "old.iso", "another.raw"} {
		os.WriteFile(filepath.Join(dir, name+managedMarkerSuffix), []byte("gomi-hypervisor\n"), 0o644)
	}

	expected := map[string]OSImage{
		"keep.qcow2":   {Name: "keep", Format: "qcow2"},
		"keep-too.img": {Name: "keep-too", Format: "img"},
	}

	removed, err := cleanupStaleFiles(dir, expected)
	if err != nil {
		t.Fatalf("cleanupStaleFiles: %v", err)
	}

	sort.Strings(removed)
	wantRemoved := []string{"another.raw", "old.iso", "stale.qcow2"}
	if len(removed) != len(wantRemoved) {
		t.Fatalf("removed = %v, want %v", removed, wantRemoved)
	}
	for i, name := range wantRemoved {
		if removed[i] != name {
			t.Errorf("removed[%d] = %q, want %q", i, removed[i], name)
		}
	}

	for _, name := range []string{"keep.qcow2", "manual.txt", "keep-too.img", "vm-disk.qcow2"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %q to still exist", name)
		}
	}

	for _, name := range wantRemoved {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("expected %q to be removed", name)
		}
	}
}

func TestCleanupStaleFiles_DoesNotRemoveUnmarkedManagedExtensions(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"kube-1.qcow2", "manual.raw", "scratch.img"} {
		os.WriteFile(filepath.Join(dir, name), []byte("vm data"), 0o644)
	}

	removed, err := cleanupStaleFiles(dir, nil)
	if err != nil {
		t.Fatalf("cleanupStaleFiles: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed = %v, want none", removed)
	}

	for _, name := range []string{"kube-1.qcow2", "manual.raw", "scratch.img"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %q to still exist", name)
		}
	}
}

func TestCleanupStaleFiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	removed, err := cleanupStaleFiles(dir, nil)
	if err != nil {
		t.Fatalf("cleanupStaleFiles: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("unexpected removals: %v", removed)
	}
}
