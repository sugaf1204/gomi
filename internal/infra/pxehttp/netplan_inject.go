package pxehttp

import (
	"fmt"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/node"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/subnet"
	"gopkg.in/yaml.v3"
	"net"
	"strings"
)

type netplanParams struct {
	IP          string
	MAC         string
	Gateway     string
	NameServers []string
	OSFamily    string
	PXEBaseURL  string
}

const debianNetplanNetworkdSwitchScript = `#!/bin/sh
set -eu

if ! command -v netplan >/dev/null 2>&1; then
	echo "netplan command is missing; install netplan.io before switching Debian networking" >&2
	exit 1
fi

netplan generate

cat >/usr/local/sbin/gomi-rollback-networking <<'EOF'
#!/bin/sh
set -eu
systemctl unmask networking.service 2>/dev/null || true
systemctl enable networking.service 2>/dev/null || true
systemctl disable systemd-networkd.service systemd-networkd.socket 2>/dev/null || true
systemctl stop systemd-networkd.service systemd-networkd.socket 2>/dev/null || true
if [ -f /etc/network/interfaces.gomi-netplan-save ]; then
	mv -f /etc/network/interfaces.gomi-netplan-save /etc/network/interfaces
fi
if [ -f /etc/default/netplan.gomi-save ]; then
	mv -f /etc/default/netplan.gomi-save /etc/default/netplan
else
	rm -f /etc/default/netplan
fi
systemctl restart networking.service 2>/dev/null || true
EOF
chmod 0755 /usr/local/sbin/gomi-rollback-networking

cat >/etc/systemd/system/gomi-network-rollback.service <<'EOF'
[Unit]
Description=Rollback GOMI netplan networkd switch

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/gomi-rollback-networking
EOF

cat >/etc/systemd/system/gomi-network-rollback.timer <<'EOF'
[Unit]
Description=Rollback GOMI netplan networkd switch unless cancelled

[Timer]
OnActiveSec=10min
AccuracySec=5s
Unit=gomi-network-rollback.service

[Install]
WantedBy=timers.target
EOF

systemctl daemon-reload
systemctl enable --now gomi-network-rollback.timer
if [ -f /etc/network/interfaces ]; then
	mv -f /etc/network/interfaces /etc/network/interfaces.gomi-netplan-save
fi
if [ -e /etc/default/netplan ]; then
	cp -a /etc/default/netplan /etc/default/netplan.gomi-save
fi
printf '%s\n' 'ENABLED=1' > /etc/default/netplan
systemctl enable systemd-networkd.service
systemctl restart systemd-networkd.service
systemctl disable --now networking.service 2>/dev/null || true
systemctl mask networking.service 2>/dev/null || true

if command -v networkctl >/dev/null 2>&1; then
	networkctl status --no-pager || true
	timeout 30 networkctl is-online || true
fi
ip addr show
ip route show
`

func debianNetplanNetworkdConfirmScript(pxeBaseURL string) string {
	healthURL := strings.TrimRight(strings.TrimSpace(pxeBaseURL), "/")
	healthURL = strings.TrimSuffix(healthURL, "/pxe")
	healthURL += "/healthz"
	return fmt.Sprintf(`#!/bin/sh
set -eu

if ! systemctl is-active --quiet systemd-networkd.service; then
	exit 0
fi
if command -v networkctl >/dev/null 2>&1; then
	timeout 30 networkctl is-online || true
fi
if curl -fsS --connect-timeout 5 --max-time 15 %s >/dev/null; then
	systemctl disable --now gomi-network-rollback.timer >/dev/null 2>&1 || true
	rm -f /etc/systemd/system/gomi-network-rollback.timer /etc/systemd/system/gomi-network-rollback.service /usr/local/sbin/gomi-rollback-networking /usr/local/sbin/gomi-confirm-netplan-networkd /etc/network/interfaces.gomi-netplan-save /etc/default/netplan.gomi-save
	systemctl daemon-reload >/dev/null 2>&1 || true
fi
exit 0
`, shellQuote(healthURL))
}

