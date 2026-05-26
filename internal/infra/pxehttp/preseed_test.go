package pxehttp

import (
	"context"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/vm"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPXEPreseed_CustomInlineByMAC(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	now := time.Now().UTC()
	target := vm.VirtualMachine{
		Name:          "vm-preseed",
		HypervisorRef: "hv-01",
		Resources:     vm.ResourceSpec{CPUCores: 2, MemoryMB: 2048, DiskGB: 20},
		InstallCfg: &vm.InstallConfig{
			Type:   vm.InstallConfigPreseed,
			Inline: "d-i passwd/username string customuser",
		},
		Phase: vm.PhaseProvisioning,
		Provisioning: vm.ProvisioningStatus{
			Active:          true,
			CompletionToken: "token-preseed-custom",
		},
		NetworkInterfaces: []vm.NetworkInterfaceStatus{
			{MAC: "52:54:00:11:22:33"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/preseed.cfg?mac=52:54:00:11:22:33", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{vms: vmSvc}
	if err := h.PXEPreseed(c); err != nil {
		t.Fatalf("PXEPreseed: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	got := rec.Body.String()
	if !strings.Contains(got, "customuser") {
		t.Fatalf("expected custom preseed, got: %s", got)
	}
	if !strings.Contains(got, "install-complete?token=token-preseed-custom&type=preseed") {
		t.Fatalf("expected install-complete callback with token, got: %s", got)
	}
	if !strings.Contains(got, "curl -fsS --connect-timeout 5 --max-time 15") {
		t.Fatalf("expected bounded install-complete curl timeout, got: %s", got)
	}
	if strings.Contains(got, "exit/poweroff") {
		t.Fatalf("expected poweroff to be removed, got: %s", got)
	}
	if !strings.Contains(got, "exit/reboot") {
		t.Fatalf("expected reboot directive, got: %s", got)
	}
}

func TestPXEPreseed_DefaultIncludesSudoAndBaseTools(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	now := time.Now().UTC()
	target := vm.VirtualMachine{
		Name:          "vm-default-preseed",
		HypervisorRef: "hv-01",
		Resources:     vm.ResourceSpec{CPUCores: 2, MemoryMB: 2048, DiskGB: 20},
		InstallCfg: &vm.InstallConfig{
			Type: vm.InstallConfigPreseed,
		},
		Network: []vm.NetworkInterface{
			{MAC: "52:54:00:11:22:44"},
		},
		Phase: vm.PhaseProvisioning,
		Provisioning: vm.ProvisioningStatus{
			Active:          true,
			CompletionToken: "token-preseed-default",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/preseed.cfg?mac=52:54:00:11:22:44", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{vms: vmSvc}
	if err := h.PXEPreseed(c); err != nil {
		t.Fatalf("PXEPreseed: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, pkg := range []string{"qemu-guest-agent", "sudo", "curl", "wget", "ca-certificates", "vim", "less", "net-tools", "iproute2", "git", "tmux", "htop", "dnsutils"} {
		if !strings.Contains(body, pkg) {
			t.Fatalf("expected package %q in default preseed package list, got: %s", pkg, body)
		}
	}
	// preseed is being phased out; the default no longer creates any user
	// (caller must supply username/keys via inline preseed). The hostname is
	// still injected from the target name.
	if !strings.Contains(body, "d-i passwd/make-user boolean false") {
		t.Fatalf("expected default preseed to disable user creation, got: %s", body)
	}
	if strings.Contains(body, "passwd/username string gomi") {
		t.Fatalf("default preseed must not create the legacy gomi user, got: %s", body)
	}
	if !strings.Contains(body, "d-i netcfg/get_hostname string vm-default-preseed") {
		t.Fatalf("expected preseed hostname to follow vm name, got: %s", body)
	}
}

func TestPXEPreseed_UsesMachineProvisioningToken(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-preseed",
		Hostname: "bm-preseed",
		MAC:      "52:54:00:99:88:77",
		Arch:     "amd64",
		Firmware: machine.FirmwareBIOS,
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeDebian,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			CompletionToken: "token-machine-preseed",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/pxe/preseed.cfg?mac=52:54:00:99:88:77", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc}
	if err := h.PXEPreseed(c); err != nil {
		t.Fatalf("PXEPreseed: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	got := rec.Body.String()
	if !strings.Contains(got, "install-complete?token=token-machine-preseed&type=preseed") {
		t.Fatalf("expected machine token callback in preseed, got: %s", got)
	}
	if !strings.Contains(got, "d-i netcfg/get_hostname string bm-preseed") {
		t.Fatalf("expected machine hostname in preseed, got: %s", got)
	}
}
