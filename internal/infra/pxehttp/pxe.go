package pxehttp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	gohttp "net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"gopkg.in/yaml.v3"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/node"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
)

// Deprecated: preseed-based provisioning is being phased out in favor of
// curtin/cloud-init. The default below intentionally creates no user; an
// inline preseed must be supplied by the caller for any usable install.
const defaultDebianPreseed = `# Locale and keyboard
d-i debian-installer/locale string en_US.UTF-8
d-i console-setup/ask_detect boolean false
d-i keyboard-configuration/xkb-keymap select us

# Networking
d-i netcfg/choose_interface select auto
d-i netcfg/get_hostname string gomi-pxe
d-i netcfg/get_domain string local

# Mirror
d-i mirror/country string manual
d-i mirror/http/hostname string deb.debian.org
d-i mirror/http/directory string /debian
d-i mirror/http/proxy string

# Users (root locked, no default user; supply via inline preseed)
d-i passwd/root-login boolean false
d-i passwd/make-user boolean false

# Time
d-i clock-setup/utc boolean true
d-i time/zone string UTC

# Partitioning
d-i partman-auto/disk string /dev/vda
d-i partman-auto/method string regular
d-i partman-lvm/device_remove_lvm boolean true
d-i partman-md/device_remove_md boolean true
d-i partman-auto/choose_recipe select atomic
d-i partman-partitioning/confirm_write_new_label boolean true
d-i partman/choose_partition select finish
d-i partman/confirm boolean true
d-i partman/confirm_nooverwrite boolean true

# Package selection
tasksel tasksel/first multiselect standard, ssh-server
d-i pkgsel/include string qemu-guest-agent sudo curl wget ca-certificates vim less net-tools iproute2 git tmux htop dnsutils
d-i popularity-contest popularity-contest/participate boolean false

# Bootloader and serial console
d-i grub-installer/only_debian boolean true
d-i grub-installer/bootdev string /dev/vda
d-i debian-installer/add-kernel-opts string console=ttyS0,115200n8

d-i preseed/late_command string in-target systemctl enable serial-getty@ttyS0.service

# Finish
d-i finish-install/reboot_in_progress note
d-i debian-installer/exit/poweroff boolean true
`

const defaultLinuxCurtinUserData = `#cloud-config
hostname: gomi-pxe
manage_etc_hosts: true
users:
  - default
ssh_pwauth: false
runcmd:
  - systemctl enable serial-getty@ttyS0.service || true
`

const defaultAutoinstallUserData = `#cloud-config
autoinstall:
  version: 1
  locale: en_US.UTF-8
  keyboard:
    layout: us
  identity:
    hostname: gomi-pxe
    username: ubuntu
    password: "!"
  ssh:
    install-server: true
    allow-pw: false
  storage:
    layout:
      name: direct
`

const defaultNoCloudVendorData = `#cloud-config
{}
`

const targetUEFIBootOrderCleanupScript = `#!/bin/sh
set -eu

[ -d /sys/firmware/efi ] || exit 0
command -v efibootmgr >/dev/null 2>&1 || exit 0

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

efibootmgr -v > "$tmpdir/before" 2>/dev/null || exit 0
boot_current=$(sed -n 's/^BootCurrent:[[:space:]]*//p' "$tmpdir/before" | sed -n '1p' | tr -d ' ' | tr '[:lower:]' '[:upper:]')
boot_next=$(sed -n 's/^BootNext:[[:space:]]*//p' "$tmpdir/before" | sed -n '1p' | tr -d ' ' | tr '[:lower:]' '[:upper:]')
boot_order=$(sed -n 's/^BootOrder:[[:space:]]*//p' "$tmpdir/before" | sed -n '1p' | tr -d ' ' | tr '[:lower:]' '[:upper:]')
[ -n "$boot_order" ] || exit 0

if [ -n "$boot_next" ]; then
	logger -t gomi-bootorder "clearing BootNext $boot_next"
	efibootmgr -N >/dev/null 2>&1 || true
fi

: > "$tmpdir/pxe4"
: > "$tmpdir/pxe6"
while IFS= read -r line; do
	case "$line" in
		Boot[0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f]*)
			id=${line#Boot}
			id=${id%%[!0-9A-Fa-f]*}
			id=$(printf '%s' "$id" | tr '[:lower:]' '[:upper:]')
			printf '%s\n' "$line" | grep -Eiq 'PXE.*IPv4|IPv4\(' && printf '%s\n' "$id" >> "$tmpdir/pxe4"
			printf '%s\n' "$line" | grep -Eiq 'PXE.*IPv6|IPv6\(' && printf '%s\n' "$id" >> "$tmpdir/pxe6"
			;;
	esac
done < "$tmpdir/before"

while IFS= read -r id; do
	[ -n "$id" ] || continue
	logger -t gomi-bootorder "deleting PXE IPv6 boot entry $id"
	efibootmgr -b "$id" -B >/dev/null 2>&1 || true
done < "$tmpdir/pxe6"

new_order=""
append_order() {
	id=$(printf '%s' "$1" | tr '[:lower:]' '[:upper:]')
	[ -n "$id" ] || return 0
	case ",$new_order," in
		*,"$id",*) return 0 ;;
	esac
	if [ -z "$new_order" ]; then
		new_order="$id"
	else
		new_order="$new_order,$id"
	fi
}

for raw_id in $(printf '%s' "$boot_order" | tr ',' ' '); do
	id=$(printf '%s' "$raw_id" | tr '[:lower:]' '[:upper:]')
	grep -qi "^$id$" "$tmpdir/pxe4" && append_order "$id"
done
append_order "$boot_current"
for raw_id in $(printf '%s' "$boot_order" | tr ',' ' '); do
	id=$(printf '%s' "$raw_id" | tr '[:lower:]' '[:upper:]')
	grep -qi "^$id$" "$tmpdir/pxe4" && continue
	grep -qi "^$id$" "$tmpdir/pxe6" && continue
	[ "$id" = "$boot_current" ] && continue
	append_order "$id"
done

[ -n "$new_order" ] || exit 0
logger -t gomi-bootorder "setting BootOrder $new_order"
efibootmgr -o "$new_order" >/dev/null 2>&1 || true
`

