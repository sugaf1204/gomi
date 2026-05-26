package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/bootenv"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/discovery"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	infraapi "github.com/sugaf1204/gomi/internal/infra/api"
	"github.com/sugaf1204/gomi/internal/infra/config"
	"github.com/sugaf1204/gomi/internal/infra/dns"
	infrasql "github.com/sugaf1204/gomi/internal/infra/sql"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/pxe"
	"github.com/sugaf1204/gomi/internal/sshkey"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
	"io/fs"
	"log"
	gohttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Runtime struct {
	Config          config.Config
	machineStore    machine.Store
	subnetStore     subnet.Store
	authStore       auth.Store
	sshkeyStore     sshkey.Store
	hwinfoStore     hwinfo.Store
	hypervisorStore hypervisor.Store
	hvTokenStore    hypervisor.TokenStore
	agentTokenStore hypervisor.AgentTokenStore
	vmStore         vm.Store
	cloudInitStore  cloudinit.Store
	osimageStore    osimage.Store
	leaseStore      pxe.LeaseStore
	machineSvc      *machine.Service
	sshkeySvc       *sshkey.Service
	hwinfoSvc       *hwinfo.Service
	discoverySvc    *discovery.Service
	hypervisorSvc   *hypervisor.Service
	vmSvc           *vm.Service
	cloudInitSvc    *cloudinit.Service
	osimageSvc      *osimage.Service
	dnsClient       *dns.PowerDNSClient
	dnsController   dns.Controller
	executor        *power.Executor
	healthCheck     func(ctx context.Context) error
	frontendFS      fs.FS
	pxeMu           sync.Mutex
	dhcpServer      *pxe.Server
	tftpServer      *pxe.TFTPServer
	pxeCancel       context.CancelFunc
	pxeDone         chan struct{}
	pxeState        pxeRuntimeState
	bootenvMgr      *bootenv.Manager
	closers         []func()
}

type pxeRuntimeState struct {
	mode     string
	iface    string
	serverIP string
	tftpAddr string
	tftpRoot string
}

func NewRuntime(cfg config.Config) (*Runtime, error) {
	cfg.DNSMode = config.NormalizeDNSMode(cfg.DNSMode)
	switch cfg.DNSMode {
	case config.DNSModeOff, config.DNSModeEmbedded, config.DNSModePowerDNS, config.DNSModeRFC2136:
	default:
		return nil, fmt.Errorf("unsupported dns mode: %s", cfg.DNSMode)
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}

	rt := &Runtime{
		Config:    cfg,
		dnsClient: dns.NewPowerDNSClient(cfg.PowerDNSBaseURL, cfg.PowerDNSAPIToken, cfg.PowerDNSServerID),
		executor:  power.NewExecutor(),
	}

	if err := rt.buildStores(cfg); err != nil {
		rt.Close()
		return nil, err
	}

	rt.machineSvc = machine.NewService(rt.machineStore, machine.WithProvisionTimeout(cfg.ProvisionTimeout))
	rt.discoverySvc = discovery.NewService(rt.machineStore)
	rt.sshkeySvc = sshkey.NewService(rt.sshkeyStore)
	rt.hwinfoSvc = hwinfo.NewService(rt.hwinfoStore)
	rt.hypervisorSvc = hypervisor.NewService(rt.hypervisorStore, rt.hvTokenStore, rt.agentTokenStore)
	rt.vmSvc = vm.NewService(rt.vmStore)
	rt.cloudInitSvc = cloudinit.NewService(rt.cloudInitStore)
	rt.osimageSvc = osimage.NewService(rt.osimageStore)
	rt.bootenvMgr = bootenv.NewManager(bootenv.Config{
		DataDir:   cfg.DataDir,
		FilesDir:  filepath.Join(cfg.DataDir, "files"),
		SourceURL: cfg.BootenvSourceURL,
	})

	if err := rt.bootstrap(context.Background()); err != nil {
		rt.Close()
		return nil, err
	}

	return rt, nil
}

