package provision

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/sshkey"
)

// BuildArtifacts generates provision artifacts for a machine.
// This is a pure function with no side effects.
func BuildArtifacts(m machine.Machine, bootBaseURL string, sshKeys []sshkey.SSHKey) (artifacts map[string]string, installCfg string, err error) {
	baseURL := strings.TrimRight(bootBaseURL, "/")
	prefix := fmt.Sprintf("%s/assets/%s", baseURL, m.Name)
	artifacts = map[string]string{}

	installCfg = buildLinuxCloudInit(m, sshKeys)
	artifacts["kernel"] = prefix + "/linux/boot-kernel"
	artifacts["initrd"] = prefix + "/linux/boot-initrd"
	artifacts["rootfs"] = prefix + "/linux/rootfs.squashfs"
	artifacts["configURL"] = prefix + "/nocloud/"
	artifacts["pxeConfig"] = buildPXEEntry(m, artifacts)
	artifacts["coreDHCP"] = buildCoreDHCPSnippet(m, "10.0.0.2")
	return artifacts, installCfg, nil
}

// SelectKeysForRefs returns the subset of allKeys whose Name appears in refs.
// If refs is empty, no keys are selected.
func SelectKeysForRefs(allKeys []sshkey.SSHKey, refs []string) []sshkey.SSHKey {
	if len(refs) == 0 {
		return nil
	}
	wanted := make(map[string]struct{}, len(refs))
	for _, r := range refs {
		if r = strings.TrimSpace(r); r != "" {
			wanted[r] = struct{}{}
		}
	}
	out := make([]sshkey.SSHKey, 0, len(wanted))
	for _, k := range allKeys {
		if _, ok := wanted[k.Name]; ok {
			out = append(out, k)
		}
	}
	return out
}

func collectPublicKeys(keys []sshkey.SSHKey) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		pk := strings.TrimSpace(k.PublicKey)
		if pk != "" {
			out = append(out, pk)
		}
	}
	return out
}

// BuildCloudInitUsers builds the cloud-config `users:` list for the given
// optional extra login user. The first entry is the YAML scalar `default`,
// which is cloud-init's magic token to materialise the distribution's
// stock user (ubuntu/debian/fedora/...). When loginUser is set, an extra
// account is appended carrying the SSH keys and an optional plaintext
// password. The keys for the default user are NOT included here; callers
// should set top-level `ssh_authorized_keys` instead — cloud-init applies
// that field to the default user automatically.
//
// Pure function: callers can reuse it for both physical machine and VM
// cloud-init generation, keeping the two surfaces symmetric.
func BuildCloudInitUsers(pubKeys []string, loginUser *machine.LoginUserSpec) []any {
	users := []any{"default"}

	if loginUser != nil && strings.TrimSpace(loginUser.Username) != "" {
		entry := map[string]any{
			"name":   strings.TrimSpace(loginUser.Username),
			"groups": []string{"adm", "sudo"},
			"shell":  "/bin/bash",
			"sudo":   "ALL=(ALL) NOPASSWD:ALL",
		}
		if pwd := strings.TrimSpace(loginUser.Password); pwd != "" {
			entry["lock_passwd"] = false
			entry["plain_text_passwd"] = pwd
		} else {
			entry["lock_passwd"] = true
		}
		if len(pubKeys) > 0 {
			entry["ssh_authorized_keys"] = pubKeys
		}
		users = append(users, entry)
	}
	return users
}

func buildLinuxCloudInit(m machine.Machine, sshKeys []sshkey.SSHKey) string {
	selected := SelectKeysForRefs(sshKeys, m.SSHKeyRefs)
	pubKeys := collectPublicKeys(selected)
	users := BuildCloudInitUsers(pubKeys, m.LoginUser)

	cfg := map[string]any{
		"hostname":         m.Hostname,
		"manage_etc_hosts": true,
		// SSH password authentication is forbidden across the deployment
		// surface; cloud-init enforces this via sshd_config.d/50-cloud-init.conf.
		"ssh_pwauth": false,
		"users":      users,
		"runcmd": []string{
			"systemctl enable ssh || true",
			"systemctl enable serial-getty@ttyS0.service || true",
		},
	}
	if len(pubKeys) > 0 {
		// Top-level ssh_authorized_keys are applied by cloud-init to the
		// distribution's default user (the YAML `default` token in users[0]).
		cfg["ssh_authorized_keys"] = pubKeys
	}

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return "# failed to render cloud-config"
	}
	return "#cloud-config\n" + string(raw)
}

func buildPXEEntry(m machine.Machine, artifacts map[string]string) string {
	label := m.Name
	if m.Firmware == machine.FirmwareUEFI {
		return fmt.Sprintf("set default=install\nmenuentry 'Install %s' {\n  linux %s initrd=boot-initrd ip=dhcp overlayroot=tmpfs:recurse=0 rw root=squash:%s ds=nocloud-net;s=%s\n  initrd %s\n}\n", label, artifacts["kernel"], artifacts["rootfs"], artifacts["configURL"], artifacts["initrd"])
	}
	return fmt.Sprintf("DEFAULT install\nLABEL install\n  KERNEL %s\n  INITRD %s\n  APPEND initrd=boot-initrd ip=dhcp overlayroot=tmpfs:recurse=0 rw root=squash:%s ds=nocloud-net;s=%s\n", artifacts["kernel"], artifacts["initrd"], artifacts["rootfs"], artifacts["configURL"])
}

func buildCoreDHCPSnippet(m machine.Machine, tftpServer string) string {
	if m.Firmware == machine.FirmwareUEFI {
		return fmt.Sprintf("# %s\nif option arch == 00:07 {\n  filename \"ipxe.efi\"\n  next-server %s\n}\n", m.Name, tftpServer)
	}
	return fmt.Sprintf("# %s\nif option arch == 00:00 {\n  filename \"undionly.kpxe\"\n  next-server %s\n}\n", m.Name, tftpServer)
}
