package pxehttp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	gohttp "net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"gopkg.in/yaml.v3"

	apiinventory "github.com/sugaf1204/gomi/api/inventory"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
)

const deployEventImageApplied = "image_applied"
const provisionArtifactImageApplied = "imageApplied"
const provisionArtifactImageAppliedAt = "imageAppliedAt"
const provisionArtifactFailureLogTail = "failureLogTail"
const maxProvisionFailureLogTailLen = 32 * 1024
const maxProvisionTimingEvents = 240
const rootFSBIOSBootPartitionSizeMB int64 = 1
const rootFSEFIPartitionSizeMB int64 = 512
const rootFSPartitionReserveMB int64 = 64
const rootFSMinimumRootPartitionSizeMB int64 = 4096

type curtinInstall struct {
	LogFile           string   `yaml:"log_file"`
	PostFiles         []string `yaml:"post_files,omitempty"`
	SaveInstallConfig string   `yaml:"save_install_config"`
	SaveInstallLog    string   `yaml:"save_install_log"`
}

type curtinAptMirrors struct {
	UbuntuArchive  string `yaml:"ubuntu_archive,omitempty"`
	UbuntuSecurity string `yaml:"ubuntu_security,omitempty"`
}

type curtinReportingHook struct {
	Type     string `yaml:"type"`
	Endpoint string `yaml:"endpoint"`
	Level    string `yaml:"level"`
}

type curtinReporting struct {
	Gomi curtinReportingHook `yaml:"gomi"`
}

type curtinBlockMeta struct {
	Devices []string `yaml:"devices"`
}

type curtinSource struct {
	Type string `yaml:"type"`
	URI  string `yaml:"uri"`
}

type curtinStorageConfig struct {
	Version int              `yaml:"version"`
	Config  []map[string]any `yaml:"config"`
}

type curtinGrub struct {
	InstallDevices []string `yaml:"install_devices"`
}

type curtinStorage struct {
	Storage curtinStorageConfig `yaml:"storage"`
	Grub    curtinGrub          `yaml:"grub"`
}

type curtinConfig struct {
	Install              curtinInstall           `yaml:"install"`
	Reporting            curtinReporting         `yaml:"reporting"`
	AptMirrors           *curtinAptMirrors       `yaml:"apt_mirrors,omitempty"`
	BlockMeta            curtinBlockMeta         `yaml:"block-meta"`
	Sources              map[string]curtinSource `yaml:"sources"`
	Storage              *curtinStorageConfig    `yaml:"storage,omitempty"`
	Grub                 *curtinGrub             `yaml:"grub,omitempty"`
	Stages               []string                `yaml:"stages"`
	PartitioningCommands map[string][]string     `yaml:"partitioning_commands,omitempty"`
	LateCommands         map[string][]string     `yaml:"late_commands"`
}

type selectedTargetDisk struct {
	Path string
	Info hwinfo.DiskInfo
}

type deployEventRequest struct {
	AttemptID        string          `json:"attemptId,omitempty"`
	Type             string          `json:"type"`
	Source           string          `json:"source,omitempty"`
	Name             string          `json:"name,omitempty"`
	Message          string          `json:"message,omitempty"`
	Reason           string          `json:"reason,omitempty"`
	Result           string          `json:"result,omitempty"`
	LogTail          string          `json:"logTail,omitempty"`
	StartedAt        string          `json:"startedAt,omitempty"`
	FinishedAt       string          `json:"finishedAt,omitempty"`
	Timestamp        any             `json:"timestamp,omitempty"`
	DurationMillis   int64           `json:"durationMs,omitempty"`
	MonotonicSeconds float64         `json:"monotonicSeconds,omitempty"`
	CurtinEventType  string          `json:"event_type,omitempty"`
	CurtinOrigin     string          `json:"origin,omitempty"`
	Description      string          `json:"description,omitempty"`
	Details          json.RawMessage `json:"details,omitempty"`
}

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
	return c.JSON(gohttp.StatusOK, inventoryResponse{
		AttemptID:       attemptID,
		CurtinConfigURL: buildPXECurtinConfigURL(base, token, attemptID),
		EventsURL:       buildPXEDeployEventsURL(base, token, attemptID),
	})
}

