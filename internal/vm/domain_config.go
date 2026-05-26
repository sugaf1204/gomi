package vm

import (
	"strings"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/libvirt"
)

func BuildLibvirtConfig(hv hypervisor.Hypervisor) libvirt.LibvirtConfig {
	port := hv.Connection.Port
	if port == 0 {
		port = 16509
	}
	return libvirt.LibvirtConfig{
		Host: hv.Connection.Host,
		Port: port,
	}
}

func BuildDomainConfig(v VirtualMachine, domainName, bootDev, pxeBaseURL string, pxeNoCloudFn func(string, InstallConfigType, string) string) libvirt.DomainConfig {
	cfg := libvirt.DomainConfig{
		Name:       domainName,
		VCPU:       v.Resources.CPUCores,
		MemoryMB:   int(v.Resources.MemoryMB),
		DiskPath:   "/var/lib/libvirt/images/" + v.Name + ".qcow2",
		DiskFormat: "qcow2",
		DiskSizeGB: v.Resources.DiskGB,
		BootDev:    bootDev,
	}

	for _, nic := range v.Network {
		cfg.Networks = append(cfg.Networks, libvirt.NetworkConfig{
			Bridge: nic.Bridge,
			MAC:    nic.MAC,
		})
	}

	if opts := v.AdvancedOptions; opts != nil {
		if len(opts.CPUPinning) > 0 {
			cfg.CPUPinning = opts.CPUPinning
		}
		if opts.CPUMode != "" {
			cfg.CPUMode = string(opts.CPUMode)
		}
		if opts.IOThreads > 0 {
			cfg.IOThreads = opts.IOThreads
		}
		if opts.DiskDriver == DiskDriverVirtio {
			cfg.DiskBus = "virtio"
		}
		if opts.DiskDriver == DiskDriverSCSI {
			cfg.DiskBus = "scsi"
		}
		if opts.NetMultiqueue > 0 {
			cfg.NetQueues = opts.NetMultiqueue
		}
	}

	if pxeBase := strings.TrimSpace(pxeBaseURL); pxeBase != "" && pxeNoCloudFn != nil {
		installType := InstallConfigPreseed
		if v.InstallCfg != nil {
			installType = v.InstallCfg.Type
		}
		mac := vmPrimaryMAC(v)
		if serial := pxeNoCloudFn(pxeBase, installType, mac); serial != "" {
			cfg.SMBIOSSerial = serial
		}
	}

	return cfg
}

func applyInstallStorageOverrides(cfg *libvirt.DomainConfig, installType InstallConfigType) {
	if cfg == nil {
		return
	}
	if installType == InstallConfigCurtin {
		cfg.DiskFormat = "qcow2"
		if cfg.DiskBus == "" {
			cfg.DiskBus = "sata"
		}
	}
}

func vmPrimaryMAC(v VirtualMachine) string {
	for _, nic := range v.Network {
		if mac := strings.ToLower(strings.TrimSpace(nic.MAC)); mac != "" {
			return mac
		}
	}
	for _, nic := range v.NetworkInterfaces {
		if mac := strings.ToLower(strings.TrimSpace(nic.MAC)); mac != "" {
			return mac
		}
	}
	return ""
}

func IsIgnorableDestroyError(err error) bool {
	if err == nil {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not running") ||
		strings.Contains(msg, "domain is not running") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "lookup domain")
}
