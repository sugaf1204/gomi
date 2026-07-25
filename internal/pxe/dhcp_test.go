package pxe

import (
	"net"
	"testing"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/iana"

	"github.com/sugaf1204/gomi/internal/subnet"
)

func TestSelectBootFile(t *testing.T) {
	boot := BootConfig{
		BIOSBootFile:      "undionly.kpxe",
		UEFIBootFile:      "ipxe.efi",
		UEFILocalBootFile: "ipxe.efi",
		IPXEScript:        "http://192.168.2.254:8080/pxe/boot.ipxe",
	}

	biosReq, err := dhcpv4.New()
	if err != nil {
		t.Fatalf("dhcpv4.New biosReq: %v", err)
	}
	biosReq.UpdateOption(dhcpv4.OptClassIdentifier("PXEClient:Arch:00000:UNDI:002001"))
	biosReq.UpdateOption(dhcpv4.OptClientArch(iana.INTEL_X86PC))
	if got := selectBootFile(biosReq, clientArch(biosReq), boot, false); got != "undionly.kpxe" {
		t.Fatalf("bios bootfile mismatch: got %q", got)
	}

	uefiReq, err := dhcpv4.New()
	if err != nil {
		t.Fatalf("dhcpv4.New uefiReq: %v", err)
	}
	uefiReq.UpdateOption(dhcpv4.OptClassIdentifier("PXEClient:Arch:00007:UNDI:003000"))
	uefiReq.UpdateOption(dhcpv4.OptClientArch(iana.EFI_X86_64))
	if got := selectBootFile(uefiReq, clientArch(uefiReq), boot, false); got != "ipxe.efi" {
		t.Fatalf("uefi bootfile mismatch: got %q", got)
	}
	if got := selectBootFile(uefiReq, clientArch(uefiReq), boot, true); got != "ipxe.efi" {
		t.Fatalf("uefi local bootfile mismatch: got %q", got)
	}

	ipxeReq, err := dhcpv4.New()
	if err != nil {
		t.Fatalf("dhcpv4.New ipxeReq: %v", err)
	}
	ipxeReq.UpdateOption(dhcpv4.OptClassIdentifier("iPXE"))
	ipxeReq.UpdateOption(dhcpv4.OptClientArch(iana.EFI_X86_64))
	ipxeReq.ClientHWAddr = net.HardwareAddr{0x52, 0x54, 0x00, 0xaa, 0xbb, 0xcc}
	wantIPXEScript := "http://192.168.2.254:8080/pxe/boot.ipxe?mac=52%3A54%3A00%3Aaa%3Abb%3Acc"
	if got := selectBootFile(ipxeReq, clientArch(ipxeReq), boot, true); got != wantIPXEScript {
		t.Fatalf("ipxe bootfile mismatch: got %q", got)
	}
}

func TestIsPXEClient(t *testing.T) {
	req, err := dhcpv4.New()
	if err != nil {
		t.Fatalf("dhcpv4.New: %v", err)
	}
	req.UpdateOption(dhcpv4.OptClassIdentifier("iPXE"))
	if !isPXEClient(req) {
		t.Fatal("expected iPXE client to be treated as PXE")
	}
	if !isIPXEClient(req) {
		t.Fatal("expected iPXE client detection to be true")
	}

	req2, err := dhcpv4.New()
	if err != nil {
		t.Fatalf("dhcpv4.New req2: %v", err)
	}
	req2.UpdateOption(dhcpv4.OptClassIdentifier("PXEClient:Arch:00000:UNDI:003016"))
	req2.UpdateOption(dhcpv4.OptUserClass("iPXE"))
	if !isIPXEClient(req2) {
		t.Fatal("expected iPXE detection via user-class to be true")
	}
}

func TestNormalizeBootConfig(t *testing.T) {
	got := normalizeBootConfig(BootConfig{})
	if got.BIOSBootFile != "undionly.kpxe" {
		t.Fatalf("unexpected default BIOS bootfile: %q", got.BIOSBootFile)
	}
	if got.UEFIBootFile != "ipxe.efi" {
		t.Fatalf("unexpected default UEFI bootfile: %q", got.UEFIBootFile)
	}
	if got.UEFILocalBootFile != "ipxe.efi" {
		t.Fatalf("unexpected default UEFI local bootfile: %q", got.UEFILocalBootFile)
	}
}

