package baremetal

import (
	"fmt"
	"strings"

	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/power"
)

// ValidateHost checks prerequisites before enrollment locks ordinary mutations.
// The current physical installer has x86 BIOS/EFI bootloader support only.
func ValidateHost(m machine.Machine) error {
	if m.Arch != "amd64" {
		return fmt.Errorf("sealed bare-metal deployment currently requires an amd64 host")
	}
	if m.Firmware != machine.FirmwareBIOS && m.Firmware != machine.FirmwareUEFI {
		return fmt.Errorf("sealed bare-metal deployment requires BIOS or UEFI firmware metadata")
	}
	if m.Role == machine.RoleHypervisor || m.Power.Type == power.PowerTypeManual {
		return fmt.Errorf("automated bare-metal deployment requires a dedicated host with power control")
	}
	return power.ValidatePowerConfig(m.Power)
}

// ValidateImage ensures both this installation and a later cleanup can preserve
// the enrolled identity. Ordinary deployment formats remain unchanged.
func ValidateImage(m machine.Machine, img osimage.OSImage) error {
	if err := ValidateHost(m); err != nil {
		return err
	}
	if img.Arch == "" || img.Arch != string(m.Arch) || (img.Manifest != nil && img.Manifest.Arch != "" && img.Manifest.Arch != img.Arch) {
		return fmt.Errorf("bare-metal image architecture must match the enrolled host")
	}
	if !img.Ready || osimage.EffectiveImageFormat(img) != osimage.FormatSquashFS || !osimage.SupportsDeploymentTarget(img, osimage.DeploymentTargetBareMetal) {
		return fmt.Errorf("a ready bare-metal SquashFS image is required")
	}
	switch img.OSFamily {
	case "ubuntu", "debian", "fedora":
	default:
		return fmt.Errorf("unsupported bare-metal OS family")
	}
	filesystem := ""
	if img.Manifest != nil {
		filesystem = strings.ToLower(strings.TrimSpace(img.Manifest.Root.RootPartition.Filesystem))
	}
	switch filesystem {
	case "", "ext4", "xfs":
		return nil
	default:
		return fmt.Errorf("sealed bare-metal deployment requires an ext4 or XFS target root filesystem")
	}
}
