package oscatalog

import (
	"strings"
	"testing"

	"github.com/sugaf1204/gomi/internal/osimage"
)

func TestListUsesRawPrebuiltArtifacts(t *testing.T) {
	t.Setenv("GOMI_OS_IMAGE_SOURCE_URL", "https://images.example.test/gomi")

	for _, entry := range List() {
		if entry.Format != osimage.FormatRAW {
			t.Fatalf("%s format = %s, want raw", entry.Name, entry.Format)
		}
		if entry.SourceFormat != "" && entry.SourceFormat != osimage.FormatRAW {
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

func TestGetUsesConfiguredSourceBase(t *testing.T) {
	t.Setenv("GOMI_OS_IMAGE_SOURCE_URL", "https://images.example.test/releases/latest/download/")

	entry, ok := Get("ubuntu-24.04-amd64-baremetal")
	if !ok {
		t.Fatal("expected ubuntu-24.04-amd64-baremetal catalog entry")
	}
	if got, want := entry.URL, "https://images.example.test/releases/latest/download/ubuntu-24.04-amd64-baremetal.raw.zst"; got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
}

func TestListIncludesCloudAndBareMetalVariants(t *testing.T) {
	t.Setenv("GOMI_OS_IMAGE_SOURCE_URL", "https://images.example.test/gomi")

	want := map[string]osimage.Variant{
		"debian-13-amd64-cloud":        osimage.VariantCloud,
		"debian-13-amd64-baremetal":    osimage.VariantBareMetal,
		"ubuntu-22.04-amd64-cloud":     osimage.VariantCloud,
		"ubuntu-22.04-amd64-baremetal": osimage.VariantBareMetal,
		"ubuntu-24.04-amd64-cloud":     osimage.VariantCloud,
		"ubuntu-24.04-amd64-baremetal": osimage.VariantBareMetal,
	}
	got := map[string]osimage.Variant{}
	for _, entry := range List() {
		got[entry.Name] = entry.Variant
	}
	for name, variant := range want {
		if got[name] != variant {
			t.Fatalf("%s variant = %q, want %q", name, got[name], variant)
		}
	}
}
