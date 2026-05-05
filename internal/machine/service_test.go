package machine_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
)

func newTestService() *machine.Service {
	b := memory.New()
	return machine.NewService(b.Machines())
}

func testMachine() machine.Machine {
	return machine.Machine{
		Name:     "svc-test-01",
		Hostname: "svc-test-01.lab", MAC: "aa:bb:cc:dd:ee:01", Arch: "amd64",
		Firmware: machine.FirmwareUEFI, Power: power.PowerConfig{Type: power.PowerTypeManual},
		OSPreset: machine.OSPreset{Family: machine.OSTypeUbuntu, Version: "24.04", ImageRef: "img"},
	}
}

func TestServiceCreate(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	m, err := svc.Create(ctx, testMachine())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if m.Phase != machine.PhaseReady {
		t.Fatalf("expected phase Ready, got %s", m.Phase)
	}
}

func TestServiceCreate_WithProvision(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	m := testMachine()
	now := time.Now().UTC()
	m.Phase = machine.PhaseProvisioning
	m.Provision = &machine.ProvisionProgress{
		Active:          true,
		StartedAt:       &now,
		Trigger:         "create",
		CompletionToken: "test-token",
	}
	created, err := svc.Create(ctx, m)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Phase != machine.PhaseProvisioning {
		t.Fatalf("expected phase Provisioning to be preserved, got %s", created.Phase)
	}
	if created.Provision == nil || created.Provision.CompletionToken != "test-token" {
		t.Fatal("expected provision with completionToken to be preserved")
	}
	if !created.Provision.Active {
		t.Fatal("expected provision.active=true to be preserved")
	}
}

func TestServiceGet(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testMachine())
	got, err := svc.Get(ctx, "svc-test-01")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "svc-test-01" {
		t.Fatalf("expected name svc-test-01, got %s", got.Name)
	}
}

func TestServiceList(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testMachine())
	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 machine, got %d", len(list))
	}
}

func TestServiceUpdateSettings(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testMachine())
	newPower := power.PowerConfig{
		Type: power.PowerTypeIPMI,
		IPMI: &power.IPMIConfig{Host: "10.0.0.1", Username: "admin", Password: "pass"},
	}
	updated, err := svc.UpdateSettings(ctx, "svc-test-01", newPower)
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if updated.Power.Type != power.PowerTypeIPMI {
		t.Fatalf("expected power type ipmi, got %s", updated.Power.Type)
	}
}

func TestServiceReinstall(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testMachine())
	m, err := svc.Reinstall(ctx, "svc-test-01", "admin", nil)
	if err != nil {
		t.Fatalf("Reinstall: %v", err)
	}
	if m.Phase != machine.PhaseProvisioning {
		t.Fatalf("expected phase Provisioning, got %s", m.Phase)
	}
	if m.Provision == nil || m.Provision.RequestedBy != "admin" {
		t.Fatal("expected provision with requestedBy=admin")
	}
	if !m.Provision.Active {
		t.Fatal("expected provision.active=true")
	}
	if m.Provision.CompletionToken == "" {
		t.Fatal("expected provision.completionToken to be set")
	}
	if m.Provision.AttemptID == "" {
		t.Fatal("expected provision.attemptId to be set")
	}
}

