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

func TestValidateOSImage_Variant(t *testing.T) {
	img := validImage()
	img.Variant = VariantDesktop
	if err := ValidateOSImage(img); err != nil {
		t.Fatalf("expected desktop variant to validate, got %v", err)
	}

	img.Variant = Variant("workstation")
	if err := ValidateOSImage(img); err == nil || !strings.Contains(err.Error(), "unsupported variant") {
		t.Fatalf("expected unsupported variant error, got %v", err)
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
	img.Manifest.Build.ModulePackages = []string{"linux-modules-extra-{kernel_release}"}
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
	img.Manifest.Build.ModulePackages = []string{"linux-modules-extra-{kernel_release}"}
	if err := ValidateOSImage(img); err != nil {
		t.Fatalf("expected manifest qcow2 format to validate, got %v", err)
	}
}

func TestValidateOSImage_BareMetalCapabilityRequiresRootPartition(t *testing.T) {
	img := validImage()
	img.Manifest = &Manifest{
		Capabilities: Capabilities{DeployTargets: []DeploymentTarget{DeploymentTargetBareMetal}},
		Root:         RootArtifact{Format: FormatQCOW2, Path: "root.qcow2"},
	}
	if err := ValidateOSImage(img); err == nil || !strings.Contains(err.Error(), "rootPartition.number") {
		t.Fatalf("expected bare-metal capability root partition error, got %v", err)
	}
	img.Manifest.Root.RootPartition.Number = 1
	img.Manifest.Build.ModulePackages = []string{"linux-modules-extra-{kernel_release}"}
	if err := ValidateOSImage(img); err != nil {
		t.Fatalf("expected bare-metal capability image to validate, got %v", err)
	}
}

func TestValidateOSImage_UbuntuBareMetalQCOW2RequiresKernelExtraModules(t *testing.T) {
	img := validImage()
	img.Manifest = &Manifest{
		Capabilities: Capabilities{DeployTargets: []DeploymentTarget{DeploymentTargetBareMetal}},
		Root: RootArtifact{
			Format:        FormatQCOW2,
			Path:          "root.qcow2",
			RootPartition: Partition{Number: 1},
		},
	}
	if err := ValidateOSImage(img); err == nil || !strings.Contains(err.Error(), "linux-modules-extra") {
		t.Fatalf("expected ubuntu bare-metal module package error, got %v", err)
	}

	img.Manifest.Build.ModulePackages = []string{"linux-modules-extra-{kernel_release}"}
	if err := ValidateOSImage(img); err != nil {
		t.Fatalf("expected ubuntu bare-metal module package metadata to validate, got %v", err)
	}
}

func TestValidateOSImage_UbuntuBareMetalQCOW2MatchesKernelExtraModulesToTargetKernel(t *testing.T) {
	img := validImage()
	img.Manifest = &Manifest{
		Capabilities: Capabilities{DeployTargets: []DeploymentTarget{DeploymentTargetBareMetal}},
		Root: RootArtifact{
			Format:        FormatQCOW2,
			Path:          "root.qcow2",
			RootPartition: Partition{Number: 1},
		},
		TargetKernel: TargetKernel{Version: "6.8.0-124-generic"},
		Build: BuildMetadata{
			ModulePackages: []string{"linux-modules-extra-6.8.0-123-generic"},
		},
	}
	if err := ValidateOSImage(img); err == nil || !strings.Contains(err.Error(), "target kernel") {
		t.Fatalf("expected stale module package to be rejected, got %v", err)
	}

	img.Manifest.Build.ModulePackages = []string{"linux-modules-extra-6.8.0-124-generic"}
	if err := ValidateOSImage(img); err != nil {
		t.Fatalf("expected target-kernel module package metadata to validate, got %v", err)
	}
}

func TestValidateOSImage_UbuntuBareMetalQCOW2AcceptsKernelModuleBundle(t *testing.T) {
	img := validImage()
	img.Manifest = &Manifest{
		Capabilities: Capabilities{DeployTargets: []DeploymentTarget{DeploymentTargetBareMetal}},
		Root: RootArtifact{
			Format:        FormatQCOW2,
			Path:          "root.qcow2",
			RootPartition: Partition{Number: 1},
		},
		TargetKernel: TargetKernel{Version: "6.8.0-124-generic"},
		Bundles: []Bundle{
			{
				ID:              "modules",
				Type:            "kernel-modules",
				KernelVersion:   "6.8.0-124-generic",
				Path:            "bundles/modules.tar.zst",
				ProvidesModules: []string{"e1000e"},
			},
		},
	}
	if err := ValidateOSImage(img); err != nil {
		t.Fatalf("expected target-kernel module bundle to validate, got %v", err)
	}

	img.Manifest.Bundles[0].KernelVersion = "6.8.0-123-generic"
	if err := ValidateOSImage(img); err == nil || !strings.Contains(err.Error(), "target kernel") {
		t.Fatalf("expected stale module bundle to be rejected, got %v", err)
	}
}

func TestValidateOSImage_DebianBareMetalQCOW2DoesNotRequireUbuntuModulePackage(t *testing.T) {
	img := validImage()
	img.OSFamily = "debian"
	img.Manifest = &Manifest{
		Capabilities: Capabilities{DeployTargets: []DeploymentTarget{DeploymentTargetBareMetal}},
		Root: RootArtifact{
			Format:        FormatQCOW2,
			Path:          "root.qcow2",
			RootPartition: Partition{Number: 1},
		},
	}
	if err := ValidateOSImage(img); err != nil {
		t.Fatalf("expected non-ubuntu bare-metal qcow2 image to validate, got %v", err)
	}
}

func TestValidateOSImage_BareMetalSquashFSRequiresManifestRootPath(t *testing.T) {
	img := validImage()
	img.Format = FormatSquashFS
	img.Manifest = &Manifest{
		Capabilities: Capabilities{DeployTargets: []DeploymentTarget{DeploymentTargetBareMetal}},
		Root:         RootArtifact{Format: FormatSquashFS},
	}
	if err := ValidateOSImage(img); err == nil || !strings.Contains(err.Error(), "manifest.root.path") {
		t.Fatalf("expected bare-metal squashfs manifest path error, got %v", err)
	}
	img.Manifest.Root.Path = "rootfs.squashfs"
	if err := ValidateOSImage(img); err != nil {
		t.Fatalf("expected bare-metal squashfs image to validate, got %v", err)
	}
}

func TestValidateOSImage_RejectsVMSquashFS(t *testing.T) {
	img := validImage()
	img.Format = FormatSquashFS
	img.Manifest = &Manifest{
		Capabilities: Capabilities{DeployTargets: []DeploymentTarget{DeploymentTargetVM}},
		Root:         RootArtifact{Format: FormatSquashFS, Path: "rootfs.squashfs"},
	}
	if err := ValidateOSImage(img); err == nil || !strings.Contains(err.Error(), "deployment target vm requires qcow2 image") {
		t.Fatalf("expected vm squashfs rejection, got %v", err)
	}
}

func TestSupportsDeploymentTarget_ManifestCapabilitiesOverrideVariant(t *testing.T) {
	img := validImage()
	img.Variant = VariantBareMetal
	img.Manifest = &Manifest{
		Capabilities: Capabilities{DeployTargets: []DeploymentTarget{DeploymentTargetVM}},
		Root:         RootArtifact{Format: FormatQCOW2, Path: "root.qcow2"},
	}
	if !SupportsDeploymentTarget(img, DeploymentTargetVM) {
		t.Fatal("expected explicit vm capability to be supported")
	}
	if SupportsDeploymentTarget(img, DeploymentTargetBareMetal) {
		t.Fatal("expected explicit capabilities to override baremetal variant")
	}
}

func TestSupportsDeploymentTarget_SquashFSBareMetalOnly(t *testing.T) {
	img := validImage()
	img.Format = FormatSquashFS
	img.Manifest = &Manifest{
		Capabilities: Capabilities{DeployTargets: []DeploymentTarget{DeploymentTargetBareMetal}},
		Root:         RootArtifact{Format: FormatSquashFS, Path: "rootfs.squashfs"},
	}
	if !SupportsDeploymentTarget(img, DeploymentTargetBareMetal) {
		t.Fatal("expected squashfs bare-metal capability to be supported")
	}
	if SupportsDeploymentTarget(img, DeploymentTargetVM) {
		t.Fatal("squashfs image must not be treated as VM-capable")
	}
}