func injectNetplanConfigFromParams(cloudConfig string, params netplanParams, spec *subnet.SubnetSpec) string {
	if !supportsNetplanOSFamily(params.OSFamily) {
		return cloudConfig
	}
	ip := net.ParseIP(strings.TrimSpace(params.IP))
	mac := strings.TrimSpace(params.MAC)
	if ip == nil && mac == "" {
		return cloudConfig
	}

	gateway := strings.TrimSpace(params.Gateway)
	if gateway == "" && spec != nil {
		gateway = subnetGateway(spec)
	}

	nameServers := make([]string, 0, len(params.NameServers))
	for _, ns := range params.NameServers {
		ns = strings.TrimSpace(ns)
		if ns != "" {
			nameServers = append(nameServers, ns)
		}
	}
	if len(nameServers) == 0 {
		nameServers = subnetNameServers(spec)
	}
	nic := netplanNIC{
		Match:     macMatch(mac),
		WakeOnLAN: mac != "",
		DHCP4:     ip == nil,
		DHCP6:     false,
	}
	if ip != nil {
		nic.Addresses = []string{fmt.Sprintf("%s/%d", ip.String(), subnetPrefixLen(spec))}
		nic.Routes = defaultRoute(gateway)
		nic.NameServers = nameserverBlock(nameServers)
	}
	netplanYAML := marshalYAMLString(struct {
		Network netplanConfig `yaml:"network"`
	}{
		Network: netplanConfig{
			Version:  2,
			Renderer: "networkd",
			Ethernets: map[string]netplanNIC{
				"id0": nic,
			},
		},
	})
	if netplanYAML == "" {
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
	writeFiles = append(writeFiles, map[string]any{
		"path":        "/etc/netplan/99-gomi-network.yaml",
		"content":     netplanYAML,
		"permissions": "0600",
	})
	if isDebianOSFamily(params.OSFamily) {
		writeFiles = append(writeFiles, map[string]any{
			"path":        "/usr/local/sbin/gomi-apply-netplan-networkd",
			"content":     debianNetplanNetworkdSwitchScript,
			"permissions": "0755",
		}, map[string]any{
			"path":        "/usr/local/sbin/gomi-confirm-netplan-networkd",
			"content":     debianNetplanNetworkdConfirmScript(params.PXEBaseURL),
			"permissions": "0755",
		})
	}
	cfg["write_files"] = writeFiles

	netplanCmds := netplanActivationCommands(params.OSFamily)
	runCmd := []any{}
	if existing, ok := cfg["runcmd"].([]any); ok {
		runCmd = existing
	}
	runCmd = append(netplanCmds, runCmd...)
	if isDebianOSFamily(params.OSFamily) {
		runCmd = append(runCmd, "/usr/local/sbin/gomi-confirm-netplan-networkd")
	}
	cfg["runcmd"] = runCmd

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return cloudConfig
	}
	return "#cloud-config\n" + string(raw)
}

// injectBridgedNetplanConfig injects a bridged netplan config into cloud-config
// write_files so that the hypervisor machine gets a bridge with a static IP.
func injectBridgedNetplanConfig(cloudConfig string, m *machine.Machine, osFamily, pxeBaseURL string, spec *subnet.SubnetSpec) string {
	if !supportsNetplanOSFamily(osFamily) {
		return cloudConfig
	}
	ip := ""
	if m.GetIPAssignment() == resource.IPAssignmentStatic {
		ip = m.StaticIP()
	}
	if ip == "" && strings.TrimSpace(m.PrimaryMAC()) == "" {
		return cloudConfig
	}
	netplanYAML := marshalYAMLString(struct {
		Network netplanConfig `yaml:"network"`
	}{
		Network: buildBridgedNetplanConfig(
			m.PrimaryMAC(),
			m.BridgeName,
			ip,
			subnetPrefixLen(spec),
			subnetGateway(spec),
			subnetNameServers(spec),
			ip == "",
			networkConfigRendererNetworkd,
		),
	})
	if netplanYAML == "" {
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
	writeFiles = append(writeFiles, map[string]any{
		"path":        "/etc/netplan/99-gomi-network.yaml",
		"content":     netplanYAML,
		"permissions": "0600",
	})
	if isDebianOSFamily(osFamily) {
		writeFiles = append(writeFiles, map[string]any{
			"path":        "/usr/local/sbin/gomi-apply-netplan-networkd",
			"content":     debianNetplanNetworkdSwitchScript,
			"permissions": "0755",
		}, map[string]any{
			"path":        "/usr/local/sbin/gomi-confirm-netplan-networkd",
			"content":     debianNetplanNetworkdConfirmScript(pxeBaseURL),
			"permissions": "0755",
		})
	}
	cfg["write_files"] = writeFiles

	netplanCmds := netplanActivationCommands(osFamily)
	runCmd := []any{}
	if existing, ok := cfg["runcmd"].([]any); ok {
		runCmd = existing
	}
	runCmd = append(netplanCmds, runCmd...)
	if isDebianOSFamily(osFamily) {
		runCmd = append(runCmd, "/usr/local/sbin/gomi-confirm-netplan-networkd")
	}
	cfg["runcmd"] = runCmd

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return cloudConfig
	}
	return "#cloud-config\n" + string(raw)
}

func injectNetplanConfigForHost(cloudConfig string, h node.Node, osFamily, pxeBaseURL string, spec *subnet.SubnetSpec) string {
	ip := ""
	if h.GetIPAssignment() == resource.IPAssignmentStatic {
		ip = h.StaticIP()
	}
	if ip == "" && strings.TrimSpace(h.PrimaryMAC()) == "" {
		return cloudConfig
	}
	return injectNetplanConfigFromParams(cloudConfig, netplanParams{
		IP:         ip,
		MAC:        h.PrimaryMAC(),
		OSFamily:   osFamily,
		PXEBaseURL: pxeBaseURL,
	}, spec)
}

func netplanActivationCommands(osFamily string) []any {
	cmds := []any{
		"rm -f /etc/cloud/cloud.cfg.d/50-curtin-networking.cfg /etc/netplan/50-cloud-init.yaml /etc/netplan/01-netcfg.yaml /etc/netplan/00-installer-config.yaml",
	}
	if isDebianOSFamily(osFamily) {
		return append(cmds, "/usr/local/sbin/gomi-apply-netplan-networkd")
	}
	if isUbuntuOSFamily(osFamily) {
		cmds = append(cmds,
			"systemctl enable --now systemd-networkd.service systemd-networkd.socket",
			"netplan apply",
		)
	} else {
		cmds = append(cmds, "netplan apply")
	}
	return append(cmds,
		"systemctl disable --now systemd-networkd-wait-online.service 2>/dev/null || true; systemctl reset-failed systemd-networkd-wait-online.service 2>/dev/null || true",
	)
}
