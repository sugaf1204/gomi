package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/resource"
)

// cascadeDeleteHypervisorRecords removes the hypervisor record and the VM
// records that reference it from GOMI. It never touches libvirt or the host:
// deleting from GOMI is a record-only operation and the domains, if any, stay
// on the machine. Missing children are ignored so retries are idempotent.
func (s *Server) cascadeDeleteHypervisorRecords(ctx context.Context, c echo.Context, hvName, reason string) error {
	if err := s.deleteHypervisorVMRecords(ctx, c, hvName, reason); err != nil {
		return err
	}
	if err := s.hypervisors.Delete(ctx, hvName); err != nil && !errors.Is(err, resource.ErrNotFound) {
		return fmt.Errorf("delete hypervisor record %s: %w", hvName, err)
	} else if err == nil {
		httputil.CreateAudit(c, s.authStore, hvName, "delete-hypervisor", "success", "record removed: "+reason, nil)
	}
	// Sweep VM records created while the hypervisor row still existed: a
	// concurrent create can pass its hypervisor existence check before the
	// row is gone and upsert after the first child listing. The sweep also
	// runs when another delete removed the row first, since that delete's
	// sweep may have listed children before this request's racing create.
	return s.deleteHypervisorVMRecords(ctx, c, hvName, reason)
}

func (s *Server) deleteHypervisorVMRecords(ctx context.Context, c echo.Context, hvName, reason string) error {
	if s.vms == nil {
		return nil
	}
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
	return nil
}

// cascadeDeleteMachineRecords removes the given hypervisor records linked to
// the machine and their VM records from GOMI. The caller passes the list it
// already authorized (admin check) so the cascade cannot act on hypervisors
// linked after that check. Record-only; see cascadeDeleteHypervisorRecords.
func (s *Server) cascadeDeleteMachineRecords(ctx context.Context, c echo.Context, machineName string, linked []hypervisor.Hypervisor) error {
	for _, hv := range linked {
		if err := s.cascadeDeleteHypervisorRecords(ctx, c, hv.Name, "cascade from machine "+machineName); err != nil {
			return err
		}
	}
	return nil
}
