package vm

import "testing"

func TestBuildDomainConfig_IgnoresUnsupportedLegacyDiskFormat(t *testing.T) {
	v := VirtualMachine{
		Name: "vm-ubuntu",
		Resources: ResourceSpec{
			CPUCores: 1,
			MemoryMB: 1024,
			DiskGB:   10,
		},
		OSImageRef: "ubuntu-24.04-amd64",
		InstallCfg: &InstallConfig{Type: InstallConfigCurtin},
		AdvancedOptions: &AdvancedOptions{
			DiskFormat: "vmdk",
		},
	}

	cfg := BuildDomainConfig(v, v.Name, "hd", "", nil)
	if cfg.DiskFormat != "qcow2" {
		t.Fatalf("expected VM domain format to stay qcow2, got %q", cfg.DiskFormat)
	}
}

func TestApplyInstallStorageOverrides_CloudImageUsesSATADisk(t *testing.T) {
	v := VirtualMachine{
		Name: "vm-debian",
		Resources: ResourceSpec{
			CPUCores: 1,
			MemoryMB: 1024,
			DiskGB:   10,
		},
		InstallCfg: &InstallConfig{Type: InstallConfigCurtin},
	}
	cfg := BuildDomainConfig(v, "vm-debian", "hd", "", nil)

	applyInstallStorageOverrides(&cfg, InstallConfigCurtin)
	if cfg.DiskFormat != "qcow2" {
		t.Fatalf("expected cloudimage disk format qcow2, got %q", cfg.DiskFormat)
	}
	if cfg.DiskBus != "sata" {
		t.Fatalf("expected cloudimage disk bus sata, got %q", cfg.DiskBus)
	}
}

func TestApplyInstallStorageOverrides_CloudImagePreservesExplicitDiskDriver(t *testing.T) {
	v := VirtualMachine{
		Name: "vm-debian",
		Resources: ResourceSpec{
			CPUCores: 1,
			MemoryMB: 1024,
			DiskGB:   10,
		},
		InstallCfg:      &InstallConfig{Type: InstallConfigCurtin},
		AdvancedOptions: &AdvancedOptions{DiskDriver: DiskDriverVirtio},
	}
	cfg := BuildDomainConfig(v, "vm-debian", "hd", "", nil)

	applyInstallStorageOverrides(&cfg, InstallConfigCurtin)
	if cfg.DiskFormat != "qcow2" {
		t.Fatalf("expected cloudimage disk format qcow2, got %q", cfg.DiskFormat)
	}
	if cfg.DiskBus != "virtio" {
		t.Fatalf("expected explicit cloudimage disk bus virtio to be preserved, got %q", cfg.DiskBus)
	}
}
