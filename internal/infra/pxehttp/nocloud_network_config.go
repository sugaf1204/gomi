package pxehttp

import (
	"fmt"
	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
	gohttp "net/http"
	"strings"
)

// PXENocloudNetworkConfig serves v2 network-config for the NoCloud datasource.
// cloud-init prioritizes this over any baked network config that ships in the image.
// Always matches by MAC address so the config is NIC-name-agnostic.
func (h *Handler) PXENocloudNetworkConfig(c echo.Context) error {
	rawMAC := c.Param("mac")
	mac := normalizeMAC(rawMAC) // always available from the URL
	ctx := c.Request().Context()

	n := h.findHostByMAC(ctx, rawMAC)
	if _, ok := n.(*vm.VirtualMachine); ok && isDebianOSFamily(h.resolveOSImageFamily(ctx, n.OSImageVariantRef())) {
		ip := ""
		var spec *subnet.SubnetSpec
		if n.GetIPAssignment() == resource.IPAssignmentStatic {
			ip = n.StaticIP()
			spec = h.resolveSubnetSpec(ctx, n)
		}
		return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8",
			[]byte(buildCloudInitV1NetworkConfig(mac, ip, spec)))
	}
	renderer := h.resolveNetworkConfigRenderer(ctx, n)

	// Hypervisor machines get a bridged network config.
	if m, ok := n.(*machine.Machine); ok && m.Role == machine.RoleHypervisor {
		bridgeName := m.BridgeName
		if bridgeName == "" {
			bridgeName = "br0"
		}
		ip := ""
		var spec *subnet.SubnetSpec
		if m.IPAssignment == resource.IPAssignmentStatic {
			ip = m.IP
			spec = h.resolveSubnetSpec(ctx, n)
		}
		return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8",
			[]byte(buildBridgedNetworkConfigWithRenderer(mac, bridgeName, ip, spec, renderer)))
	}

	if n != nil && n.GetIPAssignment() == resource.IPAssignmentStatic {
		if ip := n.StaticIP(); ip != "" {
			spec := h.resolveSubnetSpec(ctx, n)
			return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8",
				[]byte(buildNetworkConfigWithRenderer(mac, ip, spec, renderer)))
		}
	}

	return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8",
		[]byte(buildNetworkConfigWithRenderer(mac, "", nil, renderer)))
}

// buildNetworkConfig builds a netplan v2 network-config matched by MAC address.
// If ip is empty, DHCP is configured. Otherwise a static address is configured.
func buildNetworkConfig(mac, ip string, spec *subnet.SubnetSpec) string {
	return buildNetworkConfigWithRenderer(mac, ip, spec, networkConfigRendererNetworkd)
}

func buildNetworkConfigWithRenderer(mac, ip string, spec *subnet.SubnetSpec, renderer string) string {
	return marshalYAMLString(buildDirectNetplanConfig(
		mac,
		ip,
		subnetPrefixLen(spec),
		subnetGateway(spec),
		subnetNameServers(spec),
		ip == "",
		renderer,
	))
}

type cloudInitV1NetworkConfig struct {
	Version int                         `yaml:"version"`
	Config  []cloudInitV1NetworkElement `yaml:"config"`
}

type cloudInitV1NetworkElement struct {
	Type       string                     `yaml:"type"`
	Name       string                     `yaml:"name"`
	MACAddress string                     `yaml:"mac_address,omitempty"`
	Subnets    []cloudInitV1NetworkSubnet `yaml:"subnets"`
}

type cloudInitV1NetworkSubnet struct {
	Type           string   `yaml:"type"`
	Address        string   `yaml:"address,omitempty"`
	Gateway        string   `yaml:"gateway,omitempty"`
	DNSNameservers []string `yaml:"dns_nameservers,omitempty"`
}

func buildCloudInitV1NetworkConfig(mac, ip string, spec *subnet.SubnetSpec) string {
	subnetCfg := cloudInitV1NetworkSubnet{Type: "dhcp"}
	if ip = strings.TrimSpace(ip); ip != "" {
		subnetCfg = cloudInitV1NetworkSubnet{
			Type:           "static",
			Address:        fmt.Sprintf("%s/%d", ip, subnetPrefixLen(spec)),
			Gateway:        subnetGateway(spec),
			DNSNameservers: subnetNameServers(spec),
		}
	}
	return marshalYAMLString(cloudInitV1NetworkConfig{
		Version: 1,
		Config: []cloudInitV1NetworkElement{{
			Type:       "physical",
			Name:       "eth0",
			MACAddress: strings.ToLower(strings.TrimSpace(mac)),
			Subnets:    []cloudInitV1NetworkSubnet{subnetCfg},
		}},
	})
}
