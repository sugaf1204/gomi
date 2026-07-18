package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/resource"
)

// cascadeDeleteHypervisorRecords removes the hypervisor record and the VM
// records that reference it from GOMI. It never touches libvirt or the host:
// deleting from GOMI is a record-only operation and the domains, if any, stay
// on the machine. Missing children are ignored so retries are idempotent.
func (s *Server) cascadeDeleteHypervisorRecords(ctx context.Context, c echo.Context, hvName, reason string) error {
	if s.vms != nil {
		vms, err := s.vms.ListByHypervisor(ctx, hvName)
		if err != nil {
			return fmt.Errorf("list virtual machines of hypervisor %s: %w", hvName, err)
		}
		for _, v := range vms {
			if err := s.vms.Delete(ctx, v.Name); err != nil && !errors.Is(err, resource.ErrNotFound) {
				return fmt.Errorf("delete virtual machine record %s: %w", v.Name, err)
			}
			httputil.CreateAudit(c, s.authStore, v.Name, "delete-vm", "success", "record removed: "+reason, nil)
		}
	}
	if err := s.hypervisors.Delete(ctx, hvName); err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("delete hypervisor record %s: %w", hvName, err)
	}
	httputil.CreateAudit(c, s.authStore, hvName, "delete-hypervisor", "success", "record removed: "+reason, nil)
	return nil
}

// cascadeDeleteMachineRecords removes the hypervisor records linked to the
// machine and their VM records from GOMI. Record-only; see
// cascadeDeleteHypervisorRecords.
func (s *Server) cascadeDeleteMachineRecords(ctx context.Context, c echo.Context, machineName string) error {
	if s.hypervisors == nil {
		return nil
	}
	hvs, err := s.hypervisors.ListByMachineRef(ctx, machineName)
	if err != nil {
		return fmt.Errorf("list hypervisors of machine %s: %w", machineName, err)
	}
	for _, hv := range hvs {
		if err := s.cascadeDeleteHypervisorRecords(ctx, c, hv.Name, "cascade from machine "+machineName); err != nil {
			return err
		}
	}
	return nil
}
