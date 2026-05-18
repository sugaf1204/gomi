package osimage

import (
	"strings"
	"testing"
)

func validImage() OSImage {
	return OSImage{
		Name:      "ubuntu-24.04",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
		Arch:      "amd64",
		Format:    FormatQCOW2,
		Source:    SourceUpload,
	}
}

func TestValidateOSImage_Valid(t *testing.T) {
	if err := ValidateOSImage(validImage()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateOSImage_MissingName(t *testing.T) {
	img := validImage()
	img.Name = ""
	if err := ValidateOSImage(img); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestValidateOSImage_MissingOSFamily(t *testing.T) {
	img := validImage()
	img.OSFamily = ""
	if err := ValidateOSImage(img); err == nil {
		t.Fatal("expected error for missing osFamily")
	}
}

func TestValidateOSImage_UnsupportedSource(t *testing.T) {
	img := validImage()
	img.Source = SourceType("s3")
	if err := ValidateOSImage(img); err == nil {
		t.Fatal("expected error for unsupported source")
	}
}

func TestValidateOSImage_UnsupportedTopLevelFormat(t *testing.T) {
	img := validImage()
	img.Format = ImageFormat("vhdx")
	img.Manifest = &Manifest{Root: RootArtifact{Format: FormatQCOW2, Path: "root.qcow2"}}
	if err := ValidateOSImage(img); err == nil || !strings.Contains(err.Error(), "unsupported format: vhdx") {
		t.Fatalf("expected top-level format error, got %v", err)
	}
}

func TestValidateOSImage_UnsupportedManifestRootFormat(t *testing.T) {
	img := validImage()
	img.Manifest = &Manifest{Root: RootArtifact{Format: ImageFormat("vhdx"), Path: "root.vhdx"}}
	if err := ValidateOSImage(img); err == nil || !strings.Contains(err.Error(), "unsupported format: vhdx") {
		t.Fatalf("expected manifest root format error, got %v", err)
	}
}

func TestValidateOSImage_URLNeedsURL(t *testing.T) {
	img := validImage()
	img.Source = SourceURL
	img.URL = ""
	if err := ValidateOSImage(img); err == nil {
		t.Fatal("expected error for missing url")
	}
}

func TestValidateOSImage_BareMetalQCOW2RequiresManifestRootPartition(t *testing.T) {
	img := validImage()
	img.Variant = VariantBareMetal
	if err := ValidateOSImage(img); err == nil || !strings.Contains(err.Error(), "manifest.root.path") {
		t.Fatalf("expected bare-metal qcow2 manifest path error, got %v", err)
	}
	img.Manifest = &Manifest{Root: RootArtifact{Format: FormatQCOW2, Path: "root.qcow2"}}
	if err := ValidateOSImage(img); err == nil || !strings.Contains(err.Error(), "rootPartition.number") {
		t.Fatalf("expected bare-metal qcow2 root partition error, got %v", err)
	}
	img.Manifest.Root.RootPartition.Number = 1
	if err := ValidateOSImage(img); err != nil {
		t.Fatalf("expected bare-metal qcow2 with manifest to validate, got %v", err)
	}
}

func TestValidateOSImage_BareMetalQCOW2UsesManifestRootFormat(t *testing.T) {
	img := validImage()
	img.Format = ""
	img.Variant = VariantBareMetal
	img.Manifest = &Manifest{Root: RootArtifact{Format: FormatQCOW2, Path: "root.qcow2"}}
	if err := ValidateOSImage(img); err == nil || !strings.Contains(err.Error(), "rootPartition.number") {
		t.Fatalf("expected bare-metal qcow2 root partition error from manifest format, got %v", err)
	}
	img.Manifest.Root.RootPartition.Number = 1
	if err := ValidateOSImage(img); err != nil {
		t.Fatalf("expected manifest qcow2 format to validate, got %v", err)
	}
}
