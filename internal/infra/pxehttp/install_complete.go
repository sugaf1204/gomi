package pxehttp

import (
	"context"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/node"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/vm"
	"log"
	"net"
	gohttp "net/http"
	"strings"
	"time"
)

type pxeInstallCompleteReq struct {
	Token    string `json:"token"`
	Type     string `json:"type,omitempty"`
	IP       string `json:"ip,omitempty"`
	MAC      string `json:"mac,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

func (h *Handler) PXEInstallComplete(c echo.Context) error {
	token := strings.TrimSpace(c.QueryParam("token"))
	source := strings.TrimSpace(c.QueryParam("type"))

	var report node.InstallCompleteReport
	if c.Request().ContentLength > 0 {
		var req pxeInstallCompleteReq
		if err := c.Bind(&req); err != nil {
			return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
		}
		if token == "" {
			token = strings.TrimSpace(req.Token)
		}
		if source == "" {
			source = strings.TrimSpace(req.Type)
		}
		report = node.InstallCompleteReport{
			IP:       strings.TrimSpace(req.IP),
			MAC:      strings.TrimSpace(req.MAC),
			Hostname: strings.TrimSpace(req.Hostname),
		}
	}
	if token == "" {
		return c.JSON(gohttp.StatusBadRequest, jsonError("token is required"))
	}
	source = normalizeCompletionSource(source)

	targetVM, err := h.findVirtualMachineByProvisionToken(c.Request().Context(), token)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if targetVM != nil {
		if strings.TrimSpace(targetVM.Provisioning.CompletionToken) != token {
			return c.JSON(gohttp.StatusNotFound, jsonError("provisioning token not found"))
		}
		if !targetVM.Provisioning.Active {
			if targetVM.Provisioning.CompletedAt == nil {
				return c.JSON(gohttp.StatusConflict, jsonError("provisioning token is expired"))
			}
			return c.JSON(gohttp.StatusOK, installCompleteVMResponse{Status: "already-finalized", VM: *targetVM})
		}
		if err := h.ensureVirtualMachineSSHReachable(c.Request().Context(), *targetVM, report); err != nil {
			return c.JSON(gohttp.StatusConflict, jsonErrorErr(err))
		}

		now := time.Now().UTC()
		updated := *targetVM
		updated.Provisioning.Active = false
		updated.Provisioning.CompletedAt = httputil.TimePtr(now)
		updated.Provisioning.LastSignalAt = httputil.TimePtr(now)
		updated.Provisioning.CompletionSource = source
		updated.LastError = ""
		updated.UpdatedAt = now
		updated.ApplyInstallCompleteReport(report)
		if err := h.vms.Store().Upsert(c.Request().Context(), updated); err != nil {
			return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
		}
		if h.vmRuntimeSyncer != nil {
			leaseIPByMAC, leaseErr := h.leaseIPsByMAC(c.Request().Context())
			if leaseErr != nil {
				leaseIPByMAC = nil
			}
			if synced, syncErr := h.vmRuntimeSyncer.Sync(c.Request().Context(), updated, leaseIPByMAC); syncErr == nil {
				updated = synced
			}
		}
		if updated.Phase == vm.PhaseProvisioning {
			updated.Phase = vm.PhaseRunning
			updated.UpdatedAt = now
			if err := h.vms.Store().Upsert(c.Request().Context(), updated); err != nil {
				return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
			}
		}

		if h.authStore != nil {
			httputil.CreateAudit(c, h.authStore, updated.Name, "complete-vm-provisioning", "success", "vm provisioning completed by pxe signal", map[string]string{
				"source": source,
			})
		}
		return c.JSON(gohttp.StatusOK, installCompleteVMResponse{Status: "ok", VM: updated})
	}

	targetMachine, err := h.findMachineByProvisionToken(c.Request().Context(), token)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if targetMachine == nil {
		return c.JSON(gohttp.StatusNotFound, jsonError("provisioning token not found"))
	}
	if targetMachine.Provision == nil || strings.TrimSpace(targetMachine.Provision.CompletionToken) != token {
		return c.JSON(gohttp.StatusNotFound, jsonError("provisioning token not found"))
	}
	if !targetMachine.Provision.Active {
		if targetMachine.Provision.CompletedAt == nil {
			return c.JSON(gohttp.StatusConflict, jsonError("provisioning token is expired"))
		}
		return c.JSON(gohttp.StatusOK, installCompleteMachineResponse{Status: "already-finalized", Machine: *targetMachine})
	}
	if err := h.ensureMachineSSHReachable(c.Request().Context(), *targetMachine, report); err != nil {
		return c.JSON(gohttp.StatusConflict, jsonErrorErr(err))
	}

	now := time.Now().UTC()
	updatedMachine := *targetMachine
	if updatedMachine.Provision == nil {
		updatedMachine.Provision = &machine.ProvisionProgress{}
	}
	updatedMachine.Provision.Active = false
	updatedMachine.Provision.CompletedAt = httputil.TimePtr(now)
	updatedMachine.Provision.LastSignalAt = httputil.TimePtr(now)
	updatedMachine.Provision.CompletionSource = source
	updatedMachine.Provision.Message = "provisioning completed"
	updatedMachine.Phase = machine.PhaseReady
	updatedMachine.LastError = ""
	updatedMachine.UpdatedAt = now
	updatedMachine.ApplyInstallCompleteReport(report)
	updatedMachine.Provision.Message = h.finalizeBIOSBootOrder(c.Request().Context(), updatedMachine)
	if err := h.machines.Store().Upsert(c.Request().Context(), updatedMachine); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	// Auto-create Hypervisor entity when a hypervisor-role machine finishes provisioning.
	if updatedMachine.Role == machine.RoleHypervisor && h.hypervisors != nil {
		hvIP := updatedMachine.IP
		if hvIP == "" && report.IP != "" {
			hvIP = report.IP
		}
		bridgeName := updatedMachine.BridgeName
		if bridgeName == "" {
			bridgeName = "br0"
		}
		// Only create if no hypervisor with this name exists yet — avoid
		// resetting phase/capacity of an already-running hypervisor on re-provision.
		if _, err := h.hypervisors.Get(c.Request().Context(), updatedMachine.Name); err != nil {
			newHV := hypervisor.Hypervisor{
				Name: updatedMachine.Name,
				Connection: hypervisor.ConnectionSpec{
					Type: hypervisor.ConnectionTCP,
					Host: hvIP,
					Port: 16509,
				},
				MachineRef: updatedMachine.Name,
				BridgeName: bridgeName,
				Phase:      hypervisor.PhasePending,
			}
			if _, createErr := h.hypervisors.Create(c.Request().Context(), newHV); createErr != nil {
				log.Printf("auto-create hypervisor for machine %s: %v", updatedMachine.Name, createErr)
			} else {
				log.Printf("auto-created hypervisor %s (bridge=%s, ip=%s)", updatedMachine.Name, bridgeName, hvIP)
			}
		} else {
			log.Printf("hypervisor %s already exists, skipping auto-create", updatedMachine.Name)
		}
	}

	if h.authStore != nil {
		httputil.CreateAudit(c, h.authStore, updatedMachine.Name, "complete-machine-provisioning", "success", "machine provisioning completed by pxe signal", map[string]string{
			"source": source,
		})
	}
	return c.JSON(gohttp.StatusOK, installCompleteMachineResponse{Status: "ok", Machine: updatedMachine})
}

func (h *Handler) finalizeBIOSBootOrder(ctx context.Context, m machine.Machine) string {
	return h.configureBIOSBootOrder(ctx, m, "provisioning completed")
}

func (h *Handler) configureBIOSBootOrder(ctx context.Context, m machine.Machine, baseMessage string) string {
	if strings.TrimSpace(baseMessage) == "" {
		baseMessage = "provisioning state updated"
	}
	if m.Firmware != machine.FirmwareBIOS || h.powerExecutor == nil {
		return baseMessage
	}
	if m.Power.Type != power.PowerTypeWebhook || m.Power.Webhook == nil || strings.TrimSpace(m.Power.Webhook.BootOrderURL) == "" {
		return baseMessage
	}
	mi := power.MachineInfo{
		Name:     m.Name,
		Hostname: m.Hostname,
		MAC:      m.MAC,
		IP:       m.IP,
		Power:    m.Power,
	}
	if err := h.powerExecutor.ConfigureBootOrder(ctx, mi, power.DefaultBIOSBootOrder); err != nil {
		log.Printf("pxe: machine=%s bios boot order update failed: %v", m.Name, err)
		return baseMessage + "; BIOS boot order update failed"
	}
	log.Printf("pxe: machine=%s bios boot order updated to %v", m.Name, power.DefaultBIOSBootOrder)
	return baseMessage + "; BIOS boot order updated"
}

func (h *Handler) ensureMachineSSHReachable(ctx context.Context, m machine.Machine, report node.InstallCompleteReport) error {
	if !strings.EqualFold(strings.TrimSpace(string(m.OSPreset.Family)), string(machine.OSTypeDebian)) {
		return nil
	}
	ip := installCompleteSSHProbeIP(report, m.IP)
	if ip == "" {
		return fmt.Errorf("debian install-complete is waiting for a reported SSH address")
	}
	if err := h.probeInstallCompleteSSH(ctx, ip); err != nil {
		return fmt.Errorf("debian install-complete is waiting for SSH on %s: %w", ip, err)
	}
	return nil
}

func (h *Handler) ensureVirtualMachineSSHReachable(ctx context.Context, v vm.VirtualMachine, report node.InstallCompleteReport) error {
	if !h.virtualMachineIsDebian(ctx, v) {
		return nil
	}
	ip := installCompleteSSHProbeIP(report, v.StaticIP())
	if ip == "" {
		for _, candidate := range v.IPAddresses {
			ip = strings.TrimSpace(candidate)
			if ip != "" {
				break
			}
		}
	}
	if ip == "" {
		return fmt.Errorf("debian install-complete is waiting for a reported SSH address")
	}
	if err := h.probeInstallCompleteSSH(ctx, ip); err != nil {
		return fmt.Errorf("debian install-complete is waiting for SSH on %s: %w", ip, err)
	}
	return nil
}

func installCompleteSSHProbeIP(report node.InstallCompleteReport, fallback string) string {
	ip := strings.TrimSpace(report.IP)
	if ip == "" {
		ip = strings.TrimSpace(fallback)
	}
	return ip
}

func (h *Handler) probeInstallCompleteSSH(ctx context.Context, ip string) error {
	probe := h.machineSSHProbe
	if probe == nil {
		probe = probeSSHPort
	}
	return probe(ctx, ip)
}

func (h *Handler) virtualMachineIsDebian(ctx context.Context, v vm.VirtualMachine) bool {
	if h.osimages == nil || strings.TrimSpace(v.OSImageRef) == "" {
		return false
	}
	img, err := h.osimages.Get(ctx, strings.TrimSpace(v.OSImageRef))
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(img.OSFamily), string(machine.OSTypeDebian))
}

func probeSSHPort(ctx context.Context, ip string) error {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip, "22"))
	if err != nil {
		return err
	}
	return conn.Close()
}

func normalizeCompletionSource(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch vm.InstallConfigType(normalized) {
	case vm.InstallConfigCurtin:
		return string(vm.InstallConfigCurtin)
	case vm.InstallConfigPreseed:
		return string(vm.InstallConfigPreseed)
	default:
		return "unknown"
	}
}
