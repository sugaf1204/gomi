package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	gohttp "net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
)

type createMachineRequest struct {
	MachineID string              `json:"machineId,omitempty"`
	Machine   *machineSpecRequest `json:"machine"`
}

type machineSpecRequest struct {
	Hostname      string                   `json:"hostname"`
	MAC           string                   `json:"mac"`
	IP            string                   `json:"ip,omitempty"`
	Arch          string                   `json:"arch"`
	Firmware      machine.Firmware         `json:"firmware"`
	Power         power.PowerConfig        `json:"power"`
	Network       machine.NetworkConfig    `json:"network"`
	OSPreset      machine.OSPreset         `json:"osPreset"`
	TargetDisk    string                   `json:"targetDisk,omitempty"`
	CloudInitRef  string                   `json:"cloudInitRef,omitempty"`
	CloudInitRefs []string                 `json:"cloudInitRefs,omitempty"`
	IPAssignment  machine.IPAssignmentMode `json:"ipAssignment,omitempty"`
	SubnetRef     string                   `json:"subnetRef,omitempty"`
	Role          machine.Role             `json:"role,omitempty"`
	BridgeName    string                   `json:"bridgeName,omitempty"`
	SSHKeyRefs    []string                 `json:"sshKeyRefs,omitempty"`
	LoginUser     *machine.LoginUserSpec   `json:"loginUser,omitempty"`
}

func (s *Server) CreateMachine(c echo.Context) error {
	m, err := bindCreateMachine(c)
	if err != nil {
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
	machines = filterMachines(c, machines)
	p, err := parsePagination(c, len(machines))
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, ListMachinesResponse{
		Machines:      machineResponses(paginate(machines, p)),
		NextPageToken: p.nextPageToken,
		TotalSize:     p.totalSize,
	})
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

func bindCreateMachine(c echo.Context) (machine.Machine, error) {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return machine.Machine{}, err
	}
	var req createMachineRequest
	if err := json.Unmarshal(body, &req); err == nil && req.Machine != nil {
		id := strings.TrimSpace(c.QueryParam("machineId"))
		if id == "" {
			id = strings.TrimSpace(req.MachineID)
		}
		if id == "" {
			return machine.Machine{}, fmt.Errorf("machineId is required")
		}
		return req.Machine.toMachine(resourceID("machines", id)), nil
	}
	var m machine.Machine
	if err := json.Unmarshal(body, &m); err != nil {
		return machine.Machine{}, err
	}
	normalizeMachineRequestRefs(&m)
	return m, nil
}

func (r machineSpecRequest) toMachine(id string) machine.Machine {
	m := machine.Machine{
		Name:          id,
		Hostname:      r.Hostname,
		MAC:           r.MAC,
		IP:            r.IP,
		Arch:          r.Arch,
		Firmware:      r.Firmware,
		Power:         r.Power,
		Network:       r.Network,
		OSPreset:      r.OSPreset,
		TargetDisk:    r.TargetDisk,
		CloudInitRef:  r.CloudInitRef,
		CloudInitRefs: r.CloudInitRefs,
		IPAssignment:  r.IPAssignment,
		SubnetRef:     r.SubnetRef,
		Role:          r.Role,
		BridgeName:    r.BridgeName,
		SSHKeyRefs:    r.SSHKeyRefs,
		LoginUser:     r.LoginUser,
	}
	normalizeMachineRequestRefs(&m)
	return m
}

func normalizeMachineRequestRefs(m *machine.Machine) {
	if m == nil {
		return
	}
	m.Name = resourceID("machines", m.Name)
	m.OSPreset.ImageRef = resourceID("osImages", m.OSPreset.ImageRef)
	m.CloudInitRef = resourceID("cloudInitTemplates", m.CloudInitRef)
	m.CloudInitRefs = resourceIDs("cloudInitTemplates", m.CloudInitRefs)
	m.SubnetRef = resourceID("subnets", m.SubnetRef)
	m.SSHKeyRefs = resourceIDs("sshKeys", m.SSHKeyRefs)
}

func filterMachines(c echo.Context, machines []machine.Machine) []machine.Machine {
	phase := stringFilter(c, "phase")
	role := stringFilter(c, "role")
	subnetRef := resourceID("subnets", stringFilter(c, "subnetRef"))
	ipAssignment := stringFilter(c, "ipAssignment")
	if phase == "" && role == "" && subnetRef == "" && ipAssignment == "" {
		return machines
	}
	out := make([]machine.Machine, 0, len(machines))
	for _, m := range machines {
		if !matchFilter(string(m.Phase), phase) {
			continue
		}
		if !matchFilter(string(m.Role), role) {
			continue
		}
		if !matchFilter(m.SubnetRef, subnetRef) {
			continue
		}
		if !matchFilter(string(m.IPAssignment), ipAssignment) {
			continue
		}
		out = append(out, m)
	}
	return out
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
	req.SubnetRef = resourceID("subnets", req.SubnetRef)
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
