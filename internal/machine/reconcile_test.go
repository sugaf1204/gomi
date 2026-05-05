package machine

import (
	"errors"
	"testing"

	"github.com/sugaf1204/gomi/internal/power"
)

func TestSyncState_Success(t *testing.T) {
	m := Machine{
		Name:     "r-01",
		Hostname: "r-01.lab", MAC: "aa:bb:cc:dd:ee:01", Arch: "amd64",
		Firmware: FirmwareUEFI, Power: power.PowerConfig{Type: power.PowerTypeManual},
		IP:      "10.0.0.5",
		Network: NetworkConfig{Domain: "lab.local"},
		Phase:   PhaseProvisioning,
		Provision: &ProvisionProgress{
			Message: "building",
			Artifacts: map[string]string{
				ProvisionArtifactHypervisorRegistrationToken: "registration-token",
			},
		},
	}
	result := SyncState(m, map[string]string{"kernel": "/boot/vmlinuz"}, "curtin cfg", nil)
	if !result.NeedsSave {
		t.Fatal("expected NeedsSave=true")
	}
	if !result.NeedsDNS {
		t.Fatal("expected NeedsDNS=true when IP and domain are set")
	}
	if result.Machine.Phase != PhaseProvisioning {
		t.Fatalf("expected phase Provisioning (waiting for PXE completion), got %s", result.Machine.Phase)
	}
	if got := result.Machine.Provision.Artifacts[ProvisionArtifactHypervisorRegistrationToken]; got != "registration-token" {
		t.Fatalf("expected existing registration token artifact to be preserved, got %q", got)
	}
	if got := result.Machine.Provision.Artifacts["kernel"]; got != "/boot/vmlinuz" {
		t.Fatalf("expected generated artifact to be retained, got %q", got)
	}
}

func TestSyncState_Error(t *testing.T) {
	m := Machine{
		Name:      "r-02",
		Phase:     PhaseProvisioning,
		Provision: &ProvisionProgress{Message: "building"},
	}
	result := SyncState(m, nil, "", errors.New("build failed"))
	if !result.NeedsSave {
		t.Fatal("expected NeedsSave=true")
	}
	if result.Machine.Phase != PhaseError {
		t.Fatalf("expected phase Error, got %s", result.Machine.Phase)
	}
	if result.Machine.LastError != "build failed" {
		t.Fatalf("expected LastError 'build failed', got %s", result.Machine.LastError)
	}
}

func TestSyncState_NonProvisioningSkip(t *testing.T) {
	m := Machine{
		Name:  "r-03",
		Phase: PhaseReady,
	}
	result := SyncState(m, nil, "", nil)
	if result.NeedsSave {
		t.Fatal("expected NeedsSave=false for non-provisioning machine")
	}
	if result.NeedsDNS {
		t.Fatal("expected NeedsDNS=false for non-provisioning machine")
	}
}
