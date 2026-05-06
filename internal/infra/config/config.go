package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DBDriver              string
	DBDsn                 string
	ListenAddr            string
	DataDir               string
	SessionTTL            time.Duration
	BackgroundSyncEnabled bool
	AdminUsername         string
	AdminPassword         string
	DNSMode               string
	DNSEmbeddedAddr       string
	DNSTTL                time.Duration
	PowerDNSBaseURL       string
	PowerDNSAPIToken      string
	PowerDNSServerID      string
	RFC2136Server         string
	RFC2136Zone           string
	RFC2136TSIGName       string
	RFC2136TSIGSecret     string
	RFC2136TSIGAlgorithm  string
	RFC2136Transport      string
	BootHTTPBaseURL       string
	DHCPMode              string // "full" (default), "proxy", or "off"
	DHCPIface             string // network interface for DHCP; empty = auto-detect
	TFTPAddr              string // TFTP listen address (default :69)
	TFTPRoot              string // TFTP root directory
	PXEHTTPBaseURL        string // PXE HTTP base URL (e.g. http://192.168.1.10:8080/pxe)
	PXEBootFileBIOS       string // BIOS PXE first-stage bootfile
	PXEBootFileUEFI       string // UEFI PXE first-stage bootfile
	BootenvSourceURL      string // Release-style boot environment manifest URL or local artifact directory
	ProvisionTimeout      time.Duration
}

const (
	DNSModeOff              = "off"
	DNSModeEmbedded         = "embedded"
	DNSModePowerDNS         = "powerdns"
	DNSModeRFC2136          = "rfc2136"
	defaultBootenvSourceURL = "https://github.com/sugaf1204/gomi/releases/latest/download"
)

type fileConfig struct {
	ListenAddr            *string        `yaml:"listen_addr"`
	DataDir               *string        `yaml:"data_dir"`
	SessionTTL            *string        `yaml:"session_ttl"`
	BackgroundSyncEnabled *bool          `yaml:"background_sync_enabled"`
	Admin                 adminConfig    `yaml:"admin"`
	Database              databaseConfig `yaml:"database"`
	DNS                   dnsConfig      `yaml:"dns"`
	BootHTTPBaseURL       *string        `yaml:"boot_http_base_url"`
	DHCP                  dhcpConfig     `yaml:"dhcp"`
	TFTP                  tftpConfig     `yaml:"tftp"`
	PXE                   pxeConfig      `yaml:"pxe"`
	Bootenv               bootenvConfig  `yaml:"bootenv"`
	VM                    vmConfig       `yaml:"vm"`
}

type adminConfig struct {
	Username *string `yaml:"username"`
	Password *string `yaml:"password"`
}

type databaseConfig struct {
	Driver *string `yaml:"driver"`
	DSN    *string `yaml:"dsn"`
}

type dnsConfig struct {
	Mode         *string        `yaml:"mode"`
	EmbeddedAddr *string        `yaml:"embedded_addr"`
	TTL          *string        `yaml:"ttl"`
	PowerDNS     powerDNSConfig `yaml:"powerdns"`
	RFC2136      rfc2136Config  `yaml:"rfc2136"`
}

type powerDNSConfig struct {
	BaseURL  *string `yaml:"base_url"`
	APIToken *string `yaml:"api_token"`
	ServerID *string `yaml:"server_id"`
}

type rfc2136Config struct {
	Server        *string `yaml:"server"`
	Zone          *string `yaml:"zone"`
	TSIGName      *string `yaml:"tsig_name"`
	TSIGSecret    *string `yaml:"tsig_secret"`
	TSIGAlgorithm *string `yaml:"tsig_algorithm"`
	Transport     *string `yaml:"transport"`
}

type dhcpConfig struct {
	Mode  *string `yaml:"mode"`
	Iface *string `yaml:"iface"`
}

type tftpConfig struct {
	Addr *string `yaml:"addr"`
	Root *string `yaml:"root"`
}

type pxeConfig struct {
	HTTPBaseURL  *string `yaml:"http_base_url"`
	BootFileBIOS *string `yaml:"bootfile_bios"`
	BootFileUEFI *string `yaml:"bootfile_uefi"`
}

type bootenvConfig struct {
	SourceURL *string `yaml:"source_url"`
}

type vmConfig struct {
	ProvisionTimeout *string `yaml:"provision_timeout"`
}

func NormalizeDNSMode(mode string) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return DNSModeOff
	}
	return mode
}

