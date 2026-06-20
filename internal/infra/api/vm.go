package api

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type createVirtualMachineRequest struct {
	VirtualMachineID string                     `json:"virtualMachineId,omitempty"`
	VirtualMachine   *virtualMachineSpecRequest `json:"virtualMachine"`
}

type virtualMachineSpecRequest struct {
	HypervisorRef      string                `json:"hypervisorRef"`
	Resources          vm.ResourceSpec       `json:"resources"`
	OSImageRef         string                `json:"osImageRef,omitempty"`
	CloudInitRef       string                `json:"cloudInitRef,omitempty"`
	CloudInitRefs      []string              `json:"cloudInitRefs,omitempty"`
	Network            []vm.NetworkInterface `json:"network,omitempty"`
	IPAssignment       vm.IPAssignmentMode   `json:"ipAssignment,omitempty"`
	SubnetRef          string                `json:"subnetRef,omitempty"`
	Domain             string                `json:"domain,omitempty"`
	InstallConfig      *vm.InstallConfig     `json:"installConfig,omitempty"`
	PowerControlMethod vm.PowerControlMethod `json:"powerControlMethod"`
	AdvancedOptions    *vm.AdvancedOptions   `json:"advancedOptions,omitempty"`
	SSHKeyRefs         []string              `json:"sshKeyRefs,omitempty"`
	LoginUser          *vm.LoginUserSpec     `json:"loginUser,omitempty"`
}

func (s *Server) CreateVirtualMachine(c echo.Context) error {
	v, err := bindCreateVirtualMachine(c)
	if err != nil {
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
	return c.JSON(gohttp.StatusCreated, virtualMachineResponse(created))
}

func (s *Server) ListVirtualMachines(c echo.Context) error {
	ctx := c.Request().Context()

	// Fast path: with no filters, page at the store layer (SQL LIMIT/OFFSET)
	// instead of loading and decoding the whole table for every page request.
	if !vmFilterActive(c) {
		start, size, err := parsePageRequest(c)
		if err != nil {
			return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
		}
		items, total, ok, err := s.vms.ListPage(ctx, start, size)
		if err != nil {
			return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
		}
		if ok {
			p, _ := parsePagination(c, total)
			return c.JSON(gohttp.StatusOK, ListVirtualMachinesResponse{
				VirtualMachines: virtualMachineResponses(items),
				NextPageToken:   p.nextPageToken,
				TotalSize:       p.totalSize,
			})
		}
	}

	items, err := s.vms.List(ctx)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}

	if filterByFreshPhase := stringFilter(c, "phase") != ""; filterByFreshPhase {
		items = filterVirtualMachines(c, items)
		p, perr := parsePagination(c, len(items))
		if perr != nil {
			return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(perr))
		}
		return c.JSON(gohttp.StatusOK, ListVirtualMachinesResponse{
			VirtualMachines: virtualMachineResponses(paginate(items, p)),
			NextPageToken:   p.nextPageToken,
			TotalSize:       p.totalSize,
		})
	}

	items = filterVirtualMachines(c, items)
	p, err := parsePagination(c, len(items))
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, ListVirtualMachinesResponse{
		VirtualMachines: virtualMachineResponses(paginate(items, p)),
		NextPageToken:   p.nextPageToken,
		TotalSize:       p.totalSize,
	})
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
	return c.JSON(gohttp.StatusOK, virtualMachineResponse(v))
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

func bindCreateVirtualMachine(c echo.Context) (vm.VirtualMachine, error) {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return vm.VirtualMachine{}, err
	}
	var req createVirtualMachineRequest
	if err := json.Unmarshal(body, &req); err == nil && req.VirtualMachine != nil {
		id := strings.TrimSpace(c.QueryParam("virtualMachineId"))
		if id == "" {
			id = strings.TrimSpace(req.VirtualMachineID)
		}
		if id == "" {
			return vm.VirtualMachine{}, fmt.Errorf("virtualMachineId is required")
		}
		return req.VirtualMachine.toVirtualMachine(resourceID("virtualMachines", id)), nil
	}
	var v vm.VirtualMachine
	if err := json.Unmarshal(body, &v); err != nil {
		return vm.VirtualMachine{}, err
	}
	normalizeVirtualMachineRequestRefs(&v)
	return v, nil
}

func (r virtualMachineSpecRequest) toVirtualMachine(id string) vm.VirtualMachine {
	v := vm.VirtualMachine{
		Name:               id,
		HypervisorRef:      r.HypervisorRef,
		Resources:          r.Resources,
		OSImageRef:         r.OSImageRef,
		CloudInitRef:       r.CloudInitRef,
		CloudInitRefs:      r.CloudInitRefs,
		Network:            r.Network,
		IPAssignment:       r.IPAssignment,
		SubnetRef:          r.SubnetRef,
		Domain:             r.Domain,
		InstallCfg:         r.InstallConfig,
		PowerControlMethod: r.PowerControlMethod,
		AdvancedOptions:    r.AdvancedOptions,
		SSHKeyRefs:         r.SSHKeyRefs,
		LoginUser:          r.LoginUser,
	}
	normalizeVirtualMachineRequestRefs(&v)
	return v
}

func normalizeVirtualMachineRequestRefs(v *vm.VirtualMachine) {
	if v == nil {
		return
	}
	v.Name = resourceID("virtualMachines", v.Name)
	v.HypervisorRef = resourceID("hypervisors", v.HypervisorRef)
	v.OSImageRef = resourceID("osImages", v.OSImageRef)
	v.CloudInitRef = resourceID("cloudInitTemplates", v.CloudInitRef)
	v.CloudInitRefs = resourceIDs("cloudInitTemplates", v.CloudInitRefs)
	v.SubnetRef = resourceID("subnets", v.SubnetRef)
	v.SSHKeyRefs = resourceIDs("sshKeys", v.SSHKeyRefs)
}

// vmFilterActive reports whether any list filter query parameter is set. When
// false, the list can be served straight from the store's paged reader.
func vmFilterActive(c echo.Context) bool {
	return stringFilter(c, "phase") != "" ||
		stringFilter(c, "hypervisorRef") != "" ||
		stringFilter(c, "subnetRef") != "" ||
		stringFilter(c, "ipAssignment") != ""
}

func filterVirtualMachines(c echo.Context, items []vm.VirtualMachine) []vm.VirtualMachine {
	phase := stringFilter(c, "phase")
	hypervisorRef := resourceID("hypervisors", stringFilter(c, "hypervisorRef"))
	subnetRef := resourceID("subnets", stringFilter(c, "subnetRef"))
	ipAssignment := stringFilter(c, "ipAssignment")
	if phase == "" && hypervisorRef == "" && subnetRef == "" && ipAssignment == "" {
		return items
	}
	out := make([]vm.VirtualMachine, 0, len(items))
	for _, item := range items {
		if !matchFilter(string(item.Phase), phase) {
			continue
		}
		if !matchFilter(item.HypervisorRef, hypervisorRef) {
			continue
		}
		if !matchFilter(item.SubnetRef, subnetRef) {
			continue
		}
		if !matchFilter(string(item.IPAssignment), ipAssignment) {
			continue
		}
		out = append(out, item)
	}
	return out
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
