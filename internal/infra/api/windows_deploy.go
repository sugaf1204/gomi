package api

import (
	"fmt"
	"strings"

	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
)

func applyWindowsVMImage(v *vm.VirtualMachine, img osimage.OSImage) error {
	if err := resource.ValidateWindowsHostname(v.Name); err != nil {
		return err
	}
	if err := osimage.ValidateOSImage(img); err != nil {
		return err
	}
	if !img.Ready {
		return fmt.Errorf("Windows OS image assets are not ready")
	}
	if !osimage.SupportsDeploymentTarget(img, osimage.DeploymentTargetVM) {
		return fmt.Errorf("Windows image does not support VM deployment")
	}
	inline := ""
	if v.InstallCfg != nil {
		inline = v.InstallCfg.Inline
	}
	if err := validateWindowsInputs(v.CloudInitRef, v.CloudInitRefs, v.SSHKeyRefs, v.LoginUser != nil, v.IPAssignment, inline); err != nil {
		return err
	}
	if v.Resources.CPUCores < 2 || v.Resources.MemoryMB < 4096 || v.Resources.DiskGB < 64 {
		return fmt.Errorf("Windows VMs require at least 2 CPUs, 4096 MiB memory, and 64 GiB disk")
	}
	if v.AdvancedOptions != nil && (v.AdvancedOptions.DiskDriver != "" || v.AdvancedOptions.IOThreads != 0 || v.AdvancedOptions.NetMultiqueue != 0) {
		return fmt.Errorf("Windows VMs use SATA and e1000e; custom disk drivers, IO threads, and network multiqueue are unsupported")
	}
	v.InstallCfg = &vm.InstallConfig{Type: vm.InstallConfigWindows}
	return nil
}

func validateWindowsMachineImage(m machine.Machine, img osimage.OSImage) error {
	if err := osimage.ValidateOSImage(img); err != nil {
		return err
	}
	if !img.Ready {
		return fmt.Errorf("Windows OS image assets are not ready")
	}
	return nil
}

func validateWindowsInputs(legacy string, refs, keys []string, login bool, assignment resource.IPAssignmentMode, inline string) error {
	if strings.TrimSpace(legacy) != "" || len(refs) != 0 || len(keys) != 0 || login || strings.TrimSpace(inline) != "" {
		return fmt.Errorf("Windows deployment does not support cloud-init, SSH keys, loginUser, or inline Linux config; finish account setup in Windows OOBE")
	}
	if assignment == resource.IPAssignmentStatic {
		return fmt.Errorf("Windows deployment currently requires DHCP; configure static addresses after deployment")
	}
	return nil
}

func resetLinuxVMForWindows(v *vm.VirtualMachine) {
	v.CloudInitRef = ""
	v.CloudInitRefs = nil
	v.LastDeployedCloudInitRef = ""
	v.SSHKeyRefs = nil
	v.LoginUser = nil
	v.InstallCfg = nil
	v.IPAssignment = vm.IPAssignmentDHCP
	v.Network = append([]vm.NetworkInterface(nil), v.Network...)
	for i := range v.Network {
		v.Network[i].IPAddress = ""
	}
	if v.AdvancedOptions != nil {
		opts := *v.AdvancedOptions
		opts.DiskDriver = ""
		opts.IOThreads = 0
		opts.NetMultiqueue = 0
		v.AdvancedOptions = &opts
	}
}
