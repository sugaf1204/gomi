package vm

import (
	"strings"
	"time"

	"github.com/sugaf1204/gomi/internal/node"
	"github.com/sugaf1204/gomi/internal/resource"
)

var _ node.Node = (*VirtualMachine)(nil)

func (v *VirtualMachine) ResourceName() string {
	return v.Name
}

func (v *VirtualMachine) NodeDisplayName() string {
	return v.Name
}

func (v *VirtualMachine) PrimaryMAC() string {
	for _, nic := range v.Network {
		if mac := strings.ToLower(strings.TrimSpace(nic.MAC)); mac != "" {
			return mac
		}
	}
	for _, nic := range v.NetworkInterfaces {
		if mac := strings.ToLower(strings.TrimSpace(nic.MAC)); mac != "" {
			return mac
		}
	}
	return ""
}

func (v *VirtualMachine) AllMACs() []string {
	seen := map[string]struct{}{}
	var macs []string
	add := func(raw string) {
		mac := strings.ToLower(strings.TrimSpace(raw))
		if mac == "" {
			return
		}
		if _, exists := seen[mac]; exists {
			return
		}
		seen[mac] = struct{}{}
		macs = append(macs, mac)
	}
	for _, nic := range v.Network {
		add(nic.MAC)
	}
	for _, nic := range v.NetworkInterfaces {
		add(nic.MAC)
	}
	return macs
}

func (v *VirtualMachine) PXEInstallType() resource.InstallType {
	if v.InstallCfg == nil {
		return resource.InstallPreseed
	}
	switch v.InstallCfg.Type {
	case InstallConfigCurtin:
		return resource.InstallCurtin
	case InstallConfigPreseed:
		return resource.InstallPreseed
	default:
		return resource.InstallPreseed
	}
}

func (v *VirtualMachine) IsProvisioningActive() bool {
	return v.Provisioning.Active
}

func (v *VirtualMachine) ProvisionToken() string {
	return strings.TrimSpace(v.Provisioning.CompletionToken)
}

func (v *VirtualMachine) MarkProvisionComplete(source string, now time.Time) {
	v.Provisioning.Active = false
	v.Provisioning.CompletedAt = &now
	v.Provisioning.LastSignalAt = &now
	v.Provisioning.CompletionSource = source
	v.Phase = PhaseRunning
	v.LastError = ""
	v.UpdatedAt = now
}

func (v *VirtualMachine) CloudInitRefForDeploy() string {
	return resource.ResolveCloudInitRef(v.LastDeployedCloudInitRef, v.CloudInitRef, v.CloudInitRefs)
}

func (v *VirtualMachine) CloudInitInline(expected resource.InstallType) string {
	if v.InstallCfg == nil {
		return ""
	}
	cfgType := resource.InstallType(v.InstallCfg.Type)
	if cfgType != expected {
		return ""
	}
	return strings.TrimSpace(v.InstallCfg.Inline)
}

func (v *VirtualMachine) OSImageVariantRef() string {
	return v.OSImageRef
}

func (v *VirtualMachine) GetIPAssignment() resource.IPAssignmentMode { return v.IPAssignment }
func (v *VirtualMachine) StaticIP() string {
	if len(v.Network) > 0 {
		return v.Network[0].IPAddress
	}
	return ""
}
func (v *VirtualMachine) GetSubnetRef() string { return v.SubnetRef }

func (v *VirtualMachine) ApplyInstallCompleteReport(r node.InstallCompleteReport) {
	if r.IP != "" {
		v.IPAddresses = []string{r.IP}
	}
}
