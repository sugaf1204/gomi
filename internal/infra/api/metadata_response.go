package api

import (
	"time"

	"github.com/sugaf1204/gomi/internal/bootenv"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/sshkey"
	"github.com/sugaf1204/gomi/internal/subnet"
)

type SubnetResponse struct {
	Name      string            `json:"name"`
	SubnetID  string            `json:"subnetId"`
	Spec      subnet.SubnetSpec `json:"spec"`
	CreatedAt time.Time         `json:"createdAt"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

type SSHKeyResponse struct {
	Name      string    `json:"name"`
	SSHKeyID  string    `json:"sshKeyId"`
	PublicKey string    `json:"publicKey"`
	Comment   string    `json:"comment,omitempty"`
	KeyType   string    `json:"keyType,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type CloudInitTemplateResponse struct {
	Name                string    `json:"name"`
	CloudInitTemplateID string    `json:"cloudInitTemplateId"`
	UserData            string    `json:"userData"`
	NetworkConfig       string    `json:"networkConfig,omitempty"`
	MetadataTemplate    string    `json:"metadataTemplate,omitempty"`
	Description         string    `json:"description,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type HypervisorResponse struct {
	Name          string                    `json:"name"`
	HypervisorID  string                    `json:"hypervisorId"`
	Connection    hypervisor.ConnectionSpec `json:"connection"`
	Labels        map[string]string         `json:"labels,omitempty"`
	MachineRef    string                    `json:"machineRef,omitempty"`
	BridgeName    string                    `json:"bridgeName,omitempty"`
	Phase         hypervisor.Phase          `json:"phase"`
	Capacity      *hypervisor.ResourceInfo  `json:"capacity,omitempty"`
	Used          *hypervisor.ResourceUsage `json:"used,omitempty"`
	VMCount       int                       `json:"vmCount"`
	LibvirtURI    string                    `json:"libvirtURI,omitempty"`
	LastHeartbeat *time.Time                `json:"lastHeartbeat,omitempty"`
	LastError     string                    `json:"lastError,omitempty"`
	CreatedAt     time.Time                 `json:"createdAt"`
	UpdatedAt     time.Time                 `json:"updatedAt"`
}

type BootEnvironmentResponse struct {
	Name              string        `json:"name"`
	BootEnvironmentID string        `json:"bootEnvironmentId"`
	Phase             bootenv.Phase `json:"phase"`
	Message           string        `json:"message,omitempty"`
	ArtifactDir       string        `json:"artifactDir,omitempty"`
	LogPath           string        `json:"logPath,omitempty"`
	KernelPath        string        `json:"kernelPath,omitempty"`
	InitrdPath        string        `json:"initrdPath,omitempty"`
	RootfsPath        string        `json:"rootfsPath,omitempty"`
	UpdatedAt         time.Time     `json:"updatedAt"`
}

func subnetResponses(items []subnet.Subnet) []SubnetResponse {
	out := make([]SubnetResponse, 0, len(items))
	for _, item := range items {
		out = append(out, subnetResponse(item))
	}
	return out
}

func subnetResponse(item subnet.Subnet) SubnetResponse {
	return SubnetResponse{
		Name:      resourceName("subnets", item.Name),
		SubnetID:  item.Name,
		Spec:      item.Spec,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

func sshKeyResponses(items []sshkey.SSHKey) []SSHKeyResponse {
	out := make([]SSHKeyResponse, 0, len(items))
	for _, item := range items {
		out = append(out, sshKeyResponse(item))
	}
	return out
}

func sshKeyResponse(item sshkey.SSHKey) SSHKeyResponse {
	return SSHKeyResponse{
		Name:      resourceName("sshKeys", item.Name),
		SSHKeyID:  item.Name,
		PublicKey: item.PublicKey,
		Comment:   item.Comment,
		KeyType:   item.KeyType,
		CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt,
	}
}

func cloudInitTemplateResponses(items []cloudinit.CloudInitTemplate) []CloudInitTemplateResponse {
	out := make([]CloudInitTemplateResponse, 0, len(items))
	for _, item := range items {
		out = append(out, cloudInitTemplateResponse(item))
	}
	return out
}

func cloudInitTemplateResponse(item cloudinit.CloudInitTemplate) CloudInitTemplateResponse {
	return CloudInitTemplateResponse{
		Name:                resourceName("cloudInitTemplates", item.Name),
		CloudInitTemplateID: item.Name,
		UserData:            item.UserData,
		NetworkConfig:       item.NetworkConfig,
		MetadataTemplate:    item.MetadataTemplate,
		Description:         item.Description,
		CreatedAt:           item.CreatedAt,
		UpdatedAt:           item.UpdatedAt,
	}
}

func hypervisorResponses(items []hypervisor.Hypervisor) []HypervisorResponse {
	out := make([]HypervisorResponse, 0, len(items))
	for _, item := range items {
		out = append(out, hypervisorResponse(item))
	}
	return out
}

func hypervisorResponse(item hypervisor.Hypervisor) HypervisorResponse {
	conn := item.Connection
	conn.KeyRef = resourceName("sshKeys", conn.KeyRef)
	return HypervisorResponse{
		Name:          resourceName("hypervisors", item.Name),
		HypervisorID:  item.Name,
		Connection:    conn,
		Labels:        item.Labels,
		MachineRef:    resourceName("machines", item.MachineRef),
		BridgeName:    item.BridgeName,
		Phase:         item.Phase,
		Capacity:      item.Capacity,
		Used:          item.Used,
		VMCount:       item.VMCount,
		LibvirtURI:    item.LibvirtURI,
		LastHeartbeat: item.LastHeartbeat,
		LastError:     item.LastError,
		CreatedAt:     item.CreatedAt,
		UpdatedAt:     item.UpdatedAt,
	}
}

func bootEnvironmentResponses(items []bootenv.Status) []BootEnvironmentResponse {
	out := make([]BootEnvironmentResponse, 0, len(items))
	for _, item := range items {
		out = append(out, bootEnvironmentResponse(item))
	}
	return out
}

func bootEnvironmentResponse(item bootenv.Status) BootEnvironmentResponse {
	return BootEnvironmentResponse{
		Name:              resourceName("bootEnvironments", item.Name),
		BootEnvironmentID: item.Name,
		Phase:             item.Phase,
		Message:           item.Message,
		ArtifactDir:       item.ArtifactDir,
		LogPath:           item.LogPath,
		KernelPath:        item.KernelPath,
		InitrdPath:        item.InitrdPath,
		RootfsPath:        item.RootFSPath,
		UpdatedAt:         item.UpdatedAt,
	}
}
