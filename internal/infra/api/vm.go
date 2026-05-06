package api

import (
	"context"
	crand "crypto/rand"
	"errors"
	"fmt"
	"log"
	gohttp "net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/infra/pxehttp"
	"github.com/sugaf1204/gomi/internal/libvirt"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
)

const defaultVMBridge = "br-eth0"

func (s *Server) CreateVirtualMachine(c echo.Context) error {
	var v vm.VirtualMachine
	if err := c.Bind(&v); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	ctx := c.Request().Context()
	ensureVMNetwork(&v)
	if err := s.applyInstallConfigByOSImage(ctx, &v); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	token, err := httputil.GenerateProvisioningToken()
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError("failed to issue provisioning token"))
	}
	now := time.Now().UTC()
	v.Provisioning = vm.ProvisioningStatus{
		Active:          true,
		StartedAt:       httputil.TimePtr(now),
		DeadlineAt:      httputil.TimePtr(now.Add(s.provisionTimeout)),
		CompletionToken: token,
	}

	if v.HypervisorRef == "" {
		hvList, err := s.hypervisors.List(ctx)
		if err != nil {
			return c.JSON(gohttp.StatusInternalServerError, jsonError(fmt.Sprintf("list hypervisors: %v", err)))
		}
		selected := vm.SelectHypervisor(hvList)
		if selected == "" {
			return c.JSON(gohttp.StatusBadRequest, jsonError("no ready hypervisors available for auto-placement"))
		}
		v.HypervisorRef = selected
	} else {
		if _, err := s.hypervisors.Get(ctx, v.HypervisorRef); err != nil {
			if errors.Is(err, resource.ErrNotFound) {
				return c.JSON(gohttp.StatusBadRequest, jsonError("referenced hypervisorRef not found: "+v.HypervisorRef))
			}
			return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
		}
	}

	resolveVMBridgeFromHypervisor(ctx, &v, s.hypervisors)

	created, err := s.vms.Create(ctx, v)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}

	if s.vmDeployer != nil {
		if deployErr := s.vmDeployer.Deploy(ctx, &created, pxehttp.RenderNoCloudLineConfig); deployErr != nil {
			httputil.CreateAudit(c, s.authStore, created.Name, "create-vm", "partial", "vm created but deploy failed: "+deployErr.Error(), nil)
		} else {
			httputil.CreateAudit(c, s.authStore, created.Name, "create-vm", "success", "virtual machine created", nil)
		}
	} else {
		httputil.CreateAudit(c, s.authStore, created.Name, "create-vm", "success", "virtual machine created", nil)
	}
	return c.JSON(gohttp.StatusCreated, created)
}

func (s *Server) ListVirtualMachines(c echo.Context) error {
	ctx := c.Request().Context()
	items, err := s.vms.List(ctx)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if s.vmRuntimeSyncer != nil {
		leaseIPByMAC, leaseErr := s.leaseIPsByMAC(ctx)
		if leaseErr != nil {
			log.Printf("vm-sync: list lease lookup failed: %v", leaseErr)
		}
		for i := range items {
			synced, syncErr := s.vmRuntimeSyncer.Sync(ctx, items[i], leaseIPByMAC)
			if syncErr != nil {
				log.Printf("vm-sync: list %s: %v", items[i].Name, syncErr)
				continue
			}
			items[i] = synced
		}
	}
	return c.JSON(gohttp.StatusOK, itemsResponse[vm.VirtualMachine]{Items: items})
}

