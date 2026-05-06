package provision_test

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/provision"
	"github.com/sugaf1204/gomi/internal/sshkey"
)

func TestBuildCloudInitUsers_DefaultOnlyIsScalar(t *testing.T) {
	got := provision.BuildCloudInitUsers(nil, nil)
	if len(got) != 1 {
		t.Fatalf("users len = %d, want 1", len(got))
	}
	// The first entry MUST be the YAML scalar string "default" (the cloud-init
	// magic token) and not a map like {name: "default"}, otherwise cloud-init
	// will try to create a literal "default" user instead of materialising the
	// distribution's stock user (ubuntu/debian/...).
	if s, ok := got[0].(string); !ok || s != "default" {
		t.Fatalf("users[0] = %#v, want scalar string \"default\"", got[0])
	}
}

func TestBuildCloudInitUsers_WithLoginUser(t *testing.T) {
	pubKeys := []string{"ssh-ed25519 AAAA test"}
	got := provision.BuildCloudInitUsers(pubKeys, &machine.LoginUserSpec{
		Username: "admin",
		Password: "secret",
	})
	if len(got) != 2 {
		t.Fatalf("users len = %d, want 2", len(got))
	}
	if s, ok := got[0].(string); !ok || s != "default" {
		t.Fatalf("users[0] = %#v, want scalar string \"default\"", got[0])
	}
	entry, ok := got[1].(map[string]any)
	if !ok {
		t.Fatalf("users[1] is not a map: %#v", got[1])
	}
	if entry["name"] != "admin" {
		t.Errorf("name = %v, want admin", entry["name"])
	}
	if entry["plain_text_passwd"] != "secret" {
		t.Errorf("plain_text_passwd = %v, want secret", entry["plain_text_passwd"])
	}
	if entry["lock_passwd"] != false {
		t.Errorf("lock_passwd = %v, want false (password set)", entry["lock_passwd"])
	}
	keys, _ := entry["ssh_authorized_keys"].([]string)
	if len(keys) != 1 || keys[0] != "ssh-ed25519 AAAA test" {
		t.Errorf("ssh_authorized_keys = %v, want [ssh-ed25519 AAAA test]", entry["ssh_authorized_keys"])
	}
}

func TestBuildCloudInitUsers_LoginUserWithoutPasswordLocked(t *testing.T) {
	got := provision.BuildCloudInitUsers(nil, &machine.LoginUserSpec{Username: "admin"})
	entry := got[1].(map[string]any)
	if entry["lock_passwd"] != true {
		t.Errorf("lock_passwd = %v, want true (no password)", entry["lock_passwd"])
	}
	if _, has := entry["plain_text_passwd"]; has {
		t.Errorf("plain_text_passwd must be absent when password is empty")
	}
}

func TestLoginUserPasswordConfigured(t *testing.T) {
	if provision.LoginUserPasswordConfigured(nil) {
		t.Fatal("nil login user must not enable password login")
	}
	if provision.LoginUserPasswordConfigured(&machine.LoginUserSpec{Username: "admin"}) {
		t.Fatal("login user without password must not enable password login")
	}
	if !provision.LoginUserPasswordConfigured(&machine.LoginUserSpec{Username: "admin", Password: "secret"}) {
		t.Fatal("login user with password must enable password login")
	}
}

func TestSelectKeysForRefs(t *testing.T) {
	all := []sshkey.SSHKey{
		{Name: "alice", PublicKey: "ssh-ed25519 a"},
		{Name: "bob", PublicKey: "ssh-ed25519 b"},
		{Name: "carol", PublicKey: "ssh-ed25519 c"},
	}

	t.Run("empty refs returns none", func(t *testing.T) {
		got := provision.SelectKeysForRefs(all, nil)
		if len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}
	})

	t.Run("filter by refs", func(t *testing.T) {
		got := provision.SelectKeysForRefs(all, []string{"alice", "carol"})
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
		names := []string{got[0].Name, got[1].Name}
		if names[0] != "alice" || names[1] != "carol" {
			t.Errorf("names = %v, want [alice carol]", names)
		}
	})

	t.Run("unknown ref is silently dropped", func(t *testing.T) {
		got := provision.SelectKeysForRefs(all, []string{"nonexistent"})
		if len(got) != 0 {
			t.Errorf("len = %d, want 0", len(got))
		}
	})
}

func TestBuildArtifacts_NoSSHKeysWhenRefsUnspecified(t *testing.T) {
	_, installCfg, err := provision.BuildArtifacts(machine.Machine{
		Name:     "bm-no-key",
		Hostname: "bm-no-key",
		Firmware: machine.FirmwareUEFI,
	}, "http://boot.local/pxe", []sshkey.SSHKey{
		{Name: "alice", PublicKey: "ssh-ed25519 AAAA test"},
	})
	if err != nil {
		t.Fatalf("BuildArtifacts: %v", err)
	}
	if strings.Contains(installCfg, "ssh_authorized_keys") {
		t.Fatalf("expected no SSH authorized keys when sshKeyRefs is unspecified, got:\n%s", installCfg)
	}
}