const targetUEFIBootOrderCleanupService = `[Unit]
Description=GOMI UEFI BootOrder cleanup
After=sysinit.target
ConditionPathExists=/sys/firmware/efi

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/gomi-fix-uefi-bootorder

[Install]
WantedBy=multi-user.target
`

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

type pxeTarget struct {
	node        node.Node
	installType vm.InstallConfigType
	variant     string
}

func (h *Handler) PXEBootScript(c echo.Context) error {
	base := h.resolvePXEBaseURL(c)
	rawMAC := c.QueryParam("mac")
	target, provisioning, err := h.resolvePXETarget(c.Request().Context(), rawMAC)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	if !provisioning {
		script := renderPXELocalBootScript(base)
		return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8", []byte(script))
	}

	mac := normalizeMAC(rawMAC)
	token := pxeTargetToken(target)
	completeURL := buildPXEInstallCompleteURL(base, token, target.installType)
	script := renderPXEInstallScriptWithVariant(base, target.installType, mac, completeURL, target.variant, target.node)
	return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8", []byte(script))
}

func (h *Handler) PXEPreseed(c echo.Context) error {
	rawMAC := c.QueryParam("mac")
	target, _, err := h.resolvePXETarget(c.Request().Context(), rawMAC)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	base := h.resolvePXEBaseURL(c)
	token := pxeTargetToken(target)
	completeURL := buildPXEInstallCompleteURL(base, token, vm.InstallConfigPreseed)

	var body string
	if inline, found, err := h.resolvePXEInstallInline(c.Request().Context(), rawMAC, vm.InstallConfigPreseed); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	} else if found {
		body = inline
	} else {
		body = defaultDebianPreseed
	}
	hostname := pxeTargetHostname(target)
	body = injectPreseedHostname(body, hostname)
	return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8", []byte(injectPreseedCompletion(body, completeURL, hostname)))
}

func (h *Handler) PXENocloudUserData(c echo.Context) error {
	rawMAC := c.Param("mac")
	ctx := c.Request().Context()
	target, _, err := h.resolvePXETarget(ctx, rawMAC)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	base := h.resolvePXEBaseURL(c)
	token := pxeTargetToken(target)
	sourceType := normalizePXEUserDataInstallType(target.installType)
	completeURL := buildPXEInstallCompleteURL(base, token, sourceType)
	hostname := pxeTargetHostname(target)

	var body string
	if inline, found, err := h.resolvePXEInstallInline(ctx, rawMAC, sourceType); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	} else if found {
		body = inline
	} else {
		body = defaultPXEUserDataByInstallType(sourceType)
	}

	result := injectCloudConfigCompletion(body, completeURL, hostname)

	// Inject the registered SSH keys and any per-target login user. Both the
	// distribution's default user (created by `users: [default]`) and the
	// optional extra user receive the keys; password SSH stays disabled.
	result = h.injectSSHKeysAndLoginUser(ctx, result, target.node)

	if m, ok := target.node.(*machine.Machine); ok {
		result = injectWoLShutdownAgent(result, base, m)
	}

	if m, ok := target.node.(*machine.Machine); ok && m.Role == machine.RoleHypervisor {
		registrationToken, err := h.ensureHypervisorRegistrationToken(ctx, m)
		if err != nil {
			return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		result = injectHypervisorSetup(result, base, m.Name, registrationToken)
	}

	if target.node != nil && target.node.GetIPAssignment() == resource.IPAssignmentStatic {
		if m, ok := target.node.(*machine.Machine); ok && m.Role == machine.RoleHypervisor {
			result = injectBridgedNetplanConfig(result, m, h.resolveSubnetSpec(ctx, target.node))
		} else {
			result = injectNetplanConfigForHost(result, target.node, h.resolveSubnetSpec(ctx, target.node))
		}
	}

	return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8", []byte(result))
}

func (h *Handler) PXENocloudMetaData(c echo.Context) error {
	rawMAC := c.Param("mac")
	ctx := c.Request().Context()
	hostname := "gomi-pxe"

	if n := h.findHostByMAC(ctx, rawMAC); n != nil {
		if name := sanitizeHostnameForLinux(n.NodeDisplayName()); name != "" {
			hostname = name
		}
	}

	body := fmt.Sprintf("instance-id: gomi-%s\nlocal-hostname: %s\n", macToken(rawMAC), hostname)
	return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8", []byte(body))
}

func (h *Handler) PXENocloudVendorData(c echo.Context) error {
	return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8", []byte(defaultNoCloudVendorData))
}

// PXENocloudNetworkConfig serves netplan v2 network-config for the NoCloud datasource.
// cloud-init prioritizes this over any /etc/netplan/*.yaml that ships in the cloud image.
// Always matches by MAC address so the config is NIC-name-agnostic.
func (h *Handler) PXENocloudNetworkConfig(c echo.Context) error {
	rawMAC := c.Param("mac")
	mac := normalizeMAC(rawMAC) // always available from the URL
	ctx := c.Request().Context()

	n := h.findHostByMAC(ctx, rawMAC)

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
			[]byte(buildBridgedNetworkConfig(mac, bridgeName, ip, spec)))
	}

	if n != nil && n.GetIPAssignment() == resource.IPAssignmentStatic {
		if ip := n.StaticIP(); ip != "" {
			spec := h.resolveSubnetSpec(ctx, n)
			return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8",
				[]byte(buildNetworkConfig(mac, ip, spec)))
		}
	}

	return c.Blob(gohttp.StatusOK, "text/plain; charset=utf-8",
		[]byte(buildNetworkConfig(mac, "", nil)))
}

