package libvirt

import (
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
