package sql_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	infrasql "github.com/sugaf1204/gomi/internal/infra/sql"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/pxe"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/sshkey"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
)

func newTestBackend(t *testing.T) *infrasql.Backend {
	t.Helper()
	b, err := infrasql.New("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := b.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func TestMachineStore(t *testing.T) {
	b := newTestBackend(t)
	s := b.Machines()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	m := machine.Machine{
		Name:      "srv1",
		Hostname:  "srv1.local",
		MAC:       "AA:BB:CC:DD:EE:01",
		Arch:      "amd64",
		Firmware:  machine.FirmwareUEFI,
		Power:     power.PowerConfig{Type: power.PowerTypeManual},
		Phase:     machine.PhaseReady,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.Upsert(ctx, m); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := s.Get(ctx, "srv1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Hostname != "srv1.local" {
		t.Errorf("hostname = %q, want srv1.local", got.Hostname)
	}

	// GetByMAC (case-insensitive)
	got, err = s.GetByMAC(ctx, "aa:bb:cc:dd:ee:01")
	if err != nil {
		t.Fatalf("GetByMAC: %v", err)
	}
	if got.Name != "srv1" {
		t.Errorf("GetByMAC name = %q, want srv1", got.Name)
	}

	// List
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("List len = %d, want 1", len(list))
	}

	// Delete
	if err := s.Delete(ctx, "srv1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = s.Get(ctx, "srv1")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("Get after delete: got %v, want ErrNotFound", err)
	}

	// Delete non-existent
	if err := s.Delete(ctx, "nope"); !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("Delete non-existent: got %v, want ErrNotFound", err)
	}
}

