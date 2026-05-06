package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFromEnvDNSDefaults(t *testing.T) {
	clearConfigEnv(t)

	cfg := FromEnv()
	if cfg.ListenAddr != "0.0.0.0:8080" {
		t.Fatalf("expected listen addr 0.0.0.0:8080, got %q", cfg.ListenAddr)
	}
	if cfg.DNSMode != DNSModeOff {
		t.Fatalf("expected DNSMode off, got %q", cfg.DNSMode)
	}
	if cfg.DNSEmbeddedAddr != ":53" {
		t.Fatalf("expected embedded addr :53, got %q", cfg.DNSEmbeddedAddr)
	}
	if cfg.DNSTTL != 300*time.Second {
		t.Fatalf("expected DNS TTL 300s, got %s", cfg.DNSTTL)
	}
}

func TestFromEnvDoesNotDefaultBootstrapAdmin(t *testing.T) {
	clearConfigEnv(t)

	cfg := FromEnv()
	if cfg.AdminUsername != "" || cfg.AdminPassword != "" {
		t.Fatalf("admin bootstrap must be opt-in, got %q/%q", cfg.AdminUsername, cfg.AdminPassword)
	}
}

func TestFromEnvBootenvSourceURLDefaultsToGOMIRelease(t *testing.T) {
	clearConfigEnv(t)

	cfg := FromEnv()
	if cfg.BootenvSourceURL != "https://github.com/sugaf1204/gomi/releases/latest/download" {
		t.Fatalf("unexpected bootenv source URL: %q", cfg.BootenvSourceURL)
	}
}

func TestFromEnvBootenvSourceURLOverride(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("GOMI_BOOTENV_SOURCE_URL", " /srv/gomi/bootenv ")

	cfg := FromEnv()
	if cfg.BootenvSourceURL != "/srv/gomi/bootenv" {
		t.Fatalf("expected bootenv source URL override, got %q", cfg.BootenvSourceURL)
	}
}

func TestFromEnvDNSOverrides(t *testing.T) {
	clearConfigEnv(t)
	t.Setenv("GOMI_DNS_MODE", " Embedded ")
	t.Setenv("GOMI_DNS_EMBEDDED_ADDR", "127.0.0.1:1053")
	t.Setenv("GOMI_DNS_TTL", "30s")
	t.Setenv("GOMI_RFC2136_SERVER", "dns.example:53")
	t.Setenv("GOMI_RFC2136_ZONE", "lab.example")
	t.Setenv("GOMI_RFC2136_TSIG_NAME", "gomi-key")
	t.Setenv("GOMI_RFC2136_TSIG_SECRET", "c2VjcmV0")
	t.Setenv("GOMI_RFC2136_TSIG_ALGORITHM", "hmac-sha512.")
	t.Setenv("GOMI_RFC2136_TRANSPORT", "tcp")

	cfg := FromEnv()
	if cfg.DNSMode != DNSModeEmbedded {
		t.Fatalf("expected DNSMode embedded, got %q", cfg.DNSMode)
	}
	if cfg.DNSEmbeddedAddr != "127.0.0.1:1053" {
		t.Fatalf("expected embedded addr override, got %q", cfg.DNSEmbeddedAddr)
	}
	if cfg.DNSTTL != 30*time.Second {
		t.Fatalf("expected DNS TTL 30s, got %s", cfg.DNSTTL)
	}
	if cfg.RFC2136Server != "dns.example:53" || cfg.RFC2136Zone != "lab.example" || cfg.RFC2136TSIGName != "gomi-key" || cfg.RFC2136TSIGSecret != "c2VjcmV0" || cfg.RFC2136TSIGAlgorithm != "hmac-sha512." || cfg.RFC2136Transport != "tcp" {
		t.Fatalf("unexpected rfc2136 config: %#v", cfg)
	}
}

