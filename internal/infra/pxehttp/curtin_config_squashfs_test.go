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
	"strings"
	"testing"
	"time"
)

func TestPXECurtinConfig_SquashFSImageUsesFSImageAndStorageConfig(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	hwInfoSvc := hwinfo.NewService(backend.HWInfo())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()

	img := osimage.OSImage{
		Name:      "ubuntu-22.04-amd64-baremetal",
		OSFamily:  "ubuntu",
		OSVersion: "22.04",
		Arch:      "amd64",
		Format:    osimage.FormatSquashFS,
		Source:    osimage.SourceURL,
		Ready:     true,
		LocalPath: "/var/lib/gomi/data/images/ubuntu-22.04-amd64-baremetal",
		Manifest: &osimage.Manifest{
			Root: osimage.RootArtifact{
				Format: osimage.FormatSquashFS,
				Path:   "rootfs.squashfs",
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.OSImages().Upsert(context.Background(), img); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	target := machine.Machine{
		Name:     "bm-squashfs-plan",
		Hostname: "bm-squashfs-plan",
		MAC:      "52:54:00:aa:bb:13",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family:   machine.OSTypeUbuntu,
			Version:  "22.04",
			ImageRef: "ubuntu-22.04-amd64-baremetal",
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-squashfs-plan",
			CompletionToken: "token-squashfs-plan",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	if _, err := hwInfoSvc.Upsert(context.Background(), hwinfo.HardwareInfo{
		Name:        "bm-squashfs-plan-hwinfo",
		MachineName: "bm-squashfs-plan",
		AttemptID:   "attempt-squashfs-plan",
		Disks: []hwinfo.DiskInfo{
			{
				Name:   "nvme0n1",
				Path:   "/dev/nvme0n1",
				Type:   "disk",
				SizeMB: 65536,
			},
		},
	}); err != nil {
		t.Fatalf("upsert hwinfo: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/curtin-config?token=token-squashfs-plan&attempt_id=attempt-squashfs-plan", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc, hwinfo: hwInfoSvc, osimages: osImageSvc}
	if err := h.PXECurtinConfig(c); err != nil {
		t.Fatalf("PXECurtinConfig: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"type: fsimage",
		"uri: http://192.168.2.254:8080/pxe/artifacts/os-images/ubuntu-22.04-amd64-baremetal/rootfs.squashfs?attempt_id=attempt-squashfs-plan&token=token-squashfs-plan",
		"storage:",
		"ptable: gpt",
		"flag: bios_grub",
		"path: /boot/efi",
		"fstype: ext4",
		"root_fstype='ext4'",
		"root_opts='defaults,errors=remount-ro'",
		"size: 1M",
		"size: 512M",
		"size: 64959M",
		"install_devices:",
		"- /dev/nvme0n1",
		"partitioning_commands:",
		"builtin:",
		"- curtin",
		"- block-meta",
		"- custom",
		"ssh_deletekeys: false",
		"dev/null",
		"mknod -m 666",
		"ssh-keygen -A",
		"grub-install",
		"grub-mkconfig",
		"--target=x86_64-efi",
		"--removable",
		"systemctl enable systemd-networkd",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected curtin squashfs config to contain %q, got:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{
		"- curthooks",
		"package: linux-image-amd64",
		"fallback-package: linux-image-amd64",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("squashfs curtin config must not ask curtin to install packages via %q, got:\n%s", forbidden, body)
		}
	}
	if strings.Contains(body, "size: -1") {
		t.Fatalf("expected concrete root partition size, got:\n%s", body)
	}
}

func TestPXECurtinConfig_DebianSquashFSSkipsCurtinPackageInstall(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	hwInfoSvc := hwinfo.NewService(backend.HWInfo())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()

	img := osimage.OSImage{
		Name:      "debian-13-amd64-baremetal",
		OSFamily:  "debian",
		OSVersion: "13",
		Arch:      "amd64",
		Format:    osimage.FormatSquashFS,
		Source:    osimage.SourceURL,
		Ready:     true,
		LocalPath: "/var/lib/gomi/data/images/debian-13-amd64-baremetal",
		Manifest: &osimage.Manifest{
			Root: osimage.RootArtifact{
				Format: osimage.FormatSquashFS,
				Path:   "rootfs.squashfs",
				RootPartition: osimage.Partition{
					Filesystem: "xfs",
				},
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.OSImages().Upsert(context.Background(), img); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	target := machine.Machine{
		Name:     "bm-debian-squashfs-plan",
		Hostname: "bm-debian-squashfs-plan",
		MAC:      "52:54:00:aa:bb:14",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family:   machine.OSTypeDebian,
			Version:  "13",
			ImageRef: "debian-13-amd64-baremetal",
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-debian-squashfs-plan",
			CompletionToken: "token-debian-squashfs-plan",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	if _, err := hwInfoSvc.Upsert(context.Background(), hwinfo.HardwareInfo{
		Name:        "bm-debian-squashfs-plan-hwinfo",
		MachineName: "bm-debian-squashfs-plan",
		AttemptID:   "attempt-debian-squashfs-plan",
		Disks: []hwinfo.DiskInfo{
			{
				Name:   "nvme0n1",
				Path:   "/dev/nvme0n1",
				Type:   "disk",
				SizeMB: 65536,
			},
		},
	}); err != nil {
		t.Fatalf("upsert hwinfo: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/curtin-config?token=token-debian-squashfs-plan&attempt_id=attempt-debian-squashfs-plan", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc, hwinfo: hwInfoSvc, osimages: osImageSvc}
	if err := h.PXECurtinConfig(c); err != nil {
		t.Fatalf("PXECurtinConfig: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"type: fsimage",
		"grub-install",
		"grub-mkconfig",
		"--target=x86_64-efi",
		"--removable",
		"systemctl enable systemd-networkd",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected debian squashfs curtin config to contain %q, got:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{
		"- curthooks",
		"kernel:",
		"package: linux-image-amd64",
		"fallback-package: linux-image-amd64",
		"--target=i386-pc",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("debian squashfs curtin config must not ask curtin to install packages via %q, got:\n%s", forbidden, body)
		}
	}
}
