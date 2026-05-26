package libvirt

import (
	"bytes"
	"encoding/xml"
	"fmt"
)

// xmlDomain represents a minimal libvirt domain XML structure.
type xmlDomain struct {
	XMLName   xml.Name    `xml:"domain"`
	Type      string      `xml:"type,attr"`
	Name      string      `xml:"name"`
	Memory    xmlMemory   `xml:"memory"`
	VCPU      int         `xml:"vcpu"`
	IOThreads int         `xml:"iothreads,omitempty"`
	CPU       *xmlCPU     `xml:"cpu,omitempty"`
	CPUTune   *xmlCPUTune `xml:"cputune,omitempty"`
	Features  xmlFeatures `xml:"features"`
	OS        xmlOS       `xml:"os"`
	SysInfo   *xmlSysInfo `xml:"sysinfo,omitempty"`
	Devices   xmlDevices  `xml:"devices"`
}

type xmlCPU struct {
	Mode string `xml:"mode,attr"`
}

type xmlCPUTune struct {
	VCPUPins []xmlVCPUPin `xml:"vcpupin"`
}

type xmlVCPUPin struct {
	VCPU   int    `xml:"vcpu,attr"`
	CPUSet string `xml:"cpuset,attr"`
}

type xmlFeatures struct {
	ACPI *struct{} `xml:"acpi,omitempty"`
	APIC *struct{} `xml:"apic,omitempty"`
}

type xmlMemory struct {
	Unit  string `xml:"unit,attr"`
	Value int    `xml:",chardata"`
}

type xmlOS struct {
	Type   xmlOSType  `xml:"type"`
	Boot   xmlBoot    `xml:"boot"`
	SMBIOS *xmlSMBIOS `xml:"smbios,omitempty"`
}

type xmlOSType struct {
	Arch    string `xml:"arch,attr"`
	Machine string `xml:"machine,attr"`
	Value   string `xml:",chardata"`
}

type xmlBoot struct {
	Dev string `xml:"dev,attr"`
}

type xmlSMBIOS struct {
	Mode string `xml:"mode,attr"`
}

type xmlSysInfo struct {
	Type   string           `xml:"type,attr"`
	System xmlSysInfoSystem `xml:"system"`
}

type xmlSysInfoSystem struct {
	Entries []xmlSysInfoEntry `xml:"entry"`
}

type xmlSysInfoEntry struct {
	Name  string `xml:"name,attr"`
	Value string `xml:",chardata"`
}

type xmlDevices struct {
	Controllers []xmlController `xml:"controller,omitempty"`
	Disks       []xmlDisk       `xml:"disk"`
	Interfaces  []xmlInterface  `xml:"interface"`
	Serial      *xmlSerial      `xml:"serial,omitempty"`
	Console     *xmlConsole     `xml:"console,omitempty"`
	Graphics    xmlGraphics     `xml:"graphics"`
}

type xmlController struct {
	Type  string `xml:"type,attr"`
	Model string `xml:"model,attr"`
}

type xmlDisk struct {
	Type     string        `xml:"type,attr"`
	Device   string        `xml:"device,attr"`
	Driver   xmlDriver     `xml:"driver"`
	Source   xmlDiskSource `xml:"source"`
	Target   xmlDiskTarget `xml:"target"`
	ReadOnly *struct{}     `xml:"readonly,omitempty"`
}

type xmlDriver struct {
	Name     string `xml:"name,attr"`
	Type     string `xml:"type,attr"`
	IOThread string `xml:"iothread,attr,omitempty"`
}

type xmlDiskSource struct {
	File string `xml:"file,attr"`
}

type xmlDiskTarget struct {
	Dev string `xml:"dev,attr"`
	Bus string `xml:"bus,attr"`
}

type xmlInterface struct {
	Type   string              `xml:"type,attr"`
	Source xmlIfSource         `xml:"source"`
	MAC    *xmlMAC             `xml:"mac,omitempty"`
	Model  xmlModel            `xml:"model"`
	Driver *xmlInterfaceDriver `xml:"driver,omitempty"`
}

type xmlInterfaceDriver struct {
	Name   string `xml:"name,attr"`
	Queues int    `xml:"queues,attr,omitempty"`
}

type xmlIfSource struct {
	Bridge string `xml:"bridge,attr"`
}

type xmlMAC struct {
	Address string `xml:"address,attr"`
}

type xmlModel struct {
	Type string `xml:"type,attr"`
}

type xmlConsole struct {
	Type   string           `xml:"type,attr"`
	Target xmlConsoleTarget `xml:"target"`
}

type xmlSerial struct {
	Type   string          `xml:"type,attr"`
	Target xmlSerialTarget `xml:"target"`
}

type xmlSerialTarget struct {
	Type  string          `xml:"type,attr"`
	Port  int             `xml:"port,attr"`
	Model *xmlSerialModel `xml:"model,omitempty"`
}

type xmlSerialModel struct {
	Name string `xml:"name,attr"`
}

type xmlConsoleTarget struct {
	Type string `xml:"type,attr"`
	Port int    `xml:"port,attr"`
}

