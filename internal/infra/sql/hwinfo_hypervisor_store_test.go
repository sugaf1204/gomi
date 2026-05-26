package sql_test

import (
	"context"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	"testing"
	"time"
)

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
