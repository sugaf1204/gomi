package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/sugaf1204/gomi/internal/app"
	"github.com/sugaf1204/gomi/internal/infra/config"
	infrasql "github.com/sugaf1204/gomi/internal/infra/sql"
	"github.com/sugaf1204/gomi/internal/setupadmin"
	"github.com/sugaf1204/gomi/web"
)

func main() {
	args := os.Args[1:]
	if setupIndex := indexArg(args, "setup"); setupIndex >= 0 {
		setupArgs := append([]string{}, args[setupIndex+1:]...)
		setupArgs = append(setupArgs, args[:setupIndex]...)
		os.Exit(runSetupCommand(setupArgs))
	}
	runServer()
}

func runServer() {
	configPath := configPathFromArgs(os.Args[1:], os.Getenv("GOMI_CONFIG"))
	envCfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	flag.String("config", configPath, "YAML config file")
	listen := flag.String("listen", envCfg.ListenAddr, "listen address")
	dbDriver := flag.String("db-driver", envCfg.DBDriver, "database driver: sqlite or postgres")
	dbDsn := flag.String("db-dsn", envCfg.DBDsn, "database DSN")
	bgSyncEnabled := flag.Bool("background-sync-enabled", envCfg.BackgroundSyncEnabled, "enable background sync loops")
	dataDir := flag.String("data-dir", envCfg.DataDir, "data directory")
	adminUser := flag.String("admin-user", envCfg.AdminUsername, "optional bootstrap admin username")
	adminPassword := flag.String("admin-password", envCfg.AdminPassword, "optional bootstrap admin password")
	dnsMode := flag.String("dns-mode", envCfg.DNSMode, "DNS integration mode: off, embedded, powerdns, or rfc2136")
	dnsEmbeddedAddr := flag.String("dns-embedded-addr", envCfg.DNSEmbeddedAddr, "embedded authoritative DNS listen address")
	dnsTTL := flag.Duration("dns-ttl", envCfg.DNSTTL, "DNS record TTL")
	powerdnsBaseURL := flag.String("powerdns-base-url", envCfg.PowerDNSBaseURL, "PowerDNS API base URL")
	powerdnsToken := flag.String("powerdns-api-token", envCfg.PowerDNSAPIToken, "PowerDNS API token")
	powerdnsServerID := flag.String("powerdns-server-id", envCfg.PowerDNSServerID, "PowerDNS server ID")
	rfc2136Server := flag.String("rfc2136-server", envCfg.RFC2136Server, "RFC2136 DDNS server address")
	rfc2136Zone := flag.String("rfc2136-zone", envCfg.RFC2136Zone, "RFC2136 DDNS zone")
	rfc2136TSIGName := flag.String("rfc2136-tsig-name", envCfg.RFC2136TSIGName, "RFC2136 TSIG key name")
	rfc2136TSIGSecret := flag.String("rfc2136-tsig-secret", envCfg.RFC2136TSIGSecret, "RFC2136 TSIG secret (base64)")
	rfc2136TSIGAlgorithm := flag.String("rfc2136-tsig-algorithm", envCfg.RFC2136TSIGAlgorithm, "RFC2136 TSIG algorithm")
	rfc2136Transport := flag.String("rfc2136-transport", envCfg.RFC2136Transport, "RFC2136 transport: udp or tcp")
	bootBaseURL := flag.String("boot-http-base-url", envCfg.BootHTTPBaseURL, "Boot assets base URL")
	dhcpMode := flag.String("dhcp-mode", envCfg.DHCPMode, "DHCP mode: full, proxy, or off")
	dhcpIface := flag.String("dhcp-iface", envCfg.DHCPIface, "DHCP listen interface (auto-detect if empty)")
	tftpAddr := flag.String("tftp-addr", envCfg.TFTPAddr, "TFTP listen address")
	tftpRoot := flag.String("tftp-root", envCfg.TFTPRoot, "TFTP root directory")
	pxeHTTPBaseURL := flag.String("pxe-http-base-url", envCfg.PXEHTTPBaseURL, "PXE HTTP base URL")
	pxeBootFileBIOS := flag.String("pxe-bootfile-bios", envCfg.PXEBootFileBIOS, "BIOS PXE first-stage bootfile")
	pxeBootFileUEFI := flag.String("pxe-bootfile-uefi", envCfg.PXEBootFileUEFI, "UEFI PXE first-stage bootfile")
	bootenvSourceURL := flag.String("bootenv-source-url", envCfg.BootenvSourceURL, "Boot environment source URL or local artifact directory")
	vmProvisionTimeout := flag.Duration("vm-provision-timeout", envCfg.ProvisionTimeout, "VM provisioning completion timeout")

	flag.Parse()

	cfg := config.Config{
		DBDriver:              *dbDriver,
		DBDsn:                 *dbDsn,
		ListenAddr:            *listen,
		DataDir:               *dataDir,
		SessionTTL:            envCfg.SessionTTL,
		BackgroundSyncEnabled: *bgSyncEnabled,
		AdminUsername:         *adminUser,
		AdminPassword:         *adminPassword,
		DNSMode:               config.NormalizeDNSMode(*dnsMode),
		DNSEmbeddedAddr:       *dnsEmbeddedAddr,
		DNSTTL:                *dnsTTL,
		PowerDNSBaseURL:       *powerdnsBaseURL,
		PowerDNSAPIToken:      *powerdnsToken,
		PowerDNSServerID:      *powerdnsServerID,
		RFC2136Server:         *rfc2136Server,
		RFC2136Zone:           *rfc2136Zone,
		RFC2136TSIGName:       *rfc2136TSIGName,
		RFC2136TSIGSecret:     *rfc2136TSIGSecret,
		RFC2136TSIGAlgorithm:  *rfc2136TSIGAlgorithm,
		RFC2136Transport:      *rfc2136Transport,
		BootHTTPBaseURL:       *bootBaseURL,
		DHCPMode:              *dhcpMode,
		DHCPIface:             *dhcpIface,
		TFTPAddr:              *tftpAddr,
		TFTPRoot:              *tftpRoot,
		PXEHTTPBaseURL:        *pxeHTTPBaseURL,
		PXEBootFileBIOS:       *pxeBootFileBIOS,
		PXEBootFileUEFI:       *pxeBootFileUEFI,
		BootenvSourceURL:      *bootenvSourceURL,
		ProvisionTimeout:      *vmProvisionTimeout,
	}
	config.Finalize(&cfg)

	rt, err := app.NewRuntime(cfg)
	if err != nil {
		log.Fatalf("failed to initialize runtime: %v", err)
	}
	defer rt.Close()

	frontendFS, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		log.Fatalf("failed to load frontend assets: %v", err)
	}
	rt.SetFrontendFS(frontendFS)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("starting gomi server driver=%s listen=%s", cfg.DBDriver, cfg.ListenAddr)
	if err := rt.StartServer(ctx); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func runSetupCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: gomi setup admin --username USER (--password-file PATH | --password-stdin)")
		return 2
	}
	switch args[0] {
	case "admin":
		return runSetupAdmin(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown setup command: %s\n", args[0])
		return 2
	}
}

