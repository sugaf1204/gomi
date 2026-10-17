package machine

import (
	"fmt"
	"github.com/sugaf1204/gomi/internal/resource"
)

func validateWindowsTarget(m Machine) error {
	if err := resource.ValidateWindowsHostname(m.NodeDisplayName()); err != nil {
		return err
	}
	if m.Firmware != FirmwareUEFI || CanonicalArch(m.Arch) != "amd64" {
		return fmt.Errorf("Windows deployment requires amd64 UEFI target")
	}
	if m.Role == RoleHypervisor {
		return fmt.Errorf("Windows hypervisor-role machines are unsupported")
	}
	if m.SealedBootstrap != nil {
		return fmt.Errorf("Windows sealed bootstrap is unsupported")
	}
	if m.TargetDisk != "" {
		return fmt.Errorf("Windows cannot use Linux targetDisk paths; exactly one fixed disk is required")
	}
	if len(m.OSPreset.InstallConfig) != 0 || m.CloudInitRef != "" || len(m.CloudInitRefs) != 0 || len(m.SSHKeyRefs) != 0 || m.LoginUser != nil {
		return fmt.Errorf("Windows deployment does not support cloud-init, SSH keys, loginUser, or Linux installConfig; finish account setup in Windows OOBE")
	}
	if m.IPAssignment == IPAssignmentModeStatic {
		return fmt.Errorf("Windows deployment currently requires DHCP")
	}
	return nil
}

func resetLinuxDeploymentForWindows(m *Machine) {
	m.CloudInitRef = ""
	m.CloudInitRefs = nil
	m.LastDeployedCloudInitRef = ""
	m.SSHKeyRefs = nil
	m.LoginUser = nil
	m.TargetDisk = ""
	m.IPAssignment = IPAssignmentModeDHCP
	m.IP = ""
}
