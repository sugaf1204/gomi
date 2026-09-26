package baremetal

import (
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/power"
	"testing"
)

func eligibleHost() machine.Machine {
	return machine.Machine{Arch: "amd64", Firmware: machine.FirmwareBIOS, Power: power.PowerConfig{Type: power.PowerTypeWebhook, Webhook: &power.WebhookConfig{PowerOnURL: "http://power.test/on", PowerOffURL: "http://power.test/off"}}}
}
func TestHostEligibilityBeforeEnrollment(t *testing.T) {
	for _, name := range []string{"manual", "hypervisor", "missing-power", "arm64", "unknown-firmware"} {
		t.Run(name, func(t *testing.T) {
			m := eligibleHost()
			switch name {
			case "manual":
				m.Power = power.PowerConfig{Type: power.PowerTypeManual}
			case "hypervisor":
				m.Role = machine.RoleHypervisor
			case "missing-power":
				m.Power = power.PowerConfig{}
			case "arm64":
				m.Arch = "arm64"
			case "unknown-firmware":
				m.Firmware = ""
			}
			if ValidateHost(m) == nil {
				t.Fatal("accepted ineligible host")
			}
		})
	}
}
func TestImageEligibilityPreservesFutureCleanup(t *testing.T) {
	for _, family := range []string{"ubuntu", "debian", "fedora"} {
		for _, filesystem := range []string{"", "ext4", "xfs", "btrfs"} {
			t.Run(family+"/"+filesystem, func(t *testing.T) {
				img := osimage.OSImage{Arch: "amd64", OSFamily: family, Format: osimage.FormatSquashFS, Ready: true, Manifest: &osimage.Manifest{Root: osimage.RootArtifact{Path: "rootfs.squashfs", Format: osimage.FormatSquashFS, RootPartition: osimage.Partition{Filesystem: filesystem}}}}
				err := ValidateImage(eligibleHost(), img)
				if (err != nil) != (filesystem == "btrfs") {
					t.Fatalf("filesystem %s: %v", filesystem, err)
				}
				for _, arch := range []string{"", "arm64"} {
					img.Arch = arch
					if ValidateImage(eligibleHost(), img) == nil {
						t.Fatalf("accepted architecture %q", arch)
					}
				}
			})
		}
	}
}
