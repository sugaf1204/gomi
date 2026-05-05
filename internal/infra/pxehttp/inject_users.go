package pxehttp

import (
	"context"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/node"
	"github.com/sugaf1204/gomi/internal/provision"
	"github.com/sugaf1204/gomi/internal/sshkey"
	"github.com/sugaf1204/gomi/internal/vm"
)

// targetUserSpec returns the per-target SSHKeyRefs and optional LoginUser for
// the host being provisioned. When the underlying node type doesn't expose
// these fields (or n is nil), zero values are returned.
func targetUserSpec(n node.Node) (refs []string, loginUser *machine.LoginUserSpec) {
	if n == nil {
		return nil, nil
	}
	switch t := n.(type) {
	case *machine.Machine:
		return t.SSHKeyRefs, t.LoginUser
	case *vm.VirtualMachine:
		// vm.LoginUserSpec mirrors machine.LoginUserSpec on purpose; convert
		// to the canonical type so downstream code can stay generic.
		var converted *machine.LoginUserSpec
		if t.LoginUser != nil {
			converted = &machine.LoginUserSpec{
				Username: t.LoginUser.Username,
				Password: t.LoginUser.Password,
			}
		}
		return t.SSHKeyRefs, converted
	default:
		return nil, nil
	}
}

// injectSSHKeysAndLoginUser merges the registered SSH keys (filtered by the
// target's SSHKeyRefs when set) and the optional per-target login user into
// the cloud-config user-data. It also forces ssh_pwauth=false so password
// SSH login is rejected by sshd. When sshkeyStore is unavailable or yields
// no usable keys and no extra user is configured, the input is returned as
// is (still flipping ssh_pwauth to false for safety).
func (h *Handler) injectSSHKeysAndLoginUser(ctx context.Context, cloudConfig string, n node.Node) string {
	trimmed := strings.TrimSpace(cloudConfig)
	if trimmed == "" {
		return cloudConfig
	}

	cfg := map[string]any{}
	if err := yaml.Unmarshal([]byte(trimmed), &cfg); err != nil {
		return cloudConfig
	}

	refs, loginUser := targetUserSpec(n)

	var allKeys []sshkey.SSHKey
	if h.sshkeys != nil {
		if listed, err := h.sshkeys.List(ctx); err == nil {
			allKeys = listed
		}
	}
	selected := provision.SelectKeysForRefs(allKeys, refs)

	pubKeys := make([]string, 0, len(selected))
	for _, k := range selected {
		if pk := strings.TrimSpace(k.PublicKey); pk != "" {
			pubKeys = append(pubKeys, pk)
		}
	}

	cfg["ssh_pwauth"] = false

	// Top-level ssh_authorized_keys is applied by cloud-init to the
	// distribution's default user when `users: [default, ...]` is in effect.
	if len(pubKeys) > 0 {
		cfg["ssh_authorized_keys"] = pubKeys
	}

	// Only override `users:` when we actually have something to inject;
	// otherwise leave the upstream cloud-config (which may carry curated
	// custom users from an inline cloud-init template) untouched.
	if len(pubKeys) > 0 || loginUser != nil {
		cfg["users"] = provision.BuildCloudInitUsers(pubKeys, loginUser)
	}

	raw, err := yaml.Marshal(cfg)
	if err != nil {
		return cloudConfig
	}
	return "#cloud-config\n" + string(raw)
}
