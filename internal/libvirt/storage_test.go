package libvirt

import (
	"testing"
)

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
