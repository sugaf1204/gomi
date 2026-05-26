package vm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
)

const hypervisorImageDir = "/var/lib/libvirt/images"

var cloudImageBackingLocks sync.Map

type cloudImageStorage interface {
	VolumeExists(ctx context.Context, name string, format string) (bool, error)
	CreateVolumeFromReader(ctx context.Context, name string, sizeBytes int64, format string, r io.Reader) error
	DeleteVolume(ctx context.Context, name string) error
}

func (d *Deployer) resolveCloudImageBacking(ctx context.Context, osImageRef string) (string, string, error) {
	_, backingPath, backingFormat, err := d.resolveCloudImageBackingImage(ctx, osImageRef)
	return backingPath, backingFormat, err
}

func (d *Deployer) prepareCloudImageBacking(ctx context.Context, storage cloudImageStorage, osImageRef string) (string, string, error) {
	img, backingPath, backingFormat, err := d.resolveCloudImageBackingImage(ctx, osImageRef)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(img.URL) == "" {
		return backingPath, backingFormat, nil
	}
	volumeName := cloudImageURLVolumeBaseName(img, backingFormat)
	backingPath = filepath.Join(hypervisorImageDir, cloudImageVolumeName(volumeName, backingFormat))
	unlock := lockCloudImageBacking(volumeName, backingFormat)
	defer unlock()

	exists, err := storage.VolumeExists(ctx, volumeName, backingFormat)
	if err != nil {
		return "", "", fmt.Errorf("check cloud image backing %s: %w", img.Name, err)
	}
	if exists {
		return backingPath, backingFormat, nil
	}
	if err := d.uploadCloudImageBacking(ctx, storage, img, volumeName, backingFormat); err != nil {
		return "", "", err
	}
	return backingPath, backingFormat, nil
}

func (d *Deployer) resolveCloudImageBackingImage(ctx context.Context, osImageRef string) (osimage.OSImage, string, string, error) {
	if d.OSImages == nil {
		return osimage.OSImage{}, "", "", errors.New("os image service is not configured")
	}
	ref := strings.TrimSpace(osImageRef)
	if ref == "" {
		return osimage.OSImage{}, "", "", errors.New("osImageRef is required for cloudimage deployment")
	}
	img, err := d.OSImages.Get(ctx, ref)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return osimage.OSImage{}, "", "", fmt.Errorf("referenced osImageRef not found: %s", ref)
		}
		return osimage.OSImage{}, "", "", err
	}
	if !img.Ready {
		return osimage.OSImage{}, "", "", fmt.Errorf("osImage %s is not ready", ref)
	}
	backingFormat := strings.TrimSpace(string(img.Format))
	if img.Manifest != nil && strings.TrimSpace(string(img.Manifest.Root.Format)) != "" {
		backingFormat = strings.TrimSpace(string(img.Manifest.Root.Format))
	}
	if backingFormat == "" {
		backingFormat = "qcow2"
	}
	if backingFormat != "qcow2" {
		return osimage.OSImage{}, "", "", fmt.Errorf("cloudimage deployment requires qcow2 OS image, got %s", backingFormat)
	}
	if img.Manifest == nil && strings.TrimSpace(img.LocalPath) == "" && strings.TrimSpace(img.URL) == "" {
		return osimage.OSImage{}, "", "", fmt.Errorf("osImage %s has no localPath or url", ref)
	}
	backingPath := filepath.Join(hypervisorImageDir, cloudImageVolumeName(cloudImageBackingVolumeBaseName(img, backingFormat), backingFormat))
	return img, backingPath, backingFormat, nil
}

func lockCloudImageBacking(name string, format string) func() {
	value, _ := cloudImageBackingLocks.LoadOrStore(cloudImageVolumeName(name, format), &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func cloudImageVolumeName(name string, format string) string {
	if format == "" {
		format = "qcow2"
	}
	suffix := "." + format
	if strings.HasSuffix(name, suffix) {
		return name
	}
	return name + suffix
}

func cloudImageBackingVolumeBaseName(img osimage.OSImage, format string) string {
	name := strings.TrimSpace(img.Name)
	if strings.TrimSpace(img.URL) == "" {
		return name
	}
	return cloudImageURLVolumeBaseName(img, format)
}

func cloudImageURLVolumeBaseName(img osimage.OSImage, format string) string {
	name := strings.TrimSpace(img.Name)
	suffix := "." + format
	base := strings.TrimSuffix(name, suffix)

	h := sha256.New()
	_, _ = io.WriteString(h, "url="+strings.TrimSpace(img.URL))
	_, _ = io.WriteString(h, "\nchecksum="+normalizeSHA256(img.Checksum))
	_, _ = fmt.Fprintf(h, "\nsize=%d", img.SizeBytes)
	if !img.CreatedAt.IsZero() {
		_, _ = io.WriteString(h, "\ncreated="+img.CreatedAt.UTC().Format(time.RFC3339Nano))
	}
	return fmt.Sprintf("%s-%s", base, hex.EncodeToString(h.Sum(nil))[:12])
}
