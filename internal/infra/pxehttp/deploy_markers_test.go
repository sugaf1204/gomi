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

func countProvisionTimings(timings []machine.ProvisionTiming, name string) int {
	count := 0
	for _, timing := range timings {
		if timing.Name == name {
			count++
		}
	}
	return count
}

func findProvisionTiming(t *testing.T, timings []machine.ProvisionTiming, name string) machine.ProvisionTiming {
	t.Helper()
	for _, timing := range timings {
		if timing.Name == name {
			return timing
		}
	}
	t.Fatalf("expected timing %q, got %#v", name, timings)
	return machine.ProvisionTiming{}
}

func fetchPXEBootScript(t *testing.T, h *Handler, mac string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/boot.ipxe?mac="+mac, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	if err := h.PXEBootScript(c); err != nil {
		t.Fatalf("PXEBootScript: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected boot script status: %d body=%s", rec.Code, rec.Body.String())
	}
	return rec
}

func TestPXEBootScriptRecordsInstallMarkerOnce(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-boot-marker",
		Hostname: "bm-boot-marker",
		MAC:      "52:54:00:aa:dd:01",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family:   machine.OSTypeUbuntu,
			ImageRef: "ubuntu-24.04-server",
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-boot-marker",
			CompletionToken: "token-boot-marker",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	h := &Handler{machines: machineSvc}
	firstBody := fetchPXEBootScript(t, h, target.MAC).Body.String()
	secondBody := fetchPXEBootScript(t, h, target.MAC).Body.String()
	if firstBody != secondBody {
		t.Fatalf("boot script body changed between fetches:\nfirst=%s\nsecond=%s", firstBody, secondBody)
	}

	stored, err := backend.Machines().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get machine: %v", err)
	}
	if got := countProvisionTimings(stored.Provision.Timings, serverTimingPXEBootScript); got != 1 {
		t.Fatalf("expected exactly one %s marker, got %d (%#v)", serverTimingPXEBootScript, got, stored.Provision.Timings)
	}
	marker := findProvisionTiming(t, stored.Provision.Timings, serverTimingPXEBootScript)
	if marker.Source != "server" || marker.EventType != "marker" || marker.Timestamp == nil {
		t.Fatalf("unexpected marker shape: %#v", marker)
	}
}

func TestPXEBootScriptRecordsLocalBootMarkerAfterImageApplied(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-localboot-marker",
		Hostname: "bm-localboot-marker",
		MAC:      "52:54:00:aa:dd:02",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		Phase:    machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-localboot-marker",
			CompletionToken: "token-localboot-marker",
			Artifacts: map[string]string{
				provisionArtifactImageApplied:   "true",
				provisionArtifactImageAppliedAt: now.Add(-30 * time.Second).Format(time.RFC3339),
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	h := &Handler{machines: machineSvc}
	body := fetchPXEBootScript(t, h, target.MAC).Body.String()
	if !strings.Contains(body, "iseq ${platform} efi && goto local_efi || goto local_bios") {
		t.Fatalf("expected local boot script after image_applied, got: %s", body)
	}
	fetchPXEBootScript(t, h, target.MAC)

	stored, err := backend.Machines().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get machine: %v", err)
	}
	if got := countProvisionTimings(stored.Provision.Timings, serverTimingPXEBootScriptLocalBoot); got != 1 {
		t.Fatalf("expected exactly one %s marker, got %d (%#v)", serverTimingPXEBootScriptLocalBoot, got, stored.Provision.Timings)
	}
	if got := countProvisionTimings(stored.Provision.Timings, serverTimingPXEBootScript); got != 0 {
		t.Fatalf("did not expect install marker on local boot path, got %d", got)
	}
}

func TestPXEBootScriptIdleOrUnknownMachineRecordsNothing(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	idle := machine.Machine{
		Name:      "bm-idle",
		Hostname:  "bm-idle",
		MAC:       "52:54:00:aa:dd:03",
		Arch:      "amd64",
		Firmware:  machine.FirmwareUEFI,
		Phase:     machine.PhaseReady,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), idle); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	h := &Handler{machines: machineSvc}
	fetchPXEBootScript(t, h, idle.MAC)
	fetchPXEBootScript(t, h, "52:54:00:ff:ff:ff")

	stored, err := backend.Machines().Get(context.Background(), idle.Name)
	if err != nil {
		t.Fatalf("get machine: %v", err)
	}
	if stored.Provision != nil && len(stored.Provision.Timings) > 0 {
		t.Fatalf("expected no timings for idle machine, got %#v", stored.Provision.Timings)
	}
}