func TestServiceReinstall_AppliesSpecOverrides(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()

	m := testMachine()
	m.Hostname = "old-host"
	m.OSPreset = machine.OSPreset{Family: machine.OSTypeUbuntu, Version: "24.04", ImageRef: "ubuntu-24.04"}
	m.CloudInitRefs = []string{"ci-old"}
	m.LastDeployedCloudInitRef = "ci-old"
	m.SubnetRef = "subnet-old"
	m.IPAssignment = machine.IPAssignmentModeStatic
	m.IP = "192.0.2.10"
	m.Network = machine.NetworkConfig{Domain: "old.example"}
	m.Arch = "amd64"
	m.Firmware = machine.FirmwareUEFI
	m.Power = power.PowerConfig{Type: power.PowerTypeManual}
	m.Role = machine.RoleDefault
	m.BridgeName = ""
	if err := svc.Store().Upsert(ctx, m); err != nil {
		t.Fatalf("seed machine: %v", err)
	}

	cloudInitRefs := []string{"  ci-new  "}
	ipAssignment := machine.IPAssignmentModeDHCP
	subnetRef := "subnet-new"
	hostname := "new-host"
	mac := "52:54:00:00:00:99"
	arch := "AARCH64"
	firmware := machine.FirmwareBIOS
	powerCfg := power.PowerConfig{Type: power.PowerTypeWebhook, Webhook: &power.WebhookConfig{PowerOnURL: "https://power/on", PowerOffURL: "https://power/off"}}
	role := machine.RoleHypervisor
	bridgeName := "br-ex"
	targetDisk := "/dev/disk/by-id/current-disk"
	updated, err := svc.Reinstall(ctx, "svc-test-01", "admin", &machine.ReinstallOptions{
		Hostname:   &hostname,
		MAC:        &mac,
		Arch:       &arch,
		Firmware:   &firmware,
		Power:      &powerCfg,
		OSPreset:   &machine.OSPreset{Family: machine.OSTypeDebian, Version: "13", ImageRef: "debian-13-amd64"},
		TargetDisk: &targetDisk,
		Network: &machine.NetworkConfig{
			Domain: "new.example",
		},
		CloudInitRefs: &cloudInitRefs,
		IPAssignment:  &ipAssignment,
		SubnetRef:     &subnetRef,
		Role:          &role,
		BridgeName:    &bridgeName,
	})
	if err != nil {
		t.Fatalf("Reinstall: %v", err)
	}

	if updated.Hostname != "new-host" {
		t.Fatalf("expected hostname=new-host, got %q", updated.Hostname)
	}
	if updated.MAC != "52:54:00:00:00:99" {
		t.Fatalf("expected mac override to be applied, got %q", updated.MAC)
	}
	if updated.Arch != "arm64" {
		t.Fatalf("expected arch=arm64, got %q", updated.Arch)
	}
	if updated.Firmware != machine.FirmwareBIOS {
		t.Fatalf("expected firmware=bios, got %q", updated.Firmware)
	}
	if updated.Power.Type != power.PowerTypeWebhook || updated.Power.Webhook == nil || updated.Power.Webhook.PowerOnURL != "https://power/on" {
		t.Fatalf("expected power override to be applied, got %+v", updated.Power)
	}
	if updated.OSPreset.Family != machine.OSTypeDebian || updated.OSPreset.Version != "13" || updated.OSPreset.ImageRef != "debian-13-amd64" {
		t.Fatalf("expected osPreset override to be applied, got %+v", updated.OSPreset)
	}
	if updated.TargetDisk != "/dev/disk/by-id/current-disk" {
		t.Fatalf("expected targetDisk override to be applied, got %q", updated.TargetDisk)
	}
	if updated.Network.Domain != "new.example" {
		t.Fatalf("expected domain=new.example, got %q", updated.Network.Domain)
	}
	if updated.SubnetRef != "subnet-new" {
		t.Fatalf("expected subnetRef=subnet-new, got %q", updated.SubnetRef)
	}
	if updated.IPAssignment != machine.IPAssignmentModeDHCP {
		t.Fatalf("expected ipAssignment=dhcp, got %q", updated.IPAssignment)
	}
	if updated.IP != "" {
		t.Fatalf("expected static IP to be cleared, got %q", updated.IP)
	}
	if len(updated.CloudInitRefs) != 1 || updated.CloudInitRefs[0] != "ci-new" {
		t.Fatalf("expected cloudInitRefs=[ci-new], got %v", updated.CloudInitRefs)
	}
	if updated.LastDeployedCloudInitRef != "ci-new" {
		t.Fatalf("expected lastDeployedCloudInitRef=ci-new, got %q", updated.LastDeployedCloudInitRef)
	}
	if updated.Role != machine.RoleHypervisor {
		t.Fatalf("expected role=hypervisor, got %q", updated.Role)
	}
	if updated.BridgeName != "br-ex" {
		t.Fatalf("expected bridgeName=br-ex, got %q", updated.BridgeName)
	}
}

func TestServiceReinstall_BackfillsLastDeployedCloudInitRef(t *testing.T) {
	tests := []struct {
		name                 string
		cloudInitRefs        []string
		legacyCloudInitRef   string
		existingLastDeployed string
		want                 string
	}{
		{
			name:          "from cloudInitRefs",
			cloudInitRefs: []string{"", "  ci-01  ", "ci-02"},
			want:          "ci-01",
		},
		{
			name:               "from legacy cloudInitRef",
			legacyCloudInitRef: "  ci-legacy  ",
			want:               "ci-legacy",
		},
		{
			name:                 "keep existing",
			cloudInitRefs:        []string{"ci-01"},
			existingLastDeployed: "ci-current",
			want:                 "ci-current",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newTestService()
			ctx := context.Background()

			m := testMachine()
			m.CloudInitRefs = tt.cloudInitRefs
			m.CloudInitRef = tt.legacyCloudInitRef
			m.LastDeployedCloudInitRef = tt.existingLastDeployed
			if err := svc.Store().Upsert(ctx, m); err != nil {
				t.Fatalf("seed machine: %v", err)
			}

			got, err := svc.Reinstall(ctx, "svc-test-01", "admin", nil)
			if err != nil {
				t.Fatalf("Reinstall: %v", err)
			}
			if got.LastDeployedCloudInitRef != tt.want {
				t.Fatalf("expected lastDeployedCloudInitRef %q, got %q", tt.want, got.LastDeployedCloudInitRef)
			}

			saved, err := svc.Get(ctx, "svc-test-01")
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if saved.LastDeployedCloudInitRef != tt.want {
				t.Fatalf("expected saved lastDeployedCloudInitRef %q, got %q", tt.want, saved.LastDeployedCloudInitRef)
			}
		})
	}
}

func TestServiceDelete(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testMachine())
	if err := svc.Delete(ctx, "svc-test-01"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := svc.Get(ctx, "svc-test-01")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestServiceGetByMAC(t *testing.T) {
	svc := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testMachine())
	got, err := svc.GetByMAC(ctx, "AA:BB:CC:DD:EE:01")
	if err != nil {
		t.Fatalf("GetByMAC: %v", err)
	}
	if got.Name != "svc-test-01" {
		t.Fatalf("expected name svc-test-01, got %s", got.Name)
	}
}