// buildNetworkConfig builds a netplan v2 network-config matched by MAC address.
// If ip is empty, DHCP is configured. Otherwise a static address is configured.
func buildNetworkConfig(mac, ip string, spec *subnet.SubnetSpec) string {
	var sb strings.Builder
	sb.WriteString("version: 2\nethernets:\n  gomi-nic:\n")

	if mac != "" {
		sb.WriteString(fmt.Sprintf("    match:\n      macaddress: %q\n", strings.ToLower(mac)))
		sb.WriteString("    wakeonlan: true\n")
	}

	if ip == "" {
		sb.WriteString("    dhcp4: true\n    dhcp6: false\n")
		return sb.String()
	}

	prefixLen := 24
	if spec != nil && spec.CIDR != "" {
		if _, ipNet, err := net.ParseCIDR(spec.CIDR); err == nil {
			ones, _ := ipNet.Mask.Size()
			prefixLen = ones
		}
	}

	sb.WriteString(fmt.Sprintf("    addresses:\n      - %s/%d\n", ip, prefixLen))
	sb.WriteString("    dhcp4: false\n")

	if gateway := strings.TrimSpace(func() string {
		if spec != nil {
			return spec.DefaultGateway
		}
		return ""
	}()); gateway != "" {
		sb.WriteString(fmt.Sprintf("    routes:\n      - to: default\n        via: %s\n", gateway))
	}

	nameservers := func() []string {
		if spec != nil && len(spec.DNSServers) > 0 {
			return spec.DNSServers
		}
		return []string{"8.8.8.8", "8.8.4.4"}
	}()
	sb.WriteString("    nameservers:\n      addresses:\n")
	for _, ns := range nameservers {
		sb.WriteString(fmt.Sprintf("        - %s\n", ns))
	}

	return sb.String()
}

