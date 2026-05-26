package pxehttp

import (
	"context"
	"errors"
	"fmt"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
	"gopkg.in/yaml.v3"
	"strings"
)

func withDeployCloudInitDefaults(userData string, disableResizeRootfs bool) string {
	trimmed := strings.TrimSpace(userData)
	if trimmed == "" {
		return ""
	}
	header := "#cloud-config"
	if strings.HasPrefix(trimmed, "## template: jinja") {
		header = "## template: jinja\n#cloud-config"
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "## template: jinja"))
	}
	cfg := map[string]any{}
	if err := yaml.Unmarshal([]byte(trimmed), &cfg); err != nil {
		return strings.TrimRight(userData, "\n") + "\n"
	}
	if _, ok := cfg["locale"]; !ok {
		cfg["locale"] = false
	}
	if _, ok := cfg["resize_rootfs"]; disableResizeRootfs && !ok {
		cfg["resize_rootfs"] = false
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return strings.TrimRight(userData, "\n") + "\n"
	}
	return header + "\n" + string(raw)
}

func (h *Handler) resolveCloudInitUserData(ctx context.Context, cloudInitRef string) (string, bool, error) {
	if h.cloudInits == nil {
		return "", false, nil
	}
	template, err := h.cloudInits.Get(ctx, cloudInitRef)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	userData := strings.TrimSpace(template.UserData)
	if userData == "" {
		return "", false, nil
	}
	return userData + "\n", true, nil
}

func injectPreseedCompletion(content, completeURL, hostname string) string {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(content), "\r\n", "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	const latePrefix = "d-i preseed/late_command string"
	commands := make([]string, 0, 3)
	filtered := make([]string, 0, len(lines)+4)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, latePrefix):
			existing := strings.TrimSpace(strings.TrimPrefix(trimmed, latePrefix))
			if existing != "" {
				commands = append(commands, existing)
			}
		case strings.HasPrefix(trimmed, "d-i debian-installer/exit/poweroff"):
			continue
		default:
			filtered = append(filtered, line)
		}
	}

	commands = append(commands, "in-target apt-get update")
	commands = append(commands, "in-target systemctl enable serial-getty@ttyS0.service")
	if sanitized := sanitizeHostnameForLinux(hostname); sanitized != "" {
		commands = append(commands, fmt.Sprintf("in-target /bin/sh -c 'echo %s >/etc/hostname'", sanitized))
		commands = append(commands, fmt.Sprintf("in-target /bin/sh -c 'sed -i \"s/^127.0.1.1.*/127.0.1.1 %s/\" /etc/hosts'", sanitized))
	}
	if completeURL != "" {
		commands = append(commands, preseedInstallCompleteCommand(completeURL))
	}

	filtered = append(filtered,
		fmt.Sprintf("%s %s", latePrefix, strings.Join(commands, "; ")),
		"d-i finish-install/reboot_in_progress note",
		"d-i debian-installer/exit/reboot boolean true",
	)
	return strings.TrimSpace(strings.Join(filtered, "\n")) + "\n"
}

func injectPreseedHostname(content, hostname string) string {
	sanitized := sanitizeHostnameForLinux(hostname)
	if sanitized == "" {
		return strings.TrimSpace(content) + "\n"
	}

	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(content), "\r\n", "\n"), "\n")
	filtered := make([]string, 0, len(lines)+1)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "d-i netcfg/get_hostname string") {
			continue
		}
		if strings.HasPrefix(trimmed, "d-i netcfg/hostname string") {
			continue
		}
		filtered = append(filtered, line)
	}
	filtered = append(filtered,
		fmt.Sprintf("d-i netcfg/get_hostname string %s", sanitized),
		fmt.Sprintf("d-i netcfg/hostname string %s", sanitized),
		"d-i netcfg/override_dhcp boolean true",
	)
	return strings.TrimSpace(strings.Join(filtered, "\n")) + "\n"
}

func sanitizeHostnameForLinux(raw string) string {
	in := strings.ToLower(strings.TrimSpace(raw))
	if in == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 63 {
		out = strings.Trim(out[:63], "-")
	}
	return out
}

func preseedInstallCompleteCommand(completeURL string) string {
	escaped := strings.ReplaceAll(completeURL, `"`, `\"`)
	tokenVal, typeVal := parseTokenAndTypeFromURL(completeURL)
	return fmt.Sprintf(
		`in-target /bin/sh -c 'IP=$(hostname -I 2>/dev/null | awk "{print \$1}"); DEV=$(ip -o route get 1 2>/dev/null | awk "{for(i=1;i<=NF;i++){if(\$i==\"dev\"){print \$(i+1);exit}}}"); MAC=$(cat /sys/class/net/${DEV}/address 2>/dev/null); curl -fsS --connect-timeout 5 --max-time 15 -X POST -H "Content-Type: application/json" -d "{\"token\":\"%s\",\"type\":\"%s\",\"ip\":\"${IP}\",\"mac\":\"${MAC}\"}" "%s" || curl -fsS --connect-timeout 5 --max-time 15 -X POST "%s" || true'`,
		tokenVal, typeVal, escaped, escaped,
	)
}

func defaultPXEUserDataByInstallType(installType vm.InstallConfigType) string {
	return defaultLinuxCurtinUserData
}