func (r *Runtime) buildStores(cfg config.Config) error {
	backend, err := infrasql.New(cfg.DBDriver, cfg.DBDsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	r.closers = append(r.closers, func() { _ = backend.Close() })

	if err := backend.Migrate(); err != nil {
		return fmt.Errorf("migrate database: %w", err)
	}

	log.Printf("database ready: driver=%s", cfg.DBDriver)

	r.machineStore = backend.Machines()
	r.subnetStore = backend.Subnets()
	r.authStore = backend.Auth()
	r.sshkeyStore = backend.SSHKeys()
	r.hwinfoStore = backend.HWInfo()
	r.hypervisorStore = backend.Hypervisors()
	r.hvTokenStore = backend.HypervisorTokens()
	r.agentTokenStore = backend.AgentTokens()
	r.vmStore = backend.VMs()
	r.cloudInitStore = backend.CloudInits()
	r.osimageStore = backend.OSImages()
	r.leaseStore = backend.DHCPLeases()
	r.healthCheck = backend.Health
	return nil
}

func (r *Runtime) StartServer(ctx context.Context) error {
	pxeBaseURL := strings.TrimRight(r.Config.PXEHTTPBaseURL, "/")

	vmDeployer := &vm.Deployer{
		Hypervisors: r.hypervisorSvc,
		OSImages:    r.osimageSvc,
		VMs:         r.vmSvc,
		PXEBaseURL:  pxeBaseURL,
		ListenAddr:  r.Config.ListenAddr,
	}
	vmRuntimeSyncer := &vm.RuntimeSyncer{
		Hypervisors: r.hypervisorSvc,
		VMs:         r.vmSvc,
	}
	vmMigrator := &vm.Migrator{
		Hypervisors: r.hypervisorSvc,
		VMs:         r.vmSvc,
	}

	r.configureDNSController()
	var dnsRecords infraapi.DNSRecordManager
	if embedded, ok := r.dnsController.(*dns.EmbeddedServer); ok {
		dnsRecords = embedded
	}

	srv := infraapi.NewServer(infraapi.ServerConfig{
		Machines:         r.machineSvc,
		PowerExecutor:    r.executor,
		Subnets:          r.subnetStore,
		AuthStore:        r.authStore,
		AuthService:      infraapi.NewAuthService(r.authStore, r.Config.SessionTTL),
		Discovery:        r.discoverySvc,
		SSHKeys:          r.sshkeySvc,
		HWInfo:           r.hwinfoSvc,
		Hypervisors:      r.hypervisorSvc,
		AgentTokenStore:  r.agentTokenStore,
		VMs:              r.vmSvc,
		CloudInits:       r.cloudInitSvc,
		OSImages:         r.osimageSvc,
		DNSRecords:       dnsRecords,
		LeaseStore:       r.leaseStore,
		FilesDir:         filepath.Join(r.Config.DataDir, "files"),
		ImageStorageDir:  filepath.Join(r.Config.DataDir, "images"),
		HealthCheck:      r.healthCheck,
		FrontendFS:       r.frontendFS,
		PXEHTTPBaseURL:   pxeBaseURL,
		PXETFTPRoot:      r.Config.TFTPRoot,
		ProvisionTimeout: r.Config.ProvisionTimeout,
		VMDeployer:       vmDeployer,
		VMRuntimeSyncer:  vmRuntimeSyncer,
		VMMigrator:       vmMigrator,
		BootEnvs:         r.bootenvMgr,
	})

	if r.Config.BackgroundSyncEnabled {
		go r.runSyncLoop(ctx)
		go r.runPowerPollLoop(ctx)
	}

	go r.runPXEManager(ctx)
	if r.Config.DNSMode != config.DNSModeOff {
		go r.startDNS(ctx)
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Start(r.Config.ListenAddr); err != nil && !errors.Is(err, gohttp.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (r *Runtime) SetFrontendFS(fsys fs.FS) {
	r.frontendFS = fsys
}

func (r *Runtime) Close() {
	for _, fn := range r.closers {
		fn()
	}
}
