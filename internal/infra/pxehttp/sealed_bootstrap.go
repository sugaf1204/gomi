package pxehttp

import (
	"encoding/base64"
	"fmt"
	"github.com/sugaf1204/gomi/internal/machine"
	"gopkg.in/yaml.v3"
	"regexp"
	"strings"
)

var fingerprintPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var claimOwnerPattern = regexp.MustCompile(`^capi-[a-z0-9-]+$`)

// All plaintext is produced on the physical installer, in a root-only /run
// directory. The public curtin configuration contains only ciphertext and the
// fingerprint pinned during administrator enrollment.
func configureSealedBootstrap(cfg *curtinConfig, m *machine.Machine, disk string) error {
	seed := m.SealedBootstrap
	if seed == nil {
		return nil
	}
	if !fingerprintPattern.MatchString(seed.KeyFingerprint) || !claimOwnerPattern.MatchString(seed.Owner) {
		return fmt.Errorf("invalid enrolled bootstrap identity")
	}
	if !seed.Cleanup && len(seed.Envelope) == 0 {
		return fmt.Errorf("sealed bootstrap envelope is required")
	}
	stage := "/run/gomi-sealed-bootstrap"
	early := fmt.Sprintf(`set -eu; umask 077; install -d -m 700 %[1]s; gomi-preserve-identity --disk %s --fingerprint %s --output %[1]s/identity`, stage, shellQuote(disk), shellQuote(seed.KeyFingerprint))
	if !seed.Cleanup {
		early += fmt.Sprintf(`; printf '%%s' %s | base64 -d > %[2]s/envelope.json; gomi-unseal-bootstrap --key %[2]s/identity/ssh_host_rsa_key --host %s --owner %s --input %[2]s/envelope.json --output %[2]s/bootstrap.yaml`, shellQuote(base64.StdEncoding.EncodeToString(seed.Envelope)), stage, shellQuote(m.Name), shellQuote(seed.Owner))
	}
	cfg.EarlyCommands = map[string][]string{"00-gomi-preserve-bootstrap": {"sh", "-c", early}}
	late := fmt.Sprintf(`set -eu; install -d -m 755 "$TARGET_MOUNT_POINT/etc/ssh"; for key in %[1]s/identity/ssh_host_*_key; do install -m 600 "$key" "$TARGET_MOUNT_POINT/etc/ssh/$(basename "$key")"; install -m 644 "$key.pub" "$TARGET_MOUNT_POINT/etc/ssh/$(basename "$key").pub"; done`, stage)
	if !seed.Cleanup {
		late += fmt.Sprintf(`; gomi-merge-bootstrap --bootstrap %s/bootstrap.yaml --seed "$TARGET_MOUNT_POINT/var/lib/cloud/seed/nocloud" --owner %s`, stage, shellQuote(seed.Owner))
	}
	cfg.LateCommands["99-gomi-sealed-bootstrap"] = []string{"sh", "-c", late}
	return nil
}

// Mark the callback explicitly after every normal GOMI injector has run. Other
// injectors may append commands, so its position in runcmd is not a contract.
func markSealedCompletion(body string) (string, error) {
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(body), &cfg); err != nil {
		return "", err
	}
	commands, ok := cfg["runcmd"].([]any)
	if !ok {
		return "", fmt.Errorf("missing deployment commands")
	}
	found := 0
	for i, entry := range commands {
		command, ok := entry.(string)
		if ok && strings.HasPrefix(command, "sh -c 'for i in ") && strings.Contains(command, "/install-complete?") {
			commands[i] = "# gomi-capi-completion\ntest -f /run/cluster-api/bootstrap-success.complete || exit 1\n" + command
			found++
		}
	}
	if found != 1 {
		return "", fmt.Errorf("missing unique deployment completion callback")
	}
	cfg["runcmd"] = commands
	return renderCloudConfig(body, cfg)
}