func (s *Server) GetVirtualMachine(c echo.Context) error {
	name := c.Param("name")
	ctx := c.Request().Context()
	v, err := s.vms.Get(ctx, name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if s.vmRuntimeSyncer != nil {
		leaseIPByMAC, leaseErr := s.leaseIPsByMAC(ctx)
		if leaseErr != nil {
			log.Printf("vm-sync: get %s lease lookup failed: %v", name, leaseErr)
		}
		if synced, syncErr := s.vmRuntimeSyncer.Sync(ctx, v, leaseIPByMAC); syncErr != nil {
			log.Printf("vm-sync: get %s: %v", name, syncErr)
		} else {
			v = synced
		}
	}
	return c.JSON(gohttp.StatusOK, v)
}

func (s *Server) DeleteVirtualMachine(c echo.Context) error {
	name := c.Param("name")
	ctx := c.Request().Context()
	v, err := s.vms.Get(ctx, name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if err := s.deleteVirtualMachineRuntime(ctx, v); err != nil {
		httputil.CreateAudit(c, s.authStore, name, "delete-vm", "failure", err.Error(), nil)
		return c.JSON(gohttp.StatusBadGateway, jsonErrorErr(err))
	}
	if err := s.vms.Delete(ctx, name); err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, name, "delete-vm", "success", "virtual machine deleted", nil)
	return c.NoContent(gohttp.StatusNoContent)
}

func (s *Server) deleteVirtualMachineRuntime(ctx context.Context, v vm.VirtualMachine) error {
	if s.vmRuntimeDeleter != nil {
		return s.vmRuntimeDeleter(ctx, v)
	}
	if s.hypervisors == nil {
		return nil
	}
	hvRef := strings.TrimSpace(v.HypervisorRef)
	if hvRef == "" {
		return nil
	}
	hv, err := s.hypervisors.Get(ctx, hvRef)
	if err != nil {
		return fmt.Errorf("resolve hypervisor %s for delete: %w", hvRef, err)
	}
	cfg := vm.BuildLibvirtConfig(hv)
	exec, err := libvirt.NewExecutor(cfg)
	if err != nil {
		return fmt.Errorf("connect to hypervisor %s for delete: %w", hv.Name, err)
	}
	defer exec.Close()

	domainName := strings.TrimSpace(v.LibvirtDomain)
	if domainName == "" {
		domainName = v.Name
	}
	if err := exec.DestroyDomain(ctx, domainName); err != nil && !vm.IsIgnorableDestroyError(err) {
		return fmt.Errorf("stop domain %s before delete: %w", domainName, err)
	}
	if err := exec.UndefineDomain(ctx, domainName); err != nil && !vm.IsIgnorableDestroyError(err) {
		return fmt.Errorf("undefine domain %s before delete: %w", domainName, err)
	}
	if err := exec.DeleteVolume(ctx, v.Name); err != nil {
		return fmt.Errorf("delete volume %s: %w", v.Name, err)
	}
	return nil
}

type vmPowerActionReq struct {
	Action string `json:"action"`
}

type vmReinstallReq struct {
	Confirm            string                `json:"confirm,omitempty"`
	HypervisorRef      string                `json:"hypervisorRef,omitempty"`
	Resources          *vm.ResourceSpec      `json:"resources,omitempty"`
	OSImageRef         string                `json:"osImageRef,omitempty"`
	Network            []vm.NetworkInterface `json:"network,omitempty"`
	SubnetRef          *string               `json:"subnetRef,omitempty"`
	Domain             *string               `json:"domain,omitempty"`
	InstallConfig      *vm.InstallConfig     `json:"installConfig,omitempty"`
	CloudInitRef       string                `json:"cloudInitRef,omitempty"`
	CloudInitRefs      []string              `json:"cloudInitRefs,omitempty"`
	AdvancedOptions    *vm.AdvancedOptions   `json:"advancedOptions,omitempty"`
	PowerControlMethod vm.PowerControlMethod `json:"powerControlMethod,omitempty"`
	IPAssignment       string                `json:"ipAssignment,omitempty"`
	IP                 string                `json:"ip,omitempty"`
	SSHKeyRefs         *[]string             `json:"sshKeyRefs,omitempty"`
	LoginUser          *vm.LoginUserSpec     `json:"loginUser,omitempty"`
}

func (s *Server) PowerOnVM(c echo.Context) error {
	return s.runVMPowerAction(c, "power-on")
}

func (s *Server) PowerOffVM(c echo.Context) error {
	return s.runVMPowerAction(c, "power-off")
}

func (s *Server) ReinstallVM(c echo.Context) error {
	name := c.Param("name")
	ctx := c.Request().Context()

	var req vmReinstallReq
	if c.Request().ContentLength > 0 {
		if err := c.Bind(&req); err != nil {
			return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
		}
	}
	if strings.TrimSpace(req.Confirm) != "" && strings.TrimSpace(req.Confirm) != name {
		return c.JSON(gohttp.StatusBadRequest, jsonError("confirm must match vm name"))
	}

	current, err := s.vms.Get(ctx, name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if req.SubnetRef != nil {
		subnetRef := strings.TrimSpace(*req.SubnetRef)
		if subnetRef != "" {
			if _, err := s.subnets.Get(ctx, subnetRef); err != nil {
				return c.JSON(gohttp.StatusBadRequest, jsonError("referenced subnetRef not found"))
			}
		}
	}
	applyReinstallSpec(&current, &req)
	if req.AdvancedOptions == nil {
		dropLegacyRawDiskFormat(&current)
	}
	ensureVMNetwork(&current)
	resolveVMBridgeFromHypervisor(ctx, &current, s.hypervisors)

	if req.InstallConfig != nil {
		applyInstallConfigInline(&current, req.InstallConfig.Inline)
	}
	if cloudInitRef := strings.TrimSpace(req.CloudInitRef); cloudInitRef != "" {
		applyReinstallCloudInitRef(&current, cloudInitRef)
	}
	if err := s.applyInstallConfigByOSImage(ctx, &current); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}

	if current.LastDeployedCloudInitRef == "" {
		if ref := preferredCloudInitRef(current); ref != "" {
			current.LastDeployedCloudInitRef = ref
		}
	}

	token, err := httputil.GenerateProvisioningToken()
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError("failed to issue provisioning token"))
	}
	now := time.Now().UTC()
	current.Phase = vm.PhaseProvisioning
	current.LastPowerAction = "redeploy"
	current.LastError = ""
	current.Provisioning = vm.ProvisioningStatus{
		Active:          true,
		StartedAt:       httputil.TimePtr(now),
		DeadlineAt:      httputil.TimePtr(now.Add(s.provisionTimeout)),
		CompletionToken: token,
	}
	current.UpdatedAt = now

	if err := vm.ValidateVirtualMachine(current); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	if err := s.vms.Store().Upsert(ctx, current); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}

	if s.vmDeployer != nil {
		if err := s.vmDeployer.Redeploy(ctx, current, pxehttp.RenderNoCloudLineConfig); err != nil {
			_ = s.updateVMPXEProvisioningError(ctx, current, err)
			httputil.CreateAudit(c, s.authStore, name, "redeploy-vm", "failure", err.Error(), nil)
			return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
		}
	}

	updated, getErr := s.vms.Get(ctx, name)
	if getErr != nil {
		httputil.CreateAudit(c, s.authStore, name, "redeploy-vm", "partial", "vm os redeploy started but failed to load updated status", nil)
		return c.JSON(gohttp.StatusAccepted, current)
	}
	httputil.CreateAudit(c, s.authStore, name, "redeploy-vm", "success", "vm os redeploy started", nil)
	return c.JSON(gohttp.StatusAccepted, updated)
}

