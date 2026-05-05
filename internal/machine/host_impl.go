package machine

import (
	"strings"
	"time"

	"github.com/sugaf1204/gomi/internal/node"
	"github.com/sugaf1204/gomi/internal/resource"
)

var _ node.Node = (*Machine)(nil)

func (m *Machine) ResourceName() string {
	return m.Name
}

func (m *Machine) NodeDisplayName() string {
	if h := strings.TrimSpace(m.Hostname); h != "" {
		return h
	}
	return m.Name
}

func (m *Machine) PrimaryMAC() string {
	return strings.ToLower(strings.TrimSpace(m.MAC))
}

func (m *Machine) AllMACs() []string {
	mac := m.PrimaryMAC()
	if mac == "" {
		return nil
	}
	return []string{mac}
}

func (m *Machine) PXEInstallType() resource.InstallType {
	return resource.InstallCurtin
}

func (m *Machine) IsProvisioningActive() bool {
	return m.Provision != nil && m.Provision.Active
}

func (m *Machine) ProvisionToken() string {
	if m.Provision == nil {
		return ""
	}
	return strings.TrimSpace(m.Provision.CompletionToken)
}

func (m *Machine) MarkProvisionComplete(source string, now time.Time) {
	if m.Provision == nil {
		m.Provision = &ProvisionProgress{}
	}
	m.Provision.Active = false
	m.Provision.CompletedAt = &now
	m.Provision.LastSignalAt = &now
	m.Provision.CompletionSource = source
	m.Provision.Message = "provisioning completed"
	m.Phase = PhaseReady
	m.LastError = ""
	m.UpdatedAt = now
}

func (m *Machine) CloudInitRefForDeploy() string {
	return resource.ResolveCloudInitRef(m.LastDeployedCloudInitRef, m.CloudInitRef, m.CloudInitRefs)
}

func (m *Machine) CloudInitInline(_ resource.InstallType) string {
	return ""
}

func (m *Machine) OSImageVariantRef() string {
	return m.OSPreset.ImageRef
}

func (m *Machine) GetIPAssignment() resource.IPAssignmentMode { return m.IPAssignment }
func (m *Machine) StaticIP() string                           { return m.IP }
func (m *Machine) GetSubnetRef() string                       { return m.SubnetRef }

func (m *Machine) ApplyInstallCompleteReport(r node.InstallCompleteReport) {
	if r.IP != "" && m.IPAssignment != IPAssignmentModeStatic {
		m.IP = r.IP
	}
}
