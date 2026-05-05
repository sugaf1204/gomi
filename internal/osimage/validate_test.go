package osimage

import (
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

func TestValidateOSImage_URLNeedsURL(t *testing.T) {
	img := validImage()
	img.Source = SourceURL
	img.URL = ""
	if err := ValidateOSImage(img); err == nil {
		t.Fatal("expected error for missing url")
	}
}
