package libvirt

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	golibvirt "github.com/digitalocean/go-libvirt"
)

func (e *rpcExecutor) CreateVolume(_ context.Context, name string, sizeGB int, format string) error {
	pool, err := e.l.StoragePoolLookupByName("default")
	if err != nil {
		return fmt.Errorf("lookup storage pool 'default': %w", err)
	}

	switch format {
	case "qcow2", "raw":
	default:
		return fmt.Errorf("unsupported disk format: %s (must be qcow2 or raw)", format)
	}
	sizeBytes := int64(sizeGB) * 1024 * 1024 * 1024

	volXML := fmt.Sprintf(`<volume type='file'>
  <name>%s</name>
  <capacity unit='bytes'>%d</capacity>
  <target>
    <format type='%s'/>
  </target>
</volume>`, xmlEscape(volumeFileName(name, format)), sizeBytes, xmlEscape(format))

	_, err = e.l.StorageVolCreateXML(pool, volXML, 0)
	if err != nil {
		return fmt.Errorf("create volume %s: %w", name, err)
	}
	return nil
}

func (e *rpcExecutor) CreateOverlayVolume(_ context.Context, name string, sizeGB int, backingPath string, backingFormat string) error {
	pool, err := e.l.StoragePoolLookupByName("default")
	if err != nil {
		return fmt.Errorf("lookup storage pool 'default': %w", err)
	}

	if backingFormat == "" {
		backingFormat = "qcow2"
	}
	sizeBytes := int64(sizeGB) * 1024 * 1024 * 1024

	volXML := fmt.Sprintf(`<volume type='file'>
  <name>%s.qcow2</name>
  <capacity unit='bytes'>%d</capacity>
  <target>
    <format type='qcow2'/>
  </target>
  <backingStore>
    <path>%s</path>
    <format type='%s'/>
  </backingStore>
</volume>`, name, sizeBytes, backingPath, backingFormat)

	_, err = e.l.StorageVolCreateXML(pool, volXML, 0)
	if err != nil {
		return fmt.Errorf("create overlay volume %s: %w", name, err)
	}
	return nil
}

func (e *rpcExecutor) VolumeExists(_ context.Context, name string, format string) (bool, error) {
	pool, err := e.l.StoragePoolLookupByName("default")
	if err != nil {
		return false, fmt.Errorf("lookup storage pool 'default': %w", err)
	}
	if format == "" {
		format = "qcow2"
	}
	_, err = e.l.StorageVolLookupByName(pool, volumeFileName(name, format))
	if err != nil {
		if isNoStorageVolumeError(err) {
			return false, nil
		}
		return false, fmt.Errorf("lookup storage volume %s: %w", volumeFileName(name, format), err)
	}
	return true, nil
}

func (e *rpcExecutor) CreateVolumeFromReader(ctx context.Context, name string, sizeBytes int64, format string, r io.Reader) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	pool, err := e.l.StoragePoolLookupByName("default")
	if err != nil {
		return fmt.Errorf("lookup storage pool 'default': %w", err)
	}
	switch format {
	case "qcow2", "raw":
	default:
		return fmt.Errorf("unsupported disk format: %s (must be qcow2 or raw)", format)
	}
	if sizeBytes <= 0 {
		return fmt.Errorf("volume size must be positive")
	}

	volName := volumeFileName(name, format)
	volXML := fmt.Sprintf(`<volume type='file'>
  <name>%s</name>
  <capacity unit='bytes'>%d</capacity>
  <target>
    <format type='%s'/>
  </target>
</volume>`, xmlEscape(volName), sizeBytes, xmlEscape(format))

	vol, err := e.l.StorageVolCreateXML(pool, volXML, 0)
	if err != nil {
		return fmt.Errorf("create volume %s: %w", volName, err)
	}
	if err := e.l.StorageVolUpload(vol, r, 0, 0, 0); err != nil {
		_ = e.l.StorageVolDelete(vol, 0)
		return fmt.Errorf("upload volume %s: %w", volName, err)
	}
	return nil
}

func (e *rpcExecutor) DeleteVolume(_ context.Context, name string) error {
	pool, err := e.l.StoragePoolLookupByName("default")
	if err != nil {
		return fmt.Errorf("lookup storage pool 'default': %w", err)
	}

	candidates := []string{name + ".qcow2", name + ".img", name + ".raw", name + "-cidata.raw"}
	for _, volName := range candidates {
		vol, err := e.l.StorageVolLookupByName(pool, volName)
		if err != nil {
			continue
		}
		if err := e.l.StorageVolDelete(vol, 0); err != nil {
			return fmt.Errorf("delete volume %s: %w", volName, err)
		}
	}
	return nil
}

func volumeFileName(name string, format string) string {
	if format == "" {
		format = "qcow2"
	}
	suffix := "." + format
	if strings.HasSuffix(name, suffix) {
		return name
	}
	return name + suffix
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

func isNoStorageVolumeError(err error) bool {
	var libvirtErr golibvirt.Error
	return errors.As(err, &libvirtErr) && libvirtErr.Code == uint32(golibvirt.ErrNoStorageVol)
}
