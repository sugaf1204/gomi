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

	apiinventory "github.com/sugaf1204/gomi/api/inventory"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
)

const deployEventImageApplied = "image_applied"
const provisionArtifactImageApplied = "imageApplied"
const provisionArtifactImageAppliedAt = "imageAppliedAt"

type deployEventRequest struct {
	AttemptID string          `json:"attemptId,omitempty"`
	Type      string          `json:"type"`
	Message   string          `json:"message,omitempty"`
	Reason    string          `json:"reason,omitempty"`
	LogTail   string          `json:"logTail,omitempty"`
	Details   json.RawMessage `json:"details,omitempty"`
}

func (h *Handler) PXEInventory(c echo.Context) error {
	token := strings.TrimSpace(c.QueryParam("token"))
	if token == "" {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "token is required"})
	}

	ctx := c.Request().Context()
	target, err := h.requireProvisioningMachine(ctx, token)
	if err != nil {
		status := gohttp.StatusInternalServerError
		if err == resource.ErrNotFound {
			status = gohttp.StatusNotFound
		}
		return c.JSON(status, map[string]string{"error": err.Error()})
	}

	attemptID := strings.TrimSpace(target.Provision.AttemptID)
	if attemptID == "" {
		return c.JSON(gohttp.StatusConflict, map[string]string{"error": "provisioning attempt id is required"})
	}
	var info hwinfo.HardwareInfo
	if strings.HasPrefix(c.Request().Header.Get("Content-Type"), "text/plain") {
		body, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		info = parseTextInventory(string(body))
	} else {
		body, err := io.ReadAll(c.Request().Body)
		if err != nil {
			return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		var payload apiinventory.HardwareInventory
		if err := json.Unmarshal(body, &payload); err != nil {
			if legacyErr := json.Unmarshal(body, &info); legacyErr != nil {
				return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "invalid body"})
			}
		} else {
			info = hwinfo.FromInventory(payload)
		}
	}
	info.Name = target.Name + "-hwinfo"
	info.MachineName = target.Name
	info.AttemptID = attemptID
	if h.hwinfo != nil {
		if _, err := h.hwinfo.Upsert(ctx, info); err != nil {
			return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
	}

	if err := h.updateProvisionProgress(ctx, target.Name, func(m *machine.Machine) {
		if m.Provision == nil {
			m.Provision = &machine.ProvisionProgress{}
		}
		m.Provision.AttemptID = attemptID
		m.Provision.InventoryID = info.Name
		m.Provision.LastSignalAt = timePtr(time.Now().UTC())
		m.Provision.Message = "hardware inventory received"
	}); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	base := h.resolvePXEBaseURL(c)
	return c.JSON(gohttp.StatusOK, map[string]string{
		"attemptId":       attemptID,
		"curtinConfigUrl": buildPXECurtinConfigURL(base, token, attemptID),
		"eventsUrl":       buildPXEDeployEventsURL(base, token, attemptID),
	})
}

