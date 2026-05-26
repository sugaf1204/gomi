package pxehttp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/vm"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPXEInstallComplete_ByToken(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	now := time.Now().UTC()
	deadline := now.Add(10 * time.Minute)
	target := vm.VirtualMachine{
		Name:          "vm-complete",
		HypervisorRef: "hv-missing",
		Resources:     vm.ResourceSpec{CPUCores: 2, MemoryMB: 2048, DiskGB: 20},
		Phase:         vm.PhaseProvisioning,
		Provisioning: vm.ProvisioningStatus{
			Active:          true,
			StartedAt:       &now,
			DeadlineAt:      &deadline,
			CompletionToken: "token-finish-01",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-finish-01&type=preseed", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{vms: vmSvc}
	if err := h.PXEInstallComplete(c); err != nil {
		t.Fatalf("PXEInstallComplete: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	vmBody, _ := payload["vm"].(map[string]any)
	if vmBody["phase"] != string(vm.PhaseRunning) {
		t.Fatalf("expected phase running after completion fallback, got %v", vmBody["phase"])
	}
	provisioning, _ := vmBody["provisioning"].(map[string]any)
	if active, _ := provisioning["active"].(bool); active {
		t.Fatalf("expected provisioning.active=false, got %v", provisioning["active"])
	}
	if provisioning["completedAt"] == nil {
		t.Fatalf("expected provisioning.completedAt to be set, got %v", provisioning)
	}
}

func TestPXEInstallComplete_DebianVMWaitsForSSHBeforeFinalizing(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	osImageSvc := osimage.NewService(backend.OSImages())
	now := time.Now().UTC()
	img := osimage.OSImage{
		Name:      "debian-13-amd64-cloud",
		OSFamily:  "debian",
		OSVersion: "13",
		Arch:      "amd64",
		Ready:     true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.OSImages().Upsert(context.Background(), img); err != nil {
		t.Fatalf("upsert os image: %v", err)
	}
	target := vm.VirtualMachine{
		Name:          "vm-debian-complete",
		HypervisorRef: "hv-missing",
		Resources:     vm.ResourceSpec{CPUCores: 2, MemoryMB: 2048, DiskGB: 20},
		OSImageRef:    "debian-13-amd64-cloud",
		Phase:         vm.PhaseProvisioning,
		Provisioning: vm.ProvisioningStatus{
			Active:          true,
			StartedAt:       &now,
			CompletionToken: "token-vm-debian-finish-01",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-vm-debian-finish-01&type=curtin", strings.NewReader(`{"ip":"192.168.122.151"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{
		vms:      vmSvc,
		osimages: osImageSvc,
		machineSSHProbe: func(_ context.Context, ip string) error {
			if ip != "192.168.122.151" {
				t.Fatalf("unexpected SSH probe IP: %s", ip)
			}
			return fmt.Errorf("connection refused")
		},
	}
	if err := h.PXEInstallComplete(c); err != nil {
		t.Fatalf("PXEInstallComplete: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected conflict while SSH is unreachable, got %d body=%s", rec.Code, rec.Body.String())
	}
	stored, err := backend.VMs().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get vm: %v", err)
	}
	if !stored.Provisioning.Active || stored.Phase != vm.PhaseProvisioning {
		t.Fatalf("vm must remain provisioning while SSH is unreachable: %#v", stored)
	}

	req = httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-vm-debian-finish-01&type=curtin", strings.NewReader(`{"ip":"192.168.122.151"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	h.machineSSHProbe = func(_ context.Context, ip string) error {
		if ip != "192.168.122.151" {
			t.Fatalf("unexpected SSH probe IP: %s", ip)
		}
		return nil
	}
	if err := h.PXEInstallComplete(c); err != nil {
		t.Fatalf("PXEInstallComplete retry: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected completion after SSH becomes reachable, got %d body=%s", rec.Code, rec.Body.String())
	}
	stored, err = backend.VMs().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get vm after retry: %v", err)
	}
	if stored.Provisioning.Active || stored.Phase != vm.PhaseRunning {
		t.Fatalf("vm must finalize after SSH becomes reachable: %#v", stored)
	}
}

func TestPXEInstallComplete_ByToken_Machine(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-complete",
		Hostname: "bm-complete",
		MAC:      "52:54:00:66:66:66",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeUbuntu,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			StartedAt:       &now,
			CompletionToken: "token-machine-finish-01",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-machine-finish-01&type=curtin", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{machines: machineSvc}
	if err := h.PXEInstallComplete(c); err != nil {
		t.Fatalf("PXEInstallComplete: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	machineBody, _ := payload["machine"].(map[string]any)
	if machineBody["phase"] != string(machine.PhaseReady) {
		t.Fatalf("expected machine phase ready after completion, got %v", machineBody["phase"])
	}
	provisioning, _ := machineBody["provision"].(map[string]any)
	if active, _ := provisioning["active"].(bool); active {
		t.Fatalf("expected provision.active=false, got %v", provisioning["active"])
	}
	if provisioning["completedAt"] == nil {
		t.Fatalf("expected provision.completedAt to be set, got %v", provisioning)
	}

	req = httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-machine-finish-01&type=curtin", nil)
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	if err := h.PXEInstallComplete(c); err != nil {
		t.Fatalf("PXEInstallComplete retry: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected idempotent completion retry to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var retryPayload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &retryPayload); err != nil {
		t.Fatalf("parse retry response: %v", err)
	}
	if retryPayload["status"] != "already-finalized" {
		t.Fatalf("expected already-finalized retry response, got %v", retryPayload["status"])
	}
}

func TestPXEInstallComplete_DebianWaitsForSSHBeforeFinalizing(t *testing.T) {
	backend := memory.New()
	machineSvc := machine.NewService(backend.Machines())
	now := time.Now().UTC()
	target := machine.Machine{
		Name:     "bm-debian-complete",
		Hostname: "bm-debian-complete",
		MAC:      "52:54:00:66:66:67",
		Arch:     "amd64",
		Firmware: machine.FirmwareUEFI,
		OSPreset: machine.OSPreset{
			Family: machine.OSTypeDebian,
		},
		Phase: machine.PhaseProvisioning,
		Provision: &machine.ProvisionProgress{
			Active:          true,
			StartedAt:       &now,
			CompletionToken: "token-debian-finish-01",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.Machines().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert machine: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-debian-finish-01&type=curtin", strings.NewReader(`{"ip":"192.168.2.151"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	probeCalls := 0
	h := &Handler{
		machines: machineSvc,
		machineSSHProbe: func(_ context.Context, ip string) error {
			probeCalls++
			if ip != "192.168.2.151" {
				t.Fatalf("unexpected SSH probe IP: %s", ip)
			}
			return fmt.Errorf("connection refused")
		},
	}
	if err := h.PXEInstallComplete(c); err != nil {
		t.Fatalf("PXEInstallComplete: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected conflict while SSH is unreachable, got %d body=%s", rec.Code, rec.Body.String())
	}
	if probeCalls != 1 {
		t.Fatalf("expected one SSH probe, got %d", probeCalls)
	}
	stored, err := backend.Machines().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get machine: %v", err)
	}
	if stored.Provision == nil || !stored.Provision.Active || stored.Phase != machine.PhaseProvisioning {
		t.Fatalf("machine must remain provisioning while SSH is unreachable: %#v", stored)
	}

	req = httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-debian-finish-01&type=curtin", strings.NewReader(`{"ip":"192.168.2.151"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	c = e.NewContext(req, rec)
	h.machineSSHProbe = func(_ context.Context, ip string) error {
		probeCalls++
		if ip != "192.168.2.151" {
			t.Fatalf("unexpected SSH probe IP: %s", ip)
		}
		return nil
	}
	if err := h.PXEInstallComplete(c); err != nil {
		t.Fatalf("PXEInstallComplete retry: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected completion after SSH becomes reachable, got %d body=%s", rec.Code, rec.Body.String())
	}
	stored, err = backend.Machines().Get(context.Background(), target.Name)
	if err != nil {
		t.Fatalf("get machine after retry: %v", err)
	}
	if stored.Provision == nil || stored.Provision.Active || stored.Phase != machine.PhaseReady {
		t.Fatalf("machine must finalize after SSH becomes reachable: %#v", stored)
	}
}

func TestPXEInstallComplete_ExpiredToken(t *testing.T) {
	backend := memory.New()
	vmSvc := vm.NewService(backend.VMs())
	now := time.Now().UTC()
	target := vm.VirtualMachine{
		Name:          "vm-expired",
		HypervisorRef: "hv-01",
		Resources:     vm.ResourceSpec{CPUCores: 2, MemoryMB: 2048, DiskGB: 20},
		Phase:         vm.PhaseError,
		Provisioning: vm.ProvisioningStatus{
			Active:          false,
			CompletionToken: "token-expired",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := backend.VMs().Upsert(context.Background(), target); err != nil {
		t.Fatalf("upsert vm: %v", err)
	}

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/pxe/install-complete?token=token-expired&type=preseed", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	h := &Handler{vms: vmSvc}
	if err := h.PXEInstallComplete(c); err != nil {
		t.Fatalf("PXEInstallComplete: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d body=%s", rec.Code, rec.Body.String())
	}
}
