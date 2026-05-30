package api

import (
	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/infra/dns"
	"github.com/sugaf1204/gomi/internal/pxe"
)

type ListMachinesResponse struct {
	Machines      []MachineResponse `json:"machines"`
	NextPageToken string            `json:"nextPageToken,omitempty"`
	TotalSize     int               `json:"totalSize"`
}

type ListVirtualMachinesResponse struct {
	VirtualMachines []VirtualMachineResponse `json:"virtualMachines"`
	NextPageToken   string                   `json:"nextPageToken,omitempty"`
	TotalSize       int                      `json:"totalSize"`
}

type ListHypervisorsResponse struct {
	Hypervisors   []HypervisorResponse `json:"hypervisors"`
	NextPageToken string               `json:"nextPageToken,omitempty"`
	TotalSize     int                  `json:"totalSize"`
}

type ListSubnetsResponse struct {
	Subnets       []SubnetResponse `json:"subnets"`
	NextPageToken string           `json:"nextPageToken,omitempty"`
	TotalSize     int              `json:"totalSize"`
}

type ListSSHKeysResponse struct {
	SSHKeys       []SSHKeyResponse `json:"sshKeys"`
	NextPageToken string           `json:"nextPageToken,omitempty"`
	TotalSize     int              `json:"totalSize"`
}

type ListCloudInitTemplatesResponse struct {
	CloudInitTemplates []CloudInitTemplateResponse `json:"cloudInitTemplates"`
	NextPageToken      string                      `json:"nextPageToken,omitempty"`
	TotalSize          int                         `json:"totalSize"`
}

type ListOSImagesResponse struct {
	OSImages      []OSImageResponse `json:"osImages"`
	NextPageToken string            `json:"nextPageToken,omitempty"`
	TotalSize     int               `json:"totalSize"`
}

type ListBootEnvironmentsResponse struct {
	BootEnvironments []BootEnvironmentResponse `json:"bootEnvironments"`
	NextPageToken    string                    `json:"nextPageToken,omitempty"`
	TotalSize        int                       `json:"totalSize"`
}

type ListDHCPLeasesResponse struct {
	DHCPLeases    []pxe.DHCPLease `json:"dhcpLeases"`
	NextPageToken string          `json:"nextPageToken,omitempty"`
	TotalSize     int             `json:"totalSize"`
}

type ListAuditEventsResponse struct {
	AuditEvents   []auth.AuditEvent `json:"auditEvents"`
	NextPageToken string            `json:"nextPageToken,omitempty"`
	TotalSize     int               `json:"totalSize"`
}

type ListDNSRecordsResponse struct {
	DNSRecords    []dns.DynamicRecord `json:"dnsRecords"`
	NextPageToken string              `json:"nextPageToken,omitempty"`
	TotalSize     int                 `json:"totalSize"`
}
