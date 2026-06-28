package pxehttp

import (
	"encoding/json"
	"fmt"
	"github.com/labstack/echo/v4"
	apiinventory "github.com/sugaf1204/gomi/api/inventory"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
	"io"
	gohttp "net/http"
	"strconv"
	"strings"
	"time"
)

func (h *Handler) PXEInventory(c echo.Context) error {
	requestStarted := time.Now().UTC()
	token := strings.TrimSpace(c.QueryParam("token"))
	if token == "" {
		return c.JSON(gohttp.StatusBadRequest, jsonError("token is required"))
	}

	ctx := c.Request().Context()
	target, err := h.requireProvisioningMachine(ctx, token)
	if err != nil {
		status := gohttp.StatusInternalServerError
		if err == resource.ErrNotFound {
			status = gohttp.StatusNotFound
		}
		return c.JSON(status, jsonErrorErr(err))
	}

	attemptID := strings.TrimSpace(target.Provision.AttemptID)
	if attemptID == "" {
		return c.JSON(gohttp.StatusConflict, jsonError("provisioning attempt id is required"))
	}
	decodeStarted := time.Now().UTC()
	var info hwinfo.HardwareInfo
	if strings.HasPrefix(c.Request().Header.Get("Content-Type"), "text/plain") {
		body, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
		}
		info = parseTextInventory(string(body))
	} else {
		body, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
		}
		var payload apiinventory.HardwareInventory
		if err := json.Unmarshal(body, &payload); err != nil {
			if legacyErr := json.Unmarshal(body, &info); legacyErr != nil {
				return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
			}
		} else {
			info = hwinfo.FromInventory(payload)
		}
	}
	decodeFinished := time.Now().UTC()
	info.Name = target.Name + "-hwinfo"
	info.MachineName = target.Name
	info.AttemptID = attemptID
	storeStarted := time.Now().UTC()
	if h.hwinfo != nil {
		if _, err := h.hwinfo.Upsert(ctx, info); err != nil {
			return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
		}
	}
	storeFinished := time.Now().UTC()

	if err := h.updateProvisionProgress(ctx, target.Name, func(m *machine.Machine) {
		if m.Provision == nil {
			m.Provision = &machine.ProvisionProgress{}
		}
		m.Provision.AttemptID = attemptID
		m.Provision.InventoryID = info.Name
		m.Provision.LastSignalAt = timePtr(time.Now().UTC())
		m.Provision.Message = "hardware inventory received"
		m.Provision.Timings = appendProvisionTiming(m.Provision.Timings,
			serverTiming("server.inventory.decode", "read and decode hardware inventory", "success", decodeStarted, decodeFinished, 0),
			serverTiming("server.inventory.store", "store hardware inventory", "success", storeStarted, storeFinished, 0),
			serverTiming("server.inventory.total", "handle hardware inventory callback", "success", requestStarted, time.Now().UTC(), 0),
		)
	}); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}

	base := h.resolvePXEBaseURL(c)
	eventsURL := buildPXEDeployEventsURL(base, token, attemptID)
	response := inventoryResponse{
		AttemptID:       attemptID,
		DeployMode:      "curtin",
		CurtinConfigURL: buildPXECurtinConfigURL(base, token, attemptID),
		EventsURL:       eventsURL,
	}
	if h.osimages != nil && strings.TrimSpace(target.OSPreset.ImageRef) != "" {
		img, err := h.osimages.Get(ctx, strings.TrimSpace(target.OSPreset.ImageRef))
		if err != nil {
			return c.JSON(gohttp.StatusNotFound, jsonError(fmt.Sprintf("os image %q not found: %v", target.OSPreset.ImageRef, err)))
		}
		if !osimage.SupportsDeploymentTarget(img, osimage.DeploymentTargetBareMetal) {
			return c.JSON(gohttp.StatusConflict, jsonError(fmt.Sprintf("os image %q does not support bare-metal deployment", target.OSPreset.ImageRef)))
		}
		if rootImageFormat(img) == osimage.FormatSquashFS {
			return c.JSON(gohttp.StatusOK, response)
		}
		if rootImageFormat(img) != osimage.FormatQCOW2 {
			return c.JSON(gohttp.StatusConflict, jsonError(fmt.Sprintf("bare-metal deploy requires qcow2 or squashfs OS image, got %s", rootImageFormat(img))))
		}
		deploy, err := h.buildDiskImageDeployResponse(base, token, attemptID, target, img, &info)
		if err != nil {
			return c.JSON(gohttp.StatusConflict, jsonErrorErr(err))
		}
		response.DeployMode = "disk-image"
		response.CurtinConfigURL = ""
		response.DiskImageDeploy = deploy
	}
	return c.JSON(gohttp.StatusOK, response)
}

func parseTextInventory(body string) hwinfo.HardwareInfo {
	var info hwinfo.HardwareInfo
	for _, line := range strings.Split(body, "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) == 0 || strings.TrimSpace(fields[0]) == "" {
			continue
		}
		switch fields[0] {
		case "runtime":
			if len(fields) > 1 {
				info.Runtime.KernelVersion = fields[1]
			}
		case "boot":
			if len(fields) > 1 {
				info.Boot.FirmwareMode = fields[1]
			}
			if len(fields) > 2 {
				info.Boot.EFIVars = fields[2] == "true" || fields[2] == "1"
			}
		case "disk":
			if len(fields) < 5 {
				continue
			}
			sizeMB, _ := strconv.ParseInt(fields[3], 10, 64)
			disk := hwinfo.DiskInfo{
				Name:      fields[1],
				Path:      fields[2],
				SizeMB:    sizeMB,
				Type:      "disk",
				Removable: fields[4] == "true" || fields[4] == "1",
			}
			if len(fields) > 5 {
				disk.ByID = splitInventoryCSV(fields[5])
			}
			if len(fields) > 6 {
				disk.ByPath = splitInventoryCSV(fields[6])
			}
			info.Disks = append(info.Disks, disk)
		case "nic":
			if len(fields) < 4 {
				continue
			}
			info.NICs = append(info.NICs, hwinfo.NICInfo{
				Name:  fields[1],
				MAC:   fields[2],
				State: fields[3],
			})
		}
	}
	return info
}

func splitInventoryCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func validateAttemptInventory(target *machine.Machine, info *hwinfo.HardwareInfo) error {
	if target == nil || target.Provision == nil {
		return fmt.Errorf("provisioning state is required")
	}
	attemptID := strings.TrimSpace(target.Provision.AttemptID)
	if attemptID == "" {
		return fmt.Errorf("hardware inventory for current attempt is required")
	}
	if info == nil {
		return fmt.Errorf("hardware inventory for current attempt is required")
	}
	if strings.TrimSpace(info.AttemptID) != attemptID {
		return fmt.Errorf("hardware inventory is stale or from a different attempt")
	}
	return nil
}

func validateAttemptParam(target *machine.Machine, attemptID string) error {
	if target == nil || target.Provision == nil {
		return fmt.Errorf("provisioning state is required")
	}
	current := strings.TrimSpace(target.Provision.AttemptID)
	if current == "" {
		return fmt.Errorf("provisioning attempt id is required")
	}
	if strings.TrimSpace(attemptID) == "" {
		return fmt.Errorf("attempt_id is required")
	}
	if strings.TrimSpace(attemptID) != current {
		return fmt.Errorf("attempt_id does not match current provisioning attempt")
	}
	return nil
}