func (h *Handler) PXECurtinConfig(c echo.Context) error {
	token := strings.TrimSpace(c.QueryParam("token"))
	if token == "" {
		return c.JSON(gohttp.StatusBadRequest, jsonError("token is required"))
	}
	queryAttemptID := strings.TrimSpace(c.QueryParam("attempt_id"))
	ctx := c.Request().Context()
	target, err := h.requireProvisioningMachine(ctx, token)
	if err != nil {
		status := gohttp.StatusInternalServerError
		if err == resource.ErrNotFound {
			status = gohttp.StatusNotFound
		}
		return c.JSON(status, jsonErrorErr(err))
	}
	if err := validateAttemptParam(target, queryAttemptID); err != nil {
		return c.JSON(gohttp.StatusConflict, jsonErrorErr(err))
	}
	if h.osimages == nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError("os image service not available"))
	}
	imageRef := strings.TrimSpace(target.OSPreset.ImageRef)
	if imageRef == "" {
		return c.JSON(gohttp.StatusBadRequest, jsonError("machine has no image reference"))
	}
	img, err := h.osimages.Get(ctx, imageRef)
	if err != nil {
		return c.JSON(gohttp.StatusNotFound, jsonError(fmt.Sprintf("os image %q not found: %v", imageRef, err)))
	}
	info, _ := h.hardwareInfo(ctx, target.Name)
	if err := validateAttemptInventory(target, info); err != nil {
		return c.JSON(gohttp.StatusConflict, jsonErrorErr(err))
	}
	config, err := h.buildCurtinInstallConfig(ctx, c, target, img, info)
	if err != nil {
		return c.JSON(gohttp.StatusConflict, jsonErrorErr(err))
	}
	configJSON, _ := json.Marshal(config)
	_ = h.updateProvisionProgress(ctx, target.Name, func(m *machine.Machine) {
		if m.Provision == nil {
			m.Provision = &machine.ProvisionProgress{}
		}
		m.Provision.AttemptID = target.Provision.AttemptID
		m.Provision.CurtinConfig = configJSON
		m.Provision.Message = "curtin config generated"
		m.Provision.LastSignalAt = timePtr(time.Now().UTC())
	})
	return c.Blob(gohttp.StatusOK, "text/yaml; charset=utf-8", []byte(config))
}

func (h *Handler) PXEDeployEvents(c echo.Context) error {
	token := strings.TrimSpace(c.QueryParam("token"))
	if token == "" {
		return c.JSON(gohttp.StatusBadRequest, jsonError("token is required"))
	}
	queryAttemptID := strings.TrimSpace(c.QueryParam("attempt_id"))
	ctx := c.Request().Context()
	target, err := h.requireProvisioningMachine(ctx, token)
	if err != nil {
		status := gohttp.StatusInternalServerError
		if err == resource.ErrNotFound {
			status = gohttp.StatusNotFound
		}
		return c.JSON(status, jsonErrorErr(err))
	}
	var req deployEventRequest
	if strings.HasPrefix(c.Request().Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		req.Type = c.FormValue("type")
		req.Source = c.FormValue("source")
		req.Name = c.FormValue("name")
		req.Message = c.FormValue("message")
		req.Reason = c.FormValue("reason")
		req.Result = c.FormValue("result")
		req.LogTail = c.FormValue("logTail")
		req.AttemptID = c.FormValue("attemptId")
		req.StartedAt = c.FormValue("startedAt")
		req.FinishedAt = c.FormValue("finishedAt")
		req.Timestamp = c.FormValue("timestamp")
		if duration := strings.TrimSpace(c.FormValue("durationMs")); duration != "" {
			req.DurationMillis, _ = strconv.ParseInt(duration, 10, 64)
		}
		if monotonic := strings.TrimSpace(c.FormValue("monotonicSeconds")); monotonic != "" {
			req.MonotonicSeconds, _ = strconv.ParseFloat(monotonic, 64)
		}
	} else if err := c.Bind(&req); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	if req.Source == "" {
		req.Source = strings.TrimSpace(c.QueryParam("source"))
	}
	eventType := strings.ToLower(strings.TrimSpace(req.Type))
	if eventType == "" && strings.TrimSpace(req.CurtinEventType) != "" {
		eventType = strings.ToLower(strings.TrimSpace(req.CurtinEventType))
	}
	if eventType == "" && (strings.TrimSpace(req.Source) != "" || strings.TrimSpace(req.Name) != "") {
		eventType = "timing"
	}
	if eventType == "" {
		return c.JSON(gohttp.StatusBadRequest, jsonError("type is required"))
	}
	if req.AttemptID != "" && queryAttemptID != "" && strings.TrimSpace(req.AttemptID) != queryAttemptID {
		return c.JSON(gohttp.StatusConflict, jsonError("attempt_id query and body mismatch"))
	}
	if queryAttemptID == "" {
		queryAttemptID = strings.TrimSpace(req.AttemptID)
	}
	if err := validateAttemptParam(target, queryAttemptID); err != nil {
		return c.JSON(gohttp.StatusConflict, jsonErrorErr(err))
	}
	now := time.Now().UTC()
	if err := h.updateProvisionProgress(ctx, target.Name, func(m *machine.Machine) {
		if m.Provision == nil {
			m.Provision = &machine.ProvisionProgress{}
		}
		m.Provision.LastSignalAt = &now
		if timing, ok := provisionTimingFromDeployEvent(req, eventType, now); ok {
			m.Provision.Timings = appendProvisionTiming(m.Provision.Timings, timing)
		}
		message := eventMessage(eventType, firstNonEmpty(req.Message, req.Description))
		if !isTimingOnlyDeployEvent(eventType) {
			m.Provision.Message = message
		}
		if eventType == "failed" {
			m.Phase = machine.PhaseError
			m.LastError = firstNonEmpty(req.Reason, req.Message, "deploy failed")
			m.Provision.Active = false
			m.Provision.FinishedAt = &now
			m.Provision.FailureReason = m.LastError
			if logTail := trimLogTail(req.LogTail, maxProvisionFailureLogTailLen); logTail != "" {
				if m.Provision.Artifacts == nil {
					m.Provision.Artifacts = map[string]string{}
				}
				m.Provision.Artifacts[provisionArtifactFailureLogTail] = logTail
			}
		} else if eventType == deployEventImageApplied {
			if m.Provision.Artifacts == nil {
				m.Provision.Artifacts = map[string]string{}
			}
			m.Provision.Artifacts[provisionArtifactImageApplied] = "true"
			m.Provision.Artifacts[provisionArtifactImageAppliedAt] = now.Format(time.RFC3339)
			m.Provision.Message = h.configureBIOSBootOrder(ctx, *m, message)
		}
	}); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, statusResponse{Status: "ok"})
}

