package pxehttp

import (
	"fmt"
	"github.com/sugaf1204/gomi/internal/node"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/subnet"
	"gopkg.in/yaml.v3"
	"strings"
)

func injectDebianIfupdownConfigForHost(cloudConfig string, h node.Node, spec *subnet.SubnetSpec) string {
	if h == nil {
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
		"path":        "/usr/local/sbin/gomi-apply-debian-ifupdown",
		"content":     debianIfupdownApplyScript(mac, ip, spec),
		"permissions": "0755",
	})
	cfg["write_files"] = writeFiles

	runCmd := []any{}
	if existing, ok := cfg["runcmd"].([]any); ok {
		runCmd = existing
	}
	runCmd = append([]any{"/usr/local/sbin/gomi-apply-debian-ifupdown"}, runCmd...)
	cfg["runcmd"] = runCmd

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return cloudConfig
	}
	return "#cloud-config\n" + string(raw)
}

func debianIfupdownApplyScript(mac, ip string, spec *subnet.SubnetSpec) string {
	prefixLen := subnetPrefixLen(spec)
	gateway := subnetGateway(spec)
	nameservers := strings.Join(subnetNameServers(spec), " ")
	return fmt.Sprintf(`#!/bin/sh
set -eu

target_mac=%s
target_ip=%s
prefix_len=%s
gateway=%s
nameservers=%s

iface=""
for path in /sys/class/net/*; do
	[ -e "$path/address" ] || continue
	if [ "$(tr '[:upper:]' '[:lower:]' < "$path/address")" = "$target_mac" ]; then
		iface=${path##*/}
		break
	fi
done
[ -n "$iface" ] || exit 1

mkdir -p /etc/network/interfaces.d
if [ -n "$target_ip" ]; then
	cat >/etc/network/interfaces.d/99-gomi.cfg <<EOF
auto $iface
allow-hotplug $iface
iface $iface inet static
    address $target_ip/$prefix_len
EOF
	if [ -n "$gateway" ]; then
		printf '    gateway %%s\n' "$gateway" >>/etc/network/interfaces.d/99-gomi.cfg
	fi
	if [ -n "$nameservers" ]; then
		printf '    dns-nameservers %%s\n' "$nameservers" >>/etc/network/interfaces.d/99-gomi.cfg
		: >/etc/resolv.conf
		for ns in $nameservers; do
			printf 'nameserver %%s\n' "$ns" >>/etc/resolv.conf
		done
	fi
	ip link set "$iface" up
	ip addr flush dev "$iface" || true
	ip addr add "$target_ip/$prefix_len" dev "$iface"
	if [ -n "$gateway" ]; then
		ip route replace default via "$gateway" dev "$iface"
	fi
else
	cat >/etc/network/interfaces.d/99-gomi.cfg <<EOF
auto $iface
allow-hotplug $iface
iface $iface inet dhcp
EOF
	if command -v dhclient >/dev/null 2>&1; then
		dhclient -v "$iface" || true
	fi
fi

systemctl unmask networking.service 2>/dev/null || true
systemctl enable networking.service 2>/dev/null || true
systemctl restart networking.service 2>/dev/null || true
ip addr show "$iface" || true
ip route show || true
`, shellQuote(strings.ToLower(strings.TrimSpace(mac))), shellQuote(strings.TrimSpace(ip)), shellQuote(fmt.Sprint(prefixLen)), shellQuote(gateway), shellQuote(nameservers))
}
