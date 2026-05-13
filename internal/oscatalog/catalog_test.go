package oscatalog

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sugaf1204/gomi/internal/osimage"
)

func TestListUsesPrebuiltArtifacts(t *testing.T) {
	t.Setenv("GOMI_OS_IMAGE_SOURCE_URL", "https://images.example.test/gomi")

	entries, err := ListWithContext(context.Background())
	if err != nil {
		t.Fatalf("list catalog: %v", err)
	}
	seen := map[string]struct{}{}
	for _, entry := range entries {
		seen[entry.Name] = struct{}{}
		switch entry.Name {
		case "debian-13-amd64-baremetal", "ubuntu-22.04-amd64-baremetal", "ubuntu-24.04-amd64-baremetal":
			if !strings.HasPrefix(entry.URL, "https://images.example.test/gomi/") {
				t.Fatalf("%s URL = %q, want configured source base", entry.Name, entry.URL)
			}
			if entry.Format != osimage.FormatSquashFS || entry.SourceFormat != osimage.FormatSquashFS {
				t.Fatalf("%s formats = %s/%s, want squashfs", entry.Name, entry.Format, entry.SourceFormat)
			}
			if entry.SourceCompression != "" {
				t.Fatalf("%s sourceCompression = %q, want empty", entry.Name, entry.SourceCompression)
			}
			if !strings.HasSuffix(entry.URL, ".rootfs.squashfs") {
				t.Fatalf("%s URL = %q, want prebuilt .rootfs.squashfs artifact", entry.Name, entry.URL)
			}
		case "debian-13-amd64-cloud":
			if entry.URL != "https://cloud.debian.org/images/cloud/trixie/latest/debian-13-genericcloud-amd64.qcow2" {
				t.Fatalf("%s URL = %q, want Debian official qcow2", entry.Name, entry.URL)
			}
			if entry.Format != osimage.FormatQCOW2 || entry.SourceFormat != osimage.FormatQCOW2 {
				t.Fatalf("%s formats = %s/%s, want qcow2", entry.Name, entry.Format, entry.SourceFormat)
			}
			if entry.SourceCompression != "" {
				t.Fatalf("%s sourceCompression = %q, want empty", entry.Name, entry.SourceCompression)
			}
			if entry.Checksum != "" {
				t.Fatalf("%s checksum = %q, want empty for rolling official image URL", entry.Name, entry.Checksum)
			}
		default:
			if !strings.HasPrefix(entry.URL, "https://github.com/sugaf1204/gomi/releases/download/v0.0.2/") {
				t.Fatalf("%s URL = %q, want fixed legacy release asset URL", entry.Name, entry.URL)
			}
			if entry.Format != osimage.FormatRAW || entry.SourceFormat != osimage.FormatRAW {
				t.Fatalf("%s formats = %s/%s, want raw", entry.Name, entry.Format, entry.SourceFormat)
			}
			if entry.SourceCompression != "zstd" {
				t.Fatalf("%s sourceCompression = %q, want zstd", entry.Name, entry.SourceCompression)
			}
			if !strings.HasSuffix(entry.URL, ".raw.zst") {
				t.Fatalf("%s URL = %q, want prebuilt .raw.zst artifact", entry.Name, entry.URL)
			}
			if !strings.HasPrefix(entry.Checksum, "sha256:") {
				t.Fatalf("%s checksum = %q, want sha256 release asset checksum", entry.Name, entry.Checksum)
			}
		}
	}
	for _, name := range []string{
		"debian-13-amd64-baremetal",
		"debian-13-amd64-cloud",
		"ubuntu-22.04-amd64-baremetal",
		"ubuntu-22.04-amd64-cloud",
		"ubuntu-24.04-amd64-baremetal",
		"ubuntu-24.04-amd64-cloud",
	} {
		if _, ok := seen[name]; !ok {
			t.Fatalf("default catalog missing %s", name)
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
  - name: ubuntu-22.04-amd64-baremetal
    osFamily: ubuntu
    osVersion: "22.04"
    arch: amd64
    variant: baremetal
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
	if got["ubuntu-22.04-amd64-baremetal"].URL != "https://release.example.test/assets/override.raw.zst" {
		t.Fatalf("default entry was not overlaid with relative URL base resolution: %#v", got["ubuntu-22.04-amd64-baremetal"])
	}
	if got["vendor-image"].URL != "https://vendor.example.test/root.raw.zst" {
		t.Fatalf("absolute external URL was modified: %q", got["vendor-image"].URL)
	}
}

func TestSquashFSEntryManifestIsRuntimeMetadataOnly(t *testing.T) {
	path := writeCatalog(t, `
entries:
  - name: ubuntu-22.04-amd64-baremetal
    osFamily: ubuntu
    osVersion: "22.04"
    arch: amd64
    variant: baremetal
    format: squashfs
    sourceFormat: squashfs
    url: ubuntu-22.04-amd64-baremetal.rootfs.squashfs
    bootEnvironment: ubuntu-minimal-cloud-amd64
`)
	entries, err := Load(context.Background(), LoadOptions{
		CatalogFile:     path,
		ReplaceExternal: true,
	})
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %#v", entries)
	}
	entry := entries[0]
	if got := entry.OSImage().Manifest; got == nil || got.Root.Path != "rootfs.squashfs" {
		t.Fatalf("manifest = %#v", got)
	}
}

func TestCatalogSchemaRejectsBuildMetadata(t *testing.T) {
	path := writeCatalog(t, `
entries:
  - name: invalid-image
    osFamily: ubuntu
    osVersion: "22.04"
    arch: amd64
    variant: baremetal
    format: squashfs
    sourceFormat: squashfs
    url: invalid.rootfs.squashfs
    bootEnvironment: ubuntu-minimal-cloud-amd64
    build:
      source:
        url: https://images.example.test/root.tar.xz
`)
	_, err := Load(context.Background(), LoadOptions{
		CatalogFile:     path,
		ReplaceExternal: true,
	})
	if err == nil || !strings.Contains(err.Error(), "validate OS catalog schema") {
		t.Fatalf("expected schema validation error for build metadata, got %v", err)
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

func TestCatalogSchemaAcceptsCloudQCOW2(t *testing.T) {
	path := writeCatalog(t, `
entries:
  - name: qcow2-image
    osFamily: custom
    osVersion: "1"
    arch: amd64
    variant: cloud
    format: qcow2
    sourceFormat: qcow2
    url: invalid.qcow2
    bootEnvironment: ubuntu-minimal-cloud-amd64
`)
	entries, err := Load(context.Background(), LoadOptions{
		CatalogFile:     path,
		ReplaceExternal: true,
	})
	if err != nil {
		t.Fatalf("expected cloud qcow2 to be accepted, got %v", err)
	}
	if len(entries) != 1 || entries[0].Format != osimage.FormatQCOW2 {
		t.Fatalf("entries = %#v, want qcow2", entries)
	}
}

func TestCatalogSchemaRejectsBareMetalQCOW2(t *testing.T) {
	path := writeCatalog(t, `
entries:
  - name: qcow2-baremetal-image
    osFamily: custom
    osVersion: "1"
    arch: amd64
    variant: baremetal
    format: qcow2
    sourceFormat: qcow2
    url: invalid.qcow2
    bootEnvironment: ubuntu-minimal-cloud-amd64
`)
	_, err := Load(context.Background(), LoadOptions{
		CatalogFile:     path,
		ReplaceExternal: true,
	})
	if err == nil || !strings.Contains(err.Error(), "validate OS catalog schema") {
		t.Fatalf("expected schema validation error for bare-metal qcow2, got %v", err)
	}
}

func TestCatalogSchemaRejectsCompressedSquashFS(t *testing.T) {
	path := writeCatalog(t, `
entries:
  - name: compressed-squashfs-image
    osFamily: custom
    osVersion: "1"
    arch: amd64
    variant: baremetal
    format: squashfs
    sourceFormat: squashfs
    sourceCompression: zstd
    url: invalid.rootfs.squashfs.zst
    bootEnvironment: ubuntu-minimal-cloud-amd64
`)
	_, err := Load(context.Background(), LoadOptions{
		CatalogFile:     path,
		ReplaceExternal: true,
	})
	if err == nil || !strings.Contains(err.Error(), "validate OS catalog schema") {
		t.Fatalf("expected schema validation error for compressed squashfs, got %v", err)
	}
}

func TestCatalogSchemaRejectsMismatchedSourceFormat(t *testing.T) {
	path := writeCatalog(t, `
entries:
  - name: mismatched-source-format
    osFamily: custom
    osVersion: "1"
    arch: amd64
    variant: baremetal
    format: squashfs
    sourceFormat: raw
    url: invalid.rootfs.squashfs
    bootEnvironment: ubuntu-minimal-cloud-amd64
`)
	_, err := Load(context.Background(), LoadOptions{
		CatalogFile:     path,
		ReplaceExternal: true,
	})
	if err == nil || !strings.Contains(err.Error(), "validate OS catalog schema") {
		t.Fatalf("expected schema validation error for mismatched sourceFormat, got %v", err)
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
