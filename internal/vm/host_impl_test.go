package vm

import (
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/resource"
)

func TestVM_ResourceName(t *testing.T) {
	v := &VirtualMachine{Name: "vm-01"}
	if got := v.ResourceName(); got != "vm-01" {
		t.Fatalf("expected vm-01, got %s", got)
	}
}

func TestVM_NodeDisplayName(t *testing.T) {
	v := &VirtualMachine{Name: "vm-01"}
	if got := v.NodeDisplayName(); got != "vm-01" {
		t.Fatalf("expected vm-01, got %s", got)
	}
}

func TestVM_PrimaryMAC(t *testing.T) {
	v := &VirtualMachine{
		Network: []NetworkInterface{
			{MAC: "52:54:00:AA:BB:CC"},
		},
	}
	if got := v.PrimaryMAC(); got != "52:54:00:aa:bb:cc" {
		t.Fatalf("expected normalized mac, got %s", got)
	}
}

func TestVM_PrimaryMAC_FallbackToStatus(t *testing.T) {
	v := &VirtualMachine{
		NetworkInterfaces: []NetworkInterfaceStatus{
			{MAC: "52:54:00:DD:EE:FF"},
		},
	}
	if got := v.PrimaryMAC(); got != "52:54:00:dd:ee:ff" {
		t.Fatalf("expected fallback mac, got %s", got)
	}
}

func TestVM_AllMACs(t *testing.T) {
	v := &VirtualMachine{
		Network: []NetworkInterface{
			{MAC: "52:54:00:aa:bb:cc"},
			{MAC: "52:54:00:dd:ee:ff"},
		},
		NetworkInterfaces: []NetworkInterfaceStatus{
			{MAC: "52:54:00:aa:bb:cc"}, // duplicate
			{MAC: "52:54:00:11:22:33"},
		},
	}
	macs := v.AllMACs()
	if len(macs) != 3 {
		t.Fatalf("expected 3 unique macs, got %v", macs)
	}
}

func TestVM_PXEInstallType(t *testing.T) {
	tests := []struct {
		name string
		cfg  *InstallConfig
		want resource.InstallType
	}{
		{"nil config", nil, resource.InstallPreseed},
		{"preseed", &InstallConfig{Type: InstallConfigPreseed}, resource.InstallPreseed},
		{"curtin", &InstallConfig{Type: InstallConfigCurtin}, resource.InstallCurtin},
		{"unknown", &InstallConfig{Type: "unknown"}, resource.InstallPreseed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &VirtualMachine{InstallCfg: tt.cfg}
			if got := v.PXEInstallType(); got != tt.want {
				t.Errorf("PXEInstallType() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestVM_IsProvisioningActive(t *testing.T) {
	v := &VirtualMachine{}
	if v.IsProvisioningActive() {
		t.Fatal("expected inactive by default")
	}
	v.Provisioning.Active = true
	if !v.IsProvisioningActive() {
		t.Fatal("expected active")
	}
}

func TestVM_ProvisionToken(t *testing.T) {
	v := &VirtualMachine{}
	if got := v.ProvisionToken(); got != "" {
		t.Fatalf("expected empty, got %s", got)
	}
	v.Provisioning.CompletionToken = " tok-vm "
	if got := v.ProvisionToken(); got != "tok-vm" {
		t.Fatalf("expected trimmed token, got %s", got)
	}
}

func TestVM_MarkProvisionComplete(t *testing.T) {
	now := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	v := &VirtualMachine{
		Phase: PhaseProvisioning,
		Provisioning: ProvisioningStatus{
			Active: true,
		},
		LastError: "some error",
	}
	v.MarkProvisionComplete("curtin", now)

	if v.Phase != PhaseRunning {
		t.Fatalf("expected Running, got %s", v.Phase)
	}
	if v.Provisioning.Active {
		t.Fatal("expected active=false")
	}
	if v.Provisioning.CompletedAt == nil || !v.Provisioning.CompletedAt.Equal(now) {
		t.Fatal("expected completedAt set")
	}
	if v.Provisioning.CompletionSource != "curtin" {
		t.Fatalf("expected source curtin, got %s", v.Provisioning.CompletionSource)
	}
	if v.LastError != "" {
		t.Fatal("expected empty lastError")
	}
}

func TestVM_CloudInitRefForDeploy(t *testing.T) {
	v := &VirtualMachine{
		LastDeployedCloudInitRef: "ci-deployed",
		CloudInitRefs:            []string{"ci-1", "ci-2"},
	}
	if got := v.CloudInitRefForDeploy(); got != "ci-deployed" {
		t.Fatalf("expected ci-deployed, got %s", got)
	}
	v.LastDeployedCloudInitRef = ""
	if got := v.CloudInitRefForDeploy(); got != "ci-1" {
		t.Fatalf("expected ci-1, got %s", got)
	}
}

func TestVM_CloudInitInline(t *testing.T) {
	v := &VirtualMachine{}
	if got := v.CloudInitInline(resource.InstallPreseed); got != "" {
		t.Fatalf("expected empty for nil config, got %s", got)
	}

	v.InstallCfg = &InstallConfig{Type: InstallConfigPreseed, Inline: " custom-preseed "}
	if got := v.CloudInitInline(resource.InstallPreseed); got != "custom-preseed" {
		t.Fatalf("expected custom-preseed, got %s", got)
	}
	if got := v.CloudInitInline(resource.InstallCurtin); got != "" {
		t.Fatalf("expected empty for mismatched type, got %s", got)
	}
}

func TestVM_OSImageVariantRef(t *testing.T) {
	v := &VirtualMachine{OSImageRef: "ubuntu-24.04"}
	if got := v.OSImageVariantRef(); got != "ubuntu-24.04" {
		t.Fatalf("expected ubuntu-24.04, got %s", got)
	}
}
