package pxehttp

import (
	"context"
	"fmt"
	"github.com/sugaf1204/gomi/internal/machine"
	"gopkg.in/yaml.v3"
	"strings"
	"time"
)

// injectHypervisorSetup adds libvirt/KVM packages and runcmd entries to a
// cloud-config YAML string so the machine boots as a ready hypervisor.
func injectHypervisorSetup(cloudConfig, pxeBaseURL, hypervisorName, registrationToken string, osFamily string) string {
	trimmed := strings.TrimSpace(cloudConfig)
	if trimmed == "" {
		trimmed = "#cloud-config\n{}"
	}
	cfg := map[string]any{}
	if err := yaml.Unmarshal([]byte(trimmed), &cfg); err != nil {
		return cloudConfig
	}

	hvPackages := hypervisorSetupPackages(osFamily)
	hvRuncmds := hypervisorSetupRuncmds(osFamily)

	serverBase := ""
	if base := strings.TrimRight(strings.TrimSpace(pxeBaseURL), "/"); base != "" {
		serverBase = strings.TrimSuffix(base, "/pxe")
		filesBase := serverBase + "/files"
		hvRuncmds = append(hvRuncmds,
			fmt.Sprintf(`sh -c '%s; curl -sfL -o /usr/bin/gomi-hypervisor "%s/gomi-hypervisor-linux-${ARCH}" && chmod +x /usr/bin/gomi-hypervisor || true'`, gomiArchShellSnippet(), filesBase),
		)
	}
	if serverBase != "" && strings.TrimSpace(registrationToken) != "" {
		setupURL := serverBase + "/api/v1/hypervisors/setup-and-register.sh"
		registerCmd := fmt.Sprintf(
			`set -euo pipefail; tmp=$(mktemp); trap 'rm -f "$tmp"' EXIT; for i in $(seq 1 60); do if curl -sfL -o "$tmp" %s; then GOMI_SERVER=%s GOMI_TOKEN=%s GOMI_HOSTNAME=%s bash "$tmp"; exit $?; fi; sleep 2; done; curl -sfL -o "$tmp" %s; GOMI_SERVER=%s GOMI_TOKEN=%s GOMI_HOSTNAME=%s bash "$tmp"`,
			shellQuote(setupURL),
			shellQuote(serverBase),
			shellQuote(strings.TrimSpace(registrationToken)),
			shellQuote(strings.TrimSpace(hypervisorName)),
			shellQuote(setupURL),
			shellQuote(serverBase),
			shellQuote(strings.TrimSpace(registrationToken)),
			shellQuote(strings.TrimSpace(hypervisorName)),
		)
		hvRuncmds = append(hvRuncmds, "bash -c "+shellQuote(registerCmd))
	}

	// Merge packages.
	var pkgList []any
	if existing, ok := cfg["packages"].([]any); ok {
		pkgList = existing
	}
	pkgList = append(pkgList, hvPackages...)
	if len(pkgList) > 0 {
		cfg["packages"] = pkgList
	} else {
		delete(cfg, "packages")
	}

	// Append runcmd.
	var runList []any
	if existing, ok := cfg["runcmd"].([]any); ok {
		runList = existing
	}
	cfg["runcmd"] = append(runList, hvRuncmds...)

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return cloudConfig
	}
	return "#cloud-config\n" + string(raw)
}

func hypervisorSetupPackages(osFamily string) []any {
	switch strings.ToLower(strings.TrimSpace(osFamily)) {
	case "fedora", "rhel", "redhat", "centos", "rocky", "almalinux":
		return nil
	default:
		return []any{
			"libvirt-daemon-system",
			"libvirt-clients",
			"qemu-system",
			"virtinst",
			"cloud-image-utils",
			"curl",
			"jq",
			"zstd",
			"xz-utils",
		}
	}
}

func hypervisorSetupRuncmds(osFamily string) []any {
	switch strings.ToLower(strings.TrimSpace(osFamily)) {
	case "fedora", "rhel", "redhat", "centos", "rocky", "almalinux":
		return []any{
			"mkdir -p /var/lib/gomi/data/images",
		}
	default:
		return []any{
			configureLibvirtBridgeNetfilterCommand(),
			libvirtTCPAuthNoneCommand(),
			"systemctl enable libvirtd-tcp.socket || true",
			"systemctl stop libvirtd.service || true",
			"systemctl start libvirtd-tcp.socket || true",
			`sh -c 'virsh pool-define-as default dir --target /var/lib/libvirt/images && virsh pool-build default && virsh pool-start default && virsh pool-autostart default || true'`,
			"mkdir -p /var/lib/gomi/data/images",
		}
	}
}

func gomiArchShellSnippet() string {
	return `ARCH=$(uname -m); case "${ARCH}" in x86_64|amd64) ARCH="amd64" ;; aarch64|arm64) ARCH="arm64" ;; *) echo "Unsupported architecture: ${ARCH}"; exit 1 ;; esac`
}

func libvirtTCPAuthNoneCommand() string {
	return `sh -c 'conf=/etc/libvirt/libvirtd.conf; if grep -qE '\''^[[:space:]]*#?[[:space:]]*auth_tcp[[:space:]]*='\'' "$conf"; then sed -i -E '\''s|^[[:space:]]*#?[[:space:]]*auth_tcp[[:space:]]*=.*|auth_tcp = "none"|'\'' "$conf"; else printf '\''\nauth_tcp = "none"\n'\'' >> "$conf"; fi'`
}

func configureLibvirtBridgeNetfilterCommand() string {
	return `sh -c 'modprobe br_netfilter 2>/dev/null || true; cat > /etc/sysctl.d/99-gomi-libvirt-bridge.conf <<EOF
net.bridge.bridge-nf-call-iptables = 0
net.bridge.bridge-nf-call-ip6tables = 0
net.bridge.bridge-nf-call-arptables = 0
EOF
sysctl -p /etc/sysctl.d/99-gomi-libvirt-bridge.conf >/dev/null 2>&1 || true'`
}

func hypervisorRegistrationToken(m *machine.Machine) string {
	if m == nil || m.Provision == nil || m.Provision.Artifacts == nil {
		return ""
	}
	return strings.TrimSpace(m.Provision.Artifacts[machine.ProvisionArtifactHypervisorRegistrationToken])
}

func (h *Handler) ensureHypervisorRegistrationToken(ctx context.Context, m *machine.Machine) (string, error) {
	if token := hypervisorRegistrationToken(m); token != "" {
		return token, nil
	}
	if h == nil || h.hypervisors == nil || m == nil || m.Role != machine.RoleHypervisor {
		return "", nil
	}

	token, err := h.hypervisors.CreateToken(ctx)
	if err != nil {
		return "", fmt.Errorf("create hypervisor registration token: %w", err)
	}
	if m.Provision == nil {
		m.Provision = &machine.ProvisionProgress{}
	}
	if m.Provision.Artifacts == nil {
		m.Provision.Artifacts = map[string]string{}
	}
	m.Provision.Artifacts[machine.ProvisionArtifactHypervisorRegistrationToken] = token.Token
	m.Provision.Artifacts[machine.ProvisionArtifactHypervisorRegistrationTokenExpiresAt] = token.ExpiresAt.Format(time.RFC3339)
	m.UpdatedAt = time.Now().UTC()
	if h.machines != nil {
		if err := h.machines.Store().Upsert(ctx, *m); err != nil {
			return "", fmt.Errorf("store hypervisor registration token: %w", err)
		}
	}
	return token.Token, nil
}
