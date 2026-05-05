package cloudinit

import (
	"errors"
	"strings"
)

var (
	ErrInvalidName     = errors.New("name is required")
	ErrInvalidUserData = errors.New("userData is required")
)

func ValidateCloudInitTemplate(t CloudInitTemplate) error {
	if strings.TrimSpace(t.Name) == "" {
		return ErrInvalidName
	}
	if strings.TrimSpace(t.UserData) == "" {
		return ErrInvalidUserData
	}
	return nil
}
