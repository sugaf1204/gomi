package libvirt

import "fmt"

// DomainConfig holds all parameters needed to define a libvirt domain.
type DomainConfig struct {
	Name         string
	VCPU         int
	MemoryMB     int
	DiskPath     string // path to the OS image on the hypervisor
	DiskFormat   string // qcow2, raw
	DiskSizeGB   int    // disk size hint (used when creating overlay)
	Networks     []NetworkConfig
	CloudInit    string         // path to cloud-init ISO on the hypervisor (optional)
	BootDev      string         // "hd" (default) or "network" (PXE)
	SMBIOSSerial string         // optional NoCloud line config (e.g. ds=nocloud-net;s=http://...)
	CPUPinning   map[int]string // vCPU→pCPU mapping (e.g. 0:"0", 1:"2")
	CPUMode      string         // libvirt CPU mode: host-passthrough, host-model, maximum
	IOThreads    int            // number of IO threads
	DiskBus      string         // "virtio" (default) or "scsi"
	NetQueues    int            // virtio-net multiqueue count
}

// NetworkConfig describes a single network interface for a domain.
type NetworkConfig struct {
	Bridge string
	MAC    string
	Queues int // multiqueue queues count (0 = disabled)
}

// GraphicsInfo holds runtime VNC/SPICE graphics connection information.
type GraphicsInfo struct {
	Type   string // "vnc" or "spice"
	Port   int    // actual port number (e.g. 5900+N)
	Listen string // listen address
}

// DomainInfo holds current state information about a domain.
type DomainInfo struct {
	Name  string
	State DomainState
	UUID  string
}

// InterfaceInfo holds runtime network interface information for a domain.
type InterfaceInfo struct {
	Name        string
	MAC         string
	IPAddresses []string
}

// DomainState represents the running state of a libvirt domain.
type DomainState string

const (
	StateRunning DomainState = "running"
	StateShutoff DomainState = "shutoff"
	StatePaused  DomainState = "paused"
	StateCrashed DomainState = "crashed"
	StateUnknown DomainState = "unknown"
)

// LibvirtConfig holds TCP connection parameters for reaching a hypervisor's libvirtd.
type LibvirtConfig struct {
	Host string // libvirtd host
	Port int    // default 16509
}

// Validate checks that DomainConfig has all required fields.
func (c DomainConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("domain name is required")
	}
	if c.VCPU <= 0 {
		return fmt.Errorf("vcpu must be positive")
	}
	if c.MemoryMB <= 0 {
		return fmt.Errorf("memoryMB must be positive")
	}
	if c.DiskPath == "" {
		return fmt.Errorf("disk path is required")
	}
	if c.DiskFormat == "" {
		return fmt.Errorf("disk format is required")
	}
	if c.DiskFormat != "qcow2" && c.DiskFormat != "raw" {
		return fmt.Errorf("unsupported disk format: %s (must be qcow2 or raw)", c.DiskFormat)
	}
	switch c.CPUMode {
	case "", "host-passthrough", "host-model", "maximum":
	default:
		return fmt.Errorf("unsupported cpu mode: %s", c.CPUMode)
	}
	return nil
}

// Validate checks that LibvirtConfig has the minimum required fields.
func (c LibvirtConfig) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("libvirt host is required")
	}
	return nil
}

// ParseDomainState converts a virsh domain state string to DomainState.
func ParseDomainState(raw string) DomainState {
	switch raw {
	case "running":
		return StateRunning
	case "shut off", "shutoff":
		return StateShutoff
	case "paused":
		return StatePaused
	case "crashed":
		return StateCrashed
	default:
		return StateUnknown
	}
}