// injectHypervisorSetup adds libvirt/KVM packages and runcmd entries to a
// cloud-config YAML string so the machine boots as a ready hypervisor.
func injectHypervisorSetup(cloudConfig, pxeBaseURL, hypervisorName, registrationToken string) string {
	trimmed := strings.TrimSpace(cloudConfig)
	if trimmed == "" {
		trimmed = "#cloud-config\n{}"
	}
	cfg := map[string]any{}
	if err := yaml.Unmarshal([]byte(trimmed), &cfg); err != nil {
		return cloudConfig
	}

	hvPackages := []any{
		"libvirt-daemon-system",
		"libvirt-clients",
		"qemu-system",
		"virtinst",
		"cloud-image-utils",
		"curl",
		"jq",
		"zstd",
	}
	hvRuncmds := []any{
		libvirtTCPAuthNoneCommand(),
		"systemctl enable libvirtd-tcp.socket || true",
		"systemctl stop libvirtd.service || true",
		"systemctl start libvirtd-tcp.socket || true",
		`sh -c 'virsh pool-define-as default dir --target /var/lib/libvirt/images && virsh pool-build default && virsh pool-start default && virsh pool-autostart default || true'`,
		"mkdir -p /var/lib/gomi/data/images",
	}

	serverBase := ""
	if base := strings.TrimRight(strings.TrimSpace(pxeBaseURL), "/"); base != "" {
		serverBase = strings.TrimSuffix(base, "/pxe")
		filesBase := serverBase + "/files"
		hvRuncmds = append(hvRuncmds,
			fmt.Sprintf(`sh -c 'ARCH=$(dpkg --print-architecture); curl -sfL -o /usr/bin/gomi-hypervisor "%s/gomi-hypervisor-linux-${ARCH}" && chmod +x /usr/bin/gomi-hypervisor || true'`, filesBase),
		)
	}
	if serverBase != "" && strings.TrimSpace(registrationToken) != "" {
		setupURL := serverBase + "/api/v1/hypervisors/setup-and-register.sh"
		registerCmd := fmt.Sprintf(
			"set -euo pipefail; curl -sfL %s | GOMI_SERVER=%s GOMI_TOKEN=%s GOMI_HOSTNAME=%s bash",
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
	cfg["packages"] = append(pkgList, hvPackages...)

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

func libvirtTCPAuthNoneCommand() string {
	return `sh -c 'conf=/etc/libvirt/libvirtd.conf; if grep -qE '\''^[[:space:]]*#?[[:space:]]*auth_tcp[[:space:]]*='\'' "$conf"; then sed -i -E '\''s|^[[:space:]]*#?[[:space:]]*auth_tcp[[:space:]]*=.*|auth_tcp = "none"|'\'' "$conf"; else printf '\''\nauth_tcp = "none"\n'\'' >> "$conf"; fi'`
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

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

// buildBridgedNetworkConfig builds a netplan v2 config that creates a bridge
// with the matched physical NIC as a member. Used for hypervisor machines so
// VMs can share the same physical network.
func buildBridgedNetworkConfig(mac, bridgeName, ip string, spec *subnet.SubnetSpec) string {
	var sb strings.Builder
	sb.WriteString("version: 2\nethernets:\n  gomi-nic:\n")
	if mac != "" {
		sb.WriteString(fmt.Sprintf("    match:\n      macaddress: %q\n", strings.ToLower(mac)))
		sb.WriteString("    wakeonlan: true\n")
	}
	sb.WriteString("    dhcp4: false\n    dhcp6: false\n")

	sb.WriteString(fmt.Sprintf("bridges:\n  %s:\n    interfaces: [gomi-nic]\n", bridgeName))
	if mac != "" {
		sb.WriteString(fmt.Sprintf("    macaddress: %q\n", strings.ToLower(mac)))
	}

	if ip == "" {
		sb.WriteString("    dhcp4: true\n    dhcp6: false\n")
		return sb.String()
	}

	prefixLen := 24
	if spec != nil && spec.CIDR != "" {
		if _, ipNet, err := net.ParseCIDR(spec.CIDR); err == nil {
			ones, _ := ipNet.Mask.Size()
			prefixLen = ones
		}
	}

	sb.WriteString(fmt.Sprintf("    addresses:\n      - %s/%d\n", ip, prefixLen))
	sb.WriteString("    dhcp4: false\n")

	if gateway := strings.TrimSpace(func() string {
		if spec != nil {
			return spec.DefaultGateway
		}
		return ""
	}()); gateway != "" {
		sb.WriteString(fmt.Sprintf("    routes:\n      - to: default\n        via: %s\n", gateway))
	}

	nameservers := func() []string {
		if spec != nil && len(spec.DNSServers) > 0 {
			return spec.DNSServers
		}
		return []string{"8.8.8.8", "8.8.4.4"}
	}()
	sb.WriteString("    nameservers:\n      addresses:\n")
	for _, ns := range nameservers {
		sb.WriteString(fmt.Sprintf("        - %s\n", ns))
	}

	return sb.String()
}

type pxeInstallCompleteReq struct {
	Token    string `json:"token"`
	Type     string `json:"type,omitempty"`
	IP       string `json:"ip,omitempty"`
	MAC      string `json:"mac,omitempty"`
	Hostname string `json:"hostname,omitempty"`
}

func (h *Handler) PXEInstallComplete(c echo.Context) error {
	token := strings.TrimSpace(c.QueryParam("token"))
	source := strings.TrimSpace(c.QueryParam("type"))

	var report node.InstallCompleteReport
	if c.Request().ContentLength > 0 {
		var req pxeInstallCompleteReq
		if err := c.Bind(&req); err != nil {
			return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "invalid body"})
		}
		if token == "" {
			token = strings.TrimSpace(req.Token)
		}
		if source == "" {
			source = strings.TrimSpace(req.Type)
		}
		report = node.InstallCompleteReport{
			IP:       strings.TrimSpace(req.IP),
			MAC:      strings.TrimSpace(req.MAC),
			Hostname: strings.TrimSpace(req.Hostname),
		}
	}
	if token == "" {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "token is required"})
	}
	source = normalizeCompletionSource(source)

	targetVM, err := h.findVirtualMachineByProvisionToken(c.Request().Context(), token)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if targetVM != nil {
		if strings.TrimSpace(targetVM.Provisioning.CompletionToken) != token {
			return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "provisioning token not found"})
		}
		if !targetVM.Provisioning.Active {
			if targetVM.Provisioning.CompletedAt == nil {
				return c.JSON(gohttp.StatusConflict, map[string]string{"error": "provisioning token is expired"})
			}
			return c.JSON(gohttp.StatusOK, map[string]any{
				"status": "already-finalized",
				"vm":     targetVM,
			})
		}

		now := time.Now().UTC()
		updated := *targetVM
		updated.Provisioning.Active = false
		updated.Provisioning.CompletedAt = httputil.TimePtr(now)
		updated.Provisioning.LastSignalAt = httputil.TimePtr(now)
		updated.Provisioning.CompletionSource = source
		updated.LastError = ""
		updated.UpdatedAt = now
		updated.ApplyInstallCompleteReport(report)
		if err := h.vms.Store().Upsert(c.Request().Context(), updated); err != nil {
			return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		if h.vmRuntimeSyncer != nil {
			leaseIPByMAC, leaseErr := h.leaseIPsByMAC(c.Request().Context())
			if leaseErr != nil {
				leaseIPByMAC = nil
			}
			if synced, syncErr := h.vmRuntimeSyncer.Sync(c.Request().Context(), updated, leaseIPByMAC); syncErr == nil {
				updated = synced
			}
		}
		if updated.Phase == vm.PhaseProvisioning {
			updated.Phase = vm.PhaseRunning
			updated.UpdatedAt = now
			if err := h.vms.Store().Upsert(c.Request().Context(), updated); err != nil {
				return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
			}
		}

		if h.authStore != nil {
			httputil.CreateAudit(c, h.authStore, updated.Name, "complete-vm-provisioning", "success", "vm provisioning completed by pxe signal", map[string]string{
				"source": source,
			})
		}
		return c.JSON(gohttp.StatusOK, map[string]any{
			"status": "ok",
			"vm":     updated,
		})
	}

	targetMachine, err := h.findMachineByProvisionToken(c.Request().Context(), token)
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	if targetMachine == nil {
		return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "provisioning token not found"})
	}
	if targetMachine.Provision == nil || strings.TrimSpace(targetMachine.Provision.CompletionToken) != token {
		return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "provisioning token not found"})
	}
	if !targetMachine.Provision.Active {
		if targetMachine.Provision.CompletedAt == nil {
			return c.JSON(gohttp.StatusConflict, map[string]string{"error": "provisioning token is expired"})
		}
		return c.JSON(gohttp.StatusOK, map[string]any{
			"status":  "already-finalized",
			"machine": targetMachine,
		})
	}

	now := time.Now().UTC()
	updatedMachine := *targetMachine
	if updatedMachine.Provision == nil {
		updatedMachine.Provision = &machine.ProvisionProgress{}
	}
	updatedMachine.Provision.Active = false
	updatedMachine.Provision.CompletedAt = httputil.TimePtr(now)
	updatedMachine.Provision.LastSignalAt = httputil.TimePtr(now)
	updatedMachine.Provision.CompletionSource = source
	updatedMachine.Provision.Message = "provisioning completed"
	updatedMachine.Phase = machine.PhaseReady
	updatedMachine.LastError = ""
	updatedMachine.UpdatedAt = now
	updatedMachine.ApplyInstallCompleteReport(report)
	updatedMachine.Provision.Message = h.finalizeBIOSBootOrder(c.Request().Context(), updatedMachine)
	if err := h.machines.Store().Upsert(c.Request().Context(), updatedMachine); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	// Auto-create Hypervisor entity when a hypervisor-role machine finishes provisioning.
	if updatedMachine.Role == machine.RoleHypervisor && h.hypervisors != nil {
		hvIP := updatedMachine.IP
		if hvIP == "" && report.IP != "" {
			hvIP = report.IP
		}
		bridgeName := updatedMachine.BridgeName
		if bridgeName == "" {
			bridgeName = "br0"
		}
		// Only create if no hypervisor with this name exists yet — avoid
		// resetting phase/capacity of an already-running hypervisor on re-provision.
		if _, err := h.hypervisors.Get(c.Request().Context(), updatedMachine.Name); err != nil {
			newHV := hypervisor.Hypervisor{
				Name: updatedMachine.Name,
				Connection: hypervisor.ConnectionSpec{
					Type: hypervisor.ConnectionTCP,
					Host: hvIP,
					Port: 16509,
				},
				MachineRef: updatedMachine.Name,
				BridgeName: bridgeName,
				Phase:      hypervisor.PhasePending,
			}
			if _, createErr := h.hypervisors.Create(c.Request().Context(), newHV); createErr != nil {
				log.Printf("auto-create hypervisor for machine %s: %v", updatedMachine.Name, createErr)
			} else {
				log.Printf("auto-created hypervisor %s (bridge=%s, ip=%s)", updatedMachine.Name, bridgeName, hvIP)
			}
		} else {
			log.Printf("hypervisor %s already exists, skipping auto-create", updatedMachine.Name)
		}
	}

	if h.authStore != nil {
		httputil.CreateAudit(c, h.authStore, updatedMachine.Name, "complete-machine-provisioning", "success", "machine provisioning completed by pxe signal", map[string]string{
			"source": source,
		})
	}
	return c.JSON(gohttp.StatusOK, map[string]any{
		"status":  "ok",
		"machine": updatedMachine,
	})
}

