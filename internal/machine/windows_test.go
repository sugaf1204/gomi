package machine

import (
	"github.com/sugaf1204/gomi/internal/resource"
	"testing"
)

func TestWindowsTargetRejectsLinuxConfiguration(t *testing.T) {
	m := Machine{Name: "win-test", Arch: "amd64", Firmware: FirmwareUEFI, OSPreset: OSPreset{Family: OSTypeWindows}}
	if err := validateWindowsTarget(m); err != nil {
		t.Fatal(err)
	}
	if m.PXEInstallType() != resource.InstallWindows {
		t.Fatal("Windows boot selected Linux")
	}
	cases := []struct {
		name   string
		change func(*Machine)
	}{
		{"bios", func(m *Machine) { m.Firmware = FirmwareBIOS }},
		{"arm", func(m *Machine) { m.Arch = "arm64" }},
		{"hypervisor", func(m *Machine) { m.Role = RoleHypervisor }},
		{"linux-disk", func(m *Machine) { m.TargetDisk = "/dev/sda" }},
		{"cloud-init", func(m *Machine) { m.CloudInitRefs = []string{"seed"} }},
		{"static", func(m *Machine) { m.IPAssignment = IPAssignmentModeStatic }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			changed := m
			tt.change(&changed)
			if validateWindowsTarget(changed) == nil {
				t.Fatal("unsupported config accepted")
			}
		})
	}
}
