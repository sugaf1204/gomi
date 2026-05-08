package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/oscatalog"
	"github.com/sugaf1204/gomi/internal/osimage"
)

func TestValidateCatalogArtifactRejectsConversion(t *testing.T) {
	entry := oscatalog.Entry{
		Name:         "ubuntu-24.04-amd64",
		Format:       osimage.FormatRAW,
		SourceFormat: osimage.FormatQCOW2,
		URL:          "https://images.example.test/ubuntu-24.04-amd64.qcow2",
	}

	err := validateCatalogArtifact(entry)
	if err == nil {
		t.Fatal("expected qcow2 source to be rejected")
	}
	if !strings.Contains(err.Error(), "source must match artifact format") {
		t.Fatalf("expected raw-only error, got %v", err)
	}
}

func TestValidateCatalogArtifactAcceptsRaw(t *testing.T) {
	entry := oscatalog.Entry{
		Name:         "ubuntu-24.04-amd64",
		Format:       osimage.FormatRAW,
		SourceFormat: osimage.FormatRAW,
		URL:          "https://images.example.test/ubuntu-24.04-amd64.raw",
	}

	if err := validateCatalogArtifact(entry); err != nil {
		t.Fatalf("expected raw artifact to be accepted, got %v", err)
	}
}

func TestDownloadCatalogRawArtifactDecompressesZstdToRaw(t *testing.T) {
	raw := []byte("raw-disk-image")
	var compressed bytes.Buffer
	zw, err := zstd.NewWriter(&compressed)
	if err != nil {
		t.Fatalf("create zstd writer: %v", err)
	}
	if _, err := zw.Write(raw); err != nil {
		t.Fatalf("write zstd payload: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zstd writer: %v", err)
	}
	compressedSum := sha256.Sum256(compressed.Bytes())

	restore := stubCatalogHTTPClient(t, func(req *http.Request) (*http.Response, error) {
		return httpResponse(200, compressed.Bytes()), nil
	})
	defer restore()

	storageDir := t.TempDir()
	s := &Server{imageStorageDir: storageDir}
	entry := oscatalog.Entry{
		Name:              "ubuntu-24.04-amd64",
		Format:            osimage.FormatRAW,
		SourceFormat:      osimage.FormatRAW,
		SourceCompression: "zstd",
		URL:               "https://images.example.test/ubuntu-24.04-amd64.raw.zst",
		Checksum:          "sha256:" + hex.EncodeToString(compressedSum[:]),
	}
	img := entry.OSImage()

	localPath, localChecksum, err := s.downloadCatalogRawArtifact(context.Background(), entry, img)
	if err != nil {
		t.Fatalf("download catalog raw artifact: %v", err)
	}
	if filepath.Base(localPath) != "ubuntu-24.04-amd64.raw" {
		t.Fatalf("local path = %q, want .raw output", localPath)
	}
	got, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read local raw image: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("raw payload = %q, want %q", got, raw)
	}
	rawSum := sha256.Sum256(raw)
	if localChecksum != hex.EncodeToString(rawSum[:]) {
		t.Fatalf("local checksum = %q, want decompressed raw checksum", localChecksum)
	}
}

