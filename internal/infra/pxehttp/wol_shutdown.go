package pxehttp

import (
	"fmt"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
	"gopkg.in/yaml.v3"
	"strings"
)

const targetWoLShutdownService = `[Unit]
Description=GOMI WoL shutdown daemon
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/gomi-wol-daemon --env-file /etc/gomi/wol-daemon.env
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
`

func injectWoLShutdownAgent(cloudConfig, pxeBaseURL string, m *machine.Machine) string {
	if m == nil || m.Power.Type != power.PowerTypeWoL || m.Power.WoL == nil {
		return cloudConfig
	}
	wol := m.Power.WoL
	if strings.TrimSpace(wol.HMACSecret) == "" || strings.TrimSpace(wol.Token) == "" {
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

	port := wol.ShutdownUDPPort
	if port == 0 {
		port = power.DefaultWoLShutdownUDPPort
	}
	ttlSeconds := wol.TokenTTLSeconds
	if ttlSeconds == 0 {
		ttlSeconds = power.DefaultWoLShutdownTTLSeconds
	}
	serverBase := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(pxeBaseURL), "/"), "/pxe")
	filesBase := serverBase + "/files"
	if strings.TrimSpace(serverBase) == "" || strings.TrimSpace(filesBase) == "/files" {
		return cloudConfig
	}

	env := strings.Builder{}
	env.WriteString(systemdEnvLine("GOMI_WOL_LISTEN", fmt.Sprintf(":%d", port)))
	env.WriteString(systemdEnvLine("GOMI_WOL_SECRET", wol.HMACSecret))
	env.WriteString(systemdEnvLine("GOMI_WOL_TOKEN", wol.Token))
	env.WriteString(systemdEnvLine("GOMI_WOL_TTL", fmt.Sprintf("%ds", ttlSeconds)))
	env.WriteString(systemdEnvLine("GOMI_SERVER_URL", serverBase))
	env.WriteString(systemdEnvLine("GOMI_MACHINE_NAME", m.Name))

	writeFiles := []any{}
	if existing, ok := cfg["write_files"].([]any); ok {
		writeFiles = existing
	}
	writeFiles = append(writeFiles, map[string]any{
		"path":        "/etc/gomi/wol-daemon.env",
		"permissions": "0600",
		"content":     env.String(),
	}, map[string]any{
		"path":        "/etc/systemd/system/gomi-wol-daemon.service",
		"permissions": "0644",
		"content":     targetWoLShutdownService,
	}, map[string]any{
		"path":        "/usr/local/sbin/gomi-install-wol-daemon",
		"permissions": "0755",
		"content":     buildWoLShutdownInstallerScript(filesBase),
	})
	cfg["write_files"] = writeFiles

	runCmd := []any{}
	if existing, ok := cfg["runcmd"].([]any); ok {
		runCmd = existing
	}
	runCmd = append(runCmd, "/usr/local/sbin/gomi-install-wol-daemon")
	cfg["runcmd"] = runCmd

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return cloudConfig
	}
	return "#cloud-config\n" + string(raw)
}

func buildWoLShutdownInstallerScript(filesBase string) string {
	base := strings.TrimRight(filesBase, "/")
	return fmt.Sprintf(`#!/bin/sh
set -eu

arch=$(dpkg --print-architecture 2>/dev/null || uname -m)
case "$arch" in
    x86_64) arch=amd64 ;;
    aarch64) arch=arm64 ;;
esac

url="%s/gomi-wol-daemon-linux-${arch}"
tmp=$(mktemp)
cleanup() { rm -f "$tmp"; }
trap cleanup EXIT

if command -v curl >/dev/null 2>&1; then
    curl -fsSL -o "$tmp" "$url"
elif command -v wget >/dev/null 2>&1; then
    wget -qO "$tmp" "$url"
elif command -v python3 >/dev/null 2>&1; then
    python3 - "$url" "$tmp" <<'PY'
import sys
import urllib.request

urllib.request.urlretrieve(sys.argv[1], sys.argv[2])
PY
else
    echo "no downloader found for $url" >&2
    exit 1
fi

install -m 0755 "$tmp" /usr/local/bin/gomi-wol-daemon
systemctl daemon-reload
systemctl enable --now gomi-wol-daemon.service
`, base)
}

func systemdEnvLine(key, value string) string {
	escaped := strings.NewReplacer(
		"\\", "\\\\",
		`"`, `\"`,
		"\n", "",
		"\r", "",
	).Replace(strings.TrimSpace(value))
	return fmt.Sprintf("%s=\"%s\"\n", key, escaped)
}
