package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/libvirt"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
	"log"
	gohttp "net/http"
	"strings"
	"time"
)

type vmPowerActionReq struct {
	Action string `json:"action"`
}

func (s *Server) PowerOnVM(c echo.Context) error {
	return s.runVMPowerAction(c, "power-on")
}

func (s *Server) PowerOffVM(c echo.Context) error {
	return s.runVMPowerAction(c, "power-off")
}

func (s *Server) runVMPowerAction(c echo.Context, action string) error {
	name := c.Param("name")
	ctx := c.Request().Context()

	v, err := s.vms.Get(ctx, name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}

	hv, err := s.hypervisors.Get(ctx, v.HypervisorRef)
	if err != nil {
		msg := fmt.Sprintf("hypervisor %q not found: %v", v.HypervisorRef, err)
		httputil.CreateAudit(c, s.authStore, name, action, "failure", msg, nil)
		return c.JSON(gohttp.StatusInternalServerError, jsonError(msg))
	}

	libvirtErr := s.executeLibvirtPowerAction(ctx, hv, v, action)
	if libvirtErr != nil {
		log.Printf("libvirt %s for vm %s: %v (updating status anyway)", action, name, libvirtErr)
	}

	var targetPhase vm.Phase
	var lastErr string
	switch action {
	case "power-on":
		targetPhase = vm.PhaseRunning
	case "power-off":
		targetPhase = vm.PhaseStopped
	}

	if libvirtErr != nil {
		targetPhase = vm.PhaseError
		lastErr = libvirtErr.Error()
	}

	updated, err := s.vms.UpdateStatus(ctx, name, targetPhase, action, lastErr)
	if err != nil {
		httputil.CreateAudit(c, s.authStore, name, action, "failure", err.Error(), nil)
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}

	if libvirtErr != nil {
		httputil.CreateAudit(c, s.authStore, name, action, "failure", libvirtErr.Error(), nil)
		return c.JSON(gohttp.StatusInternalServerError, struct {
			Error string `json:"error"`
			Phase string `json:"phase"`
		}{
			Error: fmt.Sprintf("libvirt action failed: %v", libvirtErr),
			Phase: string(updated.Phase),
		})
	}

	httputil.CreateAudit(c, s.authStore, name, action, "success", "vm power action complete", nil)
	return c.JSON(gohttp.StatusOK, updated)
}

func (s *Server) executeLibvirtPowerAction(ctx context.Context, hv hypervisor.Hypervisor, v vm.VirtualMachine, action string) error {
	cfg := vm.BuildLibvirtConfig(hv)
	exec, err := libvirt.NewExecutor(cfg)
	if err != nil {
		return fmt.Errorf("connect to hypervisor %s: %w", hv.Name, err)
	}
	defer exec.Close()

	domainName := v.LibvirtDomain
	if domainName == "" {
		domainName = v.Name
	}

	switch action {
	case "power-on":
		return exec.StartDomain(ctx, domainName)
	case "power-off":
		return powerOffDomain(ctx, exec, domainName)
	default:
		return fmt.Errorf("unknown vm power action: %s", action)
	}
}

var (
	vmGracefulPowerOffTimeout = 20 * time.Second
	vmPowerOffPollInterval    = 2 * time.Second
)

type vmPowerOffExecutor interface {
	ShutdownDomain(ctx context.Context, name string) error
	DestroyDomain(ctx context.Context, name string) error
	DomainInfo(ctx context.Context, name string) (*libvirt.DomainInfo, error)
}

func powerOffDomain(ctx context.Context, exec vmPowerOffExecutor, domainName string) error {
	if err := exec.ShutdownDomain(ctx, domainName); err != nil {
		if vm.IsIgnorableDestroyError(err) {
			return nil
		}
		return err
	}

	gracefulCtx, cancel := context.WithTimeout(ctx, vmGracefulPowerOffTimeout)
	defer cancel()
	ticker := time.NewTicker(vmPowerOffPollInterval)
	defer ticker.Stop()

	for {
		info, err := exec.DomainInfo(gracefulCtx, domainName)
		if err == nil && info != nil {
			if info.State == libvirt.StateShutoff || info.State == libvirt.StatePaused {
				return nil
			}
		}

		select {
		case <-gracefulCtx.Done():
			if err := exec.DestroyDomain(ctx, domainName); err != nil && !vm.IsIgnorableDestroyError(err) {
				return fmt.Errorf("graceful shutdown timed out and force power-off failed: %w", err)
			}
			return nil
		case <-ticker.C:
		}
	}
}

func (s *Server) leaseIPsByMAC(ctx context.Context) (map[string]string, error) {
	if s.leaseStore == nil {
		return nil, nil
	}
	leases, err := s.leaseStore.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(leases))
	for _, lease := range leases {
		mac := strings.ToLower(strings.TrimSpace(lease.MAC))
		ip := strings.TrimSpace(lease.IP)
		if mac == "" || ip == "" {
			continue
		}
		out[mac] = ip
	}
	return out, nil
}
