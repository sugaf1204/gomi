package pxehttp

import (
	"fmt"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/node"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/subnet"
	"gopkg.in/yaml.v3"
	"strings"
)

type networkManagerConnectionFile struct {
	path    string
	content string
}

func injectNetworkManagerConfigForHost(cloudConfig string, h node.Node, osFamily string, spec *subnet.SubnetSpec) string {
	if !isNetworkManagerOSFamily(osFamily) {
		return cloudConfig
	}
	ip := ""
	if h.GetIPAssignment() == resource.IPAssignmentStatic {
		ip = h.StaticIP()
	}
	mac := strings.TrimSpace(h.PrimaryMAC())
	if ip == "" && mac == "" {
		return cloudConfig
	}
	content := buildNetworkManagerEthernetConnection(mac, ip, spec)
	if content == "" {
		return cloudConfig
	}
	return injectNetworkManagerConnections(cloudConfig, []networkManagerConnectionFile{
		{
			path:    "/etc/NetworkManager/system-connections/gomi-nic.nmconnection",
			content: content,
		},
	}, []string{"gomi-nic"})
}

func injectBridgedNetworkManagerConfig(cloudConfig string, m *machine.Machine, osFamily string, spec *subnet.SubnetSpec) string {
	if !isNetworkManagerOSFamily(osFamily) {
		return cloudConfig
	}
	ip := ""
	if m.GetIPAssignment() == resource.IPAssignmentStatic {
		ip = m.StaticIP()
	}
	mac := strings.TrimSpace(m.PrimaryMAC())
	if ip == "" && mac == "" {
		return cloudConfig
	}
	bridgeName := strings.TrimSpace(m.BridgeName)
	if bridgeName == "" {
		bridgeName = "br0"
	}
	bridgeContent := buildNetworkManagerBridgeConnection(bridgeName, ip, spec)
	portContent := buildNetworkManagerBridgePortConnection(mac, bridgeName)
	if bridgeContent == "" || portContent == "" {
		return cloudConfig
	}
	return injectNetworkManagerConnections(cloudConfig, []networkManagerConnectionFile{
		{
			path:    "/etc/NetworkManager/system-connections/gomi-bridge.nmconnection",
			content: bridgeContent,
		},
		{
			path:    "/etc/NetworkManager/system-connections/gomi-nic.nmconnection",
			content: portContent,
		},
	}, []string{bridgeName, "gomi-nic"})
}

func injectNetworkManagerConnections(cloudConfig string, files []networkManagerConnectionFile, upConnections []string) string {
	if len(files) == 0 {
		return cloudConfig
	}
	trimmed := strings.TrimSpace(cloudConfig)
	if trimmed == "" {
		trimmed = "#cloud-config\n{}"
	}
	cfg := map[string]any{}
	if err := yaml.Unmarshal([]byte(trimmed), &cfg); err != nil {
		return cloudConfig
	}

	writeFiles := []any{}
	if existing, ok := cfg["write_files"].([]any); ok {
		writeFiles = existing
	}
	for _, file := range files {
		writeFiles = append(writeFiles, map[string]any{
			"path":        file.path,
			"content":     file.content,
			"permissions": "0600",
		})
	}
	cfg["write_files"] = writeFiles

	runCmd := []any{}
	if existing, ok := cfg["runcmd"].([]any); ok {
		runCmd = existing
	}
	nmCmds := []any{
		"chmod 600 /etc/NetworkManager/system-connections/gomi-*.nmconnection",
		"nmcli connection reload || systemctl reload NetworkManager || true",
	}
	for _, name := range upConnections {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		nmCmds = append(nmCmds, "nmcli connection up "+shellQuote(name)+" || true")
	}
	runCmd = append(nmCmds, runCmd...)
	cfg["runcmd"] = runCmd

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return cloudConfig
	}
	return "#cloud-config\n" + string(raw)
}

func buildNetworkManagerEthernetConnection(mac, ip string, spec *subnet.SubnetSpec) string {
	mac = strings.ToLower(strings.TrimSpace(mac))
	ip = strings.TrimSpace(ip)
	var b strings.Builder
	b.WriteString("[connection]\n")
	b.WriteString("id=gomi-nic\n")
	b.WriteString("type=ethernet\n")
	b.WriteString("autoconnect=true\n\n")
	b.WriteString("[ethernet]\n")
	if mac != "" {
		b.WriteString("mac-address=")
		b.WriteString(mac)
		b.WriteString("\n")
	}
	b.WriteString("wake-on-lan=64\n\n")
	writeNetworkManagerIPv4Config(&b, ip, spec)
	b.WriteString("[ipv6]\n")
	b.WriteString("method=ignore\n")
	return b.String()
}

func buildNetworkManagerBridgeConnection(bridgeName, ip string, spec *subnet.SubnetSpec) string {
	bridgeName = strings.TrimSpace(bridgeName)
	if bridgeName == "" {
		bridgeName = "br0"
	}
	ip = strings.TrimSpace(ip)
	var b strings.Builder
	b.WriteString("[connection]\n")
	b.WriteString("id=")
	b.WriteString(bridgeName)
	b.WriteString("\n")
	b.WriteString("type=bridge\n")
	b.WriteString("interface-name=")
	b.WriteString(bridgeName)
	b.WriteString("\n")
	b.WriteString("autoconnect=true\n\n")
	b.WriteString("[bridge]\n")
	b.WriteString("stp=false\n\n")
	writeNetworkManagerIPv4Config(&b, ip, spec)
	b.WriteString("[ipv6]\n")
	b.WriteString("method=ignore\n")
	return b.String()
}

func buildNetworkManagerBridgePortConnection(mac, bridgeName string) string {
	mac = strings.ToLower(strings.TrimSpace(mac))
	bridgeName = strings.TrimSpace(bridgeName)
	if bridgeName == "" {
		bridgeName = "br0"
	}
	var b strings.Builder
	b.WriteString("[connection]\n")
	b.WriteString("id=gomi-nic\n")
	b.WriteString("type=ethernet\n")
	b.WriteString("autoconnect=true\n")
	b.WriteString("master=")
	b.WriteString(bridgeName)
	b.WriteString("\n")
	b.WriteString("slave-type=bridge\n\n")
	b.WriteString("[ethernet]\n")
	if mac != "" {
		b.WriteString("mac-address=")
		b.WriteString(mac)
		b.WriteString("\n")
	}
	b.WriteString("wake-on-lan=64\n\n")
	b.WriteString("[ipv4]\n")
	b.WriteString("method=disabled\n\n")
	b.WriteString("[ipv6]\n")
	b.WriteString("method=ignore\n")
	return b.String()
}

func writeNetworkManagerIPv4Config(b *strings.Builder, ip string, spec *subnet.SubnetSpec) {
	b.WriteString("[ipv4]\n")
	if strings.TrimSpace(ip) == "" {
		b.WriteString("method=auto\n\n")
		return
	}
	b.WriteString("method=manual\n")
	b.WriteString("address1=")
	b.WriteString(strings.TrimSpace(ip))
	b.WriteString("/")
	b.WriteString(fmt.Sprint(subnetPrefixLen(spec)))
	if gateway := subnetGateway(spec); gateway != "" {
		b.WriteString(",")
		b.WriteString(gateway)
	}
	b.WriteString("\n")
	if nameservers := subnetNameServers(spec); len(nameservers) > 0 {
		b.WriteString("dns=")
		b.WriteString(strings.Join(nameservers, ";"))
		b.WriteString(";\n")
	}
	b.WriteString("\n")
}
