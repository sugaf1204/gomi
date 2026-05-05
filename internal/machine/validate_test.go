package machine

import (
	"testing"

	"github.com/sugaf1204/gomi/internal/power"
)

func validMachine() Machine {
	return Machine{
		Name:     "test-01",
		Hostname: "test-01.lab", MAC: "aa:bb:cc:dd:ee:01", Arch: "amd64",
		Firmware: FirmwareUEFI,
		Power:    power.PowerConfig{Type: power.PowerTypeManual},
		OSPreset: OSPreset{Family: OSTypeUbuntu, Version: "24.04", ImageRef: "img"},
	}
}

func TestValidateMachine_Valid(t *testing.T) {
	if err := ValidateMachine(validMachine()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateMachine_ArbitraryLinuxFamily(t *testing.T) {
	m := validMachine()
	m.OSPreset.Family = "rocky"
	m.OSPreset.Version = "9"
	if err := ValidateMachine(m); err != nil {
		t.Fatalf("expected arbitrary Linux family to be accepted, got %v", err)
	}
}

func TestValidateMachine_MissingName(t *testing.T) {
	m := validMachine()
	m.Name = ""
	if err := ValidateMachine(m); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestValidateMachine_InvalidMAC(t *testing.T) {
	m := validMachine()
	m.MAC = "not-a-mac"
	if err := ValidateMachine(m); err == nil {
		t.Fatal("expected error for invalid MAC")
	}
}

func TestValidateMachine_MissingArch(t *testing.T) {
	m := validMachine()
	m.Arch = ""
	if err := ValidateMachine(m); err == nil {
		t.Fatal("expected error for missing arch")
	}
	m.Arch = "sparc"
	if err := ValidateMachine(m); err == nil {
		t.Fatal("expected error for unsupported arch")
	}
}

func TestValidateMachine_MissingOSFamily(t *testing.T) {
	m := validMachine()
	m.OSPreset.Family = ""
	if err := ValidateMachine(m); err == nil {
		t.Fatal("expected error for missing OS family")
	}
}

func TestValidateMachine_InvalidFirmware(t *testing.T) {
	m := validMachine()
	m.Firmware = "arm"
	if err := ValidateMachine(m); err == nil {
		t.Fatal("expected error for unsupported firmware")
	}
}

func TestValidateMachine_TargetDiskMustBeWholeDisk(t *testing.T) {
	m := validMachine()
	m.TargetDisk = "/dev/disk/by-id/nvme-GOMI_TEST"
	if err := ValidateMachine(m); err != nil {
		t.Fatalf("expected whole disk targetDisk to be accepted, got %v", err)
	}
	m.TargetDisk = "/dev/disk/by-id/nvme-GOMI_TEST-part1"
	if err := ValidateMachine(m); err == nil {
		t.Fatal("expected partition targetDisk to be rejected")
	}
}

func TestValidateMachine_InvalidPowerType(t *testing.T) {
	m := validMachine()
	m.Power = power.PowerConfig{Type: "invalid"}
	if err := ValidateMachine(m); err == nil {
		t.Fatal("expected error for invalid power type")
	}
}

func TestValidateMachine_LegacyCloudInitRefAccepted(t *testing.T) {
	m := validMachine()
	m.CloudInitRef = "ci-legacy"
	if err := ValidateMachine(m); err != nil {
		t.Fatalf("expected no error for legacy cloudInitRef, got %v", err)
	}
}

func TestValidateMachine_DuplicateCloudInitRefs(t *testing.T) {
	m := validMachine()
	m.CloudInitRefs = []string{"ci-01", "ci-01"}
	if err := ValidateMachine(m); err == nil {
		t.Fatal("expected error for duplicate cloudInitRefs")
	}
}

func TestValidateMachine_DuplicateLegacyAndCloudInitRefs(t *testing.T) {
	m := validMachine()
	m.CloudInitRef = "ci-legacy"
	m.CloudInitRefs = []string{"ci-legacy", "ci-02"}
	if err := ValidateMachine(m); err == nil {
		t.Fatal("expected error for duplicate cloudInitRef and cloudInitRefs")
	}
}
