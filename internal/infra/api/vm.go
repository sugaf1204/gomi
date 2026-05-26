package api

import (
	"context"
	crand "crypto/rand"
	"errors"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/infra/pxehttp"
	"github.com/sugaf1204/gomi/internal/libvirt"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
	"log"
	gohttp "net/http"
	"strings"
	"time"
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
