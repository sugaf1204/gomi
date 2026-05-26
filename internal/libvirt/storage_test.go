package libvirt

import (
	"fmt"
	"testing"

	golibvirt "github.com/digitalocean/go-libvirt"
)

func TestIsNoStorageVolumeError(t *testing.T) {
	if !isNoStorageVolumeError(golibvirt.Error{Code: uint32(golibvirt.ErrNoStorageVol), Message: "no storage vol"}) {
		t.Fatal("expected ErrNoStorageVol to be classified as missing volume")
	}
	wrapped := fmt.Errorf("lookup volume: %w", golibvirt.Error{Code: uint32(golibvirt.ErrNoStorageVol), Message: "no storage vol"})
	if !isNoStorageVolumeError(wrapped) {
		t.Fatal("expected wrapped ErrNoStorageVol to be classified as missing volume")
	}
	if isNoStorageVolumeError(golibvirt.Error{Code: uint32(golibvirt.ErrRPC), Message: "rpc failed"}) {
		t.Fatal("expected non-missing libvirt error to propagate")
	}
}

func TestVolumeFileName(t *testing.T) {
	tests := []struct {
		name   string
		format string
		want   string
	}{
		{name: "vm-01", format: "", want: "vm-01.qcow2"},
		{name: "vm-01", format: "qcow2", want: "vm-01.qcow2"},
		{name: "vm-01.qcow2", format: "qcow2", want: "vm-01.qcow2"},
		{name: "vm-01-cidata", format: "raw", want: "vm-01-cidata.raw"},
		{name: "vm-01-cidata.raw", format: "raw", want: "vm-01-cidata.raw"},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/"+tt.format, func(t *testing.T) {
			if got := volumeFileName(tt.name, tt.format); got != tt.want {
				t.Fatalf("volumeFileName(%q, %q) = %q, want %q", tt.name, tt.format, got, tt.want)
			}
		})
	}
}