func TestRunCatalogInstallStoresDecompressedRawChecksum(t *testing.T) {
	raw := []byte("raw-disk-image")
	var compressed bytes.Buffer
	zw, err := zstd.NewWriter(&compressed)
	if err != nil {
		t.Fatalf("create zstd writer: %v", err)
	}
	if _, err := zw.Write(raw); err != nil {
		t.Fatalf("write zstd payload: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zstd writer: %v", err)
	}
	compressedSum := sha256.Sum256(compressed.Bytes())
	rawSum := sha256.Sum256(raw)

	restore := stubCatalogHTTPClient(t, func(req *http.Request) (*http.Response, error) {
		return httpResponse(200, compressed.Bytes()), nil
	})
	defer restore()

	backend := memory.New()
	osImages := osimage.NewService(backend.OSImages())
	entry := oscatalog.Entry{
		Name:              "legacy-raw",
		OSFamily:          "ubuntu",
		OSVersion:         "22.04",
		Arch:              "amd64",
		Variant:           osimage.VariantCloud,
		Format:            osimage.FormatRAW,
		SourceFormat:      osimage.FormatRAW,
		SourceCompression: "zstd",
		URL:               "https://images.example.test/legacy.raw.zst",
		Checksum:          "sha256:" + hex.EncodeToString(compressedSum[:]),
	}
	if _, err := osImages.Create(context.Background(), entry.OSImage()); err != nil {
		t.Fatalf("create os image: %v", err)
	}
	s := &Server{
		imageStorageDir: t.TempDir(),
		osimages:        osImages,
	}

	s.runCatalogInstall(entry)

	img, err := osImages.Get(context.Background(), entry.Name)
	if err != nil {
		t.Fatalf("get installed image: %v", err)
	}
	if !img.Ready {
		t.Fatalf("image ready = false, error = %q", img.Error)
	}
	if img.Checksum != "sha256:"+hex.EncodeToString(rawSum[:]) {
		t.Fatalf("checksum = %q, want decompressed raw checksum", img.Checksum)
	}
	got, err := os.ReadFile(img.LocalPath)
	if err != nil {
		t.Fatalf("read installed raw image: %v", err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("installed raw payload = %q, want %q", got, raw)
	}
}

func TestDownloadCatalogSquashFSArtifactStoresDirectoryLayout(t *testing.T) {
	payload := []byte("squashfs-bytes")
	restore := stubCatalogHTTPClient(t, func(req *http.Request) (*http.Response, error) {
		return httpResponse(200, payload), nil
	})
	defer restore()

	storageDir := t.TempDir()
	s := &Server{imageStorageDir: storageDir}
	entry := oscatalog.Entry{
		Name:         "ubuntu-22.04-amd64-baremetal",
		Format:       osimage.FormatSquashFS,
		SourceFormat: osimage.FormatSquashFS,
		URL:          "https://images.example.test/ubuntu-22.04-amd64-baremetal.rootfs.squashfs",
	}
	img := entry.OSImage()

	localPath, _, err := s.downloadCatalogArtifact(context.Background(), entry, img)
	if err != nil {
		t.Fatalf("download catalog squashfs artifact: %v", err)
	}
	if filepath.Base(localPath) != "ubuntu-22.04-amd64-baremetal" {
		t.Fatalf("local path = %q, want directory output", localPath)
	}
	got, err := os.ReadFile(filepath.Join(localPath, "rootfs.squashfs"))
	if err != nil {
		t.Fatalf("read local squashfs image: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("squashfs payload = %q, want %q", got, payload)
	}
}

func TestDownloadURLImageFileStoresManifestSquashFSDirectoryLayout(t *testing.T) {
	payload := []byte("squashfs-bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rootfs.squashfs" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	storageDir := t.TempDir()
	s := &Server{imageStorageDir: storageDir}
	img := osimage.OSImage{
		Name:   "custom-squashfs",
		Format: osimage.FormatSquashFS,
		Source: osimage.SourceURL,
		URL:    server.URL + "/rootfs.squashfs",
		Manifest: &osimage.Manifest{
			Root: osimage.RootArtifact{
				Format: osimage.FormatSquashFS,
				Path:   "rootfs.squashfs",
			},
		},
	}

	localPath, err := s.downloadURLImageFile(context.Background(), img)
	if err != nil {
		t.Fatalf("download URL image file: %v", err)
	}
	if filepath.Base(localPath) != "custom-squashfs" {
		t.Fatalf("local path = %q, want artifact directory", localPath)
	}
	got, err := os.ReadFile(filepath.Join(localPath, "rootfs.squashfs"))
	if err != nil {
		t.Fatalf("read local squashfs image: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("squashfs payload = %q, want %q", got, payload)
	}
	if _, err := os.Stat(filepath.Join(storageDir, "custom-squashfs.squashfs")); !os.IsNotExist(err) {
		t.Fatalf("flat squashfs artifact should not exist, err=%v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func stubCatalogHTTPClient(t *testing.T, fn roundTripFunc) func() {
	t.Helper()
	prev := catalogHTTPClient
	catalogHTTPClient = &http.Client{Transport: fn}
	return func() {
		catalogHTTPClient = prev
	}
}

func httpResponse(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}
}