func TestMachineStorePartialMachineUpdatesPreserveUnrelatedFields(t *testing.T) {
	b := newTestBackend(t)
	s := b.Machines()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	stateAt := now.Add(1 * time.Minute)
	finishedAt := now.Add(2 * time.Minute)

	m := machine.Machine{
		Name:                     "srv-partial",
		Hostname:                 "srv-partial.local",
		MAC:                      "AA:BB:CC:DD:EE:02",
		IP:                       "192.0.2.10",
		Arch:                     "amd64",
		Firmware:                 machine.FirmwareUEFI,
		Power:                    power.PowerConfig{Type: power.PowerTypeManual},
		Phase:                    machine.PhaseProvisioning,
		Provision:                &machine.ProvisionProgress{Active: true, AttemptID: "attempt-partial", FinishedAt: &finishedAt},
		LastPowerAction:          "power-off",
		LastDeployedCloudInitRef: "cloudinit-a",
		LastError:                "old-error",
		PowerState:               power.PowerStateRunning,
		PowerStateAt:             &stateAt,
		CreatedAt:                now,
		UpdatedAt:                now,
	}
	if err := s.Upsert(ctx, m); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	if err := s.UpdatePowerActionStatus(ctx, m.Name, power.ActionPowerOn, nil, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("UpdatePowerActionStatus nil error: %v", err)
	}
	got, err := s.Get(ctx, m.Name)
	if err != nil {
		t.Fatalf("Get after action update: %v", err)
	}
	if got.LastPowerAction != string(power.ActionPowerOn) {
		t.Fatalf("LastPowerAction = %q, want power-on", got.LastPowerAction)
	}
	if got.LastError != "old-error" {
		t.Fatalf("LastError changed on nil update: %q", got.LastError)
	}
	if got.Provision == nil || got.Provision.AttemptID != "attempt-partial" || !got.Provision.Active {
		t.Fatalf("Provision was not preserved: %#v", got.Provision)
	}
	if got.LastDeployedCloudInitRef != "cloudinit-a" {
		t.Fatalf("LastDeployedCloudInitRef = %q", got.LastDeployedCloudInitRef)
	}
	if got.PowerState != power.PowerStateRunning || got.PowerStateAt == nil || !got.PowerStateAt.Equal(stateAt) {
		t.Fatalf("power state changed unexpectedly: state=%q at=%v", got.PowerState, got.PowerStateAt)
	}

	nextErr := "next-error"
	if err := s.UpdatePowerActionStatus(ctx, m.Name, power.ActionPowerOff, &nextErr, now.Add(4*time.Minute)); err != nil {
		t.Fatalf("UpdatePowerActionStatus error: %v", err)
	}
	got, err = s.Get(ctx, m.Name)
	if err != nil {
		t.Fatalf("Get after error update: %v", err)
	}
	if got.LastPowerAction != string(power.ActionPowerOff) || got.LastError != nextErr {
		t.Fatalf("power action fields not updated: action=%q error=%q", got.LastPowerAction, got.LastError)
	}

	nextStateAt := now.Add(5 * time.Minute)
	if err := s.UpdatePowerStateStatus(ctx, m.Name, power.PowerStateStopped, nextStateAt, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("UpdatePowerStateStatus: %v", err)
	}
	got, err = s.Get(ctx, m.Name)
	if err != nil {
		t.Fatalf("Get after state update: %v", err)
	}
	if got.PowerState != power.PowerStateStopped || got.PowerStateAt == nil || !got.PowerStateAt.Equal(nextStateAt) {
		t.Fatalf("power state fields not updated: state=%q at=%v", got.PowerState, got.PowerStateAt)
	}
	if got.LastPowerAction != string(power.ActionPowerOff) || got.LastError != nextErr {
		t.Fatalf("power action fields changed during state update: action=%q error=%q", got.LastPowerAction, got.LastError)
	}
	if got.Provision == nil || got.Provision.AttemptID != "attempt-partial" {
		t.Fatalf("Provision changed during state update: %#v", got.Provision)
	}

	if err := s.UpdateDynamicIPAddress(ctx, m.Name, m.MAC, "192.0.2.11", now.Add(6*time.Minute)); err != nil {
		t.Fatalf("UpdateDynamicIPAddress: %v", err)
	}
	got, err = s.Get(ctx, m.Name)
	if err != nil {
		t.Fatalf("Get after IP update: %v", err)
	}
	if got.IP != "192.0.2.11" {
		t.Fatalf("IP = %q, want 192.0.2.11", got.IP)
	}
	if got.LastPowerAction != string(power.ActionPowerOff) || got.LastError != nextErr || got.PowerState != power.PowerStateStopped {
		t.Fatalf("status changed during IP update: action=%q error=%q state=%q", got.LastPowerAction, got.LastError, got.PowerState)
	}

	if err := s.UpdateDynamicIPAddress(ctx, m.Name, "AA:BB:CC:DD:EE:FF", "192.0.2.12", now.Add(7*time.Minute)); err != nil {
		t.Fatalf("UpdateDynamicIPAddress mismatched MAC: %v", err)
	}
	got, err = s.Get(ctx, m.Name)
	if err != nil {
		t.Fatalf("Get after mismatched MAC update: %v", err)
	}
	if got.IP != "192.0.2.11" {
		t.Fatalf("IP changed despite MAC mismatch: %q", got.IP)
	}

	got.IPAssignment = machine.IPAssignmentModeStatic
	got.IP = "192.0.2.50"
	if err := s.Upsert(ctx, got); err != nil {
		t.Fatalf("Upsert static machine: %v", err)
	}
	if err := s.UpdateDynamicIPAddress(ctx, m.Name, m.MAC, "192.0.2.13", now.Add(8*time.Minute)); err != nil {
		t.Fatalf("UpdateDynamicIPAddress static machine: %v", err)
	}
	got, err = s.Get(ctx, m.Name)
	if err != nil {
		t.Fatalf("Get after static IP update: %v", err)
	}
	if got.IP != "192.0.2.50" {
		t.Fatalf("static IP changed during dynamic IP update: %q", got.IP)
	}

	if err := s.UpdatePowerActionStatus(ctx, "missing", power.ActionPowerOn, nil, now); !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("UpdatePowerActionStatus missing = %v, want ErrNotFound", err)
	}
	if err := s.UpdatePowerStateStatus(ctx, "missing", power.PowerStateStopped, now, now); !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("UpdatePowerStateStatus missing = %v, want ErrNotFound", err)
	}
	if err := s.UpdateDynamicIPAddress(ctx, "missing", m.MAC, "192.0.2.12", now); !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("UpdateDynamicIPAddress missing = %v, want ErrNotFound", err)
	}
}

