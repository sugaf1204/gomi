package pxehttp

import (
	"context"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPXEInstallComplete_BIOSMachineConfiguresBootOrder(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-bios-01",
		Hostname: "bm-bios-01",
		MAC:      "52:54:00:11:22:33",
		Arch:     "amd64",
		Firmware: machine.FirmwareBIOS,
		Power: power.PowerConfig{
			Type: power.PowerTypeWebhook,
			Webhook: &power.WebhookConfig{
				PowerOnURL:   "https://power.example/on",
				PowerOffURL:  "https://power.example/off",
				BootOrderURL: "https://power.example/boot-order",
			},
		},
		OSPreset: machine.OSPreset{
			Family:   machine.OSTypeUbuntu,
			ImageRef: "ubuntu-24.04-server",
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-bios-finish-01",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	exec := &stubPowerExecutor{}
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-bios-finish-01&type=curtin", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc, powerExecutor: exec}
	if err := h.PXEInstallComplete(c); err != nil {
		t.Fatalf("PXEInstallComplete: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if len(exec.calls) != 1 {
		t.Fatalf("expected 1 boot order call, got %d", len(exec.calls))
	}
	if exec.calls[0].machine.Name != target.Name {
		t.Fatalf("expected machine %s, got %s", target.Name, exec.calls[0].machine.Name)
	}
	if len(exec.calls[0].order) != len(power.DefaultBIOSBootOrder) {
		t.Fatalf("expected boot order length %d, got %d", len(power.DefaultBIOSBootOrder), len(exec.calls[0].order))
	}
	for i, item := range power.DefaultBIOSBootOrder {
		if exec.calls[0].order[i] != item {
			t.Fatalf("expected boot order[%d]=%s, got %s", i, item, exec.calls[0].order[i])
		}
	}

	stored, err := backend.Machines().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get machine: %v", err)
	}
	if stored.Phase != machine.PhaseReady {
		t.Fatalf("expected machine phase ready, got %s", stored.Phase)
	}
	if stored.Provision == nil || !strings.Contains(stored.Provision.Message, "BIOS boot order updated") {
		t.Fatalf("expected provision message to mention BIOS boot order update, got %+v", stored.Provision)
	}
}

func TestPXEDeployEvents_ImageAppliedLocalBootsAndConfiguresBIOSBootOrder(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-image-applied-01",
		Hostname: "bm-image-applied-01",
		MAC:      "52:54:00:44:55:66",
		Arch:     "amd64",
		Firmware: machine.FirmwareBIOS,
		Power: power.PowerConfig{
			Type: power.PowerTypeWebhook,
			Webhook: &power.WebhookConfig{
				PowerOnURL:   "https://power.example/on",
				PowerOffURL:  "https://power.example/off",
				BootOrderURL: "https://power.example/boot-order",
			},
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-image-applied-01",
			CompletionToken: "token-image-applied-01",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	exec := &stubPowerExecutor{}
	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/deploy-events?token=token-image-applied-01&attempt_id=attempt-image-applied-01", strings.NewReader(`{"type":"image_applied"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc, powerExecutor: exec}
	if err := h.PXEDeployEvents(c); err != nil {
		t.Fatalf("PXEDeployEvents: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if len(exec.calls) != 1 {
		t.Fatalf("expected 1 boot order call, got %d", len(exec.calls))
	}

	stored, err := backend.Machines().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get machine: %v", err)
	}
	if stored.Phase != machine.PhaseProvisioning {
		t.Fatalf("expected machine to remain provisioning until target OS callback, got %s", stored.Phase)
	}
	if stored.Provision == nil || !stored.Provision.Active {
		t.Fatalf("expected provision to remain active, got %+v", stored.Provision)
	}
	if stored.Provision.Artifacts[provisionArtifactImageApplied] != "true" {
		t.Fatalf("expected imageApplied artifact, got %+v", stored.Provision.Artifacts)
	}
	if !strings.Contains(stored.Provision.Message, "waiting for target OS first boot") ||
		!strings.Contains(stored.Provision.Message, "BIOS boot order updated") {
		t.Fatalf("unexpected provision message: %q", stored.Provision.Message)
	}

	bootReq := httptest.NewRequest(http.MethodGet, "/pxe/boot.ipxe?mac=52:54:00:44:55:66", nil)
	bootRec := httptest.NewRecorder()
	bootCtx := e.NewContext(bootReq, bootRec)
	if err := h.PXEBootScript(bootCtx); err != nil {
		t.Fatalf("PXEBootScript: %v", err)
	}
	if bootRec.Code != http.StatusOK {
		t.Fatalf("unexpected boot status: %d body=%s", bootRec.Code, bootRec.Body.String())
	}
	body := bootRec.Body.String()
	if !strings.Contains(body, "iseq ${platform} efi && goto local_efi || goto local_bios") ||
		!strings.Contains(body, "sanboot --no-describe --drive 0 || exit 1") {
		t.Fatalf("expected local arch-neutral sanboot script after image_applied, got: %s", body)
	}
	if strings.Contains(body, "BOOTX64.EFI") {
		t.Fatalf("UEFI local boot must not force an x86-only EFI filename after image_applied, got: %s", body)
	}
	if strings.Contains(body, "grubnetx64.efi") {
		t.Fatalf("UEFI local boot must not chain network GRUB after image_applied, got: %s", body)
	}
	if strings.Contains(body, "curtin-initrd") {
		t.Fatalf("did not expect redeploy script after image_applied, got: %s", body)
	}
}

func TestPXEDeployEventsStoresFailureLogTail(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-failed-log",
		Hostname: "bm-failed-log",
		MAC:      "52:54:00:44:55:77",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		Phase:    machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-failed-log",
			CompletionToken: "token-failed-log",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	form := "type=failed&message=curtin+install+failed&reason=exit+status+1&logTail=missing+wget"
	req := httptest.NewRequest(http.MethodPost, "/pxe/deploy-events?token=token-failed-log&attempt_id=attempt-failed-log", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc}
	if err := h.PXEDeployEvents(c); err != nil {
		t.Fatalf("PXEDeployEvents: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	stored, err := backend.Machines().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get machine: %v", err)
	}
	if stored.Phase != machine.PhaseError || stored.LastError != "exit status 1" {
		t.Fatalf("unexpected failure state: phase=%s error=%q", stored.Phase, stored.LastError)
	}
	if got := stored.Provision.Artifacts[provisionArtifactFailureLogTail]; got != "missing wget" {
		t.Fatalf("expected failure log tail, got %q", got)
	}
}

func TestPXEDeployEventsStoresTiming(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-timing",
		Hostname: "bm-timing",
		MAC:      "52:54:00:44:55:88",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		Phase:    machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-timing",
			CompletionToken: "token-timing",
			Message:         "running curtin install",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	form := "type=timing&source=runner&name=runner.dhcp&message=waiting+for+DHCP&result=success&startedAt=2026-05-06T01:02:03Z&finishedAt=2026-05-06T01:02:04.500Z&durationMs=1500"
	req := httptest.NewRequest(http.MethodPost, "/pxe/deploy-events?token=token-timing&attempt_id=attempt-timing", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc}
	if err := h.PXEDeployEvents(c); err != nil {
		t.Fatalf("PXEDeployEvents: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	stored, err := backend.Machines().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get machine: %v", err)
	}
	if stored.Provision.Message != "running curtin install" {
		t.Fatalf("timing event should not overwrite provision message, got %q", stored.Provision.Message)
	}
	if len(stored.Provision.Timings) != 1 {
		t.Fatalf("expected one timing event, got %#v", stored.Provision.Timings)
	}
	timing := stored.Provision.Timings[0]
	if timing.Source != "runner" || timing.Name != "runner.dhcp" || timing.Result != "success" {
		t.Fatalf("unexpected timing metadata: %#v", timing)
	}
	if timing.DurationMillis != 1500 {
		t.Fatalf("expected 1500ms duration, got %d", timing.DurationMillis)
	}
	if timing.StartedAt == nil || timing.FinishedAt == nil {
		t.Fatalf("expected started/finished timestamps: %#v", timing)
	}
}

func TestPXEProvisionEndpointsRejectInactiveOrMismatchedAttempt(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	hwInfoSvc := hwinfo.NewService(backend.HWInfo())
	now := time.Now().UTC()
	inactive := machine.Machine{
		Name:     "bm-inactive-token",
		Hostname: "bm-inactive-token",
		MAC:      "52:54:00:aa:cc:01",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		Phase:    machine.PhaseReady,
		Provision: &machine.ProvisionProgress{
			Active:          false,
			AttemptID:       "attempt-inactive",
			CompletionToken: "token-inactive",
			CompletedAt:     &now,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), inactive); err != nil {
		t.Fatalf("upsert inactive machine: %v", err)
	}
	active := machine.Machine{
		Name:     "bm-attempt-mismatch",
		Hostname: "bm-attempt-mismatch",
		MAC:      "52:54:00:aa:cc:02",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		Phase:    machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-current",
			CompletionToken: "token-current",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), active); err != nil {
		t.Fatalf("upsert active machine: %v", err)
	}

	e := echo.New()
	h := &Handler{machines: machineSvc, hwinfo: hwInfoSvc}
	req := httptest.NewRequest(http.MethodPost, "/pxe/inventory?token=token-inactive", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := h.PXEInventory(c); err != nil {
		t.Fatalf("PXEInventory inactive: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected inactive token to be rejected, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/pxe/curtin-config?token=token-inactive&attempt_id=attempt-current", nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	if err := h.PXECurtinConfig(c); err != nil {
		t.Fatalf("PXECurtinConfig inactive: %v", err)
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected curtin-config inactive token to be rejected, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/pxe/deploy-events?token=token-current&attempt_id=attempt-stale", strings.NewReader(`{"type":"image_applied"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	if err := h.PXEDeployEvents(c); err != nil {
		t.Fatalf("PXEDeployEvents stale attempt: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected stale attempt to be rejected, got %d body=%s", rec.Code, rec.Body.String())
	}
}
