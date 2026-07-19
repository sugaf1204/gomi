package vm

import (
	"errors"
	"fmt"
	"testing"

	golibvirt "github.com/digitalocean/go-libvirt"
)

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

func TestSkipHostStorageCleanup(t *testing.T) {
	notFound := fmt.Errorf("undefine domain vm-01: %w", golibvirt.Error{Code: uint32(golibvirt.ErrNoDomain), Message: "no domain"})
	tests := []struct {
		name        string
		phase       Phase
		undefineErr error
		want        bool
	}{
		{name: "missing vm with absent domain skips storage", phase: PhaseMissing, undefineErr: notFound, want: true},
		{name: "missing vm whose domain existed cleans storage", phase: PhaseMissing, undefineErr: nil, want: false},
		{name: "running vm with absent domain cleans storage", phase: PhaseRunning, undefineErr: notFound, want: false},
		{name: "missing vm with untyped error cleans storage", phase: PhaseMissing, undefineErr: errors.New("domain not found"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SkipHostStorageCleanup(tt.phase, tt.undefineErr); got != tt.want {
				t.Fatalf("SkipHostStorageCleanup(%s, %v) = %v, want %v", tt.phase, tt.undefineErr, got, tt.want)
			}
		})
	}
}
