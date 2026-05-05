package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sugaf1204/gomi/internal/subnet"
)

func TestUEFILocalBootGRUBConfigFallsBackToFirmwareBootOrder(t *testing.T) {
	if got, want := uefiLocalBootGRUBConfig, "exit 1\n"; got != want {
		t.Fatalf("UEFI local boot GRUB config = %q, want %q", got, want)
	}
}

func TestEnsureUEFILocalBootGRUBAssetsInstallsPackagedAsset(t *testing.T) {
	tftpRoot := t.TempDir()
	srcDir := t.TempDir()
	src := filepath.Join(srcDir, "grubnetx64.efi.signed")
	want := []byte("fake-grubnet")
	if err := os.WriteFile(src, want, 0o644); err != nil {
		t.Fatal(err)
	}

	oldCandidates := uefiLocalBootGRUBCandidates
	uefiLocalBootGRUBCandidates = []string{filepath.Join(srcDir, "missing"), src}
	t.Cleanup(func() { uefiLocalBootGRUBCandidates = oldCandidates })

	if err := ensureUEFILocalBootGRUBAssets(tftpRoot); err != nil {
		t.Fatalf("ensureUEFILocalBootGRUBAssets: %v", err)
	}
	gotGRUB, err := os.ReadFile(filepath.Join(tftpRoot, "grubnetx64.efi"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotGRUB) != string(want) {
		t.Fatalf("installed GRUB asset = %q, want %q", gotGRUB, want)
	}
	gotConfig, err := os.ReadFile(filepath.Join(tftpRoot, "grub", "grub.cfg"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotConfig) != uefiLocalBootGRUBConfig {
		t.Fatalf("grub.cfg = %q, want %q", gotConfig, uefiLocalBootGRUBConfig)
	}
}

func TestUEFILocalBootGRUBCandidatesPreferMonolithicImage(t *testing.T) {
	if len(uefiLocalBootGRUBCandidates) == 0 {
		t.Fatal("expected at least one UEFI local boot GRUB candidate")
	}
	if got, want := uefiLocalBootGRUBCandidates[0], "/usr/lib/grub/x86_64-efi/monolithic/grubnetx64.efi"; got != want {
		t.Fatalf("first UEFI local boot GRUB candidate = %q, want %q", got, want)
	}
}

func TestEnsureUEFILocalBootGRUBAssetsFailsWithoutPackagedAsset(t *testing.T) {
	oldCandidates := uefiLocalBootGRUBCandidates
	uefiLocalBootGRUBCandidates = []string{filepath.Join(t.TempDir(), "missing")}
	t.Cleanup(func() { uefiLocalBootGRUBCandidates = oldCandidates })

	if err := ensureUEFILocalBootGRUBAssets(t.TempDir()); err == nil {
		t.Fatal("expected missing packaged GRUB asset to fail")
	}
}

func TestEnsureIPXEBootAssetsInstallsPackagedAssets(t *testing.T) {
	tftpRoot := t.TempDir()
	srcDir := t.TempDir()
	efi := filepath.Join(srcDir, "ipxe.efi")
	bios := filepath.Join(srcDir, "undionly.kpxe")
	if err := os.WriteFile(efi, []byte("fake-efi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bios, []byte("fake-bios"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldAssets := ipxeBootAssets
	ipxeBootAssets = []tftpBootAsset{
		{dst: "ipxe.efi", candidates: []string{efi}, hint: "install ipxe"},
		{dst: "undionly.kpxe", candidates: []string{bios}, hint: "install ipxe"},
	}
	t.Cleanup(func() { ipxeBootAssets = oldAssets })

	if err := ensureIPXEBootAssets(tftpRoot); err != nil {
		t.Fatalf("ensureIPXEBootAssets: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(tftpRoot, "ipxe.efi")); err != nil || string(got) != "fake-efi" {
		t.Fatalf("ipxe.efi = %q, err=%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(tftpRoot, "undionly.kpxe")); err != nil || string(got) != "fake-bios" {
		t.Fatalf("undionly.kpxe = %q, err=%v", got, err)
	}
}

func TestEnsureIPXEBootAssetsFailsWithoutPackagedAsset(t *testing.T) {
	oldAssets := ipxeBootAssets
	ipxeBootAssets = []tftpBootAsset{{dst: "ipxe.efi", candidates: []string{filepath.Join(t.TempDir(), "missing")}, hint: "install ipxe"}}
	t.Cleanup(func() { ipxeBootAssets = oldAssets })

	if err := ensureIPXEBootAssets(t.TempDir()); err == nil {
		t.Fatal("expected missing packaged iPXE asset to fail")
	}
}

func TestPXESubnetReadyRequiresAddressRangeInFullMode(t *testing.T) {
	spec := subnet.SubnetSpec{CIDR: "192.168.2.0/24"}
	if pxeSubnetReady("full", spec) {
		t.Fatal("full DHCP mode must not start without a PXE address range")
	}

	spec.PXEAddressRange = &subnet.AddressRange{Start: "192.168.2.100", End: "192.168.2.200"}
	if !pxeSubnetReady("full", spec) {
		t.Fatal("full DHCP mode should start once a PXE address range is configured")
	}
}

func TestPXESubnetReadyAllowsProxyModeWithoutAddressRange(t *testing.T) {
	spec := subnet.SubnetSpec{CIDR: "192.168.2.0/24"}
	if !pxeSubnetReady("proxy", spec) {
		t.Fatal("proxy DHCP mode should not require a lease address range")
	}
}