func Defaults() Config {
	dataDir := "/var/lib/gomi/data"
	return Config{
		DBDriver:              "sqlite",
		DBDsn:                 "",
		ListenAddr:            "0.0.0.0:8080",
		DataDir:               dataDir,
		SessionTTL:            12 * time.Hour,
		BackgroundSyncEnabled: true,
		AdminUsername:         "",
		AdminPassword:         "",
		DNSMode:               DNSModeOff,
		DNSEmbeddedAddr:       ":53",
		DNSTTL:                300 * time.Second,
		PowerDNSBaseURL:       "",
		PowerDNSAPIToken:      "",
		PowerDNSServerID:      "localhost",
		RFC2136Server:         "",
		RFC2136Zone:           "",
		RFC2136TSIGName:       "",
		RFC2136TSIGSecret:     "",
		RFC2136TSIGAlgorithm:  "hmac-sha256.",
		RFC2136Transport:      "udp",
		BootHTTPBaseURL:       "http://gomi-boot.local",
		DHCPMode:              "full",
		DHCPIface:             "",
		TFTPAddr:              ":69",
		TFTPRoot:              "",
		PXEHTTPBaseURL:        "",
		PXEBootFileBIOS:       "undionly.kpxe",
		PXEBootFileUEFI:       "ipxe.efi",
		BootenvSourceURL:      defaultBootenvSourceURL,
		ProvisionTimeout:      30 * time.Minute,
	}
}

func Finalize(cfg *Config) {
	if strings.TrimSpace(cfg.DataDir) == "" {
		cfg.DataDir = "/var/lib/gomi/data"
	}
	if strings.TrimSpace(cfg.DBDriver) == "" {
		cfg.DBDriver = "sqlite"
	}
	cfg.DNSMode = NormalizeDNSMode(cfg.DNSMode)
	if cfg.DBDsn == "" && cfg.DBDriver == "sqlite" {
		cfg.DBDsn = cfg.DataDir + "/gomi.db"
	}
	if cfg.TFTPRoot == "" {
		cfg.TFTPRoot = cfg.DataDir + "/tftp"
	}
}

func Load(path string) (Config, error) {
	cfg := Defaults()
	if strings.TrimSpace(path) != "" {
		if err := applyYAMLFile(&cfg, path); err != nil {
			return Config{}, err
		}
	}
	ApplyEnv(&cfg)
	Finalize(&cfg)
	return cfg, nil
}

func ApplyEnv(cfg *Config) {
	cfg.DBDriver = readString("GOMI_DB_DRIVER", cfg.DBDriver)
	cfg.DBDsn = readString("GOMI_DB_DSN", cfg.DBDsn)
	cfg.ListenAddr = readString("GOMI_LISTEN_ADDR", cfg.ListenAddr)
	cfg.DataDir = readString("GOMI_DATA_DIR", cfg.DataDir)
	cfg.SessionTTL = readDuration("GOMI_SESSION_TTL", cfg.SessionTTL)
	cfg.BackgroundSyncEnabled = readBool("GOMI_BACKGROUND_SYNC_ENABLED", cfg.BackgroundSyncEnabled)
	cfg.AdminUsername = readString("GOMI_ADMIN_USERNAME", cfg.AdminUsername)
	cfg.AdminPassword = readString("GOMI_ADMIN_PASSWORD", cfg.AdminPassword)
	cfg.DNSMode = NormalizeDNSMode(readString("GOMI_DNS_MODE", cfg.DNSMode))
	cfg.DNSEmbeddedAddr = readString("GOMI_DNS_EMBEDDED_ADDR", cfg.DNSEmbeddedAddr)
	cfg.DNSTTL = readDuration("GOMI_DNS_TTL", cfg.DNSTTL)
	cfg.PowerDNSBaseURL = readString("GOMI_POWERDNS_BASE_URL", cfg.PowerDNSBaseURL)
	cfg.PowerDNSAPIToken = readString("GOMI_POWERDNS_API_TOKEN", cfg.PowerDNSAPIToken)
	cfg.PowerDNSServerID = readString("GOMI_POWERDNS_SERVER_ID", cfg.PowerDNSServerID)
	cfg.RFC2136Server = readString("GOMI_RFC2136_SERVER", cfg.RFC2136Server)
	cfg.RFC2136Zone = readString("GOMI_RFC2136_ZONE", cfg.RFC2136Zone)
	cfg.RFC2136TSIGName = readString("GOMI_RFC2136_TSIG_NAME", cfg.RFC2136TSIGName)
	cfg.RFC2136TSIGSecret = readString("GOMI_RFC2136_TSIG_SECRET", cfg.RFC2136TSIGSecret)
	cfg.RFC2136TSIGAlgorithm = readString("GOMI_RFC2136_TSIG_ALGORITHM", cfg.RFC2136TSIGAlgorithm)
	cfg.RFC2136Transport = readString("GOMI_RFC2136_TRANSPORT", cfg.RFC2136Transport)
	cfg.BootHTTPBaseURL = readString("GOMI_BOOT_HTTP_BASE_URL", cfg.BootHTTPBaseURL)
	cfg.DHCPMode = readString("GOMI_DHCP_MODE", cfg.DHCPMode)
	cfg.DHCPIface = readString("GOMI_DHCP_IFACE", cfg.DHCPIface)
	cfg.TFTPAddr = readString("GOMI_TFTP_ADDR", cfg.TFTPAddr)
	cfg.TFTPRoot = readString("GOMI_TFTP_ROOT", cfg.TFTPRoot)
	cfg.PXEHTTPBaseURL = readString("GOMI_PXE_HTTP_BASE_URL", cfg.PXEHTTPBaseURL)
	cfg.PXEBootFileBIOS = readString("GOMI_PXE_BOOTFILE_BIOS", cfg.PXEBootFileBIOS)
	cfg.PXEBootFileUEFI = readString("GOMI_PXE_BOOTFILE_UEFI", cfg.PXEBootFileUEFI)
	cfg.BootenvSourceURL = readString("GOMI_BOOTENV_SOURCE_URL", cfg.BootenvSourceURL)
	cfg.ProvisionTimeout = readDuration("GOMI_VM_PROVISION_TIMEOUT", cfg.ProvisionTimeout)
}

