package vm

import (
	"time"

	"github.com/sugaf1204/gomi/internal/resource"
)

type Phase string

const (
	PhasePending      Phase = "Pending"
	PhaseCreating     Phase = "Creating"
	PhaseRunning      Phase = "Running"
	PhaseStopped      Phase = "Stopped"
	PhaseProvisioning Phase = "Provisioning"
	PhaseError        Phase = "Error"
	PhaseDeleting     Phase = "Deleting"
	PhaseMigrating    Phase = "Migrating"
)

type IPAssignmentMode = resource.IPAssignmentMode

const (
	IPAssignmentDHCP   = resource.IPAssignmentDHCP
	IPAssignmentStatic = resource.IPAssignmentStatic
)

type DiskDriver string

const (
	DiskDriverVirtio DiskDriver = "virtio"
	DiskDriverSCSI   DiskDriver = "scsi"
)

type CPUMode string

const (
	CPUModeHostPassthrough CPUMode = "host-passthrough"
	CPUModeHostModel       CPUMode = "host-model"
	CPUModeMaximum         CPUMode = "maximum"
)

type AdvancedOptions struct {
	CPUPinning    map[int]string `json:"cpuPinning,omitempty"`
	IOThreads     int            `json:"ioThreads,omitempty"`
	CPUMode       CPUMode        `json:"cpuMode,omitempty"`
	DiskDriver    DiskDriver     `json:"diskDriver,omitempty"`
	DiskFormat    string         `json:"diskFormat,omitempty"`
	NetMultiqueue int            `json:"netMultiqueue,omitempty"`
}

type PowerControlMethod string

const (
	PowerControlLibvirt PowerControlMethod = "libvirt"
)

type InstallConfigType string

const (
	InstallConfigPreseed InstallConfigType = "preseed"
	InstallConfigCurtin  InstallConfigType = "curtin"
)

type ResourceSpec struct {
	CPUCores int   `json:"cpuCores"`
	MemoryMB int64 `json:"memoryMB"`
	DiskGB   int   `json:"diskGB"`
}

type NetworkInterface struct {
	Name      string `json:"name"`
	MAC       string `json:"mac,omitempty"`
	Bridge    string `json:"bridge,omitempty"`
	Network   string `json:"network,omitempty"`
	IPAddress string `json:"ipAddress,omitempty"`
}

type InstallConfig struct {
	Type   InstallConfigType `json:"type,omitempty"`
	Inline string            `json:"inline,omitempty"`
}

// LoginUserSpec describes the OS user that receives selected SSH keys. When
// unset, keys are installed on the distribution's default cloud-init user.
type LoginUserSpec struct {
	Username string `json:"username"`
	// Password is a plaintext password. Empty means no password (key-only).
	Password string `json:"password,omitempty"`
}

type ProvisioningStatus struct {
	Active           bool       `json:"active,omitempty"`
	StartedAt        *time.Time `json:"startedAt,omitempty"`
	DeadlineAt       *time.Time `json:"deadlineAt,omitempty"`
	CompletedAt      *time.Time `json:"completedAt,omitempty"`
	CompletionToken  string     `json:"completionToken,omitempty"`
	CompletionSource string     `json:"completionSource,omitempty"`
	LastSignalAt     *time.Time `json:"lastSignalAt,omitempty"`
}

type NetworkInterfaceStatus struct {
	Name        string   `json:"name,omitempty"`
	MAC         string   `json:"mac,omitempty"`
	IPAddresses []string `json:"ipAddresses,omitempty"`
}

type VirtualMachine struct {
	Name string `json:"name"`

	// Spec fields
	HypervisorRef      string             `json:"hypervisorRef"`
	Resources          ResourceSpec       `json:"resources"`
	OSImageRef         string             `json:"osImageRef,omitempty"`
	CloudInitRef       string             `json:"cloudInitRef,omitempty"`
	CloudInitRefs      []string           `json:"cloudInitRefs,omitempty"`
	Network            []NetworkInterface `json:"network,omitempty"`
	IPAssignment       IPAssignmentMode   `json:"ipAssignment,omitempty"`
	SubnetRef          string             `json:"subnetRef,omitempty"`
	Domain             string             `json:"domain,omitempty"`
	InstallCfg         *InstallConfig     `json:"installConfig,omitempty"`
	PowerControlMethod PowerControlMethod `json:"powerControlMethod"`
	AdvancedOptions    *AdvancedOptions   `json:"advancedOptions,omitempty"`
	SSHKeyRefs         []string           `json:"sshKeyRefs,omitempty"`
	LoginUser          *LoginUserSpec     `json:"loginUser,omitempty"`

	// Status fields
	Phase                    Phase                    `json:"phase"`
	LibvirtDomain            string                   `json:"libvirtDomain,omitempty"`
	HypervisorName           string                   `json:"hypervisorName,omitempty"`
	IPAddresses              []string                 `json:"ipAddresses,omitempty"`
	NetworkInterfaces        []NetworkInterfaceStatus `json:"networkInterfaces,omitempty"`
	Provisioning             ProvisioningStatus       `json:"provisioning,omitempty"`
	LastPowerAction          string                   `json:"lastPowerAction,omitempty"`
	LastDeployedCloudInitRef string                   `json:"lastDeployedCloudInitRef,omitempty"`
	LastError                string                   `json:"lastError,omitempty"`
	CreatedOnHost            string                   `json:"createdOnHost,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
