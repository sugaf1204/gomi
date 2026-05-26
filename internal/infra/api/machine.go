package api

import (
	"errors"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
	gohttp "net/http"
	"strings"
	"time"
)

func (s *Server) CreateMachine(c echo.Context) error {
	var m machine.Machine
	if err := c.Bind(&m); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	ctx := c.Request().Context()

	// Derive osPreset.family/version from the referenced OS image.
	if err := s.resolveOSPreset(ctx, &m); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}

	if m.Role == machine.RoleHypervisor {
		if m.BridgeName == "" {
			m.BridgeName = "br0"
		}
		// Reject if a hypervisor with the same name already exists.
		if _, err := s.hypervisors.Get(ctx, m.Name); err == nil {
			return c.JSON(gohttp.StatusConflict, jsonError(fmt.Sprintf("hypervisor %q already exists", m.Name)))
		}
	}

	if err := power.FillWoLDefaults(&m.Power); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if err := power.ValidatePowerConfig(m.Power); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}

	token, err := httputil.GenerateProvisioningToken()
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError("failed to issue provisioning token"))
	}
	attemptID, err := resource.GenerateProvisioningAttemptID()
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError("failed to issue provisioning attempt id"))
	}
	now := time.Now().UTC()
	deadline := now.Add(s.provisionTimeout)
	m.Phase = machine.PhaseProvisioning
	m.Provision = &machine.ProvisionProgress{
		Active:          true,
		AttemptID:       attemptID,
		StartedAt:       &now,
		DeadlineAt:      &deadline,
		Trigger:         "create",
		CompletionToken: token,
	}
	if _, err := s.attachHypervisorRegistrationToken(ctx, &m); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}

	created, err := s.machines.Create(ctx, m)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, created.Name, "create-machine", "success", "machine created", nil)
	return c.JSON(gohttp.StatusCreated, machineResponse(created))
}

func (s *Server) ListMachines(c echo.Context) error {
	machines, err := s.machines.List(c.Request().Context())
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, itemsResponse[MachineResponse]{Items: machineResponses(machines)})
}

func (s *Server) GetMachine(c echo.Context) error {
	name := c.Param("name")
	m, err := s.machines.Get(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, machineResponse(m))
}

func (s *Server) DeleteMachine(c echo.Context) error {
	name := c.Param("name")
	if err := s.machines.Delete(c.Request().Context(), name); err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, name, "delete-machine", "success", "machine deleted", nil)
	return c.NoContent(gohttp.StatusNoContent)
}

type updateSettingsReq struct {
	Power power.PowerConfig `json:"power"`
}

func (s *Server) UpdateMachineSettings(c echo.Context) error {
	name := c.Param("name")
	var req updateSettingsReq
	if err := c.Bind(&req); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	current, err := s.machines.Get(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	powerCfg := mergePowerConfigDefaults(current.Power, req.Power)
	if err := power.FillWoLDefaults(&powerCfg); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if err := power.ValidatePowerConfig(powerCfg); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	m, err := s.machines.UpdateSettings(c.Request().Context(), name, powerCfg)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, name, "update-machine-settings", "success", "machine settings updated", map[string]string{
		"powerType": string(req.Power.Type),
	})
	return c.JSON(gohttp.StatusOK, machineResponse(m))
}

type updateNetworkReq struct {
	IP           string `json:"ip"`
	IPAssignment string `json:"ipAssignment"`
	SubnetRef    string `json:"subnetRef"`
	Domain       string `json:"domain"`
}

func (s *Server) UpdateMachineNetwork(c echo.Context) error {
	name := c.Param("name")
	var req updateNetworkReq
	if err := c.Bind(&req); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	ipAssignment := machine.IPAssignmentMode(req.IPAssignment)
	if ipAssignment != "" && ipAssignment != machine.IPAssignmentModeDHCP && ipAssignment != machine.IPAssignmentModeStatic {
		return c.JSON(gohttp.StatusBadRequest, jsonError("ipAssignment must be 'dhcp' or 'static'"))
	}
	if req.SubnetRef != "" {
		if _, err := s.subnets.Get(c.Request().Context(), req.SubnetRef); err != nil {
			return c.JSON(gohttp.StatusBadRequest, jsonError("referenced subnetRef not found"))
		}
	}
	net := machine.NetworkConfig{
		Domain: req.Domain,
	}
	m, err := s.machines.UpdateNetwork(c.Request().Context(), name, req.IP, ipAssignment, req.SubnetRef, net)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, name, "update-machine-network", "success", "machine network settings updated", nil)
	return c.JSON(gohttp.StatusOK, machineResponse(m))
}

func mergePowerConfigDefaults(current, next power.PowerConfig) power.PowerConfig {
	if current.Type != next.Type {
		return next
	}
	merged := next
	switch next.Type {
	case power.PowerTypeWoL:
		if current.WoL == nil || next.WoL == nil {
			return next
		}
		wol := *next.WoL
		if strings.TrimSpace(wol.WakeMAC) == "" {
			wol.WakeMAC = current.WoL.WakeMAC
		}
		if strings.TrimSpace(wol.BroadcastIP) == "" {
			wol.BroadcastIP = current.WoL.BroadcastIP
		}
		if wol.Port == 0 {
			wol.Port = current.WoL.Port
		}
		if strings.TrimSpace(wol.ShutdownTarget) == "" {
			wol.ShutdownTarget = current.WoL.ShutdownTarget
		}
		if wol.ShutdownUDPPort == 0 {
			wol.ShutdownUDPPort = current.WoL.ShutdownUDPPort
		}
		if strings.TrimSpace(wol.HMACSecret) == "" {
			wol.HMACSecret = current.WoL.HMACSecret
		}
		if strings.TrimSpace(wol.Token) == "" {
			wol.Token = current.WoL.Token
		}
		if wol.TokenTTLSeconds == 0 {
			wol.TokenTTLSeconds = current.WoL.TokenTTLSeconds
		}
		merged.WoL = &wol
	case power.PowerTypeIPMI:
		if current.IPMI == nil || next.IPMI == nil {
			return next
		}
		ipmi := *next.IPMI
		if strings.TrimSpace(ipmi.Password) == "" {
			ipmi.Password = current.IPMI.Password
		}
		merged.IPMI = &ipmi
	case power.PowerTypeWebhook:
		if current.Webhook == nil || next.Webhook == nil {
			return next
		}
		webhook := *next.Webhook
		if webhook.Headers == nil {
			webhook.Headers = current.Webhook.Headers
		}
		if webhook.BodyExtras == nil {
			webhook.BodyExtras = current.Webhook.BodyExtras
		}
		merged.Webhook = &webhook
	}
	return merged
}
