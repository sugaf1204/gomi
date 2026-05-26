package pxehttp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
	"gopkg.in/yaml.v3"
	gohttp "net/http"
	"strings"
	"time"
)

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
	if !img.Ready {
		return "", fmt.Errorf("os image %q is not ready", img.Name)
	}
	if img.Manifest != nil && strings.TrimSpace(img.Manifest.Root.Path) != "" {
		imageURL, err = h.artifactURL(base, img, img.Manifest.Root.Path)
		if err != nil {
			return "", err
		}
		if img.Manifest.Root.Format != "" {
			imageFormat = string(img.Manifest.Root.Format)
		}
	} else {
		imageURL, err = imageFileURL(base, img)
		if err != nil {
			return "", err
		}
	}

	var storageConfig *curtinStorage
	stages := []string{"early", "partitioning", "network", "extract", "late"}
	capability := installCapabilityForOSFamily(img.OSFamily)
	rootFilesystem, err := rootFilesystemForImage(img, capability)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(strings.TrimSpace(imageFormat)) {
	case string(osimage.FormatSquashFS):
		rootSizeMB, err := rootFSRootPartitionSizeMB(selectedDisk.Info)
		if err != nil {
			return "", err
		}
		storageConfig = buildRootFSStorageConfig(targetDisk, rootSizeMB, rootFilesystem)
	default:
		return "", fmt.Errorf("curtin deploy requires squashfs image, got format %q", imageFormat)
	}
	sourceURI := appendProvisionQuery(imageURL, token, attemptID)

	seedURL := fmt.Sprintf("%s/nocloud/%s/", strings.TrimRight(base, "/"), macToken(m.MAC))
	lateCommands := []string{
		fmt.Sprintf(`set -e; d="$TARGET_MOUNT_POINT/var/lib/cloud/seed/nocloud"; mkdir -p "$d"; for f in user-data meta-data vendor-data network-config; do curl -fsS -o "$d/$f" %s$f; done`, shellQuote(seedURL)),
		`mkdir -p "$TARGET_MOUNT_POINT/etc/cloud/cloud.cfg.d"; printf '%s\n' 'datasource_list: [ NoCloud, None ]' 'datasource:' '  NoCloud:' '    seedfrom: /var/lib/cloud/seed/nocloud/' 'ssh_deletekeys: false' > "$TARGET_MOUNT_POINT/etc/cloud/cloud.cfg.d/99_gomi_nocloud.cfg"; rm -f "$TARGET_MOUNT_POINT"/etc/cloud/cloud.cfg.d/50-curtin-networking.cfg "$TARGET_MOUNT_POINT"/etc/netplan/*.yaml`,
		`mkdir -p "$TARGET_MOUNT_POINT/dev"; if [ ! -e "$TARGET_MOUNT_POINT/dev/null" ]; then mknod -m 666 "$TARGET_MOUNT_POINT/dev/null" c 1 3; else chmod 666 "$TARGET_MOUNT_POINT/dev/null"; fi`,
		`if [ -x "$TARGET_MOUNT_POINT/usr/bin/ssh-keygen" ]; then rm -f "$TARGET_MOUNT_POINT"/etc/ssh/ssh_host_*_key "$TARGET_MOUNT_POINT"/etc/ssh/ssh_host_*_key.pub; chroot "$TARGET_MOUNT_POINT" ssh-keygen -A; fi`,
		buildRootFSFstabCommand(rootFilesystem),
		`if [ -x "$TARGET_MOUNT_POINT/usr/sbin/netplan" ] && [ -x "$TARGET_MOUNT_POINT/lib/systemd/systemd-networkd" ]; then chroot "$TARGET_MOUNT_POINT" systemctl enable systemd-networkd; fi`,
		`sed -i 's/discard,errors=remount-ro/defaults,errors=remount-ro/g' "$TARGET_MOUNT_POINT/etc/fstab" 2>/dev/null || true; sed -i -E 's/(root=[^ ]+) ro /\1 rw /g' "$TARGET_MOUNT_POINT/boot/grub/grub.cfg" 2>/dev/null || true`,
		buildRootFSBootloaderCommand(capability, targetDisk, m.Firmware, rootFilesystem),
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
				Type: "fsimage",
				URI:  sourceURI,
			},
		},
		Stages:       stages,
		LateCommands: make(map[string][]string, len(lateCommands)),
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

func (h *Handler) buildDiskImageDeployResponse(base, token, attemptID string, m *machine.Machine, img osimage.OSImage, info *hwinfo.HardwareInfo) (*diskImageDeployResponse, error) {
	if !img.Ready {
		return nil, fmt.Errorf("os image %q is not ready", img.Name)
	}
	if rootImageFormat(img) != osimage.FormatQCOW2 {
		return nil, fmt.Errorf("disk image deploy requires qcow2 image, got format %q", rootImageFormat(img))
	}
	if !osimage.SupportsDeploymentTarget(img, osimage.DeploymentTargetBareMetal) {
		return nil, fmt.Errorf("os image %q does not support bare-metal deployment", img.Name)
	}
	if img.Manifest == nil || strings.TrimSpace(img.Manifest.Root.Path) == "" {
		return nil, fmt.Errorf("bare-metal qcow2 deploy requires manifest.root.path")
	}
	if img.Manifest.Root.RootPartition.Number <= 0 {
		return nil, fmt.Errorf("bare-metal qcow2 deploy requires manifest.root.rootPartition.number")
	}
	selectedDisk, err := selectTargetDiskInfo(m, info)
	if err != nil {
		return nil, err
	}
	imageURL, err := h.artifactURL(base, img, img.Manifest.Root.Path)
	if err != nil {
		return nil, err
	}
	deploy := &diskImageDeployResponse{
		ImageURL:            appendProvisionQuery(imageURL, token, attemptID),
		Format:              string(osimage.FormatQCOW2),
		OSFamily:            img.OSFamily,
		OSVersion:           img.OSVersion,
		TargetDisk:          selectedDisk.Path,
		RootPartitionNumber: img.Manifest.Root.RootPartition.Number,
		SeedURL:             fmt.Sprintf("%s/nocloud/%s", strings.TrimRight(base, "/"), macToken(m.MAC)),
	}
	if img.Manifest.Root.EFIPartition != nil {
		deploy.EFIPartitionNumber = img.Manifest.Root.EFIPartition.Number
	}
	return deploy, nil
}

func rootImageFormat(img osimage.OSImage) osimage.ImageFormat {
	return osimage.EffectiveImageFormat(img)
}

func rootFilesystemForImage(img osimage.OSImage, cap osInstallCapability) (string, error) {
	filesystem := ""
	if img.Manifest != nil {
		filesystem = img.Manifest.Root.RootPartition.Filesystem
	}
	if strings.TrimSpace(filesystem) == "" {
		filesystem = cap.RootFilesystem
	}
	filesystem = strings.ToLower(strings.TrimSpace(filesystem))
	switch filesystem {
	case "":
		return "ext4", nil
	case "ext4", "xfs", "btrfs":
		return filesystem, nil
	default:
		return "", fmt.Errorf("unsupported root filesystem %q for squashfs deploy", filesystem)
	}
}
