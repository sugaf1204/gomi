package libvirt

import (
	golibvirt "github.com/digitalocean/go-libvirt"
	"testing"
)

func TestDomainUndefineFlagsVersionedTPMCleanup(t *testing.T) {
	base := golibvirt.DomainUndefineManagedSave | golibvirt.DomainUndefineSnapshotsMetadata
	windows := `<domain><os><nvram>/state/vars.fd</nvram></os><devices><tpm model="tpm-crb"><backend type="emulator" version="2.0"/></tpm></devices></domain>`
	cases := []struct {
		name, xml string
		version   uint64
		want      golibvirt.DomainUndefineFlagsValues
		wantError bool
	}{
		{"8.0-default-cleanup", windows, 8_000_000, base | golibvirt.DomainUndefineNvram, false},
		{"8.8-default-cleanup", windows, 8_008_000, base | golibvirt.DomainUndefineNvram, false},
		{"8.9-explicit-cleanup", windows, 8_009_000, base | golibvirt.DomainUndefineNvram | golibvirt.DomainUndefineTpm, false},
		{"modern-explicit-cleanup", windows, 12_000_000, base | golibvirt.DomainUndefineNvram | golibvirt.DomainUndefineTpm, false},
		{"8.0-auto-efi-inactive-without-nvram", `<domain><os firmware='efi'><type arch='x86_64' machine='q35'>hvm</type><firmware><feature enabled='no' name='secure-boot'/></firmware><boot dev='hd'/></os><devices><tpm model='tpm-crb'><backend type='emulator' version='2.0'/></tpm></devices></domain>`, 8_000_000, base | golibvirt.DomainUndefineNvram, false},
		{"modern-auto-efi-without-nvram", `<domain><os firmware='efi'/><devices/></domain>`, 12_000_000, base | golibvirt.DomainUndefineNvram, false},
		{"explicit-bios-unchanged", `<domain><os firmware='bios'/><devices/></domain>`, 8_000_000, base, false},
		{"linux-unchanged", `<domain><os/><devices/></domain>`, 8_000_000, base, false},
		{"linux-modern-unchanged", `<domain><os/><devices/></domain>`, 12_000_000, base, false},
		{"passthrough-no-state", `<domain><devices><tpm><backend type="passthrough"/></tpm></devices></domain>`, 12_000_000, base, false},
		{"old-persistent-state-refused", `<domain><devices><tpm><backend type="emulator" persistent_state="yes"/></tpm></devices></domain>`, 8_000_000, 0, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domainUndefineFlags(tt.xml, tt.version)
			if (err != nil) != tt.wantError || got != tt.want {
				t.Fatalf("flags=%d error=%v want=%d error=%v", got, err, tt.want, tt.wantError)
			}
		})
	}
}
