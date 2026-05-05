package libvirt

import (
	"strings"
	"testing"
)

func TestGenerateDomainXML_Basic(t *testing.T) {
	cfg := DomainConfig{
		Name:       "test-vm",
		VCPU:       2,
		MemoryMB:   2048,
		DiskPath:   "/var/lib/libvirt/images/test-vm.qcow2",
		DiskFormat: "qcow2",
		Networks:   []NetworkConfig{{Bridge: "br0", MAC: "52:54:00:aa:bb:cc"}},
	}
	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(xmlStr, "<name>test-vm</name>") {
		t.Error("expected domain name in XML")
	}
	if !strings.Contains(xmlStr, `bus="virtio"`) {
		t.Error("expected virtio disk bus")
	}
}

func TestGenerateDomainXML_CPUPinning(t *testing.T) {
	cfg := DomainConfig{
		Name:       "pinned-vm",
		VCPU:       4,
		MemoryMB:   4096,
		DiskPath:   "/var/lib/libvirt/images/pinned.qcow2",
		DiskFormat: "qcow2",
		CPUPinning: map[int]string{0: "0", 1: "2", 2: "4", 3: "6"},
	}
	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(xmlStr, "<cputune>") {
		t.Error("expected cputune element")
	}
	if !strings.Contains(xmlStr, `vcpu="0"`) {
		t.Error("expected vcpupin for vcpu 0")
	}
	if !strings.Contains(xmlStr, `cpuset="0"`) {
		t.Error("expected cpuset 0")
	}
}

func TestGenerateDomainXML_IOThreads(t *testing.T) {
	cfg := DomainConfig{
		Name:       "io-vm",
		VCPU:       2,
		MemoryMB:   2048,
		DiskPath:   "/var/lib/libvirt/images/io.qcow2",
		DiskFormat: "qcow2",
		IOThreads:  2,
	}
	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(xmlStr, "<iothreads>2</iothreads>") {
		t.Error("expected iothreads element")
	}
	if !strings.Contains(xmlStr, `iothread="1"`) {
		t.Error("expected iothread attribute on disk driver")
	}
}

func TestGenerateDomainXML_CPUMode(t *testing.T) {
	cfg := DomainConfig{
		Name:       "cpu-mode-vm",
		VCPU:       2,
		MemoryMB:   2048,
		DiskPath:   "/var/lib/libvirt/images/cpu-mode.qcow2",
		DiskFormat: "qcow2",
		CPUMode:    "host-passthrough",
	}
	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(xmlStr, `<cpu mode="host-passthrough"></cpu>`) {
		t.Error("expected cpu mode element")
	}
}

func TestGenerateDomainXML_SCSIDisk(t *testing.T) {
	cfg := DomainConfig{
		Name:       "scsi-vm",
		VCPU:       2,
		MemoryMB:   2048,
		DiskPath:   "/var/lib/libvirt/images/scsi.qcow2",
		DiskFormat: "qcow2",
		DiskBus:    "scsi",
	}
	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(xmlStr, `model="virtio-scsi"`) {
		t.Error("expected virtio-scsi controller")
	}
	if !strings.Contains(xmlStr, `bus="scsi"`) {
		t.Error("expected scsi disk bus")
	}
	if !strings.Contains(xmlStr, `dev="sda"`) {
		t.Error("expected sda disk target for scsi")
	}
}

func TestGenerateDomainXML_Multiqueue(t *testing.T) {
	cfg := DomainConfig{
		Name:       "mq-vm",
		VCPU:       4,
		MemoryMB:   4096,
		DiskPath:   "/var/lib/libvirt/images/mq.qcow2",
		DiskFormat: "qcow2",
		NetQueues:  4,
		Networks:   []NetworkConfig{{Bridge: "br0"}},
	}
	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(xmlStr, `name="vhost"`) {
		t.Error("expected vhost driver on interface")
	}
	if !strings.Contains(xmlStr, `queues="4"`) {
		t.Error("expected queues=4 on interface driver")
	}
}

func TestGenerateDomainXML_AllAdvanced(t *testing.T) {
	cfg := DomainConfig{
		Name:       "advanced-vm",
		VCPU:       4,
		MemoryMB:   8192,
		DiskPath:   "/var/lib/libvirt/images/adv.qcow2",
		DiskFormat: "qcow2",
		CPUPinning: map[int]string{0: "0", 1: "2"},
		IOThreads:  2,
		DiskBus:    "scsi",
		NetQueues:  4,
		Networks:   []NetworkConfig{{Bridge: "br0", MAC: "52:54:00:aa:bb:cc"}},
	}
	xmlStr, err := GenerateDomainXML(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	checks := []string{
		"<cputune>",
		"<iothreads>2</iothreads>",
		`model="virtio-scsi"`,
		`bus="scsi"`,
		`iothread="1"`,
		`name="vhost"`,
		`queues="4"`,
	}
	for _, check := range checks {
		if !strings.Contains(xmlStr, check) {
			t.Errorf("expected %q in XML output", check)
		}
	}
}

func TestParseDomainGraphicsFromXML_VNC(t *testing.T) {
	domainXML := `<domain type="kvm">
  <name>test</name>
  <devices>
    <graphics type="vnc" port="5901" autoport="yes" listen="0.0.0.0"/>
  </devices>
</domain>`

	info, err := parseDomainGraphicsFromXML(domainXML)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Type != "vnc" {
		t.Errorf("expected type vnc, got %s", info.Type)
	}
	if info.Port != 5901 {
		t.Errorf("expected port 5901, got %d", info.Port)
	}
	if info.Listen != "0.0.0.0" {
		t.Errorf("expected listen 0.0.0.0, got %s", info.Listen)
	}
}

func TestParseDomainGraphicsFromXML_NoVNC(t *testing.T) {
	domainXML := `<domain type="kvm">
  <name>test</name>
  <devices>
    <graphics type="spice" port="5910"/>
  </devices>
</domain>`

	_, err := parseDomainGraphicsFromXML(domainXML)
	if err == nil {
		t.Fatal("expected error for no VNC graphics")
	}
}

func TestParseDomainGraphicsFromXML_InvalidPort(t *testing.T) {
	domainXML := `<domain type="kvm">
  <name>test</name>
  <devices>
    <graphics type="vnc" port="-1"/>
  </devices>
</domain>`

	_, err := parseDomainGraphicsFromXML(domainXML)
	if err == nil {
		t.Fatal("expected error for port -1")
	}
}