func TestHandleFullOmitsBootOptionsForUnregisteredPXEClient(t *testing.T) {
	server, req, spec, boot, pool := newDHCPTestRequest(t)

	resp, err := server.handleFull(req, spec, boot, false, false, pool)
	if err != nil {
		t.Fatalf("handleFull: %v", err)
	}
	if resp.YourIPAddr == nil || resp.YourIPAddr.IsUnspecified() {
		t.Fatal("unregistered client must still receive an IP address")
	}
	if got := resp.Options.Get(dhcpv4.OptionTFTPServerName); len(got) != 0 {
		t.Fatalf("unregistered client received option 66: %q", got)
	}
	if got := resp.Options.Get(dhcpv4.OptionBootfileName); len(got) != 0 {
		t.Fatalf("unregistered client received option 67: %q", got)
	}
	if resp.BootFileName != "" {
		t.Fatalf("unregistered client received boot filename field: %q", resp.BootFileName)
	}
}

func TestHandleFullIncludesBootOptionsForRegisteredPXEClient(t *testing.T) {
	server, req, spec, boot, pool := newDHCPTestRequest(t)

	resp, err := server.handleFull(req, spec, boot, true, false, pool)
	if err != nil {
		t.Fatalf("handleFull: %v", err)
	}
	if got := string(resp.Options.Get(dhcpv4.OptionTFTPServerName)); got != "192.0.2.1" {
		t.Fatalf("option 66 mismatch: %q", got)
	}
	if got := string(resp.Options.Get(dhcpv4.OptionBootfileName)); got != boot.BIOSBootFile {
		t.Fatalf("option 67 mismatch: %q", got)
	}
}

func TestHandleProxyIgnoresUnregisteredPXEClient(t *testing.T) {
	server, req, _, boot, _ := newDHCPTestRequest(t)

	resp, err := server.handleProxy(req, boot, false, false)
	if err != nil {
		t.Fatalf("handleProxy: %v", err)
	}
	if resp != nil {
		t.Fatal("proxy mode must not answer an unregistered PXE client")
	}
}

func TestHandleProxyIncludesBootOptionsForRegisteredPXEClient(t *testing.T) {
	server, req, _, boot, _ := newDHCPTestRequest(t)

	resp, err := server.handleProxy(req, boot, true, false)
	if err != nil {
		t.Fatalf("handleProxy: %v", err)
	}
	if resp == nil {
		t.Fatal("proxy mode must answer a registered PXE client")
	}
	if got := string(resp.Options.Get(dhcpv4.OptionTFTPServerName)); got != "192.0.2.1" {
		t.Fatalf("option 66 mismatch: %q", got)
	}
	if got := string(resp.Options.Get(dhcpv4.OptionBootfileName)); got != boot.BIOSBootFile {
		t.Fatalf("option 67 mismatch: %q", got)
	}
}

func newDHCPTestRequest(t *testing.T) (*Server, *dhcpv4.DHCPv4, subnet.SubnetSpec, BootConfig, *leasePool) {
	t.Helper()
	spec := subnet.SubnetSpec{
		CIDR: "192.0.2.0/24",
		PXEAddressRange: &subnet.AddressRange{
			Start: "192.0.2.100",
			End:   "192.0.2.110",
		},
	}
	boot := normalizeBootConfig(BootConfig{})
	server := NewServer("full", "test0", net.ParseIP("192.0.2.1"), spec, boot, nil)
	req, err := dhcpv4.New(
		dhcpv4.WithMessageType(dhcpv4.MessageTypeDiscover),
		dhcpv4.WithHwAddr(net.HardwareAddr{0x52, 0x54, 0x00, 0xaa, 0xbb, 0xcc}),
		dhcpv4.WithOption(dhcpv4.OptClassIdentifier("PXEClient:Arch:00000:UNDI:002001")),
	)
	if err != nil {
		t.Fatalf("dhcpv4.New: %v", err)
	}
	return server, req, spec, boot, server.leases
}
