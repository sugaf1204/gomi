package pxehttp

import (
	"context"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPXEBootScript_CurtinInitrdForLinuxMachineProvisioning(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()

	target := machine.Machine{
		Name:     "bm-linux-01",
		Hostname: "bm-linux-01",
		MAC:      "52:54:00:aa:11:22",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family: "debian",
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-machine-linux",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/boot.ipxe?mac=52:54:00:aa:11:22", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc}
	if err := h.PXEBootScript(c); err != nil {
		t.Fatalf("PXEBootScript: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()

	// Must use split boot-kernel, boot-initrd, and SquashFS rootfs artifacts.
	if !strings.Contains(body, "files/linux/boot-kernel") {
		t.Fatalf("expected linux boot-kernel path, got: %s", body)
	}
	if !strings.Contains(body, "files/linux/boot-initrd") {
		t.Fatalf("expected boot-initrd path, got: %s", body)
	}
	if !strings.Contains(body, "root=squash:${base}/files/linux/rootfs.squashfs") {
		t.Fatalf("expected squashfs rootfs path, got: %s", body)
	}
	// Must pass gomi.base and gomi.token kernel parameters (token = provision CompletionToken)
	if !strings.Contains(body, "gomi.base=") {
		t.Fatalf("expected gomi.base kernel parameter, got: %s", body)
	}
	if !strings.Contains(body, "gomi.token=token-machine-linux") {
		t.Fatalf("expected gomi.token=token-machine-linux (provision token), got: %s", body)
	}
	if !strings.Contains(body, "BOOTIF=01-52-54-00-aa-11-22") {
		t.Fatalf("expected BOOTIF for deterministic PXE NIC selection, got: %s", body)
	}
	if !strings.Contains(body, "gomi.boot_mac=52:54:00:aa:11:22") {
		t.Fatalf("expected boot MAC kernel parameter, got: %s", body)
	}
	// Must include ip=dhcp for kernel-level network init
	if !strings.Contains(body, "ip=dhcp") {
		t.Fatalf("expected ip=dhcp kernel parameter, got: %s", body)
	}
	for _, param := range []string{"earlyprintk=", "efi=debug", "ignore_loglevel", "initcall_debug", "earlycon=efifb", "keep_bootcon", "console=ttyS0"} {
		if strings.Contains(body, param) {
			t.Fatalf("did not expect debug kernel parameter %q, got: %s", param, body)
		}
	}
	for _, marker := range []string{
		"echo GOMI: before dhcp",
		"echo GOMI: before kernel",
		"echo GOMI: before initrd",
		"echo GOMI: after initrd",
		"imgstat",
		"echo GOMI: booting",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("expected PXE progress marker %q, got: %s", marker, body)
		}
	}
	// Must NOT contain autoinstall or ISO references
	if strings.Contains(body, "autoinstall") {
		t.Fatalf("curtin boot script must not contain autoinstall, got: %s", body)
	}
	if strings.Contains(body, "ubuntu.iso") {
		t.Fatalf("curtin boot script must not reference ubuntu.iso, got: %s", body)
	}
	// Must NOT contain local boot (sanboot)
	if strings.Contains(body, "sanboot") {
		t.Fatalf("bare metal machine must not use local boot (sanboot), got: %s", body)
	}
}

func TestPXEBootScript_DesktopMachineStillUsesArtifactCurtinPath(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()

	// Create a desktop OS image to resolve variant.
	img := osimage.OSImage{
		Name:      "ubuntu-desktop-24.04",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
		Arch:      "amd64",
		Format:    osimage.FormatISO,
		Source:    osimage.SourceUpload,
		Variant:   osimage.VariantDesktop,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.OSImages().Upsert(context.Background(), img); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}

	target := machine.Machine{
		Name:     "bm-desktop-01",
		Hostname: "bm-desktop-01",
		MAC:      "52:54:00:de:sk:01",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family:   machine.OSTypeUbuntu,
			ImageRef: "ubuntu-desktop-24.04",
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-desktop-machine",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/boot.ipxe?mac=52:54:00:de:sk:01", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc, osimages: osImageSvc}
	if err := h.PXEBootScript(c); err != nil {
		t.Fatalf("PXEBootScript: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()

	if !strings.Contains(body, "files/linux/boot-kernel") {
		t.Fatalf("expected linux boot-kernel path, got: %s", body)
	}
	if !strings.Contains(body, "files/linux/boot-initrd") {
		t.Fatalf("expected linux boot-initrd path, got: %s", body)
	}
	if !strings.Contains(body, "root=squash:${base}/files/linux/rootfs.squashfs") {
		t.Fatalf("expected linux squashfs rootfs path, got: %s", body)
	}
	if !strings.Contains(body, "gomi.token=token-desktop-machine") {
		t.Fatalf("expected provision token in curtin script, got: %s", body)
	}
	if strings.Contains(body, "autoinstall") {
		t.Fatalf("desktop machine must not bypass artifact deploy through autoinstall, got: %s", body)
	}
	if strings.Contains(body, "ubuntu.iso") {
		t.Fatalf("desktop machine must not reference ISO deploy path, got: %s", body)
	}
}