func (s *Server) RedeployVM(c echo.Context) error {
	return s.ReinstallVM(c)
}

func applyReinstallCloudInitRef(current *vm.VirtualMachine, cloudInitRef string) {
	existing := normalizeCloudInitRefs(current.CloudInitRef, current.CloudInitRefs)
	updated := make([]string, 0, len(existing)+1)
	updated = append(updated, cloudInitRef)
	for _, ref := range existing {
		if ref == cloudInitRef {
			continue
		}
		updated = append(updated, ref)
	}
	current.CloudInitRef = ""
	current.CloudInitRefs = updated
	current.LastDeployedCloudInitRef = cloudInitRef
}

func applyReinstallSpec(current *vm.VirtualMachine, req *vmReinstallReq) {
	if hvRef := strings.TrimSpace(req.HypervisorRef); hvRef != "" {
		current.HypervisorRef = hvRef
	}
	if req.Resources != nil {
		if req.Resources.CPUCores > 0 {
			current.Resources.CPUCores = req.Resources.CPUCores
		}
		if req.Resources.MemoryMB > 0 {
			current.Resources.MemoryMB = req.Resources.MemoryMB
		}
		if req.Resources.DiskGB > 0 {
			current.Resources.DiskGB = req.Resources.DiskGB
		}
	}
	if osImageRef := strings.TrimSpace(req.OSImageRef); osImageRef != "" {
		current.OSImageRef = osImageRef
	}
	if req.PowerControlMethod != "" {
		current.PowerControlMethod = req.PowerControlMethod
	}
	if len(req.Network) > 0 {
		current.Network = req.Network
	}
	if req.SubnetRef != nil {
		current.SubnetRef = strings.TrimSpace(*req.SubnetRef)
	}
	if req.Domain != nil {
		current.Domain = strings.TrimSpace(*req.Domain)
	}
	if req.IPAssignment != "" {
		current.IPAssignment = vm.IPAssignmentMode(req.IPAssignment)
	}
	if req.IP != "" && len(current.Network) > 0 {
		current.Network[0].IPAddress = req.IP
	} else if req.IPAssignment != "" && req.IPAssignment != string(vm.IPAssignmentStatic) && len(current.Network) > 0 {
		current.Network[0].IPAddress = ""
	}
	if req.AdvancedOptions != nil {
		current.AdvancedOptions = req.AdvancedOptions
	}
	if req.SSHKeyRefs != nil {
		current.SSHKeyRefs = normalizeSSHKeyRefs(*req.SSHKeyRefs)
	}
	if req.LoginUser != nil {
		current.LoginUser = req.LoginUser
	}
	if req.InstallConfig != nil {
		applyInstallConfigInline(current, req.InstallConfig.Inline)
	}

	refs := normalizeCloudInitRefs(req.CloudInitRef, req.CloudInitRefs)
	if len(refs) > 0 {
		current.CloudInitRef = ""
		current.CloudInitRefs = refs
		current.LastDeployedCloudInitRef = refs[0]
	}
}