func applyYAMLFile(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file %s: %w", path, err)
	}
	return ApplyYAML(cfg, data)
}

func ApplyYAML(cfg *Config, data []byte) error {
	var fc fileConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&fc); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse config yaml: %w", err)
	}
	return fc.apply(cfg)
}

func (fc fileConfig) apply(cfg *Config) error {
	applyString(&cfg.ListenAddr, fc.ListenAddr)
	applyString(&cfg.DataDir, fc.DataDir)
	if err := applyDuration(&cfg.SessionTTL, fc.SessionTTL, "session_ttl"); err != nil {
		return err
	}
	if fc.BackgroundSyncEnabled != nil {
		cfg.BackgroundSyncEnabled = *fc.BackgroundSyncEnabled
	}
	applyString(&cfg.AdminUsername, fc.Admin.Username)
	applyString(&cfg.AdminPassword, fc.Admin.Password)
	applyString(&cfg.DBDriver, fc.Database.Driver)
	applyString(&cfg.DBDsn, fc.Database.DSN)
	applyString(&cfg.DNSMode, fc.DNS.Mode)
	applyString(&cfg.DNSEmbeddedAddr, fc.DNS.EmbeddedAddr)
	if err := applyDuration(&cfg.DNSTTL, fc.DNS.TTL, "dns.ttl"); err != nil {
		return err
	}
	applyString(&cfg.PowerDNSBaseURL, fc.DNS.PowerDNS.BaseURL)
	applyString(&cfg.PowerDNSAPIToken, fc.DNS.PowerDNS.APIToken)
	applyString(&cfg.PowerDNSServerID, fc.DNS.PowerDNS.ServerID)
	applyString(&cfg.RFC2136Server, fc.DNS.RFC2136.Server)
	applyString(&cfg.RFC2136Zone, fc.DNS.RFC2136.Zone)
	applyString(&cfg.RFC2136TSIGName, fc.DNS.RFC2136.TSIGName)
	applyString(&cfg.RFC2136TSIGSecret, fc.DNS.RFC2136.TSIGSecret)
	applyString(&cfg.RFC2136TSIGAlgorithm, fc.DNS.RFC2136.TSIGAlgorithm)
	applyString(&cfg.RFC2136Transport, fc.DNS.RFC2136.Transport)
	applyString(&cfg.BootHTTPBaseURL, fc.BootHTTPBaseURL)
	applyString(&cfg.DHCPMode, fc.DHCP.Mode)
	applyString(&cfg.DHCPIface, fc.DHCP.Iface)
	applyString(&cfg.TFTPAddr, fc.TFTP.Addr)
	applyString(&cfg.TFTPRoot, fc.TFTP.Root)
	applyString(&cfg.PXEHTTPBaseURL, fc.PXE.HTTPBaseURL)
	applyString(&cfg.PXEBootFileBIOS, fc.PXE.BootFileBIOS)
	applyString(&cfg.PXEBootFileUEFI, fc.PXE.BootFileUEFI)
	applyString(&cfg.BootenvSourceURL, fc.Bootenv.SourceURL)
	return applyDuration(&cfg.ProvisionTimeout, fc.VM.ProvisionTimeout, "vm.provision_timeout")
}

func applyString(dst *string, src *string) {
	if src != nil {
		*dst = strings.TrimSpace(*src)
	}
}

func applyDuration(dst *time.Duration, src *string, field string) error {
	if src == nil {
		return nil
	}
	raw := strings.TrimSpace(*src)
	if raw == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", field, err)
	}
	*dst = d
	return nil
}

func readString(key, def string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	return v
}

func readBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return def
	}
	return v == "1" || v == "true" || v == "yes"
}

func readDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func FromEnv() Config {
	cfg := Defaults()
	ApplyEnv(&cfg)
	Finalize(&cfg)
	return cfg
}
