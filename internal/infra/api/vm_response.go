package api

import (
	"strings"
	"time"

	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
)

type VirtualMachineResponse struct {
	Name               string                    `json:"name"`
	VirtualMachineID   string                    `json:"virtualMachineId"`
	HypervisorRef      string                    `json:"hypervisorRef,omitempty"`
	Resources          vm.ResourceSpec           `json:"resources"`
	OSImageRef         string                    `json:"osImageRef,omitempty"`
	CloudInitRef       string                    `json:"cloudInitRef,omitempty"`
	CloudInitRefs      []string                  `json:"cloudInitRefs,omitempty"`
	Network            []vm.NetworkInterface     `json:"network,omitempty"`
	IPAssignment       resource.IPAssignmentMode `json:"ipAssignment,omitempty"`
	SubnetRef          string                    `json:"subnetRef,omitempty"`
	Domain             string                    `json:"domain,omitempty"`
	InstallConfig      *vm.InstallConfig         `json:"installConfig,omitempty"`
	PowerControlMethod vm.PowerControlMethod     `json:"powerControlMethod"`
	AdvancedOptions    *vm.AdvancedOptions       `json:"advancedOptions,omitempty"`
	SSHKeyRefs         []string                  `json:"sshKeyRefs,omitempty"`
	LoginUser          *LoginUserResponse        `json:"loginUser,omitempty"`

	Phase                    vm.Phase                    `json:"phase"`
	LibvirtDomain            string                      `json:"libvirtDomain,omitempty"`
	HypervisorName           string                      `json:"hypervisorName,omitempty"`
	IPAddresses              []string                    `json:"ipAddresses,omitempty"`
	NetworkInterfaces        []vm.NetworkInterfaceStatus `json:"networkInterfaces,omitempty"`
	Provisioning             ProvisioningStatusResponse  `json:"provisioning,omitempty"`
	LastPowerAction          string                      `json:"lastPowerAction,omitempty"`
	LastDeployedCloudInitRef string                      `json:"lastDeployedCloudInitRef,omitempty"`
	LastError                string                      `json:"lastError,omitempty"`
	CreatedOnHost            string                      `json:"createdOnHost,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ProvisioningStatusResponse struct {
	Active           bool       `json:"active,omitempty"`
	StartedAt        *time.Time `json:"startedAt,omitempty"`
	DeadlineAt       *time.Time `json:"deadlineAt,omitempty"`
	CompletedAt      *time.Time `json:"completedAt,omitempty"`
	CompletionSource string     `json:"completionSource,omitempty"`
	LastSignalAt     *time.Time `json:"lastSignalAt,omitempty"`
}

func virtualMachineResponses(items []vm.VirtualMachine) []VirtualMachineResponse {
	out := make([]VirtualMachineResponse, 0, len(items))
	for _, item := range items {
		out = append(out, virtualMachineResponse(item))
	}
	return out
}

func virtualMachineResponse(v vm.VirtualMachine) VirtualMachineResponse {
	return VirtualMachineResponse{
		Name:                     resourceName("virtualMachines", v.Name),
		VirtualMachineID:         v.Name,
		HypervisorRef:            resourceName("hypervisors", v.HypervisorRef),
		Resources:                v.Resources,
		OSImageRef:               resourceName("osImages", v.OSImageRef),
		CloudInitRef:             resourceName("cloudInitTemplates", v.CloudInitRef),
		CloudInitRefs:            resourceNames("cloudInitTemplates", v.CloudInitRefs),
		Network:                  v.Network,
		IPAssignment:             v.IPAssignment,
		SubnetRef:                resourceName("subnets", v.SubnetRef),
		Domain:                   v.Domain,
		InstallConfig:            v.InstallCfg,
		PowerControlMethod:       v.PowerControlMethod,
		AdvancedOptions:          v.AdvancedOptions,
		SSHKeyRefs:               resourceNames("sshKeys", v.SSHKeyRefs),
		LoginUser:                vmLoginUserResponse(v.LoginUser),
		Phase:                    v.Phase,
		LibvirtDomain:            v.LibvirtDomain,
		HypervisorName:           v.HypervisorName,
		IPAddresses:              v.IPAddresses,
		NetworkInterfaces:        v.NetworkInterfaces,
		Provisioning:             provisioningStatusResponse(v.Provisioning),
		LastPowerAction:          v.LastPowerAction,
		LastDeployedCloudInitRef: resourceName("cloudInitTemplates", v.LastDeployedCloudInitRef),
		LastError:                v.LastError,
		CreatedOnHost:            v.CreatedOnHost,
		CreatedAt:                v.CreatedAt,
		UpdatedAt:                v.UpdatedAt,
	}
}

func provisioningStatusResponse(status vm.ProvisioningStatus) ProvisioningStatusResponse {
	return ProvisioningStatusResponse{
		Active:           status.Active,
		StartedAt:        status.StartedAt,
		DeadlineAt:       status.DeadlineAt,
		CompletedAt:      status.CompletedAt,
		CompletionSource: status.CompletionSource,
		LastSignalAt:     status.LastSignalAt,
	}
}

func vmLoginUserResponse(user *vm.LoginUserSpec) *LoginUserResponse {
	if user == nil || strings.TrimSpace(user.Username) == "" {
		return nil
	}
	return &LoginUserResponse{
		Username:           user.Username,
		PasswordConfigured: strings.TrimSpace(user.Password) != "",
	}
}