type xmlGraphics struct {
	Type     string `xml:"type,attr"`
	Port     string `xml:"port,attr"`
	AutoPort string `xml:"autoport,attr"`
}

// GenerateDomainXML creates a libvirt domain XML from DomainConfig.
func GenerateDomainXML(cfg DomainConfig) (string, error) {
	if err := cfg.Validate(); err != nil {
		return "", fmt.Errorf("invalid domain config: %w", err)
	}

	domain := xmlDomain{
		Type: "kvm",
		Name: cfg.Name,
		Memory: xmlMemory{
			Unit:  "MiB",
			Value: cfg.MemoryMB,
		},
		VCPU: cfg.VCPU,
		Features: xmlFeatures{
			ACPI: &struct{}{},
			APIC: &struct{}{},
		},
		OS: xmlOS{
			Type: xmlOSType{
				Arch:    "x86_64",
				Machine: "pc",
				Value:   "hvm",
			},
			Boot: xmlBoot{Dev: func() string {
				if cfg.BootDev != "" {
					return cfg.BootDev
				}
				return "hd"
			}()},
		},
	}

	if cfg.SMBIOSSerial != "" {
		domain.OS.SMBIOS = &xmlSMBIOS{Mode: "sysinfo"}
		domain.SysInfo = &xmlSysInfo{
			Type: "smbios",
			System: xmlSysInfoSystem{
				Entries: []xmlSysInfoEntry{
					{Name: "serial", Value: cfg.SMBIOSSerial},
				},
			},
		}
	}

	if cfg.CPUMode != "" {
		domain.CPU = &xmlCPU{Mode: cfg.CPUMode}
	}

	// CPU pinning.
	if len(cfg.CPUPinning) > 0 {
		pins := make([]xmlVCPUPin, 0, len(cfg.CPUPinning))
		for vcpu, cpuset := range cfg.CPUPinning {
			pins = append(pins, xmlVCPUPin{VCPU: vcpu, CPUSet: cpuset})
		}
		domain.CPUTune = &xmlCPUTune{VCPUPins: pins}
	}

	// IO threads.
	if cfg.IOThreads > 0 {
		domain.IOThreads = cfg.IOThreads
	}

	// SCSI controller (when disk bus is scsi).
	diskBus := "virtio"
	diskTarget := "vda"
	switch cfg.DiskBus {
	case "scsi":
		diskBus = "scsi"
		diskTarget = "sda"
		domain.Devices.Controllers = append(domain.Devices.Controllers, xmlController{
			Type:  "scsi",
			Model: "virtio-scsi",
		})
	case "sata":
		diskBus = "sata"
		diskTarget = "sda"
	}

	// Primary disk (OS image).
	diskDriver := xmlDriver{Name: "qemu", Type: cfg.DiskFormat}
	if cfg.IOThreads > 0 {
		diskDriver.IOThread = "1"
	}
	osDisk := xmlDisk{
		Type:   "file",
		Device: "disk",
		Driver: diskDriver,
		Source: xmlDiskSource{File: cfg.DiskPath},
		Target: xmlDiskTarget{Dev: diskTarget, Bus: diskBus},
	}
	domain.Devices.Disks = append(domain.Devices.Disks, osDisk)

	// Cloud-init ISO (if provided).
	if cfg.CloudInit != "" {
		ciDisk := xmlDisk{
			Type:     "file",
			Device:   "disk",
			Driver:   xmlDriver{Name: "qemu", Type: "raw"},
			Source:   xmlDiskSource{File: cfg.CloudInit},
			Target:   xmlDiskTarget{Dev: "vdb", Bus: "virtio"},
			ReadOnly: &struct{}{},
		}
		domain.Devices.Disks = append(domain.Devices.Disks, ciDisk)
	}

	// Network interfaces.
	for _, net := range cfg.Networks {
		iface := xmlInterface{
			Type:   "bridge",
			Source: xmlIfSource{Bridge: net.Bridge},
			Model:  xmlModel{Type: "virtio"},
		}
		if net.MAC != "" {
			iface.MAC = &xmlMAC{Address: net.MAC}
		}
		queues := net.Queues
		if queues == 0 {
			queues = cfg.NetQueues
		}
		if queues > 0 {
			iface.Driver = &xmlInterfaceDriver{Name: "vhost", Queues: queues}
		}
		domain.Devices.Interfaces = append(domain.Devices.Interfaces, iface)
	}

	// Graphics.
	domain.Devices.Serial = &xmlSerial{
		Type: "pty",
		Target: xmlSerialTarget{
			Type:  "isa-serial",
			Port:  0,
			Model: &xmlSerialModel{Name: "isa-serial"},
		},
	}
	domain.Devices.Console = &xmlConsole{
		Type:   "pty",
		Target: xmlConsoleTarget{Type: "serial", Port: 0},
	}
	domain.Devices.Graphics = xmlGraphics{
		Type:     "vnc",
		Port:     "-1",
		AutoPort: "yes",
	}

	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(domain); err != nil {
		return "", fmt.Errorf("xml encode: %w", err)
	}
	return buf.String(), nil
}
