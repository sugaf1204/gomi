package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
)

// Windows WinPE can receive a different DHCP address and need not answer ICMP.
// Its authenticated disk-install callback is stronger evidence of wake-up.
func windowsWakeProgress(current, expected machine.Machine) (bool, error) {
	if expected.Provision == nil || expected.Provision.AttemptID == "" || current.Provision == nil || current.Provision.AttemptID != expected.Provision.AttemptID || current.Name != expected.Name || !strings.EqualFold(current.MAC, expected.MAC) || current.PXEInstallType() != resource.InstallWindows {
		return false, fmt.Errorf("Windows deployment attempt changed during power-on")
	}
	p := current.Provision
	return p.Artifacts["windowsInstallStarted"] == "true" || p.Artifacts["imageApplied"] == "true" || (p.CompletedAt != nil && p.CompletionSource == "windows"), nil
}

func (s *Server) powerOnWindowsRedeployMachine(ctx context.Context, info power.MachineInfo, expected machine.Machine) error {
	for {
		ready, err := s.windowsWakeObserved(ctx, expected)
		if err != nil || ready {
			return err
		}
		if err := s.powerExecutor.Execute(ctx, info, power.ActionPowerOn); err != nil {
			return err
		}
		waitCtx, cancel := context.WithTimeout(ctx, redeployPowerOffSettleTimeout)
		ready, err = s.waitWindowsWake(waitCtx, info, expected)
		expired := waitCtx.Err() != nil
		cancel()
		if ready {
			return nil
		}
		if err != nil && !expired {
			return err
		}
		if ctx.Err() != nil {
			return fmt.Errorf("machine did not report running after Wake-on-LAN retries: %w", ctx.Err())
		}
	}
}

func (s *Server) windowsWakeObserved(ctx context.Context, expected machine.Machine) (bool, error) {
	current, err := s.machines.Get(ctx, expected.Name)
	if err != nil {
		return false, err
	}
	ready, err := windowsWakeProgress(current, expected)
	if err == nil && !ready && !current.Provision.Active {
		return false, fmt.Errorf("Windows deployment is no longer active")
	}
	return ready, err
}

func (s *Server) waitWindowsWake(ctx context.Context, info power.MachineInfo, expected machine.Machine) (bool, error) {
	ticker := time.NewTicker(redeployPowerOffPollInterval)
	defer ticker.Stop()
	for {
		ready, err := s.windowsWakeObserved(ctx, expected)
		if err != nil || ready {
			return ready, err
		}
		// Preserve ICMP confirmation where it works, but refresh the DHCP address.
		current, err := s.machines.Get(ctx, expected.Name)
		if err != nil {
			return false, err
		}
		if ready, err := windowsWakeProgress(current, expected); err != nil || ready {
			return ready, err
		}
		if strings.TrimSpace(current.IP) != "" {
			info.IP = current.IP
		}
		state, statusErr := s.powerExecutor.CheckStatus(ctx, info)
		if statusErr == nil && state == power.PowerStateRunning {
			// Fence success too: the status call may have outlived this attempt.
			_, err := s.windowsWakeObserved(ctx, expected)
			return err == nil, err
		}
		select {
		case <-ctx.Done():
			return false, nil
		case <-ticker.C:
		}
	}
}

// Snapshot CAS keeps a late power result from modifying a new attempt, or
// overwriting an installer error. A nil success never clears LastError.
func (s *Server) recordWindowsRedeployPowerAction(expected machine.Machine, action power.Action, lastError *string) {
	updater, ok := s.machines.Store().(machine.ProvisioningAttemptUpdater)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 3; i++ {
		current, err := s.machines.Get(ctx, expected.Name)
		if err != nil {
			return
		}
		progressed, err := windowsWakeProgress(current, expected)
		if err != nil || !current.Provision.Active {
			return
		}
		if lastError != nil && (progressed || current.LastError != "") {
			return
		}
		next := current
		next.LastPowerAction = string(action)
		if lastError != nil {
			next.LastError = *lastError
		}
		next.UpdatedAt = time.Now().UTC()
		written, err := updater.UpdateProvisioningAttempt(ctx, current, next)
		if err != nil || written {
			return
		}
	}
}
