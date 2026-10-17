package pxehttp

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/node"
	"github.com/sugaf1204/gomi/internal/vm"
)

type windowsStateRequest struct {
	Stage    string `json:"stage"`
	IP       string `json:"ip,omitempty"`
	MAC      string `json:"mac,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

func (h *Handler) PXEWindowsState(c echo.Context) error {
	t, err := h.windowsTarget(c.Request().Context(), c.QueryParam("token"), c.QueryParam("attempt_id"))
	if err != nil {
		return err
	}
	var req windowsStateRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid Windows state body")
	}
	if req.Stage != "install_started" && req.Stage != "image_applied" && req.Stage != "complete" && req.Stage != "failed" {
		return echo.NewHTTPError(http.StatusBadRequest, "unsupported Windows stage")
	}
	if !t.active {
		if t.completed && req.Stage == "complete" {
			return c.JSON(http.StatusOK, statusResponse{Status: "already-finalized"})
		}
		return echo.NewHTTPError(http.StatusConflict, "Windows provisioning is inactive")
	}
	if req.Stage == "install_started" && t.started {
		return echo.NewHTTPError(http.StatusConflict, "Windows installation was already claimed; start a new deployment attempt")
	}
	if req.Stage == "complete" {
		if !t.applied {
			return echo.NewHTTPError(http.StatusConflict, "Windows image has not been applied")
		}
		ip := net.ParseIP(strings.TrimSpace(req.IP))
		if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast() || normalizeMAC(req.MAC) != normalizeMAC(t.host().PrimaryMAC()) {
			return echo.NewHTTPError(http.StatusBadRequest, "completion requires deployment NIC IPv4 address and matching MAC")
		}
		if strings.TrimSpace(req.Hostname) == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "completion requires Windows hostname")
		}
	}
	if req.Stage == "image_applied" && !t.started {
		return echo.NewHTTPError(http.StatusConflict, "Windows disk installation has not started")
	}
	now := time.Now().UTC()
	report := node.InstallCompleteReport{IP: strings.TrimSpace(req.IP), MAC: normalizeMAC(req.MAC), Hostname: strings.TrimSpace(req.Hostname)}
	if t.vm != nil {
		v := *t.vm
		v.UpdatedAt, v.Provisioning.LastSignalAt = now, &now
		switch req.Stage {
		case "install_started":
			v.Provisioning.WindowsInstallStarted = true
		case "image_applied":
			v.Provisioning.ImageApplied = true
		case "failed":
			v.Provisioning.Active, v.Phase, v.LastError = false, vm.PhaseError, "Windows deployment failed; inspect the WinPE console"
		case "complete":
			v.Provisioning.Active, v.Provisioning.CompletedAt, v.Provisioning.CompletionSource = false, &now, "windows"
			v.Phase, v.LastError = vm.PhaseRunning, ""
			v.ApplyInstallCompleteReport(report)
		}
		updater, ok := h.vms.Store().(vm.ProvisioningAttemptUpdater)
		if !ok {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "VM store cannot atomically update Windows attempts")
		}
		written, err := updater.UpdateProvisioningAttempt(c.Request().Context(), *t.vm, v)
		if err != nil {
			return err
		}
		if !written {
			return echo.NewHTTPError(http.StatusConflict, "Windows VM attempt changed; retry the callback")
		}
	} else {
		m := *t.machine
		// Copy the progress and artifacts before mutating a store-owned value.
		progress := *m.Provision
		m.Provision = &progress
		m.UpdatedAt, m.Provision.LastSignalAt = now, &now
		switch req.Stage {
		case "install_started":
			artifacts := map[string]string{}
			for k, v := range m.Provision.Artifacts {
				artifacts[k] = v
			}
			artifacts["windowsInstallStarted"] = "true"
			m.Provision.Artifacts = artifacts
			m.Provision.Message = "Windows installation started; interrupted deployment requires an explicit new attempt"
		case "image_applied":
			artifacts := map[string]string{}
			for k, v := range m.Provision.Artifacts {
				artifacts[k] = v
			}
			artifacts[provisionArtifactImageApplied], artifacts[provisionArtifactImageAppliedAt] = "true", now.Format(time.RFC3339)
			m.Provision.Artifacts = artifacts
			m.Provision.Message = "Windows image applied; waiting for reboot and interactive Windows initial setup"
		case "failed":
			m.Provision.Active, m.Provision.FinishedAt, m.Phase = false, &now, machine.PhaseError
			m.LastError = "Windows deployment failed; inspect the WinPE console"
			m.Provision.FailureReason, m.Provision.Message = m.LastError, m.LastError
		case "complete":
			m.Provision.Active, m.Provision.CompletedAt, m.Provision.FinishedAt = false, &now, &now
			m.Provision.CompletionSource = "windows"
			m.Provision.Message = "Windows initial setup completed; activation is managed manually in Windows"
			m.Phase, m.LastError = machine.PhaseReady, ""
			m.ApplyInstallCompleteReport(report)
		}
		updater, ok := h.machines.Store().(machine.ProvisioningAttemptUpdater)
		if !ok {
			return echo.NewHTTPError(http.StatusServiceUnavailable, "Machine store cannot atomically update Windows attempts")
		}
		written, err := updater.UpdateProvisioningAttempt(c.Request().Context(), *t.machine, m)
		if err != nil {
			return err
		}
		if !written {
			return echo.NewHTTPError(http.StatusConflict, "Windows Machine attempt changed; retry the callback")
		}
	}
	return c.JSON(http.StatusOK, statusResponse{Status: "ok"})
}