func (h *Handler) finalizeBIOSBootOrder(ctx context.Context, m machine.Machine) string {
	return h.configureBIOSBootOrder(ctx, m, "provisioning completed")
}

func (h *Handler) configureBIOSBootOrder(ctx context.Context, m machine.Machine, baseMessage string) string {
	if strings.TrimSpace(baseMessage) == "" {
		baseMessage = "provisioning state updated"
	}
	if m.Firmware != machine.FirmwareBIOS || h.powerExecutor == nil {
		return baseMessage
	}
	if m.Power.Type != power.PowerTypeWebhook || m.Power.Webhook == nil || strings.TrimSpace(m.Power.Webhook.BootOrderURL) == "" {
		return baseMessage
	}
	mi := power.MachineInfo{
		Name:     m.Name,
		Hostname: m.Hostname,
		MAC:      m.MAC,
		IP:       m.IP,
		Power:    m.Power,
	}
	if err := h.powerExecutor.ConfigureBootOrder(ctx, mi, power.DefaultBIOSBootOrder); err != nil {
		log.Printf("pxe: machine=%s bios boot order update failed: %v", m.Name, err)
		return baseMessage + "; BIOS boot order update failed"
	}
	log.Printf("pxe: machine=%s bios boot order updated to %v", m.Name, power.DefaultBIOSBootOrder)
	return baseMessage + "; BIOS boot order updated"
}

func (h *Handler) PXEFile(c echo.Context) error {
	root := strings.TrimSpace(h.pxeFilesDir)
	if root == "" {
		root = strings.TrimSpace(h.pxeTFTPRoot)
	}
	if root == "" {
		return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "pxe file root is not configured"})
	}

	raw := strings.TrimPrefix(c.Param("*"), "/")
	rel, err := sanitizePXEPath(raw)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "invalid pxe asset path"})
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	if _, err := os.Stat(full); err != nil {
		if os.IsNotExist(err) {
			return c.JSON(gohttp.StatusNotFound, map[string]string{"error": "pxe asset not found"})
		}
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.File(full)
}

// --- helpers ---

func (h *Handler) resolvePXEBaseURL(c echo.Context) string {
	if strings.TrimSpace(h.pxeHTTPBaseURL) != "" {
		return strings.TrimRight(h.pxeHTTPBaseURL, "/")
	}
	hostStr := strings.TrimSpace(c.Request().Host)
	if hostStr == "" {
		hostStr = "127.0.0.1:8080"
	}
	return "http://" + hostStr + "/pxe"
}

func sanitizePXEPath(raw string) (string, error) {
	p := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	p = strings.TrimPrefix(p, "/")
	p = path.Clean(p)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") || strings.Contains(p, "/../") {
		return "", fmt.Errorf("invalid path")
	}
	return p, nil
}

func envBool(name string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func renderPXEInstallScriptWithVariant(base string, installType vm.InstallConfigType, mac, completeURL, variant string, n node.Node) string {
	profiles := defaultPXEBootScriptProfiles
	if _, ok := n.(*machine.Machine); ok {
		profiles = defaultBareMetalPXEBootScriptProfiles
	}
	return profiles.Script(installType, pxeBootScriptContext{
		baseURL:            base,
		mac:                mac,
		bootIF:             bootIFParam(mac),
		installCompleteURL: completeURL,
		variant:            variant,
		serialConsole:      envBool("GOMI_PXE_SERIAL_CONSOLE"),
	})
}

// RenderNoCloudLineConfig is exported so that api/vm.go can pass it as a callback to vm.Deployer.
func RenderNoCloudLineConfig(base string, installType vm.InstallConfigType, mac string) string {
	return defaultPXEBootScriptProfiles.NoCloudLineConfig(installType, pxeBootScriptContext{
		baseURL: base,
		mac:     mac,
	})
}

func renderPXELocalBootScript(base string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = "http://127.0.0.1:8080/pxe"
	}
	return fmt.Sprintf(`#!ipxe
iseq ${platform} efi && chain --autofree tftp://${next-server}/grubnetx64.efi ||
iseq ${platform} efi && chain --autofree %s/files/grubnetx64.efi ||
iseq ${platform} efi && exit 1 ||
sanboot --no-describe --drive 0x80 || exit
`, base)
}