func isTimingOnlyDeployEvent(eventType string) bool {
	switch strings.ToLower(strings.TrimSpace(eventType)) {
	case "timing", "start", "finish":
		return true
	default:
		return false
	}
}

func trimLogTail(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if value == "" || maxLen <= 0 {
		return ""
	}
	if len(value) <= maxLen {
		return value
	}
	return value[len(value)-maxLen:]
}

func provisionTimingFromDeployEvent(req deployEventRequest, eventType string, now time.Time) (machine.ProvisionTiming, bool) {
	source := strings.TrimSpace(req.Source)
	if source == "" && strings.TrimSpace(req.CurtinOrigin) != "" {
		source = strings.TrimSpace(req.CurtinOrigin)
	}
	if source == "" && strings.TrimSpace(req.CurtinEventType) != "" {
		source = "curtin"
	}
	if source == "" && eventType == "timing" {
		source = "runner"
	}
	name := strings.TrimSpace(req.Name)
	if name == "" && strings.TrimSpace(req.CurtinEventType) != "" {
		name = strings.TrimSpace(req.CurtinEventType)
	}
	if name == "" && eventType != "" {
		name = eventType
	}
	if source == "" && name == "" && req.DurationMillis <= 0 && req.MonotonicSeconds == 0 && req.StartedAt == "" && req.FinishedAt == "" && req.Timestamp == nil {
		return machine.ProvisionTiming{}, false
	}
	startedAt := parseEventTime(req.StartedAt)
	finishedAt := parseEventTime(req.FinishedAt)
	timestamp := parseEventTimeValue(req.Timestamp)
	if timestamp == nil && startedAt == nil && finishedAt == nil {
		t := now
		timestamp = &t
	}
	if strings.EqualFold(eventType, "start") && startedAt == nil && timestamp != nil {
		startedAt = timestamp
	}
	if strings.EqualFold(eventType, "finish") && finishedAt == nil && timestamp != nil {
		finishedAt = timestamp
	}
	return machine.ProvisionTiming{
		Source:           source,
		Name:             name,
		EventType:        eventType,
		Message:          firstNonEmpty(req.Message, req.Description),
		Result:           strings.TrimSpace(req.Result),
		Timestamp:        timestamp,
		StartedAt:        startedAt,
		FinishedAt:       finishedAt,
		DurationMillis:   req.DurationMillis,
		MonotonicSeconds: req.MonotonicSeconds,
	}, true
}

