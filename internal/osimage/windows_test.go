package osimage

import (
	"os"
	"path/filepath"
	"testing"
)

func windowsTestImage() OSImage {
	return OSImage{Name: "windows-11", OSFamily: "windows", OSVersion: "11", Arch: "amd64", Format: FormatWIM, Manifest: &Manifest{BootModes: []string{"uefi"}, Root: RootArtifact{Format: FormatWIM, Path: "install.wim"}, Windows: &WindowsImage{ImageIndex: 6, WinPE: WindowsPE{Wimboot: "wimboot", BCD: "BCD", BootSDI: "boot.sdi", BootWIM: "boot.wim"}}}}
}

func TestWindowsImageCapabilitiesAndValidation(t *testing.T) {
	img := windowsTestImage()
	if err := ValidateOSImage(img); err != nil {
		t.Fatal(err)
	}
	for _, target := range []DeploymentTarget{DeploymentTargetVM, DeploymentTargetBareMetal} {
		if !SupportsDeploymentTarget(img, target) {
			t.Fatalf("Windows missing target %s", target)
		}
	}
	cases := []struct {
		name   string
		change func(*OSImage)
	}{
		{"url", func(i *OSImage) { i.Source = SourceURL; i.URL = "https://example.invalid/install.wim" }},
		{"linux-wim", func(i *OSImage) { i.OSFamily = "fedora" }},
		{"missing-metadata", func(i *OSImage) { i.Manifest.Windows = nil }},
		{"arm", func(i *OSImage) { i.Arch = "arm64" }},
		{"bios", func(i *OSImage) { i.Manifest.BootModes = []string{"bios"} }},
		{"index", func(i *OSImage) { i.Manifest.Windows.ImageIndex = 0 }},
		{"traversal", func(i *OSImage) { i.Manifest.Windows.WinPE.BootWIM = "../boot.wim" }},
		{"injection", func(i *OSImage) { i.Manifest.Windows.WinPE.Wimboot = "wimboot;reboot" }},
		{"whitespace", func(i *OSImage) { i.Manifest.Windows.WinPE.Wimboot = "wim boot" }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			i := windowsTestImage()
			tt.change(&i)
			if err := ValidateOSImage(i); err == nil {
				t.Fatal("expected invalid image")
			}
		})
	}
}

func TestWindowsArtifactsReadyRequiresAllAssets(t *testing.T) {
	img := windowsTestImage()
	root := t.TempDir()
	if WindowsArtifactsReady(img, root) == nil {
		t.Fatal("missing assets accepted")
	}
	for _, rel := range img.Manifest.WindowsArtifacts() {
		if err := os.WriteFile(filepath.Join(root, rel), []byte("asset"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := WindowsArtifactsReady(img, root); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "boot.wim")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "install.wim"), filepath.Join(root, "boot.wim")); err != nil {
		t.Fatal(err)
	}
	if WindowsArtifactsReady(img, root) == nil {
		t.Fatal("symlink accepted")
	}
}

func TestWindowsOSFamilyRejectsWhitespaceAndPreservesCaseSupport(t *testing.T) {
	for _, family := range []string{"windows", "Windows", "WINDOWS"} {
		img := windowsTestImage()
		img.OSFamily = family
		if err := ValidateOSImage(img); err != nil {
			t.Fatalf("valid family %q rejected: %v", family, err)
		}
		for _, target := range []DeploymentTarget{DeploymentTargetVM, DeploymentTargetBareMetal} {
			if !SupportsDeploymentTarget(img, target) {
				t.Fatalf("family %q lost target %s", family, target)
			}
		}
	}
	for _, family := range []string{" windows", "windows ", "\tWindows\n"} {
		img := windowsTestImage()
		img.OSFamily = family
		if err := ValidateOSImage(img); err == nil {
			t.Fatalf("family %q accepted despite downstream exact routing", family)
		}
	}
	// This guard is Windows-only; do not introduce Linux canonicalization here.
	if err := validateWindowsImage(OSImage{OSFamily: " fedora ", Format: FormatQCOW2}); err != nil {
		t.Fatalf("Windows guard changed Linux validation: %v", err)
	}
}
