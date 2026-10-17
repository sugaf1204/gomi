package libvirt

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestWindowsDomainUEFITPMAndInboxDrivers(t *testing.T) {
	cfg := DomainConfig{Name: "windows", VCPU: 2, MemoryMB: 4096, DiskPath: "/windows.qcow2", DiskFormat: "qcow2", BootDev: "network", Firmware: "uefi", TPM: true, NICModel: "e1000e", DiskBus: "sata", Networks: []NetworkConfig{{Bridge: "br0"}}}
	xml, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`firmware="efi"`, `machine="q35"`, `model="tpm-crb"`, `type="emulator" version="2.0"`, `type="e1000e"`, `bus="sata"`} {
		if !strings.Contains(xml, fragment) {
			t.Fatalf("missing %s in %s", fragment, xml)
		}
	}
	changed, err := rewriteDomainBootDeviceXML(xml, "hd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(changed, `<boot dev='hd'/>`) || !strings.Contains(changed, `firmware="efi"`) {
		t.Fatal("boot rewrite lost firmware or failed")
	}
	cfg.Firmware = ""
	cfg.TPM = false
	cfg.NICModel = ""
	cfg.DiskBus = ""
	linux, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(linux, `<tpm`) || strings.Contains(linux, `firmware=`) || !strings.Contains(linux, `machine="pc"`) || !strings.Contains(linux, `type="virtio"`) {
		t.Fatal("Linux defaults changed")
	}
}

func TestWindowsNetworkBootIncludesDiskFallbackAndRewriteDeduplicates(t *testing.T) {
	cfg := DomainConfig{Name: "windows", VCPU: 4, MemoryMB: 8192, DiskPath: "/windows.qcow2", DiskFormat: "qcow2", BootDev: "network", BootFallback: "hd", Firmware: "uefi"}
	raw, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var domain xmlDomain
	if err := xml.Unmarshal([]byte(raw), &domain); err != nil {
		t.Fatal(err)
	}
	if len(domain.OS.Boot) != 2 || domain.OS.Boot[0].Dev != "network" || domain.OS.Boot[1].Dev != "hd" {
		t.Fatalf("disk fallback missing or misordered: %+v", domain.OS.Boot)
	}
	for _, device := range []string{"hd", "network"} {
		changed, err := rewriteDomainBootDeviceXML(raw, device)
		if err != nil {
			t.Fatal(err)
		}
		var updated xmlDomain
		if err := xml.Unmarshal([]byte(changed), &updated); err != nil {
			t.Fatal(err)
		}
		if len(updated.OS.Boot) != 1 || updated.OS.Boot[0].Dev != device || updated.OS.Firmware != "efi" {
			t.Fatalf("rewrite not deduplicated/preserved: %+v", updated.OS)
		}
	}
	cfg.BootFallback = ""
	raw, err = GenerateDomainXML(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := xml.Unmarshal([]byte(raw), &domain); err != nil {
		t.Fatal(err)
	}
	// Unmarshal into a fresh object: slices append when a value is reused.
	var linux xmlDomain
	if err := xml.Unmarshal([]byte(raw), &linux); err != nil {
		t.Fatal(err)
	}
	if len(linux.OS.Boot) != 1 || linux.OS.Boot[0].Dev != "network" {
		t.Fatal("single boot device default changed")
	}
}
