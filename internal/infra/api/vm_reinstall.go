package api

import (
	"context"
	"errors"
	"fmt"
	gohttp "net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/infra/pxehttp"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
)

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

func (s *Server) ReinstallVM(c echo.Context) error {
	name := c.Param("name")
	ctx := c.Request().Context()

	var req vmReinstallReq
	if c.Request().ContentLength > 0 {
		if err := c.Bind(&req); err != nil {
			return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
		}
	}
	normalizeVMReinstallRequestRefs(&req)
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
		return c.JSON(gohttp.StatusAccepted, virtualMachineResponse(current))
	}
	httputil.CreateAudit(c, s.authStore, name, "redeploy-vm", "success", "vm os redeploy started", nil)
	return c.JSON(gohttp.StatusAccepted, virtualMachineResponse(updated))
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

func normalizeVMReinstallRequestRefs(req *vmReinstallReq) {
	if req == nil {
		return
	}
	req.HypervisorRef = resourceID("hypervisors", req.HypervisorRef)
	req.OSImageRef = resourceID("osImages", req.OSImageRef)
	req.CloudInitRef = resourceID("cloudInitTemplates", req.CloudInitRef)
	req.CloudInitRefs = resourceIDs("cloudInitTemplates", req.CloudInitRefs)
	if req.SubnetRef != nil {
		v := resourceID("subnets", *req.SubnetRef)
		req.SubnetRef = &v
	}
	if req.SSHKeyRefs != nil {
		v := resourceIDs("sshKeys", *req.SSHKeyRefs)
		req.SSHKeyRefs = &v
	}
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
	if format := effectiveVMCloudImageFormat(img); format != osimage.FormatQCOW2 {
		return fmt.Errorf("cloudimage deployment requires qcow2 OS image, got %s", format)
	}
	if !osimage.SupportsDeploymentTarget(img, osimage.DeploymentTargetVM) {
		if osimage.HasExplicitDeploymentTargets(img) {
			return fmt.Errorf("cloudimage deployment requires vm-capable OS image")
		}
		return fmt.Errorf("cloudimage deployment requires cloud OS image variant, got %s", img.Variant)
	}
	if !osimage.HasExplicitDeploymentTargets(img) && img.Variant != "" && img.Variant != osimage.VariantCloud {
		return fmt.Errorf("cloudimage deployment requires cloud OS image variant, got %s", img.Variant)
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

func effectiveVMCloudImageFormat(img osimage.OSImage) osimage.ImageFormat {
	if format := osimage.EffectiveImageFormat(img); format != "" {
		return format
	}
	return osimage.FormatQCOW2
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

func normalizeCloudInitRefs(legacyRef string, refs []string) []string {
	return resource.NormalizeCloudInitRefs(legacyRef, refs)
}
