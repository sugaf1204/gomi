package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
	"log"
	gohttp "net/http"
	"strings"
	"time"
)

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
