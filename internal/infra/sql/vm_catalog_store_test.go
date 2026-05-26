package sql_test

import (
	"context"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/vm"
	"testing"
	"time"
)

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
			BootModes: []string{"bios", "uefi"},
			Root: osimage.RootArtifact{
				Format: osimage.FormatQCOW2,
				Path:   "root.qcow2",
				SHA256: "root-sha",
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