func TestLoadYAMLConfig(t *testing.T) {
	clearConfigEnv(t)
	path := filepath.Join(t.TempDir(), "gomi.yaml")
	data := []byte(`
listen_addr: "127.0.0.1:18080"
data_dir: /tmp/gomi-data
session_ttl: 2h
background_sync_enabled: false
admin:
  username: root
  password: secret
database:
  driver: postgres
  dsn: postgres://gomi:gomi@db.example/gomi?sslmode=disable
dns:
  mode: Embedded
  embedded_addr: "127.0.0.1:1053"
  ttl: 45s
  powerdns:
    base_url: http://powerdns.example/api/v1
    api_token: token
    server_id: pdns
  rfc2136:
    server: 192.0.2.53:53
    zone: lab.example
    tsig_name: gomi-key
    tsig_secret: c2VjcmV0
    tsig_algorithm: hmac-sha384.
    transport: tcp
boot_http_base_url: http://boot.example
dhcp:
  mode: proxy
  iface: br0
tftp:
  addr: "127.0.0.1:1069"
  root: /srv/tftp
pxe:
  http_base_url: http://pxe.example/pxe
  bootfile_bios: bios.kpxe
  bootfile_uefi: uefi.efi
vm:
  provision_timeout: 45m
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.ListenAddr != "127.0.0.1:18080" {
		t.Fatalf("unexpected listen addr: %q", cfg.ListenAddr)
	}
	if cfg.DataDir != "/tmp/gomi-data" {
		t.Fatalf("unexpected data dir: %q", cfg.DataDir)
	}
	if cfg.SessionTTL != 2*time.Hour {
		t.Fatalf("unexpected session ttl: %s", cfg.SessionTTL)
	}
	if cfg.BackgroundSyncEnabled {
		t.Fatalf("expected background sync disabled")
	}
	if cfg.AdminUsername != "root" || cfg.AdminPassword != "secret" {
		t.Fatalf("unexpected admin config: %q/%q", cfg.AdminUsername, cfg.AdminPassword)
	}
	if cfg.DBDriver != "postgres" || cfg.DBDsn != "postgres://gomi:gomi@db.example/gomi?sslmode=disable" {
		t.Fatalf("unexpected database config: %q %q", cfg.DBDriver, cfg.DBDsn)
	}
	if cfg.DNSMode != DNSModeEmbedded || cfg.DNSEmbeddedAddr != "127.0.0.1:1053" || cfg.DNSTTL != 45*time.Second {
		t.Fatalf("unexpected dns config: mode=%q addr=%q ttl=%s", cfg.DNSMode, cfg.DNSEmbeddedAddr, cfg.DNSTTL)
	}
	if cfg.PowerDNSBaseURL != "http://powerdns.example/api/v1" || cfg.PowerDNSAPIToken != "token" || cfg.PowerDNSServerID != "pdns" {
		t.Fatalf("unexpected powerdns config: %#v", cfg)
	}
	if cfg.RFC2136Server != "192.0.2.53:53" || cfg.RFC2136Zone != "lab.example" || cfg.RFC2136TSIGName != "gomi-key" || cfg.RFC2136TSIGSecret != "c2VjcmV0" || cfg.RFC2136TSIGAlgorithm != "hmac-sha384." || cfg.RFC2136Transport != "tcp" {
		t.Fatalf("unexpected rfc2136 config: %#v", cfg)
	}
	if cfg.BootHTTPBaseURL != "http://boot.example" {
		t.Fatalf("unexpected boot base URL: %q", cfg.BootHTTPBaseURL)
	}
	if cfg.DHCPMode != "proxy" || cfg.DHCPIface != "br0" {
		t.Fatalf("unexpected dhcp config: %q %q", cfg.DHCPMode, cfg.DHCPIface)
	}
	if cfg.TFTPAddr != "127.0.0.1:1069" || cfg.TFTPRoot != "/srv/tftp" {
		t.Fatalf("unexpected tftp config: %q %q", cfg.TFTPAddr, cfg.TFTPRoot)
	}
	if cfg.PXEHTTPBaseURL != "http://pxe.example/pxe" || cfg.PXEBootFileBIOS != "bios.kpxe" || cfg.PXEBootFileUEFI != "uefi.efi" {
		t.Fatalf("unexpected pxe config: %#v", cfg)
	}
	if cfg.ProvisionTimeout != 45*time.Minute {
		t.Fatalf("unexpected provision timeout: %s", cfg.ProvisionTimeout)
	}
}

func TestLoadYAMLAppliesDerivedDefaultsAfterFile(t *testing.T) {
	clearConfigEnv(t)
	path := filepath.Join(t.TempDir(), "gomi.yaml")
	if err := os.WriteFile(path, []byte("data_dir: /srv/gomi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.DBDsn != "/srv/gomi/gomi.db" {
		t.Fatalf("expected sqlite dsn to follow data_dir, got %q", cfg.DBDsn)
	}
	if cfg.TFTPRoot != "/srv/gomi/tftp" {
		t.Fatalf("expected tftp root to follow data_dir, got %q", cfg.TFTPRoot)
	}
}

func TestLoadYAMLEnvOverridesFile(t *testing.T) {
	clearConfigEnv(t)
	path := filepath.Join(t.TempDir(), "gomi.yaml")
	if err := os.WriteFile(path, []byte("dns:\n  mode: off\n  ttl: 120s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOMI_DNS_MODE", "embedded")
	t.Setenv("GOMI_DNS_TTL", "15s")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.DNSMode != DNSModeEmbedded || cfg.DNSTTL != 15*time.Second {
		t.Fatalf("expected env override, got mode=%q ttl=%s", cfg.DNSMode, cfg.DNSTTL)
	}
}

func TestLoadYAMLRejectsUnknownFields(t *testing.T) {
	clearConfigEnv(t)
	path := filepath.Join(t.TempDir(), "gomi.yaml")
	if err := os.WriteFile(path, []byte("unknown: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(path); err == nil {
		t.Fatalf("expected unknown field error")
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"GOMI_DB_DRIVER",
		"GOMI_DB_DSN",
		"GOMI_LISTEN_ADDR",
		"GOMI_DATA_DIR",
		"GOMI_SESSION_TTL",
		"GOMI_BACKGROUND_SYNC_ENABLED",
		"GOMI_ADMIN_USERNAME",
		"GOMI_ADMIN_PASSWORD",
		"GOMI_DNS_MODE",
		"GOMI_DNS_EMBEDDED_ADDR",
		"GOMI_DNS_TTL",
		"GOMI_POWERDNS_BASE_URL",
		"GOMI_POWERDNS_API_TOKEN",
		"GOMI_POWERDNS_SERVER_ID",
		"GOMI_RFC2136_SERVER",
		"GOMI_RFC2136_ZONE",
		"GOMI_RFC2136_TSIG_NAME",
		"GOMI_RFC2136_TSIG_SECRET",
		"GOMI_RFC2136_TSIG_ALGORITHM",
		"GOMI_RFC2136_TRANSPORT",
		"GOMI_BOOT_HTTP_BASE_URL",
		"GOMI_DHCP_MODE",
		"GOMI_DHCP_IFACE",
		"GOMI_TFTP_ADDR",
		"GOMI_TFTP_ROOT",
		"GOMI_PXE_HTTP_BASE_URL",
		"GOMI_PXE_BOOTFILE_BIOS",
		"GOMI_PXE_BOOTFILE_UEFI",
		"GOMI_BOOTENV_SOURCE_URL",
		"GOMI_VM_PROVISION_TIMEOUT",
	} {
		t.Setenv(key, "")
	}
}
