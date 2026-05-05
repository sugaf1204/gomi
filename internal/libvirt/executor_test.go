package libvirt

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestDomainConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     DomainConfig
		wantErr string
	}{
		{
			name:    "empty name",
			cfg:     DomainConfig{},
			wantErr: "domain name is required",
		},
		{
			name:    "zero vcpu",
			cfg:     DomainConfig{Name: "test-vm"},
			wantErr: "vcpu must be positive",
		},
		{
			name:    "zero memory",
			cfg:     DomainConfig{Name: "test-vm", VCPU: 2},
			wantErr: "memoryMB must be positive",
		},
		{
			name:    "empty disk path",
			cfg:     DomainConfig{Name: "test-vm", VCPU: 2, MemoryMB: 2048},
			wantErr: "disk path is required",
		},
		{
			name:    "empty disk format",
			cfg:     DomainConfig{Name: "test-vm", VCPU: 2, MemoryMB: 2048, DiskPath: "/var/lib/libvirt/images/test.qcow2"},
			wantErr: "disk format is required",
		},
		{
			name:    "invalid disk format",
			cfg:     DomainConfig{Name: "test-vm", VCPU: 2, MemoryMB: 2048, DiskPath: "/var/lib/libvirt/images/test.qcow2", DiskFormat: "vmdk"},
			wantErr: "unsupported disk format: vmdk",
		},
		{
			name: "valid config",
			cfg: DomainConfig{
				Name:       "test-vm",
				VCPU:       2,
				MemoryMB:   2048,
				DiskPath:   "/var/lib/libvirt/images/test.qcow2",
				DiskFormat: "qcow2",
			},
			wantErr: "",
		},
		{
			name: "valid config with raw format",
			cfg: DomainConfig{
				Name:       "test-vm",
				VCPU:       4,
				MemoryMB:   4096,
				DiskPath:   "/var/lib/libvirt/images/test.raw",
				DiskFormat: "raw",
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("expected error containing %q, got nil", tt.wantErr)
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLibvirtConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     LibvirtConfig
		wantErr string
	}{
		{
			name:    "empty host",
			cfg:     LibvirtConfig{},
			wantErr: "libvirt host is required",
		},
		{
			name:    "valid config with default port",
			cfg:     LibvirtConfig{Host: "192.168.1.100"},
			wantErr: "",
		},
		{
			name:    "valid config with explicit port",
			cfg:     LibvirtConfig{Host: "192.168.1.100", Port: 16509},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("expected error containing %q, got nil", tt.wantErr)
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestParseDomainState(t *testing.T) {
	tests := []struct {
		input string
		want  DomainState
	}{
		{"running", StateRunning},
		{"shut off", StateShutoff},
		{"shutoff", StateShutoff},
		{"paused", StatePaused},
		{"crashed", StateCrashed},
		{"unknown", StateUnknown},
		{"something-else", StateUnknown},
		{"", StateUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseDomainState(tt.input)
			if got != tt.want {
				t.Errorf("ParseDomainState(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMapDomainState(t *testing.T) {
	// Verify that mapDomainState covers expected libvirt state constants.
	tests := []struct {
		name  string
		input uint8
		want  DomainState
	}{
		{"running", 1, StateRunning},
		{"shutoff", 5, StateShutoff},
		{"paused", 3, StatePaused},
		{"crashed", 6, StateCrashed},
		{"unknown", 255, StateUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapDomainState(tt.input)
			if got != tt.want {
				t.Errorf("mapDomainState(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGenerateDomainXML_BasicDomain(t *testing.T) {
	cfg := DomainConfig{
		Name:       "test-vm",
		VCPU:       2,
		MemoryMB:   2048,
		DiskPath:   "/var/lib/libvirt/images/test.qcow2",
		DiskFormat: "qcow2",
		Networks: []NetworkConfig{
			{Bridge: "br0", MAC: "52:54:00:aa:bb:cc"},
		},
	}

	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	// Verify it's valid XML.
	var domain xmlDomain
	if err := xml.Unmarshal([]byte(xmlStr), &domain); err != nil {
		t.Fatalf("generated XML is not valid: %v\nXML:\n%s", err, xmlStr)
	}

	// Verify key fields.
	if domain.Type != "kvm" {
		t.Errorf("domain type = %q, want %q", domain.Type, "kvm")
	}
	if domain.Name != "test-vm" {
		t.Errorf("domain name = %q, want %q", domain.Name, "test-vm")
	}
	if domain.Memory.Value != 2048 {
		t.Errorf("memory = %d, want %d", domain.Memory.Value, 2048)
	}
	if domain.Memory.Unit != "MiB" {
		t.Errorf("memory unit = %q, want %q", domain.Memory.Unit, "MiB")
	}
	if domain.VCPU != 2 {
		t.Errorf("vcpu = %d, want %d", domain.VCPU, 2)
	}

	// Verify OS.
	if domain.OS.Type.Value != "hvm" {
		t.Errorf("os type = %q, want %q", domain.OS.Type.Value, "hvm")
	}
	if domain.OS.Boot.Dev != "hd" {
		t.Errorf("boot dev = %q, want %q", domain.OS.Boot.Dev, "hd")
	}

	// Verify disk.
	if len(domain.Devices.Disks) != 1 {
		t.Fatalf("expected 1 disk, got %d", len(domain.Devices.Disks))
	}
	disk := domain.Devices.Disks[0]
	if disk.Source.File != "/var/lib/libvirt/images/test.qcow2" {
		t.Errorf("disk source = %q, want %q", disk.Source.File, "/var/lib/libvirt/images/test.qcow2")
	}
	if disk.Driver.Type != "qcow2" {
		t.Errorf("disk driver type = %q, want %q", disk.Driver.Type, "qcow2")
	}

	// Verify network interface.
	if len(domain.Devices.Interfaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(domain.Devices.Interfaces))
	}
	iface := domain.Devices.Interfaces[0]
	if iface.Source.Bridge != "br0" {
		t.Errorf("bridge = %q, want %q", iface.Source.Bridge, "br0")
	}
	if iface.MAC == nil || iface.MAC.Address != "52:54:00:aa:bb:cc" {
		t.Errorf("mac = %v, want %q", iface.MAC, "52:54:00:aa:bb:cc")
	}
}

func TestGenerateDomainXML_WithCloudInit(t *testing.T) {
	cfg := DomainConfig{
		Name:       "ci-vm",
		VCPU:       1,
		MemoryMB:   1024,
		DiskPath:   "/var/lib/libvirt/images/ci-vm.qcow2",
		DiskFormat: "qcow2",
		CloudInit:  "/var/lib/libvirt/images/ci-vm-cidata.iso",
	}

	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	var domain xmlDomain
	if err := xml.Unmarshal([]byte(xmlStr), &domain); err != nil {
		t.Fatalf("generated XML is not valid: %v", err)
	}

	// Should have 2 disks: OS image + cloud-init ISO.
	if len(domain.Devices.Disks) != 2 {
		t.Fatalf("expected 2 disks, got %d", len(domain.Devices.Disks))
	}

	ciDisk := domain.Devices.Disks[1]
	if ciDisk.Device != "cdrom" {
		t.Errorf("cloud-init disk device = %q, want %q", ciDisk.Device, "cdrom")
	}
	if ciDisk.Source.File != "/var/lib/libvirt/images/ci-vm-cidata.iso" {
		t.Errorf("cloud-init source = %q, want %q", ciDisk.Source.File, "/var/lib/libvirt/images/ci-vm-cidata.iso")
	}
}

func TestGenerateDomainXML_MultipleNetworks(t *testing.T) {
	cfg := DomainConfig{
		Name:       "multi-net-vm",
		VCPU:       4,
		MemoryMB:   8192,
		DiskPath:   "/var/lib/libvirt/images/multi.raw",
		DiskFormat: "raw",
		Networks: []NetworkConfig{
			{Bridge: "br0", MAC: "52:54:00:11:22:33"},
			{Bridge: "br1"},
		},
	}

	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	var domain xmlDomain
	if err := xml.Unmarshal([]byte(xmlStr), &domain); err != nil {
		t.Fatalf("generated XML is not valid: %v", err)
	}

	if len(domain.Devices.Interfaces) != 2 {
		t.Fatalf("expected 2 interfaces, got %d", len(domain.Devices.Interfaces))
	}

	// First interface has MAC.
	if domain.Devices.Interfaces[0].MAC == nil {
		t.Error("first interface should have MAC")
	}
	// Second interface has no MAC (auto-assigned by libvirt).
	if domain.Devices.Interfaces[1].MAC != nil {
		t.Error("second interface should not have MAC")
	}
}

func TestGenerateDomainXML_InvalidConfig(t *testing.T) {
	cfg := DomainConfig{} // Empty config should fail validation.
	_, err := GenerateDomainXML(cfg)
	if err == nil {
		t.Error("expected error for invalid config, got nil")
	}
}

func TestRewriteDomainBootDeviceXML(t *testing.T) {
	cfg := DomainConfig{
		Name:       "bootdev-vm",
		VCPU:       2,
		MemoryMB:   2048,
		DiskPath:   "/var/lib/libvirt/images/bootdev-vm.qcow2",
		DiskFormat: "qcow2",
	}
	raw, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("GenerateDomainXML: %v", err)
	}
	if !strings.Contains(raw, "<boot dev=\"hd\"></boot>") && !strings.Contains(raw, "<boot dev='hd'/>") {
		// Encoding can render the boot element differently depending on xml encoder behavior.
		// We only require that a boot device entry exists before rewrite.
		if !strings.Contains(raw, "dev=\"hd\"") {
			t.Fatalf("expected initial boot device hd, got: %s", raw)
		}
	}

	updated, err := rewriteDomainBootDeviceXML(raw, "network")
	if err != nil {
		t.Fatalf("rewriteDomainBootDeviceXML: %v", err)
	}
	if !strings.Contains(updated, "boot dev='network'") && !strings.Contains(updated, "boot dev=\"network\"") {
		t.Fatalf("expected network boot tag, got: %s", updated)
	}

	if _, err := rewriteDomainBootDeviceXML(raw, "invalid-bootdev"); err == nil {
		t.Fatal("expected unsupported boot device error")
	}
}

func TestGenerateDomainXML_RawDiskFormat(t *testing.T) {
	cfg := DomainConfig{
		Name:       "raw-disk-vm",
		VCPU:       2,
		MemoryMB:   2048,
		DiskPath:   "/var/lib/libvirt/images/test.raw",
		DiskFormat: "raw",
		Networks: []NetworkConfig{
			{Bridge: "br0", MAC: "52:54:00:aa:bb:dd"},
		},
	}

	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	var domain xmlDomain
	if err := xml.Unmarshal([]byte(xmlStr), &domain); err != nil {
		t.Fatalf("generated XML is not valid: %v\nXML:\n%s", err, xmlStr)
	}

	if len(domain.Devices.Disks) != 1 {
		t.Fatalf("expected 1 disk, got %d", len(domain.Devices.Disks))
	}
	if domain.Devices.Disks[0].Driver.Type != "raw" {
		t.Errorf("disk driver type = %q, want %q", domain.Devices.Disks[0].Driver.Type, "raw")
	}
	if domain.Devices.Disks[0].Source.File != "/var/lib/libvirt/images/test.raw" {
		t.Errorf("disk source = %q, want raw disk path", domain.Devices.Disks[0].Source.File)
	}
}

func TestGenerateDomainXML_NoNetworks(t *testing.T) {
	cfg := DomainConfig{
		Name:       "no-net-vm",
		VCPU:       1,
		MemoryMB:   512,
		DiskPath:   "/var/lib/libvirt/images/test.qcow2",
		DiskFormat: "qcow2",
		// No Networks specified.
	}

	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	var domain xmlDomain
	if err := xml.Unmarshal([]byte(xmlStr), &domain); err != nil {
		t.Fatalf("generated XML is not valid: %v\nXML:\n%s", err, xmlStr)
	}

	if len(domain.Devices.Interfaces) != 0 {
		t.Fatalf("expected 0 interfaces, got %d", len(domain.Devices.Interfaces))
	}

	// Verify other fields are still present.
	if domain.Name != "no-net-vm" {
		t.Errorf("domain name = %q, want %q", domain.Name, "no-net-vm")
	}
	if domain.VCPU != 1 {
		t.Errorf("vcpu = %d, want 1", domain.VCPU)
	}
}

func TestGenerateDomainXML_ThreeNetworks(t *testing.T) {
	cfg := DomainConfig{
		Name:       "triple-net-vm",
		VCPU:       4,
		MemoryMB:   8192,
		DiskPath:   "/var/lib/libvirt/images/triple.qcow2",
		DiskFormat: "qcow2",
		Networks: []NetworkConfig{
			{Bridge: "br0", MAC: "52:54:00:11:11:11"},
			{Bridge: "br1", MAC: "52:54:00:22:22:22"},
			{Bridge: "br2", MAC: "52:54:00:33:33:33"},
		},
	}

	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	var domain xmlDomain
	if err := xml.Unmarshal([]byte(xmlStr), &domain); err != nil {
		t.Fatalf("generated XML is not valid: %v\nXML:\n%s", err, xmlStr)
	}

	if len(domain.Devices.Interfaces) != 3 {
		t.Fatalf("expected 3 interfaces, got %d", len(domain.Devices.Interfaces))
	}

	expectedBridges := []string{"br0", "br1", "br2"}
	expectedMACs := []string{"52:54:00:11:11:11", "52:54:00:22:22:22", "52:54:00:33:33:33"}
	for i, iface := range domain.Devices.Interfaces {
		if iface.Source.Bridge != expectedBridges[i] {
			t.Errorf("interface %d bridge = %q, want %q", i, iface.Source.Bridge, expectedBridges[i])
		}
		if iface.MAC == nil || iface.MAC.Address != expectedMACs[i] {
			t.Errorf("interface %d MAC mismatch", i)
		}
		if iface.Model.Type != "virtio" {
			t.Errorf("interface %d model = %q, want virtio", i, iface.Model.Type)
		}
	}
}

func TestGenerateDomainXML_ParseRoundTrip(t *testing.T) {
	cfg := DomainConfig{
		Name:       "roundtrip-vm",
		VCPU:       8,
		MemoryMB:   16384,
		DiskPath:   "/images/roundtrip.qcow2",
		DiskFormat: "qcow2",
		Networks: []NetworkConfig{
			{Bridge: "virbr0", MAC: "52:54:00:ff:ee:dd"},
		},
		CloudInit: "/images/roundtrip-cidata.iso",
	}

	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	// Parse the XML back.
	var domain xmlDomain
	if err := xml.Unmarshal([]byte(xmlStr), &domain); err != nil {
		t.Fatalf("round-trip parse failed: %v", err)
	}

	// Re-marshal and parse again to verify consistency.
	remarshaled, err := xml.MarshalIndent(domain, "", "  ")
	if err != nil {
		t.Fatalf("re-marshal failed: %v", err)
	}

	var domain2 xmlDomain
	if err := xml.Unmarshal(remarshaled, &domain2); err != nil {
		t.Fatalf("second parse failed: %v", err)
	}

	if domain2.Name != "roundtrip-vm" {
		t.Errorf("name after round-trip = %q, want %q", domain2.Name, "roundtrip-vm")
	}
	if domain2.VCPU != 8 {
		t.Errorf("vcpu after round-trip = %d, want 8", domain2.VCPU)
	}
	if domain2.Memory.Value != 16384 {
		t.Errorf("memory after round-trip = %d, want 16384", domain2.Memory.Value)
	}
	if len(domain2.Devices.Disks) != 2 {
		t.Errorf("disk count after round-trip = %d, want 2", len(domain2.Devices.Disks))
	}
	if len(domain2.Devices.Interfaces) != 1 {
		t.Errorf("interface count after round-trip = %d, want 1", len(domain2.Devices.Interfaces))
	}
}

func TestGenerateDomainXML_LargeMemory(t *testing.T) {
	cfg := DomainConfig{
		Name:       "big-vm",
		VCPU:       64,
		MemoryMB:   524288, // 512 GB
		DiskPath:   "/images/big.qcow2",
		DiskFormat: "qcow2",
	}

	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	var domain xmlDomain
	if err := xml.Unmarshal([]byte(xmlStr), &domain); err != nil {
		t.Fatalf("generated XML is not valid: %v", err)
	}

	if domain.Memory.Value != 524288 {
		t.Errorf("memory = %d, want 524288", domain.Memory.Value)
	}
	if domain.VCPU != 64 {
		t.Errorf("vcpu = %d, want 64", domain.VCPU)
	}
}

func TestGenerateDomainXML_ContainsExpectedElements(t *testing.T) {
	cfg := DomainConfig{
		Name:       "xml-check-vm",
		VCPU:       2,
		MemoryMB:   4096,
		DiskPath:   "/images/test.qcow2",
		DiskFormat: "qcow2",
		Networks: []NetworkConfig{
			{Bridge: "virbr0", MAC: "52:54:00:de:ad:01"},
		},
		CloudInit: "/images/cidata.iso",
	}

	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	// Check that raw XML string contains expected fragments.
	expectedFragments := []string{
		`type="kvm"`,
		`<name>xml-check-vm</name>`,
		`<vcpu>2</vcpu>`,
		`unit="MiB"`,
		`4096`,
		`>hvm</type>`,
		`dev="hd"`,
		`file="/images/test.qcow2"`,
		`type="qcow2"`,
		`dev="vda"`,
		`bus="virtio"`,
		`file="/images/cidata.iso"`,
		`device="cdrom"`,
		`bridge="virbr0"`,
		`address="52:54:00:de:ad:01"`,
		`type="pty"`,
		`type="vnc"`,
	}

	for _, frag := range expectedFragments {
		if !strings.Contains(xmlStr, frag) {
			t.Errorf("XML does not contain expected fragment %q\nXML:\n%s", frag, xmlStr)
		}
	}
}

func TestGenerateDomainXML_WithSMBIOSSerial(t *testing.T) {
	cfg := DomainConfig{
		Name:         "smbios-vm",
		VCPU:         2,
		MemoryMB:     2048,
		DiskPath:     "/images/test.qcow2",
		DiskFormat:   "qcow2",
		SMBIOSSerial: "ds=nocloud-net;s=http://192.168.2.254:8080/pxe/nocloud/525400000001/",
	}

	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("GenerateDomainXML failed: %v", err)
	}

	expectedFragments := []string{
		`<smbios mode="sysinfo"></smbios>`,
		`<sysinfo type="smbios">`,
		`<entry name="serial">ds=nocloud-net;s=http://192.168.2.254:8080/pxe/nocloud/525400000001/</entry>`,
	}
	for _, frag := range expectedFragments {
		if !strings.Contains(xmlStr, frag) {
			t.Errorf("XML does not contain expected fragment %q\nXML:\n%s", frag, xmlStr)
		}
	}
}
