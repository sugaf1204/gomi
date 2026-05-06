package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	gohttp "net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
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

type redeployReq struct {
	Confirm  string             `json:"confirm,omitempty"`
	Hostname *string            `json:"hostname,omitempty"`
	MAC      *string            `json:"mac,omitempty"`
	Arch     *string            `json:"arch,omitempty"`
	Firmware *string            `json:"firmware,omitempty"`
	Power    *power.PowerConfig `json:"power,omitempty"`
	OSPreset *struct {
		ImageRef string `json:"imageRef,omitempty"`
	} `json:"osPreset,omitempty"`
	TargetDisk *string `json:"targetDisk,omitempty"`
	Network    *struct {
		Domain string `json:"domain,omitempty"`
	} `json:"network,omitempty"`
	CloudInitRef  *string                `json:"cloudInitRef,omitempty"`
	CloudInitRefs *[]string              `json:"cloudInitRefs,omitempty"`
	SubnetRef     *string                `json:"subnetRef,omitempty"`
	IPAssignment  *string                `json:"ipAssignment,omitempty"`
	IP            *string                `json:"ip,omitempty"`
	Role          *string                `json:"role,omitempty"`
	BridgeName    *string                `json:"bridgeName,omitempty"`
	SSHKeyRefs    *[]string              `json:"sshKeyRefs,omitempty"`
	LoginUser     *machine.LoginUserSpec `json:"loginUser,omitempty"`
}