func appendProvisionTiming(events []machine.ProvisionTiming, next ...machine.ProvisionTiming) []machine.ProvisionTiming {
	for _, event := range next {
		if event.DurationMillis <= 0 && event.FinishedAt != nil {
			for i := len(events) - 1; i >= 0; i-- {
				prev := events[i]
				if prev.StartedAt == nil || prev.FinishedAt != nil {
					continue
				}
				if strings.TrimSpace(prev.Source) != strings.TrimSpace(event.Source) || strings.TrimSpace(prev.Name) != strings.TrimSpace(event.Name) {
					continue
				}
				event.StartedAt = prev.StartedAt
				event.DurationMillis = event.FinishedAt.Sub(*prev.StartedAt).Milliseconds()
				break
			}
		}
		events = append(events, event)
		if len(events) > maxProvisionTimingEvents {
			events = events[len(events)-maxProvisionTimingEvents:]
		}
	}
	return events
}

func serverTiming(name, message, result string, startedAt, finishedAt time.Time, sizeBytes int64) machine.ProvisionTiming {
	duration := finishedAt.Sub(startedAt).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	if sizeBytes > 0 {
		message = fmt.Sprintf("%s (%d bytes)", message, sizeBytes)
	}
	return machine.ProvisionTiming{
		Source:         "server",
		Name:           name,
		EventType:      "timing",
		Message:        message,
		Result:         result,
		StartedAt:      &startedAt,
		FinishedAt:     &finishedAt,
		DurationMillis: duration,
	}
}

func parseEventTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		utc := t.UTC()
		return &utc
	}
	if unix, err := strconv.ParseFloat(value, 64); err == nil && unix > 0 {
		sec := int64(unix)
		nsec := int64((unix - float64(sec)) * 1e9)
		t := time.Unix(sec, nsec).UTC()
		return &t
	}
	return nil
}

func parseEventTimeValue(value any) *time.Time {
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return parseEventTime(v)
	case float64:
		return parseEventTime(strconv.FormatFloat(v, 'f', -1, 64))
	case json.Number:
		return parseEventTime(string(v))
	default:
		return nil
	}
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

