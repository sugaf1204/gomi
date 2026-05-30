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
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
)

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
	req.TargetHypervisor = resourceID("hypervisors", req.TargetHypervisor)

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
	return c.JSON(gohttp.StatusOK, virtualMachineResponse(updated))
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
