package oscatalog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sugaf1204/gomi/internal/osimage"
)

func TestListUsesRawPrebuiltArtifacts(t *testing.T) {
	t.Setenv("GOMI_OS_IMAGE_SOURCE_URL", "https://images.example.test/gomi")

	entries, err := ListWithContext(context.Background())
	if err != nil {
		t.Fatalf("list catalog: %v", err)
	}
	for _, entry := range entries {
		if entry.Format != osimage.FormatRAW {
			t.Fatalf("%s format = %s, want raw", entry.Name, entry.Format)
		}
		if entry.SourceFormat != osimage.FormatRAW {
			t.Fatalf("%s sourceFormat = %s, want raw", entry.Name, entry.SourceFormat)
		}
		if entry.SourceCompression != "zstd" {
			t.Fatalf("%s sourceCompression = %q, want zstd", entry.Name, entry.SourceCompression)
		}
		if !strings.HasPrefix(entry.URL, "https://images.example.test/gomi/") {
			t.Fatalf("%s URL = %q, want configured source base", entry.Name, entry.URL)
		}
		if !strings.HasSuffix(entry.URL, ".raw.zst") {
			t.Fatalf("%s URL = %q, want prebuilt .raw.zst artifact", entry.Name, entry.URL)
		}
	}
}

func TestExternalCatalogFileCanReplaceDefaults(t *testing.T) {
	path := writeCatalog(t, `
entries:
  - name: external-image
    osFamily: custom
    osVersion: "1"
    arch: amd64
    variant: baremetal
    format: raw
    sourceFormat: raw
    sourceCompression: zstd
    url: https://images.example.test/external.raw.zst
    bootEnvironment: ubuntu-minimal-cloud-amd64
`)
	entries, err := Load(context.Background(), LoadOptions{
		CatalogFile:     path,
		ReplaceExternal: true,
	})
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != "external-image" {
		t.Fatalf("expected replacement catalog to contain only external-image, got %#v", entries)
	}
	if entries[0].URL != "https://images.example.test/external.raw.zst" {
		t.Fatalf("absolute URL was modified: %q", entries[0].URL)
	}
}

func TestExternalCatalogFileOverlaysDefaultByName(t *testing.T) {
	path := writeCatalog(t, `
entries:
  - name: ubuntu-24.04-amd64-cloud
    osFamily: ubuntu
    osVersion: "24.04"
    arch: amd64
    variant: cloud
    format: raw
    sourceFormat: raw
    sourceCompression: zstd
    url: override.raw.zst
    bootEnvironment: ubuntu-minimal-cloud-amd64
  - name: vendor-image
    osFamily: vendor
    osVersion: "2026"
    arch: amd64
    variant: baremetal
    format: raw
    sourceFormat: raw
    sourceCompression: zstd
    url: https://vendor.example.test/root.raw.zst
    bootEnvironment: ubuntu-minimal-cloud-amd64
`)

	entries, err := Load(context.Background(), LoadOptions{
		SourceBase:  "https://release.example.test/assets",
		CatalogFile: path,
	})
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	got := map[string]Entry{}
	for _, entry := range entries {
		got[entry.Name] = entry
	}
	if got["ubuntu-24.04-amd64-cloud"].URL != "https://release.example.test/assets/override.raw.zst" {
		t.Fatalf("default entry was not overlaid with relative URL base resolution: %#v", got["ubuntu-24.04-amd64-cloud"])
	}
	if got["vendor-image"].URL != "https://vendor.example.test/root.raw.zst" {
		t.Fatalf("absolute external URL was modified: %q", got["vendor-image"].URL)
	}
}

func TestBuildEntriesExcludeURLOnlyEntries(t *testing.T) {
	entries, err := Load(context.Background(), LoadOptions{})
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	buildEntries := BuildEntries(entries)
	if len(buildEntries) == 0 {
		t.Fatal("expected default catalog to include build recipes")
	}
	for _, entry := range buildEntries {
		if entry.Build == nil {
			t.Fatalf("build entry %s has nil build recipe", entry.Name)
		}
	}

	urlOnly := Entry{
		Name:              "url-only",
		OSFamily:          "custom",
		OSVersion:         "1",
		Arch:              "amd64",
		Format:            osimage.FormatRAW,
		SourceFormat:      osimage.FormatRAW,
		SourceCompression: "zstd",
		URL:               "https://images.example.test/root.raw.zst",
		BootEnvironment:   "ubuntu-minimal-cloud-amd64",
	}
	if got := BuildEntries([]Entry{urlOnly}); len(got) != 0 {
		t.Fatalf("expected URL-only entry to be excluded from build matrix, got %#v", got)
	}
}

func TestCatalogSchemaRejectsUnknownFields(t *testing.T) {
	path := writeCatalog(t, `
entries:
  - name: invalid-image
    osFamily: custom
    osVersion: "1"
    arch: amd64
    variant: baremetal
    format: raw
    sourceFormat: raw
    sourceCompression: zstd
    url: invalid.raw.zst
    bootEnvironment: ubuntu-minimal-cloud-amd64
    unexpected: value
`)
	_, err := Load(context.Background(), LoadOptions{
		CatalogFile:     path,
		ReplaceExternal: true,
	})
	if err == nil || !strings.Contains(err.Error(), "validate OS catalog schema") {
		t.Fatalf("expected schema validation error for unknown field, got %v", err)
	}
}

func TestCatalogSemanticValidationRejectsDuplicateNames(t *testing.T) {
	path := writeCatalog(t, `
entries:
  - name: duplicate-image
    osFamily: custom
    osVersion: "1"
    arch: amd64
    variant: baremetal
    format: raw
    sourceFormat: raw
    sourceCompression: zstd
    url: first.raw.zst
    bootEnvironment: ubuntu-minimal-cloud-amd64
  - name: duplicate-image
    osFamily: custom
    osVersion: "1"
    arch: amd64
    variant: baremetal
    format: raw
    sourceFormat: raw
    sourceCompression: zstd
    url: second.raw.zst
    bootEnvironment: ubuntu-minimal-cloud-amd64
`)
	_, err := Load(context.Background(), LoadOptions{
		CatalogFile:     path,
		ReplaceExternal: true,
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate OS catalog entry") {
		t.Fatalf("expected duplicate name error, got %v", err)
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