func normalizeSSHKeyRefs(refs []string) []string {
	out := make([]string, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, raw := range refs {
		ref := strings.TrimSpace(raw)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func dropLegacyRawDiskFormat(current *vm.VirtualMachine) {
	if current == nil || current.AdvancedOptions == nil {
		return
	}
	if strings.EqualFold(strings.TrimSpace(current.AdvancedOptions.DiskFormat), "raw") {
		current.AdvancedOptions.DiskFormat = ""
	}
}

func applyInstallConfigInline(v *vm.VirtualMachine, inline string) {
	trimmed := strings.TrimSpace(inline)
	if v.InstallCfg == nil {
		if trimmed == "" {
			return
		}
		v.InstallCfg = &vm.InstallConfig{}
	}
	v.InstallCfg.Inline = trimmed
}

func inferInstallConfigType(osFamily string) (vm.InstallConfigType, error) {
	if strings.TrimSpace(osFamily) == "" {
		return "", fmt.Errorf("osImage.osFamily is required for pxe install")
	}
	return vm.InstallConfigCurtin, nil
}

func (s *Server) applyInstallConfigByOSImage(ctx context.Context, v *vm.VirtualMachine) error {
	ref := strings.TrimSpace(v.OSImageRef)
	if ref == "" {
		return errors.New("osImageRef is required")
	}
	img, err := s.osimages.Get(ctx, ref)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return fmt.Errorf("referenced osImageRef not found: %s", ref)
		}
		return err
	}
	cfgType, err := inferInstallConfigType(img.OSFamily)
	if err != nil {
		return err
	}
	inline := ""
	if v.InstallCfg != nil {
		inline = strings.TrimSpace(v.InstallCfg.Inline)
	}
	v.InstallCfg = &vm.InstallConfig{
		Type: cfgType,
	}
	if inline != "" {
		v.InstallCfg.Inline = inline
	}
	return nil
}

func preferredCloudInitRef(v vm.VirtualMachine) string {
	if ref := strings.TrimSpace(v.LastDeployedCloudInitRef); ref != "" {
		return ref
	}
	refs := normalizeCloudInitRefs(v.CloudInitRef, v.CloudInitRefs)
	if len(refs) > 0 {
		return refs[0]
	}
	return ""
}

func ensureVMNetwork(v *vm.VirtualMachine) {
	if len(v.Network) == 0 {
		v.Network = []vm.NetworkInterface{{Bridge: defaultVMBridge}}
	}
	for i := range v.Network {
		if v.Network[i].Bridge == "" {
			v.Network[i].Bridge = defaultVMBridge
		}
		if v.Network[i].MAC == "" {
			v.Network[i].MAC = generateKVMMAC()
		}
	}
}

// resolveVMBridgeFromHypervisor replaces default bridge names with the
// hypervisor's configured bridge, so VMs join the physical network.
func resolveVMBridgeFromHypervisor(ctx context.Context, v *vm.VirtualMachine, hvSvc *hypervisor.Service) {
	if hvSvc == nil || v.HypervisorRef == "" {
		return
	}
	hv, err := hvSvc.Get(ctx, v.HypervisorRef)
	if err != nil || hv.BridgeName == "" {
		return
	}
	for i := range v.Network {
		if v.Network[i].Bridge == "" || v.Network[i].Bridge == defaultVMBridge {
			v.Network[i].Bridge = hv.BridgeName
		}
	}
}

func generateKVMMAC() string {
	var buf [3]byte
	_, _ = crand.Read(buf[:])
	return fmt.Sprintf("52:54:00:%02x:%02x:%02x", buf[0], buf[1], buf[2])
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

type vmMigrateReq struct {
	TargetHypervisor string `json:"targetHypervisor,omitempty"`
}

func (s *Server) MigrateVM(c echo.Context) error {
	name := c.Param("name")
	ctx := c.Request().Context()

	var req vmMigrateReq
	if c.Request().ContentLength > 0 {
		if err := c.Bind(&req); err != nil {
			return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
		}
	}

	v, err := s.vms.Get(ctx, name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	if v.Phase != vm.PhaseRunning {
		return c.JSON(gohttp.StatusBadRequest, jsonError("vm must be in Running phase to migrate"))
	}

	if s.vmMigrator == nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError("migration not configured"))
	}

	targetHVName := strings.TrimSpace(req.TargetHypervisor)
	sourceHVName := v.HypervisorRef
	if targetHVName == sourceHVName {
		return c.JSON(gohttp.StatusBadRequest, jsonError("target hypervisor must be different from source"))
	}

	updated, migrateErr := s.vmMigrator.Migrate(ctx, v, targetHVName)
	if migrateErr != nil {
		httputil.CreateAudit(c, s.authStore, name, "migrate-vm", "failure", migrateErr.Error(), map[string]string{
			"source": sourceHVName,
			"target": targetHVName,
		})
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(migrateErr))
	}

	httputil.CreateAudit(c, s.authStore, name, "migrate-vm", "success", fmt.Sprintf("migrated from %s to %s", sourceHVName, targetHVName), map[string]string{
		"source": sourceHVName,
		"target": targetHVName,
	})
	return c.JSON(gohttp.StatusOK, updated)
}

func (s *Server) updateVMPXEProvisioningError(ctx context.Context, current vm.VirtualMachine, reinstallErr error) error {
	now := time.Now().UTC()
	current.Phase = vm.PhaseError
	current.LastPowerAction = "redeploy"
	current.LastError = reinstallErr.Error()
	current.Provisioning.Active = false
	current.UpdatedAt = now
	return s.vms.Store().Upsert(ctx, current)
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

func normalizeCloudInitRefs(legacyRef string, refs []string) []string {
	return resource.NormalizeCloudInitRefs(legacyRef, refs)
}
