package hwinfo

import apiinventory "github.com/sugaf1204/gomi/api/inventory"

func FromInventory(in apiinventory.HardwareInventory) HardwareInfo {
	return HardwareInfo{
		MachineName: in.MachineName,
		AttemptID:   in.AttemptID,
		CPU: CPUInfo{
			Model:   in.CPU.Model,
			Cores:   in.CPU.Cores,
			Threads: in.CPU.Threads,
			Arch:    in.CPU.Arch,
			MHz:     in.CPU.MHz,
		},
		Memory: MemoryInfo{
			TotalMB: in.Memory.TotalMB,
			Slots:   in.Memory.Slots,
		},
		Disks:   disksFromInventory(in.Disks),
		NICs:    nicsFromInventory(in.NICs),
		BIOS:    BIOSInfo{Vendor: in.BIOS.Vendor, Version: in.BIOS.Version, Date: in.BIOS.Date},
		PCI:     pciFromInventory(in.PCI),
		USB:     usbFromInventory(in.USB),
		GPUs:    gpusFromInventory(in.GPUs),
		Sensors: sensorsFromInventory(in.Sensors),
		Boot: BootInfo{
			FirmwareMode: in.Boot.FirmwareMode,
			SecureBoot:   in.Boot.SecureBoot,
			EFIVars:      in.Boot.EFIVars,
		},
		Runtime: RuntimeInfo{
			KernelVersion: in.Runtime.KernelVersion,
			LoadedModules: append([]string(nil), in.Runtime.LoadedModules...),
		},
	}
}

func disksFromInventory(in []apiinventory.DiskInfo) []DiskInfo {
	out := make([]DiskInfo, 0, len(in))
	for _, disk := range in {
		out = append(out, DiskInfo{
			Name:       disk.Name,
			Path:       disk.Path,
			ByID:       append([]string(nil), disk.ByID...),
			ByPath:     append([]string(nil), disk.ByPath...),
			SizeMB:     disk.SizeMB,
			Type:       disk.Type,
			Model:      disk.Model,
			Serial:     disk.Serial,
			WWN:        disk.WWN,
			Transport:  disk.Transport,
			Rotational: disk.Rotational,
			Removable:  disk.Removable,
		})
	}
	return out
}

func nicsFromInventory(in []apiinventory.NICInfo) []NICInfo {
	out := make([]NICInfo, 0, len(in))
	for _, nic := range in {
		out = append(out, NICInfo{
			Name:              nic.Name,
			MAC:               nic.MAC,
			Speed:             nic.Speed,
			State:             nic.State,
			Driver:            nic.Driver,
			Modalias:          nic.Modalias,
			PCISlot:           nic.PCISlot,
			VendorID:          nic.VendorID,
			DeviceID:          nic.DeviceID,
			SubsystemVendorID: nic.SubsystemVendorID,
			SubsystemDeviceID: nic.SubsystemDeviceID,
		})
	}
	return out
}

func pciFromInventory(in []apiinventory.PCIDevice) []PCIDevice {
	out := make([]PCIDevice, 0, len(in))
	for _, dev := range in {
		out = append(out, PCIDevice{Slot: dev.Slot, Class: dev.Class, Vendor: dev.Vendor, Device: dev.Device})
	}
	return out
}

func usbFromInventory(in []apiinventory.USBDevice) []USBDevice {
	out := make([]USBDevice, 0, len(in))
	for _, dev := range in {
		out = append(out, USBDevice{Bus: dev.Bus, Device: dev.Device, Vendor: dev.Vendor, Name: dev.Name})
	}
	return out
}

func gpusFromInventory(in []apiinventory.GPUInfo) []GPUInfo {
	out := make([]GPUInfo, 0, len(in))
	for _, gpu := range in {
		out = append(out, GPUInfo{Name: gpu.Name, Vendor: gpu.Vendor, VRAM: gpu.VRAM})
	}
	return out
}

func sensorsFromInventory(in []apiinventory.Sensor) []Sensor {
	out := make([]Sensor, 0, len(in))
	for _, sensor := range in {
		out = append(out, Sensor{Name: sensor.Name, Type: sensor.Type, Value: sensor.Value, Unit: sensor.Unit})
	}
	return out
}
