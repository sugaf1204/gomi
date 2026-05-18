package osimage

import (
	"errors"
	"fmt"
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
	format := effectiveImageFormat(img)
	if format != "" && format != FormatQCOW2 && format != FormatISO && format != FormatSquashFS {
		return fmt.Errorf("unsupported format: %s", format)
	}
	if img.Source != "" && img.Source != SourceUpload && img.Source != SourceURL {
		return fmt.Errorf("unsupported source: %s", img.Source)
	}
	if img.Source == SourceURL && strings.TrimSpace(img.URL) == "" {
		return errors.New("url is required for url source")
	}
	if format == FormatQCOW2 && img.Variant == VariantBareMetal {
		if img.Manifest == nil || strings.TrimSpace(img.Manifest.Root.Path) == "" {
			return errors.New("manifest.root.path is required for bare-metal qcow2 images")
		}
		if img.Manifest.Root.RootPartition.Number <= 0 {
			return errors.New("manifest.root.rootPartition.number is required for bare-metal qcow2 images")
		}
	}
	return nil
}

func effectiveImageFormat(img OSImage) ImageFormat {
	if img.Manifest != nil && img.Manifest.Root.Format != "" {
		return img.Manifest.Root.Format
	}
	return img.Format
}