func (s *Server) RedeployMachine(c echo.Context) error {
	name := c.Param("name")
	user, _ := httputil.UserFromContext(c)
	ctx := c.Request().Context()

	var opts *machine.ReinstallOptions
	var req redeployReq
	if c.Request().ContentLength > 0 {
		if err := c.Bind(&req); err != nil {
			return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
		}
	}
	if strings.TrimSpace(req.Confirm) != "" && strings.TrimSpace(req.Confirm) != name {
		return c.JSON(gohttp.StatusBadRequest, jsonError("confirm must match machine name"))
	}

	current, err := s.machines.Get(ctx, name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	powerCycleFallbackIP := current.IP

	if req.Hostname != nil || req.MAC != nil || req.Arch != nil || req.Firmware != nil || req.Power != nil || req.OSPreset != nil || req.TargetDisk != nil || req.Network != nil || req.CloudInitRef != nil || req.CloudInitRefs != nil || req.SubnetRef != nil || req.IPAssignment != nil || req.IP != nil || req.Role != nil || req.BridgeName != nil || req.SSHKeyRefs != nil || req.LoginUser != nil {
		var powerCfg *power.PowerConfig
		if req.Power != nil {
			copyCfg := mergePowerConfigDefaults(current.Power, *req.Power)
			if err := power.FillWoLDefaults(&copyCfg); err != nil {
				return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
			}
			if err := power.ValidatePowerConfig(copyCfg); err != nil {
				return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
			}
			powerCfg = &copyCfg
		}

		nextPreset := current.OSPreset
		if req.OSPreset != nil && strings.TrimSpace(req.OSPreset.ImageRef) != "" {
			nextPreset.ImageRef = strings.TrimSpace(req.OSPreset.ImageRef)
			draft := current
			draft.OSPreset = nextPreset
			if err := s.resolveOSPreset(ctx, &draft); err != nil {
				return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
			}
			nextPreset = draft.OSPreset
		}

		var networkCfg *machine.NetworkConfig
		if req.Network != nil {
			networkCfg = &machine.NetworkConfig{Domain: req.Network.Domain}
		}

		var subnetRef *string
		if req.SubnetRef != nil {
			trimmed := strings.TrimSpace(*req.SubnetRef)
			if trimmed != "" {
				if _, err := s.subnets.Get(ctx, trimmed); err != nil {
					return c.JSON(gohttp.StatusBadRequest, jsonError("referenced subnetRef not found"))
				}
			}
			subnetRef = &trimmed
		}

		var ipAssignment *machine.IPAssignmentMode
		if req.IPAssignment != nil {
			mode := machine.IPAssignmentMode(strings.TrimSpace(*req.IPAssignment))
			if mode != "" && mode != machine.IPAssignmentModeDHCP && mode != machine.IPAssignmentModeStatic {
				return c.JSON(gohttp.StatusBadRequest, jsonError("ipAssignment must be 'dhcp' or 'static'"))
			}
			ipAssignment = &mode
		}

		var firmware *machine.Firmware
		if req.Firmware != nil {
			value := machine.Firmware(strings.TrimSpace(*req.Firmware))
			firmware = &value
		}

		var role *machine.Role
		if req.Role != nil {
			value := machine.Role(strings.TrimSpace(*req.Role))
			if value != machine.RoleDefault && value != machine.RoleHypervisor {
				return c.JSON(gohttp.StatusBadRequest, jsonError("role must be '' or 'hypervisor'"))
			}
			if value == machine.RoleHypervisor && current.Role != machine.RoleHypervisor {
				if _, err := s.hypervisors.Get(ctx, name); err == nil {
					return c.JSON(gohttp.StatusConflict, jsonError(fmt.Sprintf("hypervisor %q already exists", name)))
				} else if !errors.Is(err, resource.ErrNotFound) {
					return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
				}
			}
			role = &value
		}

		var bridgeName *string
		if req.BridgeName != nil || (role != nil && *role == machine.RoleHypervisor) {
			trimmed := ""
			if req.BridgeName != nil {
				trimmed = strings.TrimSpace(*req.BridgeName)
			}
			if role != nil && *role == machine.RoleHypervisor && trimmed == "" {
				trimmed = "br0"
			}
			bridgeName = &trimmed
		}

		opts = &machine.ReinstallOptions{
			Hostname:      req.Hostname,
			MAC:           req.MAC,
			Arch:          req.Arch,
			Firmware:      firmware,
			Power:         powerCfg,
			OSPreset:      &nextPreset,
			TargetDisk:    req.TargetDisk,
			Network:       networkCfg,
			CloudInitRef:  req.CloudInitRef,
			CloudInitRefs: req.CloudInitRefs,
			SubnetRef:     subnetRef,
			IPAssignment:  ipAssignment,
			IP:            req.IP,
			Role:          role,
			BridgeName:    bridgeName,
			SSHKeyRefs:    req.SSHKeyRefs,
			LoginUser:     req.LoginUser,
		}
	}

	m, err := s.machines.Reinstall(ctx, name, user.Username, opts)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		httputil.CreateAudit(c, s.authStore, name, "redeploy", "failure", err.Error(), nil)
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	if changed, err := s.attachHypervisorRegistrationToken(ctx, &m); err != nil {
		httputil.CreateAudit(c, s.authStore, name, "redeploy", "failure", err.Error(), nil)
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	} else if changed {
		if err := s.machines.Store().Upsert(ctx, m); err != nil {
			httputil.CreateAudit(c, s.authStore, name, "redeploy", "failure", err.Error(), nil)
			return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
		}
	}
	httputil.CreateAudit(c, s.authStore, name, "redeploy", "success", "redeploy started", nil)
	s.startRedeployPowerCycle(current, m, powerCycleFallbackIP)
	return c.JSON(gohttp.StatusAccepted, machineResponse(m))
}

func (s *Server) attachHypervisorRegistrationToken(ctx context.Context, m *machine.Machine) (bool, error) {
	if m == nil || m.Role != machine.RoleHypervisor || m.Provision == nil || s.hypervisors == nil {
		return false, nil
	}
	token, err := s.hypervisors.CreateToken(ctx)
	if err != nil {
		return false, fmt.Errorf("create hypervisor registration token: %w", err)
	}
	if m.Provision.Artifacts == nil {
		m.Provision.Artifacts = map[string]string{}
	}
	m.Provision.Artifacts[machine.ProvisionArtifactHypervisorRegistrationToken] = token.Token
	m.Provision.Artifacts[machine.ProvisionArtifactHypervisorRegistrationTokenExpiresAt] = token.ExpiresAt.Format(time.RFC3339)
	return true, nil
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

func (s *Server) ReinstallMachine(c echo.Context) error {
	return s.RedeployMachine(c)
}

func (s *Server) PowerOnMachine(c echo.Context) error {
	return s.runPowerAction(c, "power-on")
}

func (s *Server) PowerOffMachine(c echo.Context) error {
	return s.runPowerAction(c, "power-off")
}

func (s *Server) runPowerAction(c echo.Context, actionStr string) error {
	name := c.Param("name")
	m, err := s.machines.Get(c.Request().Context(), name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if err := power.FillWoLDefaults(&m.Power); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if err := power.ValidatePowerConfig(m.Power); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	action := power.Action(actionStr)
	mi := power.MachineInfo{
		Name:     m.Name,
		Hostname: m.Hostname,
		MAC:      m.MAC,
		IP:       m.IP,
		Power:    m.Power,
	}
	result, err := s.executePowerAction(c.Request().Context(), mi, action)
	if err != nil {
		s.recordMachinePowerAction(name, action, stringPtr(err.Error()))
		httputil.CreateAudit(c, s.authStore, name, actionStr, "failure", err.Error(), nil)
		return c.JSON(gohttp.StatusBadGateway, jsonErrorErr(err))
	}
	s.recordMachinePowerAction(name, action, stringPtr(""))
	details := map[string]string{}
	if result.RequestID != "" {
		details["requestID"] = result.RequestID
	}
	if len(details) == 0 {
		details = nil
	}
	httputil.CreateAudit(c, s.authStore, name, actionStr, "success", "power action complete", details)
	return c.JSON(gohttp.StatusOK, statusResponse{
		Status:    "ok",
		RequestID: result.RequestID,
	})
}

func (s *Server) executePowerAction(ctx context.Context, mi power.MachineInfo, action power.Action) (power.ActionResult, error) {
	if execWithResult, ok := s.powerExecutor.(PowerExecutorWithResult); ok {
		return execWithResult.ExecuteWithResult(ctx, mi, action)
	}
	return power.ActionResult{}, s.powerExecutor.Execute(ctx, mi, action)
}

var (
	redeployPowerCycleTimeout        = 75 * time.Second
	redeployPowerOffSettleTimeout    = 30 * time.Second
	redeployPowerOffPollInterval     = 2 * time.Second
	redeployWoLPowerOffMinimumSettle = 15 * time.Second
)

func (s *Server) startRedeployPowerCycle(before, after machine.Machine, fallbackIP string) {
	if s.powerExecutor == nil || before.Power.Type == "" || before.Power.Type == power.PowerTypeManual {
		return
	}
	powerOffInfo := machinePowerInfo(before, fallbackIP)
	powerOnMachine := after
	if powerOnMachine.Power.Type == "" || powerOnMachine.Power.Type == power.PowerTypeManual {
		powerOnMachine = before
	}
	powerOnInfo := machinePowerInfo(powerOnMachine, fallbackIP)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), redeployPowerCycleTimeout)
		defer cancel()

		if err := s.powerExecutor.Execute(ctx, powerOffInfo, power.ActionPowerOff); err != nil {
			s.recordMachinePowerAction(after.Name, power.ActionPowerOff, stringPtr(err.Error()))
			log.Printf("redeploy power-cycle: machine=%s power-off failed: %v", after.Name, err)
			return
		}
		s.recordMachinePowerAction(after.Name, power.ActionPowerOff, nil)

		if before.Power.Type == power.PowerTypeWoL {
			if err := sleepContext(ctx, redeployWoLPowerOffMinimumSettle); err != nil {
				s.recordMachinePowerAction(after.Name, power.ActionPowerOff, stringPtr(err.Error()))
				log.Printf("redeploy power-cycle: machine=%s interrupted while waiting after WoL power-off: %v", after.Name, err)
				return
			}
		}

		if !s.waitForMachinePowerState(ctx, powerOffInfo, power.PowerStateStopped, redeployPowerOffSettleTimeout) {
			err := fmt.Errorf("machine did not report stopped within %s after power-off", redeployPowerOffSettleTimeout)
			s.recordMachinePowerAction(after.Name, power.ActionPowerOff, stringPtr(err.Error()))
			log.Printf("redeploy power-cycle: machine=%s %v", after.Name, err)
			return
		}

		if err := s.powerExecutor.Execute(ctx, powerOnInfo, power.ActionPowerOn); err != nil {
			s.recordMachinePowerAction(after.Name, power.ActionPowerOn, stringPtr(err.Error()))
			log.Printf("redeploy power-cycle: machine=%s power-on failed: %v", after.Name, err)
			return
		}
		s.recordMachinePowerAction(after.Name, power.ActionPowerOn, nil)
	}()
}