func (h *Handler) PXEArtifact(c echo.Context) error {
	if h.osimages == nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonError("os image service not available"))
	}
	name := c.Param("name")
	raw := strings.TrimPrefix(c.Param("*"), "/")
	rel, err := sanitizePXEPath(raw)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid artifact path"))
	}
	img, err := h.osimages.Get(c.Request().Context(), name)
	if err != nil {
		return c.JSON(gohttp.StatusNotFound, jsonError("os image not found"))
	}
	if !artifactPathAllowed(img.Manifest, rel) {
		return c.JSON(gohttp.StatusNotFound, jsonError("artifact not found"))
	}
	base, err := artifactBaseDir(img)
	if err != nil {
		return c.JSON(gohttp.StatusNotFound, jsonErrorErr(err))
	}
	full, err := safeArtifactFilePath(base, rel)
	if err != nil {
		return c.JSON(gohttp.StatusNotFound, jsonError("artifact not found"))
	}
	if _, err := os.Stat(full); err != nil {
		if os.IsNotExist(err) {
			return c.JSON(gohttp.StatusNotFound, jsonError("artifact not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return h.serveProvisionedPXEFile(c, full, "server.artifact_transfer", "serve OS artifact "+rel)
}

func (h *Handler) serveProvisionedPXEFile(c echo.Context, full, name, message string) error {
	token := strings.TrimSpace(c.QueryParam("token"))
	attemptID := strings.TrimSpace(c.QueryParam("attempt_id"))
	if token == "" || attemptID == "" {
		return c.File(full)
	}
	sizeBytes := int64(0)
	if st, err := os.Stat(full); err == nil {
		sizeBytes = st.Size()
	}
	startedAt := time.Now().UTC()
	err := c.File(full)
	finishedAt := time.Now().UTC()
	result := "success"
	if err != nil {
		result = "failure"
	}
	timing := serverTiming(name, message, result, startedAt, finishedAt, sizeBytes)
	h.appendProvisionTimingByToken(c.Request().Context(), token, attemptID, timing)
	return err
}

func (h *Handler) appendProvisionTimingByToken(ctx context.Context, token, attemptID string, timing machine.ProvisionTiming) {
	target, err := h.requireProvisioningMachine(ctx, token)
	if err != nil || validateAttemptParam(target, attemptID) != nil {
		return
	}
	now := time.Now().UTC()
	_ = h.updateProvisionProgress(ctx, target.Name, func(m *machine.Machine) {
		if m.Provision == nil {
			m.Provision = &machine.ProvisionProgress{}
		}
		m.Provision.LastSignalAt = &now
		m.Provision.Timings = appendProvisionTiming(m.Provision.Timings, timing)
	})
}

func (h *Handler) buildCurtinInstallConfig(ctx context.Context, c echo.Context, m *machine.Machine, img osimage.OSImage, info *hwinfo.HardwareInfo) (string, error) {
	_ = ctx
	base := h.resolvePXEBaseURL(c)
	token := strings.TrimSpace(c.QueryParam("token"))
	attemptID := strings.TrimSpace(c.QueryParam("attempt_id"))
	if err := validateAttemptParam(m, attemptID); err != nil {
		return "", err
	}
	selectedDisk, err := selectTargetDiskInfo(m, info)
	if err != nil {
		return "", err
	}
	targetDisk := selectedDisk.Path

	imageURL := ""
	imageFormat := string(img.Format)
	imageCompression := ""
	if img.Manifest != nil && strings.TrimSpace(img.Manifest.Root.Path) != "" {
		imageURL, err = h.artifactURL(base, img, img.Manifest.Root.Path)
		if err != nil {
			return "", err
		}
		if img.Manifest.Root.Format != "" {
			imageFormat = string(img.Manifest.Root.Format)
		}
		imageCompression = strings.ToLower(strings.TrimSpace(img.Manifest.Root.Compression))
	} else {
		if !img.Ready {
			return "", fmt.Errorf("os image %q is not ready", img.Name)
		}
		imageURL, err = imageFileURL(base, img)
		if err != nil {
			return "", err
		}
	}

	sourceURI := imageURL
	sourceType := "dd-raw"
	var storageConfig *curtinStorage
	stages := []string{"early", "partitioning", "network", "extract", "late"}
	switch strings.ToLower(strings.TrimSpace(imageFormat)) {
	case "", string(osimage.FormatRAW):
		switch imageCompression {
		case "", "none":
			sourceURI = appendProvisionQuery(imageURL, token, attemptID)
		default:
			return "", fmt.Errorf("curtin deploy requires uncompressed raw image, got compression %q", imageCompression)
		}
	case string(osimage.FormatSquashFS):
		sourceType = "fsimage"
		sourceURI = appendProvisionQuery(imageURL, token, attemptID)
		rootSizeMB, err := rootFSRootPartitionSizeMB(selectedDisk.Info)
		if err != nil {
			return "", err
		}
		storageConfig = buildRootFSStorageConfig(targetDisk, rootSizeMB)
		stages = []string{"early", "partitioning", "network", "extract", "curthooks", "late"}
	default:
		return "", fmt.Errorf("curtin deploy requires raw or squashfs image, got format %q", imageFormat)
	}

	seedURL := fmt.Sprintf("%s/nocloud/%s/", strings.TrimRight(base, "/"), macToken(m.MAC))
	lateCommands := []string{
		fmt.Sprintf(`set -e; d="$TARGET_MOUNT_POINT/var/lib/cloud/seed/nocloud"; mkdir -p "$d"; for f in user-data meta-data vendor-data network-config; do curl -fsS -o "$d/$f" %s$f; done`, shellQuote(seedURL)),
		`mkdir -p "$TARGET_MOUNT_POINT/etc/cloud/cloud.cfg.d"; printf '%s\n' 'datasource_list: [ NoCloud, None ]' 'datasource:' '  NoCloud:' '    seedfrom: /var/lib/cloud/seed/nocloud/' 'ssh_deletekeys: false' > "$TARGET_MOUNT_POINT/etc/cloud/cloud.cfg.d/99_gomi_nocloud.cfg"; rm -f "$TARGET_MOUNT_POINT"/etc/netplan/*.yaml`,
		`if [ -x "$TARGET_MOUNT_POINT/usr/bin/ssh-keygen" ]; then rm -f "$TARGET_MOUNT_POINT"/etc/ssh/ssh_host_*_key "$TARGET_MOUNT_POINT"/etc/ssh/ssh_host_*_key.pub; chroot "$TARGET_MOUNT_POINT" ssh-keygen -A; fi`,
		`sed -i 's/discard,errors=remount-ro/defaults,errors=remount-ro/g' "$TARGET_MOUNT_POINT/etc/fstab" 2>/dev/null || true; sed -i -E 's/(root=[^ ]+) ro /\1 rw /g' "$TARGET_MOUNT_POINT/boot/grub/grub.cfg" 2>/dev/null || true`,
	}

	cfg := curtinConfig{
		Install: curtinInstall{
			LogFile:           "/tmp/curtin-install.log",
			PostFiles:         []string{"/tmp/curtin-install.log"},
			SaveInstallConfig: "/root/gomi-curtin.yaml",
			SaveInstallLog:    "/var/log/gomi-curtin-install.log",
		},
		Reporting: curtinReporting{
			Gomi: curtinReportingHook{
				Type:     "webhook",
				Endpoint: buildPXEDeployEventsURL(base, token, attemptID) + "&source=curtin",
				Level:    "DEBUG",
			},
		},
		BlockMeta: curtinBlockMeta{
			Devices: []string{targetDisk},
		},
		Sources: map[string]curtinSource{
			"00-root": {
				Type: sourceType,
				URI:  sourceURI,
			},
		},
		Stages:       stages,
		LateCommands: make(map[string][]string, len(lateCommands)),
	}
	if ubuntuMirror := strings.TrimRight(strings.TrimSpace(os.Getenv("GOMI_CURTIN_UBUNTU_MIRROR")), "/"); ubuntuMirror != "" && strings.EqualFold(strings.TrimSpace(img.OSFamily), "ubuntu") {
		cfg.AptMirrors = &curtinAptMirrors{
			UbuntuArchive:  ubuntuMirror,
			UbuntuSecurity: ubuntuMirror,
		}
	}
	if storageConfig != nil {
		cfg.Storage = &storageConfig.Storage
		cfg.Grub = &storageConfig.Grub
		cfg.PartitioningCommands = map[string][]string{
			"builtin": {"curtin", "block-meta", "custom"},
		}
	}
	for i, cmd := range lateCommands {
		cfg.LateCommands[fmt.Sprintf("%02d-gomi-late", i+10)] = []string{"sh", "-c", cmd}
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func buildRootFSStorageConfig(targetDisk string, rootSizeMB int64) *curtinStorage {
	return &curtinStorage{
		Storage: curtinStorageConfig{
			Version: 1,
			Config: []map[string]any{
				{
					"id":          "disk0",
					"type":        "disk",
					"path":        targetDisk,
					"ptable":      "gpt",
					"grub_device": true,
					"wipe":        "superblock-recursive",
				},
				{
					"id":     "part-bios",
					"type":   "partition",
					"device": "disk0",
					"number": 1,
					"size":   "1M",
					"flag":   "bios_grub",
				},
				{
					"id":     "part-efi",
					"type":   "partition",
					"device": "disk0",
					"number": 2,
					"size":   "512M",
					"flag":   "boot",
				},
				{
					"id":     "fmt-efi",
					"type":   "format",
					"volume": "part-efi",
					"fstype": "fat32",
					"label":  "EFI",
				},
				{
					"id":     "part-root",
					"type":   "partition",
					"device": "disk0",
					"number": 3,
					"size":   fmt.Sprintf("%dM", rootSizeMB),
				},
				{
					"id":     "fmt-root",
					"type":   "format",
					"volume": "part-root",
					"fstype": "ext4",
					"label":  "rootfs",
				},
				{
					"id":     "mount-root",
					"type":   "mount",
					"device": "fmt-root",
					"path":   "/",
				},
				{
					"id":     "mount-efi",
					"type":   "mount",
					"device": "fmt-efi",
					"path":   "/boot/efi",
				},
			},
		},
		Grub: curtinGrub{
			InstallDevices: []string{targetDisk},
		},
	}
}

func rootFSRootPartitionSizeMB(disk hwinfo.DiskInfo) (int64, error) {
	if disk.SizeMB <= 0 {
		return 0, fmt.Errorf("target disk size is required for squashfs storage config")
	}
	rootSizeMB := disk.SizeMB - rootFSBIOSBootPartitionSizeMB - rootFSEFIPartitionSizeMB - rootFSPartitionReserveMB
	if rootSizeMB < rootFSMinimumRootPartitionSizeMB {
		return 0, fmt.Errorf("target disk is too small for squashfs install: size=%dMiB minimum=%dMiB", disk.SizeMB, rootFSMinimumRootPartitionSizeMB+rootFSBIOSBootPartitionSizeMB+rootFSEFIPartitionSizeMB+rootFSPartitionReserveMB)
	}
	return rootSizeMB, nil
}

func (h *Handler) artifactURL(base string, img osimage.OSImage, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("artifact path is empty")
	}
	clean, err := sanitizePXEPath(rel)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/artifacts/os-images/%s/%s", strings.TrimRight(base, "/"), url.PathEscape(img.Name), clean), nil
}

func imageFileURL(base string, img osimage.OSImage) (string, error) {
	local := strings.TrimSpace(img.LocalPath)
	if local == "" {
		return "", fmt.Errorf("os image %q has no local path", img.Name)
	}
	return fmt.Sprintf("%s/files/images/%s", strings.TrimRight(base, "/"), url.PathEscape(filepath.Base(local))), nil
}

func appendProvisionQuery(rawURL, token, attemptID string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	if strings.TrimSpace(token) != "" {
		query.Set("token", strings.TrimSpace(token))
	}
	if strings.TrimSpace(attemptID) != "" {
		query.Set("attempt_id", strings.TrimSpace(attemptID))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func artifactBaseDir(img osimage.OSImage) (string, error) {
	local := strings.TrimSpace(img.LocalPath)
	if local == "" {
		return "", fmt.Errorf("os image has no local artifact path")
	}
	st, err := os.Stat(local)
	if err != nil {
		return "", fmt.Errorf("os image local artifact path is not available")
	}
	if !st.IsDir() {
		return "", fmt.Errorf("os image local artifact path is not an artifact directory")
	}
	return local, nil
}

func selectTargetDisk(m *machine.Machine, info *hwinfo.HardwareInfo) (string, error) {
	selected, err := selectTargetDiskInfo(m, info)
	if err != nil {
		return "", err
	}
	return selected.Path, nil
}

func selectTargetDiskInfo(m *machine.Machine, info *hwinfo.HardwareInfo) (selectedTargetDisk, error) {
	if info == nil {
		return selectedTargetDisk{}, fmt.Errorf("hardware inventory is required for target disk selection")
	}
	candidates := installableDiskCandidates(info)
	if m != nil {
		if disk := strings.TrimSpace(m.TargetDisk); disk != "" {
			return selectInventoryBackedTargetDiskOverrideInfo(disk, candidates)
		}
	}
	if len(candidates) == 0 {
		return selectedTargetDisk{}, fmt.Errorf("no installable target disk found")
	}
	if len(candidates) > 1 {
		return selectedTargetDisk{}, fmt.Errorf("ambiguous target disk: %d installable disks found", len(candidates))
	}
	disk := stableDiskPath(candidates[0])
	if !isWholeDiskPath(disk) {
		return selectedTargetDisk{}, fmt.Errorf("selected target disk is not a whole disk path: %s", disk)
	}
	return selectedTargetDisk{Path: disk, Info: candidates[0]}, nil
}

func selectInventoryBackedTargetDiskOverride(disk string, candidates []hwinfo.DiskInfo) (string, error) {
	selected, err := selectInventoryBackedTargetDiskOverrideInfo(disk, candidates)
	if err != nil {
		return "", err
	}
	return selected.Path, nil
}

func selectInventoryBackedTargetDiskOverrideInfo(disk string, candidates []hwinfo.DiskInfo) (selectedTargetDisk, error) {
	if !isWholeDiskPath(disk) {
		return selectedTargetDisk{}, fmt.Errorf("targetDisk must be a whole disk path: %s", disk)
	}
	for _, candidate := range candidates {
		for _, path := range diskInventoryPaths(candidate) {
			if path == disk {
				return selectedTargetDisk{Path: disk, Info: candidate}, nil
			}
		}
	}
	return selectedTargetDisk{}, fmt.Errorf("targetDisk is not present in current hardware inventory: %s", disk)
}

func installableDiskCandidates(info *hwinfo.HardwareInfo) []hwinfo.DiskInfo {
	if info == nil {
		return nil
	}
	candidates := make([]hwinfo.DiskInfo, 0, len(info.Disks))
	for _, disk := range info.Disks {
		if disk.Removable {
			continue
		}
		name := strings.TrimSpace(disk.Name)
		if name == "" && strings.TrimSpace(disk.Path) != "" {
			name = filepath.Base(strings.TrimSpace(disk.Path))
		}
		if isIgnoredBlockDevice(name) {
			continue
		}
		if disk.Type != "" && disk.Type != "disk" {
			continue
		}
		candidates = append(candidates, disk)
	}
	return candidates
}

func diskPathInInventory(path string, disks []hwinfo.DiskInfo) bool {
	path = strings.TrimSpace(path)
	for _, disk := range disks {
		for _, candidate := range diskInventoryPaths(disk) {
			if candidate == path {
				return true
			}
		}
	}
	return false
}

func diskInventoryPaths(d hwinfo.DiskInfo) []string {
	paths := make([]string, 0, 2+len(d.ByID)+len(d.ByPath))
	if path := strings.TrimSpace(d.Path); path != "" {
		paths = append(paths, path)
	}
	if name := strings.TrimSpace(d.Name); name != "" {
		if strings.HasPrefix(name, "/dev/") {
			paths = append(paths, name)
		} else {
			paths = append(paths, "/dev/"+name)
		}
	}
	paths = append(paths, sortedNonEmpty(d.ByID)...)
	paths = append(paths, sortedNonEmpty(d.ByPath)...)
	return paths
}

func stableDiskPath(d hwinfo.DiskInfo) string {
	if strings.TrimSpace(d.Path) != "" {
		return d.Path
	}
	if byID := sortedNonEmpty(d.ByID); len(byID) > 0 {
		return byID[0]
	}
	if byPath := sortedNonEmpty(d.ByPath); len(byPath) > 0 {
		return byPath[0]
	}
	name := strings.TrimSpace(d.Name)
	if strings.HasPrefix(name, "/dev/") {
		return name
	}
	return "/dev/" + name
}

func sortedNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func isIgnoredBlockDevice(name string) bool {
	name = strings.TrimPrefix(strings.TrimSpace(name), "/dev/")
	if name == "" {
		return true
	}
	for _, prefix := range []string{"loop", "ram", "sr", "fd", "dm-", "md", "zram"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func isWholeDiskPath(path string) bool {
	return machine.IsWholeDiskPath(path)
}

func artifactPathAllowed(manifest *osimage.Manifest, rel string) bool {
	if manifest == nil {
		return false
	}
	clean, err := sanitizePXEPath(rel)
	if err != nil {
		return false
	}
	if clean == strings.TrimSpace(manifest.Root.Path) {
		return true
	}
	for _, bundle := range manifest.Bundles {
		if clean == strings.TrimSpace(bundle.Path) {
			return true
		}
	}
	return false
}

func safeArtifactFilePath(base, rel string) (string, error) {
	baseReal, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	full := filepath.Join(baseReal, filepath.FromSlash(rel))
	fullReal, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	prefix := baseReal + string(os.PathSeparator)
	if fullReal != baseReal && !strings.HasPrefix(fullReal, prefix) {
		return "", fmt.Errorf("artifact path escapes base directory")
	}
	return fullReal, nil
}

func (h *Handler) hardwareInfo(ctx context.Context, machineName string) (*hwinfo.HardwareInfo, error) {
	if h.hwinfo == nil {
		return nil, resource.ErrNotFound
	}
	info, err := h.hwinfo.Get(ctx, machineName)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (h *Handler) requireProvisioningMachine(ctx context.Context, token string) (*machine.Machine, error) {
	target, err := h.findMachineByProvisionToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if target == nil || target.Provision == nil || strings.TrimSpace(target.Provision.CompletionToken) != strings.TrimSpace(token) {
		return nil, resource.ErrNotFound
	}
	if !target.Provision.Active {
		return nil, resource.ErrNotFound
	}
	return target, nil
}

func (h *Handler) updateProvisionProgress(ctx context.Context, name string, fn func(*machine.Machine)) error {
	if h.machines == nil {
		return fmt.Errorf("machine service not available")
	}
	m, err := h.machines.Get(ctx, name)
	if err != nil {
		return err
	}
	fn(&m)
	m.UpdatedAt = time.Now().UTC()
	return h.machines.Store().Upsert(ctx, m)
}

func buildPXECurtinConfigURL(base, token, attemptID string) string {
	q := url.Values{}
	q.Set("token", token)
	if attemptID != "" {
		q.Set("attempt_id", attemptID)
	}
	return strings.TrimRight(base, "/") + "/curtin-config?" + q.Encode()
}

func buildPXEDeployEventsURL(base, token, attemptID string) string {
	q := url.Values{}
	q.Set("token", token)
	if attemptID != "" {
		q.Set("attempt_id", attemptID)
	}
	return strings.TrimRight(base, "/") + "/deploy-events?" + q.Encode()
}

func eventMessage(eventType, message string) string {
	if strings.TrimSpace(message) != "" {
		return message
	}
	if eventType == deployEventImageApplied {
		return "image applied; waiting for target OS first boot"
	}
	return "deploy event: " + eventType
}

func machineImageApplied(m *machine.Machine) bool {
	if m == nil || m.Provision == nil || m.Provision.Artifacts == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(m.Provision.Artifacts[provisionArtifactImageApplied]), "true")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func timePtr(t time.Time) *time.Time {
	return &t
}