func (h *Handler) resolvePXETarget(ctx context.Context, rawMAC string) (pxeTarget, bool, error) {
	normalized := normalizeMAC(rawMAC)
	token := macToken(rawMAC)
	if normalized == "" && token == "" {
		return pxeTarget{installType: vm.InstallConfigPreseed}, true, nil
	}

	targetVM, _, vmErr := h.findVirtualMachineByMAC(ctx, rawMAC)
	if vmErr != nil {
		return pxeTarget{installType: vm.InstallConfigPreseed}, false, vmErr
	}
	if targetVM != nil && targetVM.IsProvisioningActive() {
		return pxeTarget{
			node:        targetVM,
			installType: vm.InstallConfigType(targetVM.PXEInstallType()),
			variant:     h.resolveOSImageVariant(ctx, targetVM.OSImageVariantRef()),
		}, true, nil
	}

	targetMachine, _, machineErr := h.findMachineByMAC(ctx, rawMAC)
	if machineErr != nil {
		return pxeTarget{installType: vm.InstallConfigPreseed}, false, machineErr
	}
	if targetMachine != nil && targetMachine.IsProvisioningActive() {
		if machineImageApplied(targetMachine) {
			return pxeTarget{node: targetMachine, installType: vm.InstallConfigCurtin}, false, nil
		}
		return pxeTarget{
			node:        targetMachine,
			installType: vm.InstallConfigType(targetMachine.PXEInstallType()),
			variant:     h.resolveOSImageVariant(ctx, targetMachine.OSImageVariantRef()),
		}, true, nil
	}

	return pxeTarget{installType: vm.InstallConfigPreseed}, false, nil
}

func (h *Handler) resolveOSImageVariant(ctx context.Context, osImageRef string) string {
	if h.osimages == nil || strings.TrimSpace(osImageRef) == "" {
		return ""
	}
	img, err := h.osimages.Get(ctx, strings.TrimSpace(osImageRef))
	if err != nil {
		return ""
	}
	return string(img.Variant)
}

func osImageVariantIsDesktop(variant string) bool {
	return strings.ToLower(strings.TrimSpace(variant)) == string(osimage.VariantDesktop)
}

func (h *Handler) findHostByMAC(ctx context.Context, rawMAC string) node.Node {
	if targetVM, foundMAC, err := h.findVirtualMachineByMAC(ctx, rawMAC); err == nil && targetVM != nil && foundMAC {
		return targetVM
	}
	if targetMachine, foundMAC, err := h.findMachineByMAC(ctx, rawMAC); err == nil && targetMachine != nil && foundMAC {
		return targetMachine
	}
	return nil
}

func normalizePXEUserDataInstallType(installType vm.InstallConfigType) vm.InstallConfigType {
	switch installType {
	case vm.InstallConfigCurtin:
		return vm.InstallConfigCurtin
	default:
		return vm.InstallConfigCurtin
	}
}

func (h *Handler) resolvePXEInstallInline(ctx context.Context, rawMAC string, expectedType vm.InstallConfigType) (string, bool, error) {
	n := h.findHostByMAC(ctx, rawMAC)
	if n == nil {
		return "", false, nil
	}

	if inline := n.CloudInitInline(resource.InstallType(expectedType)); inline != "" {
		return inline + "\n", true, nil
	}

	if !supportsCloudInitFallbackInstallType(expectedType) {
		return "", false, nil
	}

	ref := n.CloudInitRefForDeploy()
	if ref == "" {
		return "", false, nil
	}
	return h.resolveCloudInitUserData(ctx, ref)
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

func supportsCloudInitFallbackInstallType(installType vm.InstallConfigType) bool {
	return installType == vm.InstallConfigCurtin
}

func (h *Handler) findVirtualMachineByProvisionToken(ctx context.Context, token string) (*vm.VirtualMachine, error) {
	normalized := strings.TrimSpace(token)
	if normalized == "" || h.vms == nil {
		return nil, nil
	}
	items, err := h.vms.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if strings.TrimSpace(items[i].Provisioning.CompletionToken) == normalized {
			copy := items[i]
			return &copy, nil
		}
	}
	return nil, nil
}

func (h *Handler) findMachineByProvisionToken(ctx context.Context, token string) (*machine.Machine, error) {
	normalized := strings.TrimSpace(token)
	if normalized == "" || h.machines == nil {
		return nil, nil
	}
	items, err := h.machines.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].Provision == nil {
			continue
		}
		if strings.TrimSpace(items[i].Provision.CompletionToken) == normalized {
			copy := items[i]
			return &copy, nil
		}
	}
	return nil, nil
}

func normalizeCompletionSource(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch vm.InstallConfigType(normalized) {
	case vm.InstallConfigCurtin:
		return string(vm.InstallConfigCurtin)
	case vm.InstallConfigPreseed:
		return string(vm.InstallConfigPreseed)
	default:
		return "unknown"
	}
}

func pxeTargetToken(target pxeTarget) string {
	if target.node == nil {
		return ""
	}
	return target.node.ProvisionToken()
}

func pxeTargetHostname(target pxeTarget) string {
	if target.node == nil {
		return ""
	}
	return target.node.NodeDisplayName()
}

func parseTokenAndTypeFromURL(completeURL string) (string, string) {
	u, err := url.Parse(completeURL)
	if err != nil {
		return "", ""
	}
	return u.Query().Get("token"), u.Query().Get("type")
}

