package osimage

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var (
	ErrInvalidName      = errors.New("name is required")
	ErrInvalidOSFamily  = errors.New("osFamily is required")
	ErrInvalidOSVersion = errors.New("osVersion is required")
)

func ValidateOSImage(img OSImage) error {
	if strings.TrimSpace(img.Name) == "" {
		return ErrInvalidName
	}
	if strings.TrimSpace(img.OSFamily) == "" {
		return ErrInvalidOSFamily
	}
	if strings.TrimSpace(img.OSVersion) == "" {
		return ErrInvalidOSVersion
	}
	if img.Format != "" && img.Format != FormatQCOW2 && img.Format != FormatSquashFS {
		return fmt.Errorf("unsupported format: %s", img.Format)
	}
	format := EffectiveImageFormat(img)
	if format != "" && format != FormatQCOW2 && format != FormatSquashFS {
		return fmt.Errorf("unsupported format: %s", format)
	}
	if img.Manifest != nil {
		for _, target := range img.Manifest.Capabilities.DeployTargets {
			switch target {
			case DeploymentTargetVM, DeploymentTargetBareMetal:
			default:
				return fmt.Errorf("unsupported deployment target: %s", target)
			}
		}
	}
	if format != FormatQCOW2 && manifestDeclaresDeploymentTarget(img.Manifest, DeploymentTargetVM) {
		return fmt.Errorf("deployment target %s requires qcow2 image, got %s", DeploymentTargetVM, format)
	}
	if img.Source != "" && img.Source != SourceUpload && img.Source != SourceURL {
		return fmt.Errorf("unsupported source: %s", img.Source)
	}
	switch img.Variant {
	case "", VariantCloud, VariantBareMetal, VariantServer, VariantDesktop:
	default:
		return fmt.Errorf("unsupported variant: %s", img.Variant)
	}
	if img.Source == SourceURL && strings.TrimSpace(img.URL) == "" {
		return errors.New("url is required for url source")
	}
	if format == FormatQCOW2 && (img.Variant == VariantBareMetal || manifestDeclaresDeploymentTarget(img.Manifest, DeploymentTargetBareMetal)) {
		if img.Manifest == nil || strings.TrimSpace(img.Manifest.Root.Path) == "" {
			return errors.New("manifest.root.path is required for bare-metal qcow2 images")
		}
		if img.Manifest.Root.RootPartition.Number <= 0 {
			return errors.New("manifest.root.rootPartition.number is required for bare-metal qcow2 images")
		}
		if err := validateBareMetalQCOW2ModuleMetadata(img); err != nil {
			return err
		}
	}
	if format == FormatSquashFS && manifestDeclaresDeploymentTarget(img.Manifest, DeploymentTargetBareMetal) {
		if img.Manifest == nil || strings.TrimSpace(img.Manifest.Root.Path) == "" {
			return errors.New("manifest.root.path is required for bare-metal squashfs images")
		}
	}
	return nil
}

func validateBareMetalQCOW2ModuleMetadata(img OSImage) error {
	if img.Manifest == nil || !strings.EqualFold(strings.TrimSpace(img.OSFamily), "ubuntu") {
		return nil
	}
	if !ubuntuRequiresBareMetalExtraModules(img.OSVersion) {
		return nil
	}
	targetKernelVersion := strings.TrimSpace(img.Manifest.TargetKernel.Version)
	for _, pkg := range img.Manifest.Build.ModulePackages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "linux-modules-extra-{kernel_release}" {
			return nil
		}
		if targetKernelVersion != "" && pkg == "linux-modules-extra-"+targetKernelVersion {
			return nil
		}
	}
	for _, bundle := range img.Manifest.Bundles {
		if !strings.EqualFold(strings.TrimSpace(bundle.Type), "kernel-modules") {
			continue
		}
		if strings.TrimSpace(bundle.Path) == "" || len(bundle.ProvidesModules) == 0 {
			continue
		}
		bundleKernelVersion := strings.TrimSpace(bundle.KernelVersion)
		if targetKernelVersion != "" {
			if bundleKernelVersion == targetKernelVersion {
				return nil
			}
			continue
		}
		if bundleKernelVersion != "" {
			return nil
		}
	}
	return errors.New("ubuntu bare-metal qcow2 images require manifest.build.modulePackages or manifest.bundles to provide linux-modules-extra for the target kernel")
}

func ubuntuRequiresBareMetalExtraModules(version string) bool {
	major, minor, ok := parseMajorMinorVersion(version)
	if !ok {
		return true
	}
	return major < 25 || (major == 25 && minor < 10)
}

func parseMajorMinorVersion(version string) (int, int, bool) {
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return major, minor, true
}

func manifestDeclaresDeploymentTarget(manifest *Manifest, target DeploymentTarget) bool {
	if manifest == nil {
		return false
	}
	for _, candidate := range manifest.Capabilities.DeployTargets {
		if candidate == target {
			return true
		}
	}
	return false
}
