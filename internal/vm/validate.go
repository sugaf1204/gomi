package vm

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/sugaf1204/gomi/internal/resource"
)

var (
	ErrInvalidName = errors.New("name is required")
)

func ValidateVirtualMachine(v VirtualMachine) error {
	if strings.TrimSpace(v.Name) == "" {
		return ErrInvalidName
	}
	// hypervisorRef may be empty if auto-placement is used.
	if v.Resources.CPUCores <= 0 {
		return errors.New("resources.cpuCores must be positive")
	}
	if v.Resources.MemoryMB <= 0 {
		return errors.New("resources.memoryMB must be positive")
	}
	if v.Resources.DiskGB <= 0 {
		return errors.New("resources.diskGB must be positive")
	}
	if strings.TrimSpace(v.OSImageRef) == "" {
		return errors.New("osImageRef is required")
	}
	if err := resource.ValidateCloudInitRefs(v.CloudInitRef, v.CloudInitRefs); err != nil {
		return err
	}
	if v.PowerControlMethod != "" && v.PowerControlMethod != PowerControlLibvirt {
		return fmt.Errorf("unsupported powerControlMethod: %s", v.PowerControlMethod)
	}
	if err := validateAdvancedOptions(v); err != nil {
		return err
	}
	staticIP := ""
	if len(v.Network) > 0 {
		staticIP = v.Network[0].IPAddress
	}
	if err := resource.ValidateIPAssignment(v.IPAssignment, staticIP); err != nil {
		return err
	}
	if v.InstallCfg != nil {
		cfgType := strings.TrimSpace(string(v.InstallCfg.Type))
		if cfgType != "" {
			switch InstallConfigType(cfgType) {
			case InstallConfigPreseed, InstallConfigCurtin:
			default:
				return fmt.Errorf("unsupported installConfig.type: %s", cfgType)
			}
		}
		if cfgType == "" && strings.TrimSpace(v.InstallCfg.Inline) != "" {
			return errors.New("installConfig.type is required when installConfig.inline is set")
		}
	}
	if err := ValidateLoginUser(v.LoginUser); err != nil {
		return err
	}
	return nil
}

// linuxUsernamePattern matches conservative POSIX-style usernames (lowercase
// letters, digits, dash, underscore, length 1-32).
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

func validateAdvancedOptions(v VirtualMachine) error {
	opts := v.AdvancedOptions
	if opts == nil {
		return nil
	}
	if opts.IOThreads < 0 {
		return errors.New("advancedOptions.ioThreads must be non-negative")
	}
	if opts.NetMultiqueue < 0 {
		return errors.New("advancedOptions.netMultiqueue must be non-negative")
	}
	switch opts.DiskDriver {
	case "", DiskDriverVirtio, DiskDriverSCSI:
	default:
		return fmt.Errorf("unsupported advancedOptions.diskDriver: %s (must be virtio or scsi)", opts.DiskDriver)
	}
	switch opts.CPUMode {
	case "", CPUModeHostPassthrough, CPUModeHostModel, CPUModeMaximum:
	default:
		return fmt.Errorf("unsupported advancedOptions.cpuMode: %s (must be host-passthrough, host-model, or maximum)", opts.CPUMode)
	}
	switch opts.DiskFormat {
	case "", "qcow2":
	default:
		return fmt.Errorf("unsupported advancedOptions.diskFormat: %s (must be qcow2)", opts.DiskFormat)
	}
	for vcpu := range opts.CPUPinning {
		if vcpu < 0 || vcpu >= v.Resources.CPUCores {
			return fmt.Errorf("advancedOptions.cpuPinning: vcpu %d is out of range [0, %d)", vcpu, v.Resources.CPUCores)
		}
	}
	return nil
}
