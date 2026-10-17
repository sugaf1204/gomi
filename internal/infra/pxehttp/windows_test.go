package pxehttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/vm"
)

func windowsTestServer(t *testing.T, kind string) (*echo.Echo, *Handler) {
	t.Helper()
	b := memory.New()
	now, deadline := time.Now().UTC(), time.Now().UTC().Add(time.Hour)
	img := osimage.OSImage{Name: "win11", OSFamily: "windows", OSVersion: "11", Arch: "amd64", Format: osimage.FormatWIM, Ready: true,
		Manifest: &osimage.Manifest{Root: osimage.RootArtifact{Path: "install.wim", Format: osimage.FormatWIM}, Windows: &osimage.WindowsImage{ImageIndex: 6,
			WinPE: osimage.WindowsPE{Wimboot: "wimboot", BCD: "BCD", BootSDI: "boot.sdi", BootWIM: "boot.wim"}}}}
	if err := b.OSImages().Upsert(context.Background(), img); err != nil {
		t.Fatal(err)
	}
	if kind == "vm" {
		v := vm.VirtualMachine{Name: "windows-test", OSImageRef: "win11", InstallCfg: &vm.InstallConfig{Type: vm.InstallConfigWindows},
			Network: []vm.NetworkInterface{{MAC: "52:54:00:12:34:56"}}, Phase: vm.PhaseProvisioning,
			Provisioning: vm.ProvisioningStatus{Active: true, AttemptID: "attempt-test", CompletionToken: "token-test", StartedAt: &now, DeadlineAt: &deadline}}
		if err := b.VMs().Upsert(context.Background(), v); err != nil {
			t.Fatal(err)
		}
	} else {
		m := machine.Machine{Name: "windows-test", Hostname: "windows-test", MAC: "52:54:00:12:34:56", Arch: "amd64", Firmware: machine.FirmwareUEFI,
			OSPreset: machine.OSPreset{Family: "windows", ImageRef: "win11"}, Phase: machine.PhaseProvisioning,
			Provision: &machine.ProvisionProgress{Active: true, AttemptID: "attempt-test", CompletionToken: "token-test", StartedAt: &now, DeadlineAt: &deadline}}
		if err := b.Machines().Upsert(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
	h := &Handler{vms: vm.NewService(b.VMs()), machines: machine.NewService(b.Machines()), osimages: osimage.NewService(b.OSImages())}
	e := echo.New()
	e.GET("/pxe/boot.ipxe", h.PXEBootScript)
	e.GET("/pxe/windows/startnet.cmd", h.PXEWindowsStartnet)
	e.GET("/pxe/windows/install.ps1", h.PXEWindowsInstall)
	e.POST("/pxe/windows/state", h.PXEWindowsState)
	e.POST("/pxe/install-complete", h.PXEInstallComplete)
	e.POST("/pxe/deploy-events", h.PXEDeployEvents)
	return e, h
}

func windowsRequest(e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	return w
}

func TestWindowsProvisioningStagesAndRebootGuard(t *testing.T) {
	for _, kind := range []string{"vm", "machine"} {
		t.Run(kind, func(t *testing.T) {
			e, h := windowsTestServer(t, kind)
			const q = "?token=token-test&attempt_id=attempt-test"
			const boot = "/pxe/boot.ipxe?mac=52:54:00:12:34:56"
			for _, path := range []string{boot, "/pxe/windows/startnet.cmd" + q, "/pxe/windows/install.ps1" + q} {
				w := windowsRequest(e, http.MethodGet, path, "")
				if w.Code != 200 {
					t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
				}
				if strings.Contains(path, "boot.ipxe") && (!strings.Contains(w.Body.String(), "initrd --name startnet.cmd") || !strings.Contains(w.Body.String(), "/wimboot")) {
					t.Fatal(w.Body.String())
				}
			}
			complete := `{"stage":"complete","ip":"192.0.2.50","mac":"52:54:00:12:34:56","hostname":"windows-test"}`
			w := windowsRequest(e, http.MethodPost, "/pxe/windows/state"+q, complete)
			if w.Code != 409 {
				t.Fatalf("premature completion accepted: %d", w.Code)
			}
			w = windowsRequest(e, http.MethodPost, "/pxe/install-complete?token=token-test&type=curtin", "")
			if w.Code != 409 {
				t.Fatalf("generic callback bypass accepted: %d", w.Code)
			}
			w = windowsRequest(e, http.MethodPost, "/pxe/windows/state"+q, `{"stage":"install_started"}`)
			if w.Code != 200 {
				t.Fatalf("claim: %d %s", w.Code, w.Body.String())
			}
			w = windowsRequest(e, http.MethodPost, "/pxe/windows/state"+q, `{"stage":"install_started"}`)
			if w.Code != 409 {
				t.Fatalf("duplicate destructive claim accepted: %d", w.Code)
			}
			w = windowsRequest(e, http.MethodGet, boot, "")
			if w.Code != 200 || strings.Contains(w.Body.String(), "wimboot") {
				t.Fatal("interrupted install would be automatically replayed")
			}
			for i := 0; i < 2; i++ {
				w = windowsRequest(e, http.MethodPost, "/pxe/windows/state"+q, `{"stage":"image_applied"}`)
				if w.Code != 200 {
					t.Fatalf("apply retry: %d %s", w.Code, w.Body.String())
				}
			}
			w = windowsRequest(e, http.MethodGet, boot, "")
			if w.Code != 200 || strings.Contains(w.Body.String(), "wimboot") || !strings.Contains(w.Body.String(), "local_efi") {
				t.Fatalf("reboot could reinstall Windows: %d %s", w.Code, w.Body.String())
			}
			for _, path := range []string{"/pxe/windows/startnet.cmd", "/pxe/windows/install.ps1"} {
				if w := windowsRequest(e, http.MethodGet, path+q, ""); w.Code != 409 {
					t.Fatalf("installer replay permitted: %s %d", path, w.Code)
				}
			}
			w = windowsRequest(e, http.MethodPost, "/pxe/windows/state"+q, complete)
			if w.Code != 200 {
				t.Fatalf("complete: %d %s", w.Code, w.Body.String())
			}
			if w = windowsRequest(e, http.MethodPost, "/pxe/windows/state"+q, complete); w.Code != 200 {
				t.Fatalf("completion retry: %d", w.Code)
			}
			target, err := h.windowsTarget(context.Background(), "token-test", "attempt-test")
			if err != nil || target.active || !target.completed {
				t.Fatalf("not finalized: %+v %v", target, err)
			}
		})
	}
}

func TestWindowsCallbacksRejectInvalidAttemptsAndReports(t *testing.T) {
	for _, kind := range []string{"vm", "machine"} {
		t.Run(kind, func(t *testing.T) {
			e, _ := windowsTestServer(t, kind)
			for _, tc := range []struct {
				query, body string
				status      int
			}{
				{"", `{"stage":"image_applied"}`, 400},
				{"?token=bad&attempt_id=attempt-test", `{"stage":"image_applied"}`, 404},
				{"?token=token-test&attempt_id=stale", `{"stage":"image_applied"}`, 409},
				{"?token=token-test&attempt_id=attempt-test", `{"stage":"bogus"}`, 400},
				{"?token=token-test&attempt_id=attempt-test", `{"stage":"image_applied"}`, 409},
			} {
				w := windowsRequest(e, http.MethodPost, "/pxe/windows/state"+tc.query, tc.body)
				if w.Code != tc.status {
					t.Fatalf("%+v: %d %s", tc, w.Code, w.Body.String())
				}
			}
			q := "?token=token-test&attempt_id=attempt-test"
			if kind == "machine" {
				if w := windowsRequest(e, http.MethodPost, "/pxe/deploy-events"+q, `{"type":"image_applied"}`); w.Code != 409 {
					t.Fatal("Linux deploy event accepted for Windows")
				}
			}
			windowsRequest(e, http.MethodPost, "/pxe/windows/state"+q, `{"stage":"install_started"}`)
			windowsRequest(e, http.MethodPost, "/pxe/windows/state"+q, `{"stage":"image_applied"}`)
			for _, body := range []string{
				`{"stage":"complete","ip":"169.254.1.1","mac":"52:54:00:12:34:56","hostname":"test"}`,
				`{"stage":"complete","ip":"192.0.2.1","mac":"52:54:00:00:00:00","hostname":"test"}`,
				`{"stage":"complete","ip":"192.0.2.1","mac":"52:54:00:12:34:56"}`,
			} {
				if w := windowsRequest(e, http.MethodPost, "/pxe/windows/state"+q, body); w.Code != 400 {
					t.Fatalf("invalid report accepted: %d", w.Code)
				}
			}
			if w := windowsRequest(e, http.MethodPost, "/pxe/windows/state"+q, `{"stage":"failed"}`); w.Code != 200 {
				t.Fatal(w.Code)
			}
			if w := windowsRequest(e, http.MethodPost, "/pxe/windows/state"+q, `{"stage":"image_applied"}`); w.Code != 409 {
				t.Fatal("failed attempt reopened")
			}
		})
	}
}

func TestWindowsExpiredAttemptCannotInstallOrComplete(t *testing.T) {
	e, h := windowsTestServer(t, "vm")
	v, err := h.vms.Get(context.Background(), "windows-test")
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	v.Provisioning.DeadlineAt = &past
	if _, err := h.vms.UpdateExisting(context.Background(), v); err != nil {
		t.Fatal(err)
	}
	q := "?token=token-test&attempt_id=attempt-test"
	for _, path := range []string{"/pxe/windows/startnet.cmd", "/pxe/windows/install.ps1"} {
		if w := windowsRequest(e, http.MethodGet, path+q, ""); w.Code != 410 {
			t.Fatalf("expired %s: %d", path, w.Code)
		}
	}
	if w := windowsRequest(e, http.MethodPost, "/pxe/windows/state"+q, `{"stage":"image_applied"}`); w.Code != 410 {
		t.Fatal(w.Code)
	}
}
