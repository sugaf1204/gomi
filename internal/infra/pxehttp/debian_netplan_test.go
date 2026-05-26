package pxehttp

import (
	"context"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPXENocloudUserData_DebianMachineSwitchesToNetworkdWithRollback(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	deadline := now.Add(30 * time.Minute)
	target := machine.Machine{
		Name:     "bm-debian-netplan",
		Hostname: "bm-debian-netplan",
		MAC:      "84:47:09:1f:1c:d7",
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
			StartedAt:       &now,
			DeadlineAt:      &deadline,
			CompletionToken: "token-debian-netplan",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/8447091f1cd7/user-data", nil)
	req.Host = ""
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("8447091f1cd7")

	h := &Handler{machines: machineSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"renderer: networkd",
		"permissions: \"0600\"",
		"seq 1 600",
		"/usr/local/sbin/gomi-apply-netplan-networkd",
		"/usr/local/sbin/gomi-confirm-netplan-networkd",
		"netplan generate",
		"gomi-network-rollback.timer",
		"OnActiveSec=10min",
		"/etc/network/interfaces.gomi-netplan-save",
		"printf '%s\\n' 'ENABLED=1' > /etc/default/netplan",
		"/etc/default/netplan.gomi-save",
		"systemctl enable systemd-networkd.service",
		"systemctl disable --now networking.service",
		"systemctl mask networking.service",
		"systemctl is-active --quiet systemd-networkd.service",
		"networkctl status --no-pager",
		"timeout 30 networkctl is-online",
		"curl -fsS --connect-timeout 5 --max-time 15 'http://127.0.0.1:5392/healthz'",
		"ip addr show",
		"ip route show",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected Debian netplan switch user-data to contain %q, got:\n%s", want, body)
		}
	}
	if strings.Contains(body, "\n- netplan apply\n") {
		t.Fatalf("Debian netplan switch must use rollback script instead of direct netplan apply, got:\n%s", body)
	}
	if strings.Contains(body, "networkctl is-online --timeout") {
		t.Fatalf("Debian netplan switch must not use networkctl --timeout, got:\n%s", body)
	}
	applyIdx := strings.Index(body, "- /usr/local/sbin/gomi-apply-netplan-networkd")
	completeIdx := strings.Index(body, "install-complete?token=token-debian-netplan")
	confirmIdx := strings.LastIndex(body, "- /usr/local/sbin/gomi-confirm-netplan-networkd")
	if applyIdx == -1 || completeIdx == -1 || confirmIdx == -1 || !(applyIdx < completeIdx && completeIdx < confirmIdx) {
		t.Fatalf("expected Debian rollback confirmation to run after install callback, got:\n%s", body)
	}
}