func (h *Handler) PXECurtinConfig(c echo.Context) error {
	token := strings.TrimSpace(c.QueryParam("token"))
	if token == "" {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "token is required"})
	}
	queryAttemptID := strings.TrimSpace(c.QueryParam("attempt_id"))
	ctx := c.Request().Context()
	target, err := h.requireProvisioningMachine(ctx, token)
	if err != nil {
		status := gohttp.StatusInternalServerError
		if err == resource.ErrNotFound {
			status = gohttp.StatusNotFound
		}
		return c.JSON(status, map[string]string{"error": err.Error()})
	}
	if err := validateAttemptParam(target, queryAttemptID); err != nil {
		return c.JSON(gohttp.StatusConflict, map[string]string{"error": err.Error()})
	}
	if h.osimages == nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": "os image service not available"})
	}
	imageRef := strings.TrimSpace(target.OSPreset.ImageRef)
	if imageRef == "" {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "machine has no image reference"})
	}
	img, err := h.osimages.Get(ctx, imageRef)
	if err != nil {
		return c.JSON(gohttp.StatusNotFound, map[string]string{"error": fmt.Sprintf("os image %q not found: %v", imageRef, err)})
	}
	info, _ := h.hardwareInfo(ctx, target.Name)
	if err := validateAttemptInventory(target, info); err != nil {
		return c.JSON(gohttp.StatusConflict, map[string]string{"error": err.Error()})
	}
	config, err := h.buildCurtinInstallConfig(ctx, c, target, img, info)
	if err != nil {
		return c.JSON(gohttp.StatusConflict, map[string]string{"error": err.Error()})
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
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "token is required"})
	}
	queryAttemptID := strings.TrimSpace(c.QueryParam("attempt_id"))
	ctx := c.Request().Context()
	target, err := h.requireProvisioningMachine(ctx, token)
	if err != nil {
		status := gohttp.StatusInternalServerError
		if err == resource.ErrNotFound {
			status = gohttp.StatusNotFound
		}
		return c.JSON(status, map[string]string{"error": err.Error()})
	}
	var req deployEventRequest
	if strings.HasPrefix(c.Request().Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		req.Type = c.FormValue("type")
		req.Message = c.FormValue("message")
		req.Reason = c.FormValue("reason")
		req.AttemptID = c.FormValue("attemptId")
	} else if err := c.Bind(&req); err != nil {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	eventType := strings.ToLower(strings.TrimSpace(req.Type))
	if eventType == "" {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "type is required"})
	}
	if req.AttemptID != "" && queryAttemptID != "" && strings.TrimSpace(req.AttemptID) != queryAttemptID {
		return c.JSON(gohttp.StatusConflict, map[string]string{"error": "attempt_id query and body mismatch"})
	}
	if queryAttemptID == "" {
		queryAttemptID = strings.TrimSpace(req.AttemptID)
	}
	if err := validateAttemptParam(target, queryAttemptID); err != nil {
		return c.JSON(gohttp.StatusConflict, map[string]string{"error": err.Error()})
	}
	now := time.Now().UTC()
	if err := h.updateProvisionProgress(ctx, target.Name, func(m *machine.Machine) {
		if m.Provision == nil {
			m.Provision = &machine.ProvisionProgress{}
		}
		m.Provision.LastSignalAt = &now
		message := eventMessage(eventType, req.Message)
		m.Provision.Message = message
		if eventType == "failed" {
			m.Phase = machine.PhaseError
			m.LastError = firstNonEmpty(req.Reason, req.Message, "deploy failed")
			m.Provision.Active = false
			m.Provision.FinishedAt = &now
			m.Provision.FailureReason = m.LastError
		} else if eventType == deployEventImageApplied {
			if m.Provision.Artifacts == nil {
				m.Provision.Artifacts = map[string]string{}
			}
			m.Provision.Artifacts[provisionArtifactImageApplied] = "true"
			m.Provision.Artifacts[provisionArtifactImageAppliedAt] = now.Format(time.RFC3339)
			m.Provision.Message = h.configureBIOSBootOrder(ctx, *m, message)
		}
	}); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(gohttp.StatusOK, map[string]string{"status": "ok"})
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
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": "os image service not available"})
	}
	name := c.Param("name")
	raw := strings.TrimPrefix(c.Param("*"), "/")
	rel, err := sanitizePXEPath(raw)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "invalid artifact path"})
	}
	img, err := h.osimages.Get(c.Request().Context(), name)
	if err != nil {
		return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "os image not found"})
	}
	if !artifactPathAllowed(img.Manifest, rel) {
		return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "artifact not found"})
	}
	base, err := artifactBaseDir(img)
	if err != nil {
		return c.JSON(gohttp.StatusNotFound, map[string]string{"error": err.Error()})
	}
	full, err := safeArtifactFilePath(base, rel)
	if err != nil {
		return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "artifact not found"})
	}
	if _, err := os.Stat(full); err != nil {
		if os.IsNotExist(err) {
			return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "artifact not found"})
		}
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.File(full)
}

