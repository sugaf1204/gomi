package libvirt

import (
	"encoding/xml"
	"fmt"
	golibvirt "github.com/digitalocean/go-libvirt"
)

const libvirtVersionTPMUndefine uint64 = 8_009_000

// Before 8.9, undefining an inactive guest removes nonpersistent emulator TPM
// state by default. Since 8.9 an explicit flag requests state removal.
// libvirt v8.0.0: qemuDomainRemoveInactiveCommon -> qemuExtDevicesCleanupHost ->
// qemuExtTPMCleanupHost removes emulator storage unless persistent_state=yes.
func domainUndefineFlags(raw string, version uint64) (golibvirt.DomainUndefineFlagsValues, error) {
	var domain struct {
		OS struct {
			NVRAM    *struct{} `xml:"nvram"`
			Firmware string    `xml:"firmware,attr"`
		} `xml:"os"`
		Devices struct {
			TPMs []struct {
				Backend struct {
					Type            string `xml:"type,attr"`
					PersistentState string `xml:"persistent_state,attr"`
				} `xml:"backend"`
			} `xml:"tpm"`
		} `xml:"devices"`
	}
	if err := xml.Unmarshal([]byte(raw), &domain); err != nil {
		return 0, fmt.Errorf("parse domain cleanup configuration: %w", err)
	}
	flags := golibvirt.DomainUndefineManagedSave | golibvirt.DomainUndefineSnapshotsMetadata
	// Auto-selected EFI domains can omit loader/nvram from inactive XML even
	// after a first boot created their variable store. The typed firmware
	// selection is sufficient to require cleanup; BIOS domains stay unchanged.
	if domain.OS.NVRAM != nil || domain.OS.Firmware == "efi" {
		flags |= golibvirt.DomainUndefineNvram
	}
	for _, tpm := range domain.Devices.TPMs {
		if tpm.Backend.Type != "emulator" {
			continue
		}
		if version >= libvirtVersionTPMUndefine {
			flags |= golibvirt.DomainUndefineTpm
			continue
		}
		if tpm.Backend.PersistentState == "yes" {
			return 0, fmt.Errorf("removing persistent emulator TPM state requires libvirt 8.9 or newer")
		}
	}
	return flags, nil
}
