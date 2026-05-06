package osimagebuild

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMatrixUsesBuildRecipesOnly(t *testing.T) {
	catalog := writeCatalog(t, `
entries:
  - name: buildable
    osFamily: custom
    osVersion: "1"
    arch: amd64
    variant: baremetal
    format: raw
    sourceFormat: raw
    sourceCompression: zstd
    url: buildable.raw.zst
    bootEnvironment: ubuntu-minimal-cloud-amd64
    build:
      type: packer-qemu-cloud-image
      source:
        url: https://images.example.test/source.qcow2
        checksum: sha256:abc
        format: qcow2
  - name: url-only
    osFamily: custom
    osVersion: "1"
    arch: amd64
    variant: baremetal
    format: raw
    sourceFormat: raw
    sourceCompression: zstd
    url: https://images.example.test/root.raw.zst
    bootEnvironment: ubuntu-minimal-cloud-amd64
`)
	entries, err := LoadCatalog(context.Background(), LoadOptions{
		CatalogFile:     catalog,
		ReplaceExternal: true,
	})
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	matrix := BuildMatrix(entries)
	raw, err := json.Marshal(matrix)
	if err != nil {
		t.Fatalf("marshal matrix: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, "buildable") {
		t.Fatalf("expected buildable entry in matrix, got %s", got)
	}
	if strings.Contains(got, "url-only") {
		t.Fatalf("expected URL-only entry to be excluded, got %s", got)
	}
}

func TestWriteManifest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.raw.zst"), []byte("zstd-bytes"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sample.json"), []byte(`{
  "name": "sample",
  "osFamily": "custom",
  "osVersion": "1",
  "arch": "amd64",
  "variant": "baremetal",
  "artifact": "sample.raw.zst",
  "sha256": "old",
  "sizeBytes": 10
}`), 0o644); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	if err := WriteManifest(dir); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "manifest-os-images.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !strings.Contains(string(manifest), `"name": "sample"`) {
		t.Fatalf("manifest does not include sample metadata: %s", manifest)
	}
	checksums, err := os.ReadFile(filepath.Join(dir, "checksums-os-images.txt"))
	if err != nil {
		t.Fatalf("read checksums: %v", err)
	}
	if !strings.Contains(string(checksums), "sample.raw.zst") {
		t.Fatalf("checksums do not include artifact: %s", checksums)
	}
}

func TestPrepareTemplateDirSkipsMacMetadata(t *testing.T) {
	src := filepath.Join(t.TempDir(), "template")
	if err := os.MkdirAll(filepath.Join(src, "scripts"), 0o755); err != nil {
		t.Fatalf("create template: %v", err)
	}
	files := map[string]string{
		"cloud-image.pkr.hcl":        "packer {}",
		"._cloud-image.pkr.hcl":      "appledouble",
		".DS_Store":                  "finder",
		"scripts/provision.sh":       "#!/bin/sh\n",
		"scripts/._provision.sh":     "appledouble",
		"scripts/__MACOSX/ignored":   "ignored",
		"scripts/__MACOSX/._ignored": "ignored",
	}
	for name, body := range files {
		path := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create parent: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	dst, err := prepareTemplateDir(src, t.TempDir())
	if err != nil {
		t.Fatalf("prepare template: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "cloud-image.pkr.hcl")); err != nil {
		t.Fatalf("expected template file copied: %v", err)
	}
	for _, name := range []string{".DS_Store", "._cloud-image.pkr.hcl", filepath.Join("scripts", "._provision.sh"), filepath.Join("scripts", "__MACOSX")} {
		if _, err := os.Stat(filepath.Join(dst, name)); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be skipped, stat err=%v", name, err)
		}
	}
}

func writeCatalog(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	return path
}