func TestBuildArtifacts_SelectedSSHKeysDefaultToOSUser(t *testing.T) {
	_, installCfg, err := provision.BuildArtifacts(machine.Machine{
		Name:       "bm-default-key",
		Hostname:   "bm-default-key",
		Firmware:   machine.FirmwareUEFI,
		SSHKeyRefs: []string{"alice"},
	}, "http://boot.local/pxe", []sshkey.SSHKey{
		{Name: "alice", PublicKey: "ssh-ed25519 AAAA test"},
	})
	if err != nil {
		t.Fatalf("BuildArtifacts: %v", err)
	}
	cfg := parseCloudConfig(t, installCfg)
	keys, ok := cfg["ssh_authorized_keys"].([]any)
	if !ok || len(keys) != 1 || keys[0] != "ssh-ed25519 AAAA test" {
		t.Fatalf("top-level ssh_authorized_keys = %#v, want selected key", cfg["ssh_authorized_keys"])
	}
}

func TestBuildArtifacts_SelectedSSHKeysTargetLoginUserOnly(t *testing.T) {
	_, installCfg, err := provision.BuildArtifacts(machine.Machine{
		Name:       "bm-login-key",
		Hostname:   "bm-login-key",
		Firmware:   machine.FirmwareUEFI,
		SSHKeyRefs: []string{"alice"},
		LoginUser:  &machine.LoginUserSpec{Username: "admin"},
	}, "http://boot.local/pxe", []sshkey.SSHKey{
		{Name: "alice", PublicKey: "ssh-ed25519 AAAA test"},
	})
	if err != nil {
		t.Fatalf("BuildArtifacts: %v", err)
	}
	cfg := parseCloudConfig(t, installCfg)
	if _, has := cfg["ssh_authorized_keys"]; has {
		t.Fatalf("top-level ssh_authorized_keys must be absent when loginUser is set:\n%s", installCfg)
	}
	users, ok := cfg["users"].([]any)
	if !ok || len(users) != 2 {
		t.Fatalf("users = %#v, want default plus login user", cfg["users"])
	}
	entry, ok := users[1].(map[string]any)
	if !ok {
		t.Fatalf("users[1] = %#v, want map", users[1])
	}
	keys, ok := entry["ssh_authorized_keys"].([]any)
	if !ok || len(keys) != 1 || keys[0] != "ssh-ed25519 AAAA test" {
		t.Fatalf("login user ssh_authorized_keys = %#v, want selected key", entry["ssh_authorized_keys"])
	}
}

func TestBuildArtifacts_LoginUserPasswordEnablesSSHPWAuth(t *testing.T) {
	_, installCfg, err := provision.BuildArtifacts(machine.Machine{
		Name:     "bm-password",
		Hostname: "bm-password",
		Firmware: machine.FirmwareUEFI,
		LoginUser: &machine.LoginUserSpec{
			Username: "gomi",
			Password: "gomi",
		},
	}, "http://boot.local/pxe", nil)
	if err != nil {
		t.Fatalf("BuildArtifacts: %v", err)
	}
	for _, want := range []string{
		"name: gomi",
		"plain_text_passwd: gomi",
		"lock_passwd: false",
		"ssh_pwauth: true",
	} {
		if !strings.Contains(installCfg, want) {
			t.Fatalf("expected %q in cloud-config, got:\n%s", want, installCfg)
		}
	}
}

func TestBuildCloudInitUsers_RendersValidYAML(t *testing.T) {
	users := provision.BuildCloudInitUsers([]string{"ssh-ed25519 AAAA"}, &machine.LoginUserSpec{Username: "admin"})
	cfg := map[string]any{
		"users":               users,
		"ssh_authorized_keys": []string{"ssh-ed25519 AAAA"},
		"ssh_pwauth":          false,
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}
	out := string(raw)
	// Sanity: ensure the scalar `- default` line is emitted, not `- name: default`.
	if !strings.Contains(out, "- default\n") {
		t.Errorf("yaml does not include '- default' scalar line:\n%s", out)
	}
	if strings.Contains(out, "- name: default") {
		t.Errorf("yaml must NOT contain '- name: default' (would create a literal 'default' user):\n%s", out)
	}
}

func parseCloudConfig(t *testing.T, raw string) map[string]any {
	t.Helper()
	raw = strings.TrimPrefix(raw, "#cloud-config\n")
	var cfg map[string]any
	if err := yaml.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal: %v\n%s", err, raw)
	}
	return cfg
}
