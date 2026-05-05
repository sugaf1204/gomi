package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/sugaf1204/gomi/internal/oscatalog"
	"github.com/sugaf1204/gomi/internal/osimage"
)

func TestValidateCatalogRawArtifactRejectsConversion(t *testing.T) {
	entry := oscatalog.Entry{
		Name:         "ubuntu-24.04-amd64",
		Format:       osimage.FormatRAW,
		SourceFormat: osimage.FormatQCOW2,
		URL:          "https://images.example.test/ubuntu-24.04-amd64.qcow2",
	}

	err := validateCatalogRawArtifact(entry)
	if err == nil {
		t.Fatal("expected qcow2 source to be rejected")
	}
	if !strings.Contains(err.Error(), "catalog sources must be raw") {
		t.Fatalf("expected raw-only error, got %v", err)
	}
}

func TestValidateCatalogRawArtifactAcceptsRaw(t *testing.T) {
	entry := oscatalog.Entry{
		Name:         "ubuntu-24.04-amd64",
		Format:       osimage.FormatRAW,
		SourceFormat: osimage.FormatRAW,
		URL:          "https://images.example.test/ubuntu-24.04-amd64.raw",
	}

	if err := validateCatalogRawArtifact(entry); err != nil {
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(compressed.Bytes())
	}))
	defer server.Close()

	storageDir := t.TempDir()
	s := &Server{imageStorageDir: storageDir}
	entry := oscatalog.Entry{
		Name:              "ubuntu-24.04-amd64",
		Format:            osimage.FormatRAW,
		SourceFormat:      osimage.FormatRAW,
		SourceCompression: "zstd",
		URL:               server.URL + "/ubuntu-24.04-amd64.raw.zst",
	}
	img := entry.OSImage()

	localPath, err := s.downloadCatalogRawArtifact(context.Background(), entry, img)
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
}