func (h *Handler) buildCurtinInstallConfig(ctx context.Context, c echo.Context, m *machine.Machine, img osimage.OSImage, info *hwinfo.HardwareInfo) (string, error) {
	_ = ctx
	base := h.resolvePXEBaseURL(c)
	attemptID := strings.TrimSpace(c.QueryParam("attempt_id"))
	if err := validateAttemptParam(m, attemptID); err != nil {
		return "", err
	}
	targetDisk, err := selectTargetDisk(m, info)
	if err != nil {
		return "", err
	}

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
	switch strings.ToLower(strings.TrimSpace(imageFormat)) {
	case "", string(osimage.FormatRAW):
		switch imageCompression {
		case "", "none":
			sourceURI = imageURL
		default:
			return "", fmt.Errorf("curtin deploy requires uncompressed raw image, got compression %q", imageCompression)
		}
	default:
		return "", fmt.Errorf("curtin deploy requires raw image, got format %q", imageFormat)
	}

	seedURL := fmt.Sprintf("%s/nocloud/%s/", strings.TrimRight(base, "/"), macToken(m.MAC))
	lateCommands := []string{
		fmt.Sprintf(`set -e; d="$TARGET_MOUNT_POINT/var/lib/cloud/seed/nocloud"; mkdir -p "$d"; for f in user-data meta-data vendor-data network-config; do curl -fsS -o "$d/$f" %s$f; done`, shellQuote(seedURL)),
		`mkdir -p "$TARGET_MOUNT_POINT/etc/cloud/cloud.cfg.d"; printf '%s\n' 'datasource_list: [ NoCloud, None ]' 'datasource:' '  NoCloud:' '    seedfrom: /var/lib/cloud/seed/nocloud/' > "$TARGET_MOUNT_POINT/etc/cloud/cloud.cfg.d/99_gomi_nocloud.cfg"; rm -f "$TARGET_MOUNT_POINT"/etc/netplan/*.yaml`,
		`sed -i 's/discard,errors=remount-ro/defaults,errors=remount-ro/g' "$TARGET_MOUNT_POINT/etc/fstab" 2>/dev/null || true; sed -i -E 's/(root=[^ ]+) ro /\1 rw /g' "$TARGET_MOUNT_POINT/boot/grub/grub.cfg" 2>/dev/null || true`,
	}

	var b strings.Builder
	b.WriteString("install:\n")
	b.WriteString("  log_file: /tmp/curtin-install.log\n")
	b.WriteString("  save_install_config: /root/gomi-curtin.yaml\n")
	b.WriteString("  save_install_log: /var/log/gomi-curtin-install.log\n")
	if ubuntuMirror := strings.TrimRight(strings.TrimSpace(os.Getenv("GOMI_CURTIN_UBUNTU_MIRROR")), "/"); ubuntuMirror != "" && strings.EqualFold(strings.TrimSpace(img.OSFamily), "ubuntu") {
		b.WriteString("apt_mirrors:\n")
		b.WriteString("  ubuntu_archive: " + yamlQuote(ubuntuMirror) + "\n")
		b.WriteString("  ubuntu_security: " + yamlQuote(ubuntuMirror) + "\n")
	}
	b.WriteString("block-meta:\n")
	b.WriteString("  devices:\n")
	b.WriteString("    - " + yamlQuote(targetDisk) + "\n")
	b.WriteString("sources:\n")
	b.WriteString("  - dd-img: " + yamlQuote(sourceURI) + "\n")
	b.WriteString("stages:\n")
	b.WriteString("  - early\n")
	b.WriteString("  - partitioning\n")
	b.WriteString("  - network\n")
	b.WriteString("  - extract\n")
	b.WriteString("  - late\n")
	b.WriteString("late_commands:\n")
	for i, cmd := range lateCommands {
		b.WriteString(fmt.Sprintf("  %02d-gomi-late: %s\n", i+10, yamlShellCommand(cmd)))
	}
	return b.String(), nil
}

func yamlQuote(value string) string {
	return strconv.Quote(value)
}

func yamlShellCommand(command string) string {
	return "[" + yamlQuote("sh") + ", " + yamlQuote("-c") + ", " + yamlQuote(command) + "]"
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
	if info == nil {
		return "", fmt.Errorf("hardware inventory is required for target disk selection")
	}
	candidates := installableDiskCandidates(info)
	if m != nil {
		if disk := strings.TrimSpace(m.TargetDisk); disk != "" {
			return selectInventoryBackedTargetDiskOverride(disk, candidates)
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no installable target disk found")
	}
	if len(candidates) > 1 {
		return "", fmt.Errorf("ambiguous target disk: %d installable disks found", len(candidates))
	}
	disk := stableDiskPath(candidates[0])
	if !isWholeDiskPath(disk) {
		return "", fmt.Errorf("selected target disk is not a whole disk path: %s", disk)
	}
	return disk, nil
}

func selectInventoryBackedTargetDiskOverride(disk string, candidates []hwinfo.DiskInfo) (string, error) {
	if !isWholeDiskPath(disk) {
		return "", fmt.Errorf("targetDisk must be a whole disk path: %s", disk)
	}
	if !diskPathInInventory(disk, candidates) {
		return "", fmt.Errorf("targetDisk is not present in current hardware inventory: %s", disk)
	}
	return disk, nil
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
