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

func TestPXECurtinConfig_FedoraSquashFSUsesFedoraBootloaderCommands(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	hwInfoSvc := hwinfo.NewService(backend.HWInfo())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()

	img := osimage.OSImage{
		Name:      "fedora-44-amd64-baremetal",
		OSFamily:  "fedora",
		OSVersion: "44",
		Arch:      "amd64",
		Format:    osimage.FormatSquashFS,
		Source:    osimage.SourceURL,
		Ready:     true,
		LocalPath: "/var/lib/gomi/data/images/fedora-44-amd64-baremetal",
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
		Name:     "bm-fedora-squashfs-plan",
		Hostname: "bm-fedora-squashfs-plan",
		MAC:      "52:54:00:aa:bb:44",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family:   machine.OSType("fedora"),
			Version:  "44",
			ImageRef: "fedora-44-amd64-baremetal",
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-fedora-squashfs-plan",
			CompletionToken: "token-fedora-squashfs-plan",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	if _, err := hwInfoSvc.Upsert(context.Background(), hwinfo.HardwareInfo{
		Name:        "bm-fedora-squashfs-plan-hwinfo",
		MachineName: "bm-fedora-squashfs-plan",
		AttemptID:   "attempt-fedora-squashfs-plan",
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
	req := httptest.NewRequest(http.MethodGet, "/pxe/curtin-config?token=token-fedora-squashfs-plan&attempt_id=attempt-fedora-squashfs-plan", nil)
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
		"fstype: xfs",
		"root_fstype='xfs'",
		"root_opts='defaults'",
		"grub2-mkconfig",
		"/boot/grub2/grub.cfg",
		"/boot/grub2/gomi.cfg",
		"bootloader_id='fedora'",
		"/boot/efi/EFI/$bootloader_id/grub.cfg",
		"/boot/efi/EFI/BOOT/grub.cfg",
		"configfile \\$prefix/gomi.cfg",
		"/boot/efi/EFI/$bootloader_id/grubx64.efi",
		"/boot/efi/EFI/BOOT/BOOTX64.EFI",
		"/usr/lib/efi/grub2",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected fedora squashfs curtin config to contain %q, got:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{
		"grub2-install",
		`dirname '/boot/grub2/gomi.cfg'`,
		`cat > '/boot/grub2/gomi.cfg'`,
		"--target=x86_64-efi",
		"--removable",
		"--bootloader-id='fedora'",
		"root_fstype='ext4'",
		"root_opts='defaults,errors=remount-ro'",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("fedora UEFI squashfs config must avoid unsupported UEFI grub2-install path %q, got:\n%s", forbidden, body)
		}
	}
	if strings.Contains(body, "- curthooks") {
		t.Fatalf("fedora squashfs curtin config must not run curthooks, got:\n%s", body)
	}
}

func TestPXECurtinConfig_FedoraBIOSSquashFSEmbedsMinimalGrubConfig(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	hwInfoSvc := hwinfo.NewService(backend.HWInfo())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()

	img := osimage.OSImage{
		Name:      "fedora-44-amd64-baremetal",
		OSFamily:  "fedora",
		OSVersion: "44",
		Arch:      "amd64",
		Format:    osimage.FormatSquashFS,
		Source:    osimage.SourceURL,
		Ready:     true,
		LocalPath: "/var/lib/gomi/data/images/fedora-44-amd64-baremetal",
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
		Name:     "bm-fedora-bios-plan",
		Hostname: "bm-fedora-bios-plan",
		MAC:      "52:54:00:aa:bb:45",
		Arch:     "amd64",
		Firmware: machine.FirmwareBIOS,
		OSPreset: machine.OSPreset{
			Family:   machine.OSType("fedora"),
			Version:  "44",
			ImageRef: "fedora-44-amd64-baremetal",
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-fedora-bios-plan",
			CompletionToken: "token-fedora-bios-plan",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	if _, err := hwInfoSvc.Upsert(context.Background(), hwinfo.HardwareInfo{
		Name:        "bm-fedora-bios-plan-hwinfo",
		MachineName: "bm-fedora-bios-plan",
		AttemptID:   "attempt-fedora-bios-plan",
		Disks: []hwinfo.DiskInfo{
			{
				Name:   "vda",
				Path:   "/dev/vda",
				Type:   "disk",
				SizeMB: 32768,
			},
		},
	}); err != nil {
		t.Fatalf("upsert hwinfo: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/curtin-config?token=token-fedora-bios-plan&attempt_id=attempt-fedora-bios-plan", nil)
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
		"/boot/grub2/gomi.cfg",
		"root=LABEL=rootfs rw",
		"fstype: xfs",
		"root_fstype='xfs'",
		"root_opts='defaults'",
		"grub2-mkimage",
		"part_gpt 'xfs' search",
		"gomi-grub-bootstrap.cfg",
		"configfile (\\$root)$config_path",
		"sh '/boot/grub2/gomi.cfg'",
		"grub2-bios-setup",
		"-d /boot/grub2/i386-pc",
		"/dev/vda",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected fedora bios curtin config to contain %q, got:\n%s", want, body)
		}
	}
	for _, forbidden := range []string{
		"--target=x86_64-efi",
		"--removable",
		"- curthooks",
		"package: linux-image-amd64",
		"BIOS grub install skipped",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("fedora bios curtin config must not contain %q, got:\n%s", forbidden, body)
		}
	}
}

func TestBuildCurtinInstallConfigRejectsNotReadyManifestImage(t *testing.T) {
	now := time.Now().UTC()
	img := osimage.OSImage{
		Name:      "ubuntu-22.04-amd64-baremetal",
		OSFamily:  "ubuntu",
		OSVersion: "22.04",
		Arch:      "amd64",
		Format:    osimage.FormatSquashFS,
		Source:    osimage.SourceURL,
		Ready:     false,
		Manifest: &osimage.Manifest{
			Root: osimage.RootArtifact{
				Format: osimage.FormatSquashFS,
				Path:   "rootfs.squashfs",
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	target := &machine.Machine{
		Name:     "bm-squashfs-not-ready",
		Hostname: "bm-squashfs-not-ready",
		MAC:      "52:54:00:aa:bb:14",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		Phase:    machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-squashfs-not-ready",
			CompletionToken: "token-squashfs-not-ready",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	info := &hwinfo.HardwareInfo{
		MachineName: target.Name,
		AttemptID:   "attempt-squashfs-not-ready",
		Disks: []hwinfo.DiskInfo{
			{
				Name:   "vda",
				Path:   "/dev/vda",
				Type:   "disk",
				SizeMB: 32768,
			},
		},
	}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/curtin-config?token=token-squashfs-not-ready&attempt_id=attempt-squashfs-not-ready", nil)
	req.Host = "192.168.2.254:8080"
	c := e.NewContext(req, httptest.NewRecorder())

	h := &Handler{}
	_, err := h.buildCurtinInstallConfig(context.Background(), c, target, img, info)
	if err == nil || !strings.Contains(err.Error(), `os image "ubuntu-22.04-amd64-baremetal" is not ready`) {
		t.Fatalf("expected not-ready error for manifest image, got %v", err)
	}
}

func TestBuildCurtinInstallConfigRejectsUnsupportedOSFamily(t *testing.T) {
	now := time.Now().UTC()
	img := osimage.OSImage{
		Name:      "rocky-9-amd64-baremetal",
		OSFamily:  "rocky",
		OSVersion: "9",
		Arch:      "amd64",
		Format:    osimage.FormatSquashFS,
		Source:    osimage.SourceURL,
		Ready:     true,
		Manifest: &osimage.Manifest{
			Root: osimage.RootArtifact{
				Format: osimage.FormatSquashFS,
				Path:   "rootfs.squashfs",
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	target := &machine.Machine{
		Name:     "bm-rocky-squashfs-plan",
		Hostname: "bm-rocky-squashfs-plan",
		MAC:      "52:54:00:aa:bb:15",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		Phase:    machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-rocky-squashfs-plan",
			CompletionToken: "token-rocky-squashfs-plan",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	info := &hwinfo.HardwareInfo{
		MachineName: target.Name,
		AttemptID:   "attempt-rocky-squashfs-plan",
		Disks: []hwinfo.DiskInfo{
			{
				Name:   "vda",
				Path:   "/dev/vda",
				Type:   "disk",
				SizeMB: 32768,
			},
		},
	}
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/curtin-config?token=token-rocky-squashfs-plan&attempt_id=attempt-rocky-squashfs-plan", nil)
	req.Host = "192.168.2.254:8080"
	c := e.NewContext(req, httptest.NewRecorder())

	h := &Handler{}
	_, err := h.buildCurtinInstallConfig(context.Background(), c, target, img, info)
	if err == nil || !strings.Contains(err.Error(), "unsupported OS family for squashfs curtin deploy: rocky") {
		t.Fatalf("expected unsupported OS family error for rocky, got %v", err)
	}
}
