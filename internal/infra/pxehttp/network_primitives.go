package pxehttp

import (
	"fmt"
	"github.com/sugaf1204/gomi/internal/subnet"
	"gopkg.in/yaml.v3"
	"net"
	"strings"
)

type netplanConfig struct {
	Version   int                      `yaml:"version"`
	Renderer  string                   `yaml:"renderer,omitempty"`
	Ethernets map[string]netplanNIC    `yaml:"ethernets,omitempty"`
	Bridges   map[string]netplanBridge `yaml:"bridges,omitempty"`
}

type netplanNIC struct {
	Match       *netplanMatch       `yaml:"match,omitempty"`
	WakeOnLAN   bool                `yaml:"wakeonlan,omitempty"`
	DHCP4       bool                `yaml:"dhcp4"`
	DHCP6       bool                `yaml:"dhcp6,omitempty"`
	Addresses   []string            `yaml:"addresses,omitempty"`
	Routes      []netplanRoute      `yaml:"routes,omitempty"`
	NameServers *netplanNameServers `yaml:"nameservers,omitempty"`
}

type netplanBridge struct {
	Interfaces  []string            `yaml:"interfaces,omitempty"`
	MACAddress  string              `yaml:"macaddress,omitempty"`
	DHCP4       bool                `yaml:"dhcp4"`
	DHCP6       bool                `yaml:"dhcp6,omitempty"`
	Addresses   []string            `yaml:"addresses,omitempty"`
	Routes      []netplanRoute      `yaml:"routes,omitempty"`
	NameServers *netplanNameServers `yaml:"nameservers,omitempty"`
}

type netplanMatch struct {
	MACAddress string `yaml:"macaddress,omitempty"`
}

type netplanRoute struct {
	To  string `yaml:"to"`
	Via string `yaml:"via"`
}

type netplanNameServers struct {
	Addresses []string `yaml:"addresses,omitempty"`
}

const (
	networkConfigRendererNetworkd = "networkd"
)

func marshalYAMLString(value any) string {
	raw, err := yaml.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

func subnetPrefixLen(spec *subnet.SubnetSpec) int {
	prefixLen := 24
	if spec != nil && spec.CIDR != "" {
		if _, ipNet, err := net.ParseCIDR(spec.CIDR); err == nil {
			ones, _ := ipNet.Mask.Size()
			prefixLen = ones
		}
	}
	return prefixLen
}

func subnetGateway(spec *subnet.SubnetSpec) string {
	if spec == nil {
		return ""
	}
	return strings.TrimSpace(spec.DefaultGateway)
}

func subnetNameServers(spec *subnet.SubnetSpec) []string {
	if spec == nil {
		return nil
	}
	out := make([]string, 0, len(spec.DNSServers))
	for _, ns := range spec.DNSServers {
		ns = strings.TrimSpace(ns)
		if ns != "" {
			out = append(out, ns)
		}
	}
	return out
}

func macMatch(mac string) *netplanMatch {
	mac = strings.ToLower(strings.TrimSpace(mac))
	if mac == "" {
		return nil
	}
	return &netplanMatch{MACAddress: mac}
}

func nameserverBlock(values []string) *netplanNameServers {
	if len(values) == 0 {
		return nil
	}
	return &netplanNameServers{Addresses: values}
}

func defaultRoute(gateway string) []netplanRoute {
	gateway = strings.TrimSpace(gateway)
	if gateway == "" {
		return nil
	}
	return []netplanRoute{{To: "default", Via: gateway}}
}

func buildDirectNetplanConfig(mac, ip string, prefixLen int, gateway string, nameservers []string, dhcp bool, renderer string) netplanConfig {
	nic := netplanNIC{
		Match:     macMatch(mac),
		WakeOnLAN: strings.TrimSpace(mac) != "",
		DHCP4:     dhcp,
		DHCP6:     false,
	}
	if !dhcp {
		nic.Addresses = []string{fmt.Sprintf("%s/%d", ip, prefixLen)}
		nic.Routes = defaultRoute(gateway)
		nic.NameServers = nameserverBlock(nameservers)
	}
	return netplanConfig{
		Version:   2,
		Renderer:  renderer,
		Ethernets: map[string]netplanNIC{"gomi-nic": nic},
	}
}

func buildBridgedNetplanConfig(mac, bridgeName, ip string, prefixLen int, gateway string, nameservers []string, dhcp bool, renderer string) netplanConfig {
	bridgeName = strings.TrimSpace(bridgeName)
	if bridgeName == "" {
		bridgeName = "br0"
	}
	mac = strings.ToLower(strings.TrimSpace(mac))

	nic := netplanNIC{
		Match:     macMatch(mac),
		WakeOnLAN: mac != "",
		DHCP4:     false,
		DHCP6:     false,
	}
	bridge := netplanBridge{
		Interfaces: []string{"gomi-nic"},
		MACAddress: mac,
		DHCP4:      dhcp,
		DHCP6:      false,
	}
	if !dhcp {
		bridge.Addresses = []string{fmt.Sprintf("%s/%d", ip, prefixLen)}
		bridge.Routes = defaultRoute(gateway)
		bridge.NameServers = nameserverBlock(nameservers)
	}
	return netplanConfig{
		Version:   2,
		Renderer:  renderer,
		Ethernets: map[string]netplanNIC{"gomi-nic": nic},
		Bridges:   map[string]netplanBridge{bridgeName: bridge},
	}
}

// buildBridgedNetworkConfig builds a netplan v2 config that creates a bridge
// with the matched physical NIC as a member. Used for hypervisor machines so
// VMs can share the same physical network.
func buildBridgedNetworkConfig(mac, bridgeName, ip string, spec *subnet.SubnetSpec) string {
	return buildBridgedNetworkConfigWithRenderer(mac, bridgeName, ip, spec, networkConfigRendererNetworkd)
}

func buildBridgedNetworkConfigWithRenderer(mac, bridgeName, ip string, spec *subnet.SubnetSpec, renderer string) string {
	return marshalYAMLString(buildBridgedNetplanConfig(
		mac,
		bridgeName,
		ip,
		subnetPrefixLen(spec),
		subnetGateway(spec),
		subnetNameServers(spec),
		ip == "",
		renderer,
	))
}
