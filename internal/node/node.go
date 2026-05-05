package node

import (
	"time"

	"github.com/sugaf1204/gomi/internal/resource"
)

type Node interface {
	ResourceName() string
	NodeDisplayName() string

	PrimaryMAC() string
	AllMACs() []string

	PXEInstallType() resource.InstallType
	IsProvisioningActive() bool
	ProvisionToken() string
	MarkProvisionComplete(source string, now time.Time)

	CloudInitRefForDeploy() string
	CloudInitInline(expected resource.InstallType) string

	OSImageVariantRef() string

	// Network / DHCP / netplan DRY helpers
	GetIPAssignment() resource.IPAssignmentMode
	StaticIP() string
	GetSubnetRef() string

	// Install-complete HW report
	ApplyInstallCompleteReport(report InstallCompleteReport)
}

type InstallCompleteReport struct {
	IP       string
	MAC      string
	Hostname string
}