func buildPXEInstallCompleteURL(base, token string, source vm.InstallConfigType) string {
	if strings.TrimSpace(token) == "" {
		return ""
	}
	q := url.Values{}
	q.Set("token", strings.TrimSpace(token))
	if source != "" {
		q.Set("type", string(source))
	}
	return strings.TrimRight(base, "/") + "/install-complete?" + q.Encode()
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
		`in-target /bin/sh -c 'IP=$(hostname -I 2>/dev/null | awk "{print \$1}"); DEV=$(ip -o route get 1 2>/dev/null | awk "{for(i=1;i<=NF;i++){if(\$i==\"dev\"){print \$(i+1);exit}}}"); MAC=$(cat /sys/class/net/${DEV}/address 2>/dev/null); curl -fsS -X POST -H "Content-Type: application/json" -d "{\"token\":\"%s\",\"type\":\"%s\",\"ip\":\"${IP}\",\"mac\":\"${MAC}\"}" "%s" || curl -fsS -X POST "%s" || true'`,
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
			`curtin in-target -- sh -c 'IP=$(hostname -I 2>/dev/null | awk "{print \$1}"); DEV=$(ip -o route get 1 2>/dev/null | awk "{for(i=1;i<=NF;i++){if(\$i==\"dev\"){print \$(i+1);exit}}}"); MAC=$(cat /sys/class/net/${DEV}/address 2>/dev/null); curl -fsS -X POST -H "Content-Type: application/json" -d "{\"token\":\"%s\",\"type\":\"%s\",\"ip\":\"${IP}\",\"mac\":\"${MAC}\"}" "%s" || curl -fsS -X POST "%s" || true'`,
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

func injectCloudConfigCompletion(content, completeURL, hostname string) string {
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
		escaped := strings.ReplaceAll(completeURL, `"`, `\"`)
		callback := fmt.Sprintf(
			`sh -c 'IP=$(hostname -I 2>/dev/null | awk "{print \$1}"); DEV=$(ip -o route get 1 2>/dev/null | awk "{for(i=1;i<=NF;i++){if(\$i==\"dev\"){print \$(i+1);exit}}}"); MAC=$(cat /sys/class/net/${DEV}/address 2>/dev/null); curl -fsS -X POST -H "Content-Type: application/json" -d "{\"token\":\"%s\",\"type\":\"%s\",\"ip\":\"${IP}\",\"mac\":\"${MAC}\"}" "%s" || curl -fsS -X POST "%s" || true'`,
			"__TOKEN__", "__TYPE__", escaped, escaped,
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

func injectTargetUEFIBootOrderCleanup(cfg map[string]any, runCmd *[]string) {
	writeFiles := []any{}
	if existing, ok := cfg["write_files"].([]any); ok {
		writeFiles = existing
	}
	writeFiles = append(writeFiles, map[string]any{
		"path":        "/usr/local/sbin/gomi-fix-uefi-bootorder",
		"permissions": "0755",
		"content":     targetUEFIBootOrderCleanupScript,
	}, map[string]any{
		"path":        "/etc/systemd/system/gomi-bootorder-cleanup.service",
		"permissions": "0644",
		"content":     targetUEFIBootOrderCleanupService,
	})
	cfg["write_files"] = writeFiles
	*runCmd = append(*runCmd, "systemctl enable --now gomi-bootorder-cleanup.service || /usr/local/sbin/gomi-fix-uefi-bootorder || true")
}

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

func (h *Handler) findVirtualMachineByMAC(ctx context.Context, rawMAC string) (*vm.VirtualMachine, bool, error) {
	normalized := normalizeMAC(rawMAC)
	token := macToken(rawMAC)
	if normalized == "" && token == "" {
		return nil, false, nil
	}
	if h.vms == nil {
		return nil, true, nil
	}

	items, err := h.vms.List(ctx)
	if err != nil {
		return nil, true, err
	}
	for i := range items {
		if vmHasMAC(items[i], normalized, token) {
			copy := items[i]
			return &copy, true, nil
		}
	}
	return nil, true, nil
}

func vmHasMAC(vmi vm.VirtualMachine, normalized, token string) bool {
	matches := func(raw string) bool {
		candidate := normalizeMAC(raw)
		if normalized != "" && candidate == normalized {
			return true
		}
		if token != "" && macToken(candidate) == token {
			return true
		}
		return false
	}

	for _, nic := range vmi.Network {
		if matches(nic.MAC) {
			return true
		}
	}
	for _, nic := range vmi.NetworkInterfaces {
		if matches(nic.MAC) {
			return true
		}
	}
	return false
}

func (h *Handler) findMachineByMAC(ctx context.Context, rawMAC string) (*machine.Machine, bool, error) {
	normalized := normalizeMAC(rawMAC)
	token := macToken(rawMAC)
	if normalized == "" && token == "" {
		return nil, false, nil
	}
	if h.machines == nil {
		return nil, true, nil
	}

	items, err := h.machines.List(ctx)
	if err != nil {
		return nil, true, err
	}
	for i := range items {
		if machineHasMAC(items[i], normalized, token) {
			copy := items[i]
			return &copy, true, nil
		}
	}
	return nil, true, nil
}

func machineHasMAC(m machine.Machine, normalized, token string) bool {
	candidate := normalizeMAC(m.MAC)
	if normalized != "" && candidate == normalized {
		return true
	}
	if token != "" && macToken(candidate) == token {
		return true
	}
	return false
}

var nonHexPattern = regexp.MustCompile(`[^0-9a-f]`)

func normalizeMAC(raw string) string {
	m := strings.ToLower(strings.TrimSpace(raw))
	if m == "" {
		return ""
	}
	m = strings.ReplaceAll(m, "-", ":")
	if strings.Count(m, ":") == 5 {
		return m
	}
	token := macToken(m)
	if len(token) != 12 {
		return ""
	}
	parts := make([]string, 0, 6)
	for i := 0; i < len(token); i += 2 {
		parts = append(parts, token[i:i+2])
	}
	return strings.Join(parts, ":")
}

func macToken(raw string) string {
	m := strings.ToLower(strings.TrimSpace(raw))
	if m == "" {
		return ""
	}
	return nonHexPattern.ReplaceAllString(m, "")
}

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

type netplanParams struct {
	IP          string
	MAC         string
	Gateway     string
	NameServers []string
}

func injectNetplanConfigFromParams(cloudConfig string, params netplanParams, spec *subnet.SubnetSpec) string {
	ip := net.ParseIP(strings.TrimSpace(params.IP))
	if ip == nil {
		return cloudConfig
	}

	prefixLen := 24
	if spec != nil && spec.CIDR != "" {
		if _, ipNet, err := net.ParseCIDR(spec.CIDR); err == nil {
			ones, _ := ipNet.Mask.Size()
			prefixLen = ones
		}
	}

	gateway := strings.TrimSpace(params.Gateway)
	if gateway == "" && spec != nil {
		gateway = strings.TrimSpace(spec.DefaultGateway)
	}

	nameServers := params.NameServers
	if len(nameServers) == 0 && spec != nil {
		nameServers = spec.DNSServers
	}

	mac := strings.ToLower(strings.TrimSpace(params.MAC))

	var np strings.Builder
	np.WriteString("network:\n")
	np.WriteString("  version: 2\n")
	np.WriteString("  ethernets:\n")
	np.WriteString("    id0:\n")
	np.WriteString("      match:\n")
	np.WriteString(fmt.Sprintf("        macaddress: \"%s\"\n", mac))
	np.WriteString("      wakeonlan: true\n")
	np.WriteString("      dhcp4: false\n")
	np.WriteString(fmt.Sprintf("      addresses:\n        - %s/%d\n", ip.String(), prefixLen))
	if len(nameServers) > 0 {
		np.WriteString("      nameservers:\n        addresses:\n")
		for _, ns := range nameServers {
			ns = strings.TrimSpace(ns)
			if ns != "" {
				np.WriteString(fmt.Sprintf("          - \"%s\"\n", ns))
			}
		}
	}
	if gateway != "" {
		np.WriteString("      routes:\n        - to: default\n")
		np.WriteString(fmt.Sprintf("          via: %s\n", gateway))
	}

	netplanYAML := np.String()

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
		"path":        "/etc/netplan/99-gomi-static.yaml",
		"content":     netplanYAML,
		"permissions": "0644",
	})
	cfg["write_files"] = writeFiles

	netplanCmds := []any{
		"rm -f /etc/netplan/50-cloud-init.yaml /etc/netplan/01-netcfg.yaml /etc/netplan/00-installer-config.yaml",
		"netplan apply",
	}
	runCmd := []any{}
	if existing, ok := cfg["runcmd"].([]any); ok {
		runCmd = existing
	}
	runCmd = append(netplanCmds, runCmd...)
	cfg["runcmd"] = runCmd

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return cloudConfig
	}
	return "#cloud-config\n" + string(raw)
}