// buildAutoinstallUserData generates user-data in Ubuntu autoinstall format
// for bare metal machine installations via subiquity.
// The inlineCloudConfig is merged into the autoinstall's user-data section
// so that packages, runcmd, users etc. are applied to the installed system.
func buildAutoinstallUserData(inlineCloudConfig, hostname, completeURL string) string {
	cfg := map[string]any{}
	if err := yaml.Unmarshal([]byte(strings.TrimSpace(defaultAutoinstallUserData)), &cfg); err != nil {
		return defaultAutoinstallUserData
	}

	autoinstall, ok := cfg["autoinstall"].(map[string]any)
	if !ok {
		return defaultAutoinstallUserData
	}

	// Set hostname in identity section.
	if sanitized := sanitizeHostnameForLinux(hostname); sanitized != "" {
		if identity, ok := autoinstall["identity"].(map[string]any); ok {
			identity["hostname"] = sanitized
		}
	}

	// Merge inline cloud-config into autoinstall.user-data section.
	// This allows custom packages, users, runcmd to be applied post-install.
	if trimmed := strings.TrimSpace(inlineCloudConfig); trimmed != "" {
		inlineCfg := map[string]any{}
		if err := yaml.Unmarshal([]byte(trimmed), &inlineCfg); err == nil {
			// Remove cloud-config header artifacts from inline config.
			delete(inlineCfg, "hostname")
			delete(inlineCfg, "manage_etc_hosts")
			if len(inlineCfg) > 0 {
				autoinstall["user-data"] = inlineCfg
			}
		}
	}

	// Add late-commands for serial console and completion callback.
	lateCommands := []any{
		"curtin in-target -- systemctl enable serial-getty@ttyS0.service || true",
	}
	if completeURL != "" {
		escaped := strings.ReplaceAll(completeURL, `"`, `\"`)
		tokenVal, typeVal := parseTokenAndTypeFromURL(completeURL)
		callback := fmt.Sprintf(
			`curtin in-target -- sh -c 'IP=$(hostname -I 2>/dev/null | awk "{print \$1}"); DEV=$(ip -o route get 1 2>/dev/null | awk "{for(i=1;i<=NF;i++){if(\$i==\"dev\"){print \$(i+1);exit}}}"); MAC=$(cat /sys/class/net/${DEV}/address 2>/dev/null); curl -fsS --connect-timeout 5 --max-time 15 -X POST -H "Content-Type: application/json" -d "{\"token\":\"%s\",\"type\":\"%s\",\"ip\":\"${IP}\",\"mac\":\"${MAC}\"}" "%s" || curl -fsS --connect-timeout 5 --max-time 15 -X POST "%s" || true'`,
			tokenVal, typeVal, escaped, escaped,
		)
		lateCommands = append(lateCommands, callback)
	}
	autoinstall["late-commands"] = lateCommands

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return defaultAutoinstallUserData
	}
	return "#cloud-config\n" + string(raw)
}

func injectCloudConfigCompletion(content, completeURL, hostname string, completeRetries int) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		trimmed = defaultLinuxCurtinUserData
	}

	cfg := map[string]any{}
	if err := yaml.Unmarshal([]byte(trimmed), &cfg); err != nil {
		return trimmed + "\n"
	}

	if sanitized := sanitizeHostnameForLinux(hostname); sanitized != "" {
		cfg["hostname"] = sanitized
	}

	runCmd := make([]string, 0, 4)
	if existing, ok := cfg["runcmd"].([]any); ok {
		for _, v := range existing {
			if raw, ok := v.(string); ok && strings.TrimSpace(raw) != "" {
				runCmd = append(runCmd, raw)
			}
		}
	}

	runCmd = append(runCmd, "systemctl enable serial-getty@ttyS0.service || true")
	injectTargetUEFIBootOrderCleanup(cfg, &runCmd)
	if completeURL != "" {
		if completeRetries <= 0 {
			completeRetries = 60
		}
		escaped := strings.ReplaceAll(completeURL, `"`, `\"`)
		callback := fmt.Sprintf(
			`sh -c 'for i in $(seq 1 %d); do IP=$(hostname -I 2>/dev/null | awk "{print \$1}"); DEV=$(ip -o route get 1 2>/dev/null | awk "{for(i=1;i<=NF;i++){if(\$i==\"dev\"){print \$(i+1);exit}}}"); MAC=$(cat /sys/class/net/${DEV}/address 2>/dev/null); curl -fsS --connect-timeout 5 --max-time 15 -X POST -H "Content-Type: application/json" -d "{\"token\":\"%s\",\"type\":\"%s\",\"ip\":\"${IP}\",\"mac\":\"${MAC}\"}" "%s" && exit 0; curl -fsS --connect-timeout 5 --max-time 15 -X POST "%s" && exit 0; sleep 2; done; true'`,
			completeRetries, "__TOKEN__", "__TYPE__", escaped, escaped,
		)
		// Replace token/type placeholders with actual values parsed from completeURL.
		tokenVal, typeVal := parseTokenAndTypeFromURL(completeURL)
		callback = strings.ReplaceAll(callback, "__TOKEN__", tokenVal)
		callback = strings.ReplaceAll(callback, "__TYPE__", typeVal)
		runCmd = append(runCmd, callback)
	}

	finalRunCmd := make([]any, 0, len(runCmd))
	for _, cmd := range runCmd {
		finalRunCmd = append(finalRunCmd, cmd)
	}
	cfg["runcmd"] = finalRunCmd

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return trimmed + "\n"
	}
	return "#cloud-config\n" + string(raw)
}
