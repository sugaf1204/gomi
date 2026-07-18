package libvirt

import (
	"errors"
	"fmt"
	"testing"

	golibvirt "github.com/digitalocean/go-libvirt"
)

func TestIsDomainNotFoundError(t *testing.T) {
	if !IsDomainNotFoundError(golibvirt.Error{Code: uint32(golibvirt.ErrNoDomain), Message: "no domain"}) {
		t.Fatal("expected ErrNoDomain to be classified as missing domain")
	}
	wrapped := fmt.Errorf("domain info vm-01: %w", fmt.Errorf("lookup domain vm-01: %w", golibvirt.Error{Code: uint32(golibvirt.ErrNoDomain), Message: "no domain"}))
	if !IsDomainNotFoundError(wrapped) {
		t.Fatal("expected wrapped ErrNoDomain to be classified as missing domain")
	}
	if IsDomainNotFoundError(golibvirt.Error{Code: uint32(golibvirt.ErrRPC), Message: "rpc failed"}) {
		t.Fatal("expected non-missing libvirt error to propagate")
	}
	if IsDomainNotFoundError(errors.New("domain not found")) {
		t.Fatal("expected plain error text to not be classified as missing domain")
	}
}

func TestIsVolumeNotFoundError(t *testing.T) {
	if !IsVolumeNotFoundError(golibvirt.Error{Code: uint32(golibvirt.ErrNoStorageVol), Message: "no storage vol"}) {
		t.Fatal("expected ErrNoStorageVol to be classified as missing volume")
	}
	wrapped := fmt.Errorf("lookup volume: %w", golibvirt.Error{Code: uint32(golibvirt.ErrNoStorageVol), Message: "no storage vol"})
	if !IsVolumeNotFoundError(wrapped) {
		t.Fatal("expected wrapped ErrNoStorageVol to be classified as missing volume")
	}
	if IsVolumeNotFoundError(golibvirt.Error{Code: uint32(golibvirt.ErrRPC), Message: "rpc failed"}) {
		t.Fatal("expected non-missing libvirt error to propagate")
	}
}