func runSetupAdmin(args []string) int {
	configPath := configPathFromArgs(args, os.Getenv("GOMI_CONFIG"))
	envCfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		return 1
	}

	fs := flag.NewFlagSet("gomi setup admin", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	configFlag := fs.String("config", configPath, "YAML config file")
	dbDriver := fs.String("db-driver", envCfg.DBDriver, "database driver: sqlite or postgres")
	dbDsn := fs.String("db-dsn", envCfg.DBDsn, "database DSN")
	username := fs.String("username", "", "admin username to create")
	passwordFile := fs.String("password-file", "", "file containing the admin password")
	passwordStdin := fs.Bool("password-stdin", false, "read the admin password from stdin")
	ignoreAlreadyConfigured := fs.Bool("ignore-already-configured", false, "exit successfully when setup is already completed")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *configFlag != configPath {
		envCfg, err = config.Load(*configFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
			return 1
		}
		if !flagPassed(fs, "db-driver") {
			*dbDriver = envCfg.DBDriver
		}
		if !flagPassed(fs, "db-dsn") {
			*dbDsn = envCfg.DBDsn
		}
	}

	password, err := readSetupPassword(*passwordFile, *passwordStdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}

	backend, err := infrasql.New(*dbDriver, *dbDsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open database: %v\n", err)
		return 1
	}
	defer backend.Close()
	if err := backend.Migrate(); err != nil {
		fmt.Fprintf(os.Stderr, "migrate database: %v\n", err)
		return 1
	}

	err = setupadmin.CreateFirstAdmin(context.Background(), backend.Auth(), *username, password)
	if errors.Is(err, setupadmin.ErrAlreadyConfigured) && *ignoreAlreadyConfigured {
		fmt.Fprintln(os.Stdout, "setup already completed")
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "create first admin: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stdout, "created first admin: %s\n", strings.TrimSpace(*username))
	return 0
}

func readSetupPassword(path string, stdin bool) (string, error) {
	if path != "" && stdin {
		return "", errors.New("use only one of --password-file or --password-stdin")
	}
	if path == "" && !stdin {
		return "", errors.New("one of --password-file or --password-stdin is required")
	}
	var data []byte
	var err error
	if stdin {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}

func flagPassed(fs *flag.FlagSet, name string) bool {
	passed := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			passed = true
		}
	})
	return passed
}

func indexArg(args []string, want string) int {
	for i, arg := range args {
		if arg == want {
			return i
		}
		if arg == "--" {
			return -1
		}
	}
	return -1
}

func configPathFromArgs(args []string, fallback string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return fallback
		}
		if arg == "-config" || arg == "--config" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return fallback
		}
		if value, ok := strings.CutPrefix(arg, "-config="); ok {
			return value
		}
		if value, ok := strings.CutPrefix(arg, "--config="); ok {
			return value
		}
	}
	return fallback
}
