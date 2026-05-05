package machine

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
)

var (
	ErrInvalidName = errors.New("name is required")
	ErrInvalidMAC  = errors.New("mac is invalid")
)

func ValidateMachine(m Machine) error {
	if strings.TrimSpace(m.Name) == "" {
		return ErrInvalidName
	}
	if _, err := net.ParseMAC(m.MAC); err != nil {
		return ErrInvalidMAC
	}
	if CanonicalArch(m.Arch) == "" {
		return fmt.Errorf("unsupported or missing arch: %s", m.Arch)
	}
	if strings.TrimSpace(string(m.OSPreset.Family)) == "" {
		return fmt.Errorf("os family is required")
	}
	if m.Firmware != FirmwareUEFI && m.Firmware != FirmwareBIOS {
		return fmt.Errorf("unsupported firmware: %s", m.Firmware)
	}
	if disk := strings.TrimSpace(m.TargetDisk); disk != "" && !IsWholeDiskPath(disk) {
		return fmt.Errorf("targetDisk must be a whole disk path: %s", disk)
	}
	if err := resource.ValidateCloudInitRefs(m.CloudInitRef, m.CloudInitRefs); err != nil {
		return err
	}
	if err := power.ValidatePowerConfig(m.Power); err != nil {
		return err
	}
	if err := resource.ValidateIPAssignment(m.IPAssignment, m.IP); err != nil {
		return err
	}
	if err := ValidateLoginUser(m.LoginUser); err != nil {
		return err
	}
	return nil
}

// linuxUsernamePattern matches conservative POSIX-style usernames (lowercase
// letters, digits, dash, underscore, length 1-32). This excludes weird shell
// metacharacters that could escape a cloud-config users entry.
var linuxUsernamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

func ValidateLoginUser(u *LoginUserSpec) error {
	if u == nil {
		return nil
	}
	name := strings.TrimSpace(u.Username)
	if name == "" {
		return errors.New("loginUser.username is required when loginUser is set")
	}
	if !linuxUsernamePattern.MatchString(name) {
		return fmt.Errorf("loginUser.username is invalid: %q", name)
	}
	return nil
}

func IsWholeDiskPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	switch {
	case strings.HasPrefix(path, "/dev/disk/by-id/"), strings.HasPrefix(path, "/dev/disk/by-path/"):
		return !regexp.MustCompile(`-part[0-9]+$`).MatchString(filepath.Base(path))
	case regexp.MustCompile(`^/dev/(sd|vd|xvd)[a-z]+$`).MatchString(path):
		return true
	case regexp.MustCompile(`^/dev/nvme[0-9]+n[0-9]+$`).MatchString(path):
		return true
	case regexp.MustCompile(`^/dev/mmcblk[0-9]+$`).MatchString(path):
		return true
	default:
		return false
	}
}

func CanonicalArch(arch string) string {
	switch strings.ToLower(strings.TrimSpace(arch)) {
	case "amd64", "x86_64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	case "i386", "386", "x86":
		return "i386"
	default:
		return ""
	}
}
