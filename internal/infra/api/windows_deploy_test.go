package api

import (
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/vm"
	"testing"
)

func testWindowsAPIImage() osimage.OSImage {
	return osimage.OSImage{Name: "windows", OSFamily: "windows", OSVersion: "11", Arch: "amd64", Format: osimage.FormatWIM, Ready: true, Manifest: &osimage.Manifest{BootModes: []string{"uefi"}, Root: osimage.RootArtifact{Format: osimage.FormatWIM, Path: "install.wim"}, Windows: &osimage.WindowsImage{ImageIndex: 1, WinPE: osimage.WindowsPE{Wimboot: "wimboot", BCD: "BCD", BootSDI: "boot.sdi", BootWIM: "boot.wim"}}}}
}

func TestWindowsVMSelectionAndUnsupportedInputs(t *testing.T) {
	base := vm.VirtualMachine{Name: "win-test", Resources: vm.ResourceSpec{CPUCores: 2, MemoryMB: 4096, DiskGB: 64}}
	v := base
	if err := applyWindowsVMImage(&v, testWindowsAPIImage()); err != nil {
		t.Fatal(err)
	}
	if v.InstallCfg.Type != vm.InstallConfigWindows {
		t.Fatal("Windows routed into Linux")
	}
	cases := []struct {
		name   string
		change func(*vm.VirtualMachine)
	}{
		{"cloud-init", func(v *vm.VirtualMachine) { v.CloudInitRefs = []string{"linux"} }},
		{"ssh", func(v *vm.VirtualMachine) { v.SSHKeyRefs = []string{"key"} }},
		{"login", func(v *vm.VirtualMachine) { v.LoginUser = &vm.LoginUserSpec{Username: "user"} }},
		{"static", func(v *vm.VirtualMachine) { v.IPAssignment = vm.IPAssignmentStatic }},
		{"small", func(v *vm.VirtualMachine) { v.Resources.MemoryMB = 1024 }},
		{"virtio", func(v *vm.VirtualMachine) { v.AdvancedOptions = &vm.AdvancedOptions{DiskDriver: vm.DiskDriverVirtio} }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			v := base
			tt.change(&v)
			if applyWindowsVMImage(&v, testWindowsAPIImage()) == nil {
				t.Fatal("unsupported input accepted")
			}
		})
	}
	img := testWindowsAPIImage()
	img.Ready = false
	if applyWindowsVMImage(&v, img) == nil {
		t.Fatal("unready Windows accepted")
	}
}

func TestWindowsReinstallClearsPriorLinuxInputsBeforeApplyingRequest(t *testing.T) {
	v := vm.VirtualMachine{Name: "win-test", CloudInitRefs: []string{"linux"}, SSHKeyRefs: []string{"key"}, LoginUser: &vm.LoginUserSpec{Username: "linux"}, InstallCfg: &vm.InstallConfig{Type: vm.InstallConfigCurtin, Inline: "linux"}, IPAssignment: vm.IPAssignmentStatic, Network: []vm.NetworkInterface{{IPAddress: "192.0.2.1"}}, AdvancedOptions: &vm.AdvancedOptions{DiskDriver: vm.DiskDriverVirtio}}
	resetLinuxVMForWindows(&v)
	if v.LoginUser != nil || v.InstallCfg != nil || len(v.CloudInitRefs) != 0 || len(v.SSHKeyRefs) != 0 || v.IPAssignment != vm.IPAssignmentDHCP || v.Network[0].IPAddress != "" || v.AdvancedOptions.DiskDriver != "" {
		t.Fatalf("Linux config retained: %+v", v)
	}
	req := vmReinstallReq{LoginUser: &vm.LoginUserSpec{Username: "fresh-linux"}}
	applyReinstallSpec(&v, &req)
	if applyWindowsVMImage(&v, testWindowsAPIImage()) == nil {
		t.Fatal("explicit unsupported input discarded")
	}
}
