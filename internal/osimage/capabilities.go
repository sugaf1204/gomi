package osimage

import "strings"

func SupportsDeploymentTarget(img OSImage, target DeploymentTarget) bool {
	if target == "" {
		return false
	}
	if img.Manifest != nil && len(img.Manifest.Capabilities.DeployTargets) > 0 {
		for _, candidate := range img.Manifest.Capabilities.DeployTargets {
			if strings.EqualFold(string(candidate), string(target)) {
				return true
			}
		}
		return false
	}

	format := EffectiveImageFormat(img)
	switch target {
	case DeploymentTargetVM:
		return format == FormatQCOW2 && (img.Variant == "" || img.Variant == VariantCloud)
	case DeploymentTargetBareMetal:
		switch format {
		case FormatQCOW2:
			if img.Variant == VariantBareMetal {
				return true
			}
			return img.Manifest != nil &&
				strings.TrimSpace(img.Manifest.Root.Path) != "" &&
				img.Manifest.Root.RootPartition.Number > 0
		case FormatSquashFS:
			return img.Manifest != nil && strings.TrimSpace(img.Manifest.Root.Path) != ""
		default:
			return false
		}
	default:
		return false
	}
}

func HasExplicitDeploymentTargets(img OSImage) bool {
	return img.Manifest != nil && len(img.Manifest.Capabilities.DeployTargets) > 0
}

func EffectiveImageFormat(img OSImage) ImageFormat {
	if img.Manifest != nil && img.Manifest.Root.Format != "" {
		return img.Manifest.Root.Format
	}
	return img.Format
}
