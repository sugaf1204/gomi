package libvirt

import (
	"errors"

	golibvirt "github.com/digitalocean/go-libvirt"
)

// IsDomainNotFoundError reports whether err (possibly wrapped) is the libvirt
// "no domain with matching name" error. It lets callers distinguish a domain
// that was removed on the host from connection or query failures.
func IsDomainNotFoundError(err error) bool {
	var libvirtErr golibvirt.Error
	return errors.As(err, &libvirtErr) && libvirtErr.Code == uint32(golibvirt.ErrNoDomain)
}

// IsVolumeNotFoundError reports whether err (possibly wrapped) is the libvirt
// "storage volume not found" error.
func IsVolumeNotFoundError(err error) bool {
	var libvirtErr golibvirt.Error
	return errors.As(err, &libvirtErr) && libvirtErr.Code == uint32(golibvirt.ErrNoStorageVol)
}
