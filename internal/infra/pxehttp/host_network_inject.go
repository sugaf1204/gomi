package pxehttp

import (
	"context"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/node"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
	"strings"
)

func (h *Handler) resolveSubnetSpec(ctx context.Context, target node.Node) *subnet.SubnetSpec {
	if h.subnets == nil {
		return nil
	}
	if ref := target.GetSubnetRef(); ref != "" {
		if sub, err := h.subnets.Get(ctx, ref); err == nil {
			return &sub.Spec
		}
	}
	subs, err := h.subnets.List(ctx)
	if err != nil || len(subs) == 0 {
		return nil
	}
	return &subs[0].Spec
}

func injectHostNetworkConfig(cloudConfig string, h node.Node, osFamily, pxeBaseURL string, spec *subnet.SubnetSpec) string {
	if isNetworkManagerOSFamily(osFamily) {
		return injectNetworkManagerConfigForHost(cloudConfig, h, osFamily, spec)
	}
	if _, ok := h.(*vm.VirtualMachine); ok && isDebianOSFamily(osFamily) {
		return injectDebianIfupdownConfigForHost(cloudConfig, h, spec)
	}
	return injectNetplanConfigForHost(cloudConfig, h, osFamily, pxeBaseURL, spec)
}

func injectBridgedHostNetworkConfig(cloudConfig string, m *machine.Machine, osFamily, pxeBaseURL string, spec *subnet.SubnetSpec) string {
	if isNetworkManagerOSFamily(osFamily) {
		return injectBridgedNetworkManagerConfig(cloudConfig, m, osFamily, spec)
	}
	return injectBridgedNetplanConfig(cloudConfig, m, osFamily, pxeBaseURL, spec)
}

func isDebianOSFamily(osFamily string) bool {
	return strings.EqualFold(strings.TrimSpace(osFamily), "debian")
}

func isUbuntuOSFamily(osFamily string) bool {
	return strings.EqualFold(strings.TrimSpace(osFamily), "ubuntu")
}

func supportsNetplanOSFamily(osFamily string) bool {
	return !isNetworkManagerOSFamily(osFamily)
}

func isNetworkManagerOSFamily(osFamily string) bool {
	switch strings.ToLower(strings.TrimSpace(osFamily)) {
	case "fedora", "rhel", "redhat", "centos", "rocky", "alma", "almalinux":
		return true
	default:
		return false
	}
}
