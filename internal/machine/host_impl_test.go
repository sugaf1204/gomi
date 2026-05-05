package machine

import (
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/resource"
)

func TestMachine_ResourceName(t *testing.T) {
	m := &Machine{Name: "bm-01"}
	if got := m.ResourceName(); got != "bm-01" {
		t.Fatalf("expected bm-01, got %s", got)
	}
}

func TestMachine_NodeDisplayName(t *testing.T) {
	m := &Machine{Name: "bm-01", Hostname: "server-01"}
	if got := m.NodeDisplayName(); got != "server-01" {
		t.Fatalf("expected server-01, got %s", got)
	}
	m.Hostname = ""
	if got := m.NodeDisplayName(); got != "bm-01" {
		t.Fatalf("expected fallback to name bm-01, got %s", got)
	}
}

func TestMachine_PrimaryMAC(t *testing.T) {
	m := &Machine{MAC: "52:54:00:AA:BB:CC"}
	if got := m.PrimaryMAC(); got != "52:54:00:aa:bb:cc" {
		t.Fatalf("expected normalized mac, got %s", got)
	}
}

func TestMachine_AllMACs(t *testing.T) {
	m := &Machine{MAC: "52:54:00:aa:bb:cc"}
	macs := m.AllMACs()
	if len(macs) != 1 || macs[0] != "52:54:00:aa:bb:cc" {
		t.Fatalf("expected single mac, got %v", macs)
	}
	m.MAC = ""
	if macs := m.AllMACs(); macs != nil {
		t.Fatalf("expected nil macs for empty, got %v", macs)
	}
}

func TestMachine_PXEInstallType(t *testing.T) {
	tests := []struct {
		family OSType
		want   resource.InstallType
	}{
		{OSTypeUbuntu, resource.InstallCurtin},
		{OSTypeDebian, resource.InstallCurtin},
		{"rocky", resource.InstallCurtin},
	}
	for _, tt := range tests {
		m := &Machine{OSPreset: OSPreset{Family: tt.family}}
		if got := m.PXEInstallType(); got != tt.want {
			t.Errorf("PXEInstallType(%s) = %s, want %s", tt.family, got, tt.want)
		}
	}
}

func TestMachine_IsProvisioningActive(t *testing.T) {
	m := &Machine{}
	if m.IsProvisioningActive() {
		t.Fatal("expected inactive when provision is nil")
	}
	m.Provision = &ProvisionProgress{Active: false}
	if m.IsProvisioningActive() {
		t.Fatal("expected inactive when active=false")
	}
	m.Provision.Active = true
	if !m.IsProvisioningActive() {
		t.Fatal("expected active when active=true")
	}
}

func TestMachine_ProvisionToken(t *testing.T) {
	m := &Machine{}
	if got := m.ProvisionToken(); got != "" {
		t.Fatalf("expected empty token, got %s", got)
	}
	m.Provision = &ProvisionProgress{CompletionToken: " tok-123 "}
	if got := m.ProvisionToken(); got != "tok-123" {
		t.Fatalf("expected trimmed token, got %s", got)
	}
}

func TestMachine_MarkProvisionComplete(t *testing.T) {
	now := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	m := &Machine{
		Phase: PhaseProvisioning,
		Provision: &ProvisionProgress{
			Active: true,
		},
	}
	m.MarkProvisionComplete("preseed", now)

	if m.Phase != PhaseReady {
		t.Fatalf("expected phase Ready, got %s", m.Phase)
	}
	if m.Provision.Active {
		t.Fatal("expected provision active=false")
	}
	if m.Provision.CompletedAt == nil || !m.Provision.CompletedAt.Equal(now) {
		t.Fatal("expected completedAt to be set")
	}
	if m.Provision.CompletionSource != "preseed" {
		t.Fatalf("expected source preseed, got %s", m.Provision.CompletionSource)
	}
	if m.LastError != "" {
		t.Fatal("expected empty lastError")
	}
}

func TestMachine_MarkProvisionComplete_NilProvision(t *testing.T) {
	now := time.Now().UTC()
	m := &Machine{Phase: PhaseProvisioning}
	m.MarkProvisionComplete("curtin", now)
	if m.Provision == nil {
		t.Fatal("expected provision to be initialized")
	}
	if m.Phase != PhaseReady {
		t.Fatalf("expected Ready, got %s", m.Phase)
	}
}

func TestMachine_CloudInitRefForDeploy(t *testing.T) {
	m := &Machine{
		LastDeployedCloudInitRef: "ci-last",
		CloudInitRef:             "ci-legacy",
		CloudInitRefs:            []string{"ci-1"},
	}
	if got := m.CloudInitRefForDeploy(); got != "ci-last" {
		t.Fatalf("expected ci-last, got %s", got)
	}
	m.LastDeployedCloudInitRef = ""
	if got := m.CloudInitRefForDeploy(); got != "ci-1" {
		t.Fatalf("expected ci-1, got %s", got)
	}
}

func TestMachine_OSImageVariantRef(t *testing.T) {
	m := &Machine{OSPreset: OSPreset{ImageRef: "debian-13-amd64"}}
	if got := m.OSImageVariantRef(); got != "debian-13-amd64" {
		t.Fatalf("expected debian-13-amd64, got %s", got)
	}
}
