package pxehttp

import (
	"context"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPXENocloudUserData_MachineCloudInitRef(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	cloudInitSvc := cloudinit.NewService(backend.CloudInits())
	now := time.Now().UTC()

	tpl := cloudinit.CloudInitTemplate{
		Name:     "ci-machine-01",
		UserData: "#cloud-config\npackages:\n  - htop\n  - vim\n",
	}
	if _, err := cloudInitSvc.Create(context.Background(), tpl); err != nil {
		t.Fatalf("create cloud-init template: %v", err)
	}

	target := machine.Machine{
		Name:          "bm-ci",
		Hostname:      "bm-ci",
		MAC:           "52:54:00:ab:cd:ef",
		Arch:          "amd64",
		Firmware:      machine.FirmwareUEFI,
		CloudInitRefs: []string{"ci-machine-01"},
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeUbuntu,
		},
		Phase:                    machine.PhaseProvisioning,
		LastDeployedCloudInitRef: "ci-machine-01",
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-machine-ci",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/nocloud/525400abcdef/user-data", nil)
	req.Host = "192.168.2.254:8080"
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("mac")
	c.SetParamValues("525400abcdef")

	h := &Handler{machines: machineSvc, cloudInits: cloudInitSvc}
	if err := h.PXENocloudUserData(c); err != nil {
		t.Fatalf("PXENocloudUserData: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "htop") || !strings.Contains(body, "vim") {
		t.Fatalf("expected machine cloud-init user-data with htop and vim, got: %s", body)
	}
	if !strings.Contains(body, "install-complete?token=token-machine-ci&type=curtin") {
		t.Fatalf("expected install-complete callback with machine token, got: %s", body)
	}
}