func TestSubnetStore(t *testing.T) {
	b := newTestBackend(t)
	s := b.Subnets()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	sub := subnet.Subnet{
		Name:      "main",
		Spec:      subnet.SubnetSpec{CIDR: "10.0.0.0/24", DefaultGateway: "10.0.0.1"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.Upsert(ctx, sub); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := s.Get(ctx, "main")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Spec.CIDR != "10.0.0.0/24" {
		t.Errorf("CIDR = %q, want 10.0.0.0/24", got.Spec.CIDR)
	}
}

func TestSubnetChangeNotifier(t *testing.T) {
	b := newTestBackend(t)
	s := b.Subnets()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	var called atomic.Int32
	s.Subscribe(func() { called.Add(1) })

	sub := subnet.Subnet{
		Name:      "test",
		Spec:      subnet.SubnetSpec{CIDR: "10.1.0.0/24"},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.Upsert(ctx, sub); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Give goroutine time to fire
	time.Sleep(50 * time.Millisecond)
	if called.Load() == 0 {
		t.Error("ChangeNotifier was not called on Upsert")
	}
}

func TestAuthStore(t *testing.T) {
	b := newTestBackend(t)
	s := b.Auth()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// User
	user := auth.User{Username: "admin", PasswordHash: "hash", Role: auth.RoleAdmin, CreatedAt: now}
	if err := s.UpsertUser(ctx, user); err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	got, err := s.GetUser(ctx, "admin")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.Role != auth.RoleAdmin {
		t.Errorf("role = %q, want admin", got.Role)
	}

	// Session
	sess := auth.Session{Token: "tok1", Username: "admin", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := s.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	gotSess, err := s.GetSession(ctx, "tok1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if gotSess.Username != "admin" {
		t.Errorf("session username = %q", gotSess.Username)
	}

	// Expired session
	expired := auth.Session{Token: "tok2", Username: "admin", CreatedAt: now, ExpiresAt: now.Add(-time.Hour)}
	_ = s.CreateSession(ctx, expired)
	_, err = s.GetSession(ctx, "tok2")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("expired session: got %v, want ErrNotFound", err)
	}

	// AuditEvent
	event := auth.AuditEvent{
		ID: "ev1", Machine: "srv1",
		Action: "create", Actor: "admin", Result: "ok",
		CreatedAt: now,
	}
	if err := s.CreateAuditEvent(ctx, event); err != nil {
		t.Fatalf("CreateAuditEvent: %v", err)
	}
	events, err := s.ListAuditEvents(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("events len = %d, want 1", len(events))
	}
}

func TestSSHKeyStore(t *testing.T) {
	b := newTestBackend(t)
	s := b.SSHKeys()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	k := sshkey.SSHKey{
		Name:      "key1",
		PublicKey: "ssh-ed25519 AAAA...",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.Upsert(ctx, k); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := s.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.PublicKey != k.PublicKey {
		t.Errorf("PublicKey = %q, want %q", got.PublicKey, k.PublicKey)
	}

	keys, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("List len = %d, want 1", len(keys))
	}
}

func TestHWInfoStore(t *testing.T) {
	b := newTestBackend(t)
	s := b.HWInfo()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	info := hwinfo.HardwareInfo{
		Name:        "srv1",
		MachineName: "srv1",
		AttemptID:   "attempt-srv1",
		CPU:         hwinfo.CPUInfo{Model: "Xeon", Cores: 8},
		Disks: []hwinfo.DiskInfo{
			{
				Name:   "nvme0n1",
				Path:   "/dev/nvme0n1",
				ByID:   []string{"/dev/disk/by-id/nvme-test"},
				Type:   "disk",
				SizeMB: 1024,
			},
		},
		NICs: []hwinfo.NICInfo{
			{
				Name:     "enp1s0",
				MAC:      "00:11:22:33:44:55",
				Driver:   "igc",
				Modalias: "pci:v00008086d000015F3",
			},
		},
		Boot:      hwinfo.BootInfo{FirmwareMode: "uefi", EFIVars: true},
		Runtime:   hwinfo.RuntimeInfo{KernelVersion: "6.8.0-00-generic", LoadedModules: []string{"igc"}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.Upsert(ctx, info); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := s.Get(ctx, "srv1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CPU.Cores != 8 {
		t.Errorf("CPU cores = %d, want 8", got.CPU.Cores)
	}
	if got.AttemptID != "attempt-srv1" {
		t.Fatalf("attempt id was not preserved: %q", got.AttemptID)
	}
	if len(got.Disks) != 1 || got.Disks[0].ByID[0] != "/dev/disk/by-id/nvme-test" {
		t.Fatalf("disk inventory was not preserved: %+v", got.Disks)
	}
	if len(got.NICs) != 1 || got.NICs[0].Driver != "igc" {
		t.Fatalf("nic inventory was not preserved: %+v", got.NICs)
	}
	if got.Boot.FirmwareMode != "uefi" || got.Runtime.KernelVersion != "6.8.0-00-generic" {
		t.Fatalf("boot/runtime inventory was not preserved: boot=%+v runtime=%+v", got.Boot, got.Runtime)
	}
}

func TestHypervisorStore(t *testing.T) {
	b := newTestBackend(t)
	s := b.Hypervisors()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	h := hypervisor.Hypervisor{
		Name:       "hv1",
		Connection: hypervisor.ConnectionSpec{Type: hypervisor.ConnectionTCP, Host: "10.0.0.5"},
		Phase:      hypervisor.PhaseReady,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.Upsert(ctx, h); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].Connection.Host != "10.0.0.5" {
		t.Errorf("unexpected list result")
	}
}

func TestRegTokenStore(t *testing.T) {
	b := newTestBackend(t)
	s := b.HypervisorTokens()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	tok := hypervisor.RegistrationToken{
		Token:     "tok-abc",
		CreatedAt: now,
		ExpiresAt: now.Add(time.Hour),
	}
	if err := s.Create(ctx, tok); err != nil {
		t.Fatalf("Create: %v", err)
	}

	used, err := s.MarkUsed(ctx, "tok-abc", "hv1")
	if err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}
	if !used.Used || used.UsedBy != "hv1" {
		t.Errorf("MarkUsed: used=%v usedBy=%q", used.Used, used.UsedBy)
	}

	// MarkUsed again should fail
	_, err = s.MarkUsed(ctx, "tok-abc", "hv2")
	if err == nil {
		t.Error("MarkUsed twice should fail")
	}
}

func TestVMStore(t *testing.T) {
	b := newTestBackend(t)
	s := b.VMs()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	v := vm.VirtualMachine{
		Name:          "vm1",
		HypervisorRef: "hv1",
		Resources:     vm.ResourceSpec{CPUCores: 4},
		Phase:         vm.PhaseRunning,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.Upsert(ctx, v); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	byHV, err := s.ListByHypervisor(ctx, "hv1")
	if err != nil {
		t.Fatalf("ListByHypervisor: %v", err)
	}
	if len(byHV) != 1 {
		t.Errorf("ListByHypervisor len = %d, want 1", len(byHV))
	}
}

func TestCloudInitStore(t *testing.T) {
	b := newTestBackend(t)
	s := b.CloudInits()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	tmpl := cloudinit.CloudInitTemplate{
		Name:      "basic",
		UserData:  "#cloud-config\npackages:\n  - vim",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.Upsert(ctx, tmpl); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := s.Get(ctx, "basic")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserData == "" {
		t.Error("UserData should not be empty")
	}
}

func TestOSImageStore(t *testing.T) {
	b := newTestBackend(t)
	s := b.OSImages()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	img := osimage.OSImage{
		Name:      "ubuntu-22.04",
		OSFamily:  "ubuntu",
		OSVersion: "22.04",
		Format:    osimage.FormatQCOW2,
		Ready:     true,
		Manifest: &osimage.Manifest{
			SchemaVersion: "gomi.osimage.v1",
			BootModes:     []string{"bios", "uefi"},
			Root: osimage.RootArtifact{
				Format:      osimage.FormatRAW,
				Compression: "zst",
				Path:        "root.raw.zst",
				SHA256:      "root-sha",
			},
			TargetKernel: osimage.TargetKernel{Version: "5.15.0-176-generic"},
			Bundles: []osimage.Bundle{
				{
					ID:            "modules-extra-net",
					Type:          "kernel-modules",
					KernelVersion: "5.15.0-176-generic",
					Path:          "modules/5.15.0-176-generic/modules-extra-net.tar.zst",
				},
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.Upsert(ctx, img); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("List len = %d, want 1", len(list))
	}
	if list[0].Manifest == nil || list[0].Manifest.TargetKernel.Version != "5.15.0-176-generic" {
		t.Fatalf("manifest was not preserved: %+v", list[0].Manifest)
	}
}

func TestDHCPLeaseStore(t *testing.T) {
	b := newTestBackend(t)
	s := b.DHCPLeases()
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	lease := pxe.DHCPLease{
		MAC: "AA:BB:CC:DD:EE:01", IP: "10.0.0.100",
		Hostname: "srv1", PXEClient: true, LeasedAt: now,
	}
	if err := s.Upsert(ctx, lease); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || !list[0].PXEClient {
		t.Errorf("unexpected lease list: %+v", list)
	}

	if err := s.Delete(ctx, "AA:BB:CC:DD:EE:01"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, _ = s.List(ctx)
	if len(list) != 0 {
		t.Errorf("after delete len = %d", len(list))
	}
}

func TestHealth(t *testing.T) {
	b := newTestBackend(t)
	if err := b.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
}