func machinePowerInfo(m machine.Machine, fallbackIP string) power.MachineInfo {
	ip := strings.TrimSpace(m.IP)
	if ip == "" {
		ip = strings.TrimSpace(fallbackIP)
	}
	if ip == "" && m.Power.Type == power.PowerTypeWoL && m.Power.WoL != nil {
		ip = strings.TrimSpace(m.Power.WoL.ShutdownTarget)
	}
	return power.MachineInfo{
		Name:     m.Name,
		Hostname: m.Hostname,
		MAC:      m.MAC,
		IP:       ip,
		Power:    m.Power,
	}
}

func (s *Server) waitForMachinePowerState(ctx context.Context, mi power.MachineInfo, want power.PowerState, timeout time.Duration) bool {
	if s.powerExecutor == nil || timeout <= 0 {
		return false
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(redeployPowerOffPollInterval)
	defer ticker.Stop()

	for {
		state, err := s.powerExecutor.CheckStatus(waitCtx, mi)
		if err == nil && state == want {
			return true
		}
		select {
		case <-waitCtx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func stringPtr(value string) *string {
	return &value
}

func (s *Server) recordMachinePowerAction(name string, action power.Action, lastError *string) {
	if s.machines == nil {
		return
	}
	storeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	updater, ok := s.machines.Store().(machine.PowerActionStatusUpdater)
	if !ok {
		log.Printf("machine power action status: machine store cannot update partial power status for %s after %s", name, action)
		return
	}
	if err := updater.UpdatePowerActionStatus(storeCtx, name, action, lastError, time.Now().UTC()); err != nil {
		log.Printf("machine power action status: failed to update %s after %s: %v", name, action, err)
	}
}

// resolveOSPreset derives osPreset.family and osPreset.version from the
// referenced OS image. This ensures that the machine's family/version always
// match the actual image, preventing mismatches such as family=ubuntu with
// imageRef=debian-13-amd64.
func (s *Server) resolveOSPreset(ctx context.Context, m *machine.Machine) error {
	ref := strings.TrimSpace(m.OSPreset.ImageRef)
	if ref == "" {
		return nil
	}
	if s.osimages == nil {
		return fmt.Errorf("os image service not available")
	}
	img, err := s.osimages.Get(ctx, ref)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return fmt.Errorf("referenced os image not found: %s", ref)
		}
		return err
	}
	m.OSPreset.Family = machine.OSType(img.OSFamily)
	m.OSPreset.Version = img.OSVersion
	return nil
}
