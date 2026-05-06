package machine

import (
	"encoding/json"
	"time"

	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
)

type Firmware string

const (
	FirmwareUEFI Firmware = "uefi"
	FirmwareBIOS Firmware = "bios"
)

type OSType string

const (
	OSTypeUbuntu OSType = "ubuntu"
	OSTypeDebian OSType = "debian"
)

type Phase string

const (
	PhaseDiscovered   Phase = "Discovered"
	PhaseReady        Phase = "Ready"
	PhaseProvisioning Phase = "Provisioning"
	PhaseError        Phase = "Error"
)

type Role string

const (
	RoleDefault    Role = ""
	RoleHypervisor Role = "hypervisor"
)

type IPAssignmentMode = resource.IPAssignmentMode

const (
	IPAssignmentModeDHCP   = resource.IPAssignmentDHCP
	IPAssignmentModeStatic = resource.IPAssignmentStatic
)

type OSPreset struct {
	Family        OSType            `json:"family"`
	Version       string            `json:"version"`
	ImageRef      string            `json:"imageRef"`
	InstallConfig map[string]string `json:"installConfig,omitempty"`
}

type NetworkConfig struct {
	Domain string `json:"domain"`
}

// LoginUserSpec describes the OS user that receives selected SSH keys. When
// unset, keys are installed on the distribution's default cloud-init user.
type LoginUserSpec struct {
	Username string `json:"username"`
	// Password is a plaintext password. Empty means no password (key-only).
	Password string `json:"password,omitempty"`
}

type ProvisionProgress struct {
	Active           bool              `json:"active,omitempty"`
	AttemptID        string            `json:"attemptId,omitempty"`
	InventoryID      string            `json:"inventoryId,omitempty"`
	StartedAt        *time.Time        `json:"startedAt,omitempty"`
	DeadlineAt       *time.Time        `json:"deadlineAt,omitempty"`
	FinishedAt       *time.Time        `json:"finishedAt,omitempty"`
	CompletedAt      *time.Time        `json:"completedAt,omitempty"`
	Trigger          string            `json:"trigger,omitempty"`
	RequestedBy      string            `json:"requestedBy,omitempty"`
	Message          string            `json:"message,omitempty"`
	Artifacts        map[string]string `json:"artifacts,omitempty"`
	CompletionToken  string            `json:"completionToken,omitempty"`
	CompletionSource string            `json:"completionSource,omitempty"`
	LastSignalAt     *time.Time        `json:"lastSignalAt,omitempty"`
	CurtinConfig     json.RawMessage   `json:"curtinConfig,omitempty"`
	FailureReason    string            `json:"failureReason,omitempty"`
	LogURL           string            `json:"logUrl,omitempty"`
}

const (
	ProvisionArtifactHypervisorRegistrationToken          = "hypervisorRegistrationToken"
	ProvisionArtifactHypervisorRegistrationTokenExpiresAt = "hypervisorRegistrationTokenExpiresAt"
)

type Machine struct {
	Name string `json:"name"`

	// Spec fields
	Hostname      string            `json:"hostname"`
	MAC           string            `json:"mac"`
	IP            string            `json:"ip,omitempty"`
	Arch          string            `json:"arch"`
	Firmware      Firmware          `json:"firmware"`
	Power         power.PowerConfig `json:"power"`
	Network       NetworkConfig     `json:"network"`
	OSPreset      OSPreset          `json:"osPreset"`
	TargetDisk    string            `json:"targetDisk,omitempty"`
	CloudInitRef  string            `json:"cloudInitRef,omitempty"`
	CloudInitRefs []string          `json:"cloudInitRefs,omitempty"`
	IPAssignment  IPAssignmentMode  `json:"ipAssignment,omitempty"`
	SubnetRef     string            `json:"subnetRef,omitempty"`
	Role          Role              `json:"role,omitempty"`
	BridgeName    string            `json:"bridgeName,omitempty"`
	SSHKeyRefs    []string          `json:"sshKeyRefs,omitempty"`
	LoginUser     *LoginUserSpec    `json:"loginUser,omitempty"`

	// Status fields
	Phase                    Phase              `json:"phase"`
	Provision                *ProvisionProgress `json:"provision,omitempty"`
	LastPowerAction          string             `json:"lastPowerAction,omitempty"`
	LastDeployedCloudInitRef string             `json:"lastDeployedCloudInitRef,omitempty"`
	LastError                string             `json:"lastError,omitempty"`
	PowerState               power.PowerState   `json:"powerState,omitempty"`
	PowerStateAt             *time.Time         `json:"powerStateAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
