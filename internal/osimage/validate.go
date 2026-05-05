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
	if img.Format != "" && img.Format != FormatQCOW2 && img.Format != FormatRAW && img.Format != FormatISO {
		return fmt.Errorf("unsupported format: %s", img.Format)
	}
	if img.Source != "" && img.Source != SourceUpload && img.Source != SourceURL {
		return fmt.Errorf("unsupported source: %s", img.Source)
	}
	if img.Source == SourceURL && strings.TrimSpace(img.URL) == "" {
		return errors.New("url is required for url source")
	}
	return nil
}