// injectBridgedNetplanConfig injects a bridged netplan config into cloud-config
// write_files so that the hypervisor machine gets a bridge with a static IP.
func injectBridgedNetplanConfig(cloudConfig string, m *machine.Machine, spec *subnet.SubnetSpec) string {
	ip := m.StaticIP()
	if ip == "" {
		return cloudConfig
	}
	mac := strings.ToLower(strings.TrimSpace(m.PrimaryMAC()))
	bridgeName := m.BridgeName
	if bridgeName == "" {
		bridgeName = "br0"
	}

	prefixLen := 24
	if spec != nil && spec.CIDR != "" {
		if _, ipNet, err := net.ParseCIDR(spec.CIDR); err == nil {
			ones, _ := ipNet.Mask.Size()
			prefixLen = ones
		}
	}

	var np strings.Builder
	np.WriteString("network:\n")
	np.WriteString("  version: 2\n")
	np.WriteString("  ethernets:\n")
	np.WriteString("    gomi-nic:\n")
	np.WriteString(fmt.Sprintf("      match:\n        macaddress: \"%s\"\n", mac))
	np.WriteString("      wakeonlan: true\n")
	np.WriteString("      dhcp4: false\n      dhcp6: false\n")
	np.WriteString("  bridges:\n")
	np.WriteString(fmt.Sprintf("    %s:\n", bridgeName))
	np.WriteString("      interfaces: [gomi-nic]\n")
	np.WriteString(fmt.Sprintf("      macaddress: \"%s\"\n", mac))
	np.WriteString(fmt.Sprintf("      addresses:\n        - %s/%d\n", ip, prefixLen))
	np.WriteString("      dhcp4: false\n")

	if gateway := strings.TrimSpace(func() string {
		if spec != nil {
			return spec.DefaultGateway
		}
		return ""
	}()); gateway != "" {
		np.WriteString(fmt.Sprintf("      routes:\n        - to: default\n          via: %s\n", gateway))
	}

	nameservers := func() []string {
		if spec != nil && len(spec.DNSServers) > 0 {
			return spec.DNSServers
		}
		return []string{"8.8.8.8", "8.8.4.4"}
	}()
	np.WriteString("      nameservers:\n        addresses:\n")
	for _, ns := range nameservers {
		ns = strings.TrimSpace(ns)
		if ns != "" {
			np.WriteString(fmt.Sprintf("          - \"%s\"\n", ns))
		}
	}

	netplanYAML := np.String()

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
		"path":        "/etc/netplan/99-gomi-static.yaml",
		"content":     netplanYAML,
		"permissions": "0644",
	})
	cfg["write_files"] = writeFiles

	netplanCmds := []any{
		"rm -f /etc/netplan/50-cloud-init.yaml /etc/netplan/01-netcfg.yaml /etc/netplan/00-installer-config.yaml",
		"netplan apply",
	}
	runCmd := []any{}
	if existing, ok := cfg["runcmd"].([]any); ok {
		runCmd = existing
	}
	runCmd = append(netplanCmds, runCmd...)
	cfg["runcmd"] = runCmd

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return cloudConfig
	}
	return "#cloud-config\n" + string(raw)
}

func injectNetplanConfigForHost(cloudConfig string, h node.Node, spec *subnet.SubnetSpec) string {
	ip := h.StaticIP()
	if ip == "" {
		return cloudConfig
	}
	return injectNetplanConfigFromParams(cloudConfig, netplanParams{
		IP:  ip,
		MAC: h.PrimaryMAC(),
	}, spec)
}

func (h *Handler) leaseIPsByMAC(ctx context.Context) (map[string]string, error) {
	if h.leaseStore == nil {
		return nil, nil
	}
	leases, err := h.leaseStore.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(leases))
	for _, lease := range leases {
		mac := strings.ToLower(strings.TrimSpace(lease.MAC))
		ip := strings.TrimSpace(lease.IP)
		if mac == "" || ip == "" {
			continue
		}
		out[mac] = ip
	}
	return out, nil
}
