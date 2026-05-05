package inventory

import (
	"encoding/json"
	"testing"
)

func TestHardwareInventoryJSONShape(t *testing.T) {
	in := HardwareInventory{
		AttemptID: "attempt-1",
		Runtime:   RuntimeInfo{KernelVersion: "6.8.0"},
		Boot:      BootInfo{FirmwareMode: "uefi", EFIVars: true},
		Disks: []DiskInfo{{
			Name:      "vda",
			Path:      "/dev/vda",
			SizeMB:    1024,
			Type:      "disk",
			Removable: false,
			ByID:      []string{"/dev/disk/by-id/virtio-root"},
		}},
		NICs: []NICInfo{{
			Name:  "eth0",
			MAC:   "52:54:00:00:00:01",
			State: "up",
		}},
	}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out HardwareInventory
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.AttemptID != in.AttemptID || out.Runtime.KernelVersion != in.Runtime.KernelVersion {
		t.Fatalf("inventory did not round-trip: %+v", out)
	}
	if len(out.Disks) != 1 || out.Disks[0].ByID[0] != "/dev/disk/by-id/virtio-root" {
		t.Fatalf("disk inventory did not round-trip: %+v", out.Disks)
	}
	if len(out.NICs) != 1 || out.NICs[0].MAC != "52:54:00:00:00:01" {
		t.Fatalf("nic inventory did not round-trip: %+v", out.NICs)
	}
}