func TestPXEInstallCompleteRecordsRebootWaitTiming(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	appliedAt := now.Add(-90 * time.Second).Truncate(time.Second)
	target := machine.Machine{
		Name:     "bm-reboot-timing",
		Hostname: "bm-reboot-timing",
		MAC:      "52:54:00:aa:dd:04",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeUbuntu,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-reboot-timing",
			CompletionToken: "token-reboot-timing",
			Artifacts: map[string]string{
				provisionArtifactImageApplied:   "true",
				provisionArtifactImageAppliedAt: appliedAt.Format(time.RFC3339),
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	h := &Handler{machines: machineSvc}
	req := httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-reboot-timing&type=curtin", nil)
	rec := httptest.NewRecorder()
	if err := h.PXEInstallComplete(e.NewContext(req, rec)); err != nil {
		t.Fatalf("PXEInstallComplete: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	stored, err := backend.Machines().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get machine: %v", err)
	}
	reboot := findProvisionTiming(t, stored.Provision.Timings, serverTimingRebootToOS)
	if reboot.StartedAt == nil || !reboot.StartedAt.Equal(appliedAt) {
		t.Fatalf("expected reboot timing to start at imageAppliedAt %s, got %#v", appliedAt, reboot)
	}
	if reboot.FinishedAt == nil || reboot.DurationMillis < 85_000 || reboot.DurationMillis > 100_000 {
		t.Fatalf("expected ~90s reboot duration, got %#v", reboot)
	}
	marker := findProvisionTiming(t, stored.Provision.Timings, serverTimingInstallComplete)
	if marker.Timestamp == nil || !strings.Contains(marker.Message, "curtin") {
		t.Fatalf("unexpected install-complete marker: %#v", marker)
	}
	timingCount := len(stored.Provision.Timings)

	repeatRec := httptest.NewRecorder()
	repeatReq := httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-reboot-timing&type=curtin", nil)
	if err := h.PXEInstallComplete(e.NewContext(repeatReq, repeatRec)); err != nil {
		t.Fatalf("PXEInstallComplete repeat: %v", err)
	}
	if repeatRec.Code != http.StatusOK || !strings.Contains(repeatRec.Body.String(), "already-finalized") {
		t.Fatalf("expected already-finalized on repeat, got %d body=%s", repeatRec.Code, repeatRec.Body.String())
	}
	repeatStored, err := backend.Machines().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get machine after repeat: %v", err)
	}
	if len(repeatStored.Provision.Timings) != timingCount {
		t.Fatalf("expected timing count unchanged on repeat (%d), got %d", timingCount, len(repeatStored.Provision.Timings))
	}
}

func TestPXEInstallCompleteWithoutImageAppliedRecordsMarkerOnly(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-complete-no-apply",
		Hostname: "bm-complete-no-apply",
		MAC:      "52:54:00:aa:dd:05",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeUbuntu,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-complete-no-apply",
			CompletionToken: "token-complete-no-apply",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	h := &Handler{machines: machineSvc}
	req := httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-complete-no-apply&type=curtin", nil)
	rec := httptest.NewRecorder()
	if err := h.PXEInstallComplete(e.NewContext(req, rec)); err != nil {
		t.Fatalf("PXEInstallComplete: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	stored, err := backend.Machines().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get machine: %v", err)
	}
	if got := countProvisionTimings(stored.Provision.Timings, serverTimingRebootToOS); got != 0 {
		t.Fatalf("expected no reboot timing without imageAppliedAt, got %d", got)
	}
	if got := countProvisionTimings(stored.Provision.Timings, serverTimingInstallComplete); got != 1 {
		t.Fatalf("expected install-complete marker, got %d (%#v)", got, stored.Provision.Timings)
	}
}

func TestPXECurtinConfigRecordsGenerationTiming(t *testing.T) {
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
		Name:     "bm-config-timing",
		Hostname: "bm-config-timing",
		MAC:      "52:54:00:aa:dd:06",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family:   machine.OSTypeUbuntu,
			Version:  "22.04",
			ImageRef: img.Name,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			AttemptID:       "attempt-config-timing",
			CompletionToken: "token-config-timing",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}
	if _, err := hwInfoSvc.Upsert(context.Background(), hwinfo.HardwareInfo{
		Name:        "bm-config-timing-hwinfo",
		MachineName: target.Name,
		AttemptID:   "attempt-config-timing",
		Disks: []hwinfo.DiskInfo{
			{Name: "nvme0n1", Path: "/dev/nvme0n1", Type: "disk", SizeMB: 65536},
		},
	}); err != nil {
		t.Fatalf("upsert hwinfo: %v", err)
	}

	e := echo.New()
	h := &Handler{machines: machineSvc, hwinfo: hwInfoSvc, osimages: osImageSvc}
	req := httptest.NewRequest(http.MethodGet, "/pxe/curtin-config?token=token-config-timing&attempt_id=attempt-config-timing", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	if err := h.PXECurtinConfig(e.NewContext(req, rec)); err != nil {
		t.Fatalf("PXECurtinConfig: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	stored, err := backend.Machines().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get machine: %v", err)
	}
	timing := findProvisionTiming(t, stored.Provision.Timings, serverTimingCurtinConfig)
	if timing.Result != "success" || timing.StartedAt == nil || timing.FinishedAt == nil {
		t.Fatalf("unexpected curtin config timing: %#v", timing)
	}

	// A not-ready image fails config generation: the 409 must be preserved and
	// the failure must surface as a timing event instead of a silent stall.
	img.Ready = false
	if err := backend.OSImages().Upsert(context.Background(), img); err != nil {
		t.Fatalf("upsert not-ready os image: %v", err)
	}
	failReq := httptest.NewRequest(http.MethodGet, "/pxe/curtin-config?token=token-config-timing&attempt_id=attempt-config-timing", nil)
	failReq.Host = "192.168.2.254:8080"
	failRec := httptest.NewRecorder()
	if err := h.PXECurtinConfig(e.NewContext(failReq, failRec)); err != nil {
		t.Fatalf("PXECurtinConfig not-ready: %v", err)
	}
	if failRec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for not-ready image, got %d body=%s", failRec.Code, failRec.Body.String())
	}
	stored, err = backend.Machines().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get machine after failure: %v", err)
	}
	failureSeen := false
	for _, entry := range stored.Provision.Timings {
		if entry.Name == serverTimingCurtinConfig && entry.Result == "failure" && strings.Contains(entry.Message, "not ready") {
			failureSeen = true
		}
	}
	if !failureSeen {
		t.Fatalf("expected failure curtin config timing, got %#v", stored.Provision.Timings)
	}
}
