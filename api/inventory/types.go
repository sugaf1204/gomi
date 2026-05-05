package inventory

type HardwareInventory struct {
	MachineName string      `json:"machineName,omitempty"`
	AttemptID   string      `json:"attemptId,omitempty"`
	CPU         CPUInfo     `json:"cpu,omitempty"`
	Memory      MemoryInfo  `json:"memory,omitempty"`
	Disks       []DiskInfo  `json:"disks,omitempty"`
	NICs        []NICInfo   `json:"nics,omitempty"`
	BIOS        BIOSInfo    `json:"bios,omitempty"`
	PCI         []PCIDevice `json:"pci,omitempty"`
	USB         []USBDevice `json:"usb,omitempty"`
	GPUs        []GPUInfo   `json:"gpus,omitempty"`
	Sensors     []Sensor    `json:"sensors,omitempty"`
	Boot        BootInfo    `json:"boot,omitempty"`
	Runtime     RuntimeInfo `json:"runtime,omitempty"`
}

type CPUInfo struct {
	Model   string `json:"model,omitempty"`
	Cores   int    `json:"cores,omitempty"`
	Threads int    `json:"threads,omitempty"`
	Arch    string `json:"arch,omitempty"`
	MHz     string `json:"mhz,omitempty"`
}

type MemoryInfo struct {
	TotalMB int `json:"totalMB,omitempty"`
	Slots   int `json:"slots,omitempty"`
}

type DiskInfo struct {
	Name       string   `json:"name"`
	Path       string   `json:"path,omitempty"`
	ByID       []string `json:"byId,omitempty"`
	ByPath     []string `json:"byPath,omitempty"`
	SizeMB     int64    `json:"sizeMB"`
	Type       string   `json:"type,omitempty"`
	Model      string   `json:"model,omitempty"`
	Serial     string   `json:"serial,omitempty"`
	WWN        string   `json:"wwn,omitempty"`
	Transport  string   `json:"transport,omitempty"`
	Rotational bool     `json:"rotational,omitempty"`
	Removable  bool     `json:"removable,omitempty"`
}

type NICInfo struct {
	Name              string `json:"name"`
	MAC               string `json:"mac"`
	Speed             string `json:"speed,omitempty"`
	State             string `json:"state,omitempty"`
	Driver            string `json:"driver,omitempty"`
	Modalias          string `json:"modalias,omitempty"`
	PCISlot           string `json:"pciSlot,omitempty"`
	VendorID          string `json:"vendorId,omitempty"`
	DeviceID          string `json:"deviceId,omitempty"`
	SubsystemVendorID string `json:"subsystemVendorId,omitempty"`
	SubsystemDeviceID string `json:"subsystemDeviceId,omitempty"`
}

type BIOSInfo struct {
	Vendor  string `json:"vendor,omitempty"`
	Version string `json:"version,omitempty"`
	Date    string `json:"date,omitempty"`
}

type PCIDevice struct {
	Slot   string `json:"slot"`
	Class  string `json:"class"`
	Vendor string `json:"vendor"`
	Device string `json:"device"`
}

type USBDevice struct {
	Bus    string `json:"bus"`
	Device string `json:"device"`
	Vendor string `json:"vendor,omitempty"`
	Name   string `json:"name"`
}

type GPUInfo struct {
	Name   string `json:"name"`
	Vendor string `json:"vendor,omitempty"`
	VRAM   string `json:"vram,omitempty"`
}

type Sensor struct {
	Name  string  `json:"name"`
	Type  string  `json:"type"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

type BootInfo struct {
	FirmwareMode string `json:"firmwareMode,omitempty"`
	SecureBoot   string `json:"secureBoot,omitempty"`
	EFIVars      bool   `json:"efiVars,omitempty"`
}

type RuntimeInfo struct {
	KernelVersion string   `json:"kernelVersion,omitempty"`
	LoadedModules []string `json:"loadedModules,omitempty"`
}
