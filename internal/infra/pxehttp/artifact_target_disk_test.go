package pxehttp

import (
	"context"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPXEArtifact_OnlyServesManifestArtifactsAndRejectsSymlinkEscape(t *testing.T) {
	backend := memory.New()
	osImageSvc := osimage.NewService(backend.OSImages())
	artifactDir := t.TempDir()
	outsideDir := t.TempDir()
	now := time.Now().UTC()

	if err := os.WriteFile(filepath.Join(artifactDir, "rootfs.squashfs"), []byte("root"), 0o644); err != nil {
		t.Fatalf("write root artifact: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactDir, "debug.log"), []byte("debug"), 0o644); err != nil {
		t.Fatalf("write non-manifest artifact: %v", err)
	}
	modulePath := filepath.Join(artifactDir, "modules", "6.12.0-13-amd64")
	if err := os.MkdirAll(modulePath, 0o755); err != nil {
		t.Fatalf("mkdir modules: %v", err)
	}
	secretPath := filepath.Join(outsideDir, "secret.tar.zst")
	if err := os.WriteFile(secretPath, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write outside artifact: %v", err)
	}
	if err := os.Symlink(secretPath, filepath.Join(modulePath, "modules-extra-net.tar.zst")); err != nil {
		t.Fatalf("symlink module artifact: %v", err)
	}

	img := osimage.OSImage{
		Name:      "debian-13-amd64",
		OSFamily:  "debian",
		OSVersion: "13",
		Arch:      "amd64",
		Format:    osimage.FormatSquashFS,
		Source:    osimage.SourceUpload,
		Ready:     true,
		LocalPath: artifactDir,
		Manifest: &osimage.Manifest{
			BootModes: []string{"bios", "uefi"},
			Root: osimage.RootArtifact{
				Format: osimage.FormatSquashFS,
				Path:   "rootfs.squashfs",
				SHA256: "root-sha",
			},
			TargetKernel: osimage.TargetKernel{Version: "6.12.0-13-amd64"},
			Bundles: []osimage.Bundle{
				{
					ID:              "kernel-modules-6.12.0-13-amd64",
					Type:            "kernel-modules",
					KernelVersion:   "6.12.0-13-amd64",
					Path:            "modules/6.12.0-13-amd64/modules-extra-net.tar.zst",
					SHA256:          "modules-sha",
					ProvidesModules: []string{"e1000e", "igc", "r8169"},
				},
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.OSImages().Upsert(context.Background(), img); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	h := &Handler{osimages: osImageSvc}

	t.Run("serves manifest root", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/pxe/artifacts/os-images/debian-13-amd64/rootfs.squashfs", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("name", "*")
		c.SetParamValues("debian-13-amd64", "rootfs.squashfs")

		if err := h.PXEArtifact(c); err != nil {
			t.Fatalf("PXEArtifact: %v", err)
		}
		if rec.Code != http.StatusOK || rec.Body.String() != "root" {
			t.Fatalf("unexpected response: status=%d body=%q", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects file outside manifest", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/pxe/artifacts/os-images/debian-13-amd64/debug.log", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("name", "*")
		c.SetParamValues("debian-13-amd64", "debug.log")

		if err := h.PXEArtifact(c); err != nil {
			t.Fatalf("PXEArtifact: %v", err)
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects symlink escape even when manifest-listed", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/pxe/artifacts/os-images/debian-13-amd64/modules/6.12.0-13-amd64/modules-extra-net.tar.zst", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetParamNames("name", "*")
		c.SetParamValues("debian-13-amd64", "modules/6.12.0-13-amd64/modules-extra-net.tar.zst")

		if err := h.PXEArtifact(c); err != nil {
			t.Fatalf("PXEArtifact: %v", err)
		}
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func TestTargetDiskValidationAndPartitionPaths(t *testing.T) {
	wholeDisks := []string{
		"/dev/sda",
		"/dev/vda",
		"/dev/xvda",
		"/dev/nvme0n1",
		"/dev/mmcblk0",
		"/dev/disk/by-id/nvme-GOMI_TEST",
		"/dev/disk/by-path/pci-0000:00:1f.2-ata-1",
	}
	for _, disk := range wholeDisks {
		if !isWholeDiskPath(disk) {
			t.Fatalf("expected whole disk path to be allowed: %s", disk)
		}
	}
	partitions := []string{
		"/dev/sda1",
		"/dev/vda2",
		"/dev/xvda3",
		"/dev/nvme0n1p1",
		"/dev/mmcblk0p2",
		"/dev/disk/by-id/nvme-GOMI_TEST-part1",
		"/dev/disk/by-path/pci-0000:00:1f.2-ata-1-part2",
	}
	for _, disk := range partitions {
		if isWholeDiskPath(disk) {
			t.Fatalf("expected partition path to be rejected: %s", disk)
		}
		m := &machine.Machine{TargetDisk: disk}
		if _, err := selectTargetDisk(m, &hwinfo.HardwareInfo{}); err == nil {
			t.Fatalf("expected target disk override to be rejected: %s", disk)
		}
	}

	overrideInfo := &hwinfo.HardwareInfo{Disks: []hwinfo.DiskInfo{{
		Name:   "sda",
		Path:   "/dev/sda",
		ByID:   []string{"/dev/disk/by-id/current-disk"},
		ByPath: []string{"/dev/disk/by-path/current-disk"},
		Type:   "disk",
	}}}
	overrideMachine := &machine.Machine{TargetDisk: "/dev/disk/by-id/current-disk"}
	if got, err := selectTargetDisk(overrideMachine, overrideInfo); err != nil || got != "/dev/disk/by-id/current-disk" {
		t.Fatalf("expected inventory-backed target override, got disk=%q err=%v", got, err)
	}
	staleOverride := &machine.Machine{TargetDisk: "/dev/disk/by-id/stale-disk"}
	if _, err := selectTargetDisk(staleOverride, overrideInfo); err == nil {
		t.Fatal("expected stale target disk override to be rejected")
	}
}
