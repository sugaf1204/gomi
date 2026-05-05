package api

import (
	"context"
	"io/fs"
	gohttp "net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/bootenv"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/discovery"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/infra/pxehttp"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/pxe"
	"github.com/sugaf1204/gomi/internal/sshkey"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
)

type Server struct {
	echo             *echo.Echo
	machines         *machine.Service
	powerExecutor    PowerExecutor
	subnets          subnet.Store
	authStore        auth.Store
	authService      *AuthService
	discovery        *discovery.Service
	sshkeys          *sshkey.Service
	hwinfo           *hwinfo.Service
	hypervisors      *hypervisor.Service
	vms              *vm.Service
	cloudInits       *cloudinit.Service
	osimages         *osimage.Service
	leaseStore       pxe.LeaseStore
	agentTokenStore  hypervisor.AgentTokenStore
	filesDir         string
	imageStorageDir  string
	provisionTimeout time.Duration
	startTime        time.Time
	vmDeployer       *vm.Deployer
	vmRuntimeSyncer  *vm.RuntimeSyncer
	vmMigrator       *vm.Migrator
	vmRuntimeDeleter func(ctx context.Context, v vm.VirtualMachine) error
	bootenvs         *bootenv.Manager
	catalogMu        sync.Mutex
	catalogInstalls  map[string]struct{}
	setupMu          sync.Mutex
}

type PowerExecutor interface {
	Execute(ctx context.Context, m power.MachineInfo, action power.Action) error
	CheckStatus(ctx context.Context, m power.MachineInfo) (power.PowerState, error)
	ConfigureBootOrder(ctx context.Context, m power.MachineInfo, order power.BootOrder) error
}

type PowerExecutorWithResult interface {
	ExecuteWithResult(ctx context.Context, m power.MachineInfo, action power.Action) (power.ActionResult, error)
}

type AuthService struct {
	store      auth.Store
	sessionTTL time.Duration
}

func NewAuthService(store auth.Store, sessionTTL time.Duration) *AuthService {
	return &AuthService{store: store, sessionTTL: sessionTTL}
}

type ServerConfig struct {
	Machines         *machine.Service
	PowerExecutor    PowerExecutor
	Subnets          subnet.Store
	AuthStore        auth.Store
	AuthService      *AuthService
	Discovery        *discovery.Service
	SSHKeys          *sshkey.Service
	HWInfo           *hwinfo.Service
	Hypervisors      *hypervisor.Service
	VMs              *vm.Service
	CloudInits       *cloudinit.Service
	OSImages         *osimage.Service
	LeaseStore       pxe.LeaseStore
	AgentTokenStore  hypervisor.AgentTokenStore
	FilesDir         string
	ImageStorageDir  string
	PXEHTTPBaseURL   string
	PXETFTPRoot      string
	ProvisionTimeout time.Duration
	HealthCheck      func(ctx context.Context) error
	FrontendFS       fs.FS // embedded frontend assets (optional)
	VMDeployer       *vm.Deployer
	VMRuntimeSyncer  *vm.RuntimeSyncer
	VMMigrator       *vm.Migrator
	VMRuntimeDeleter func(ctx context.Context, v vm.VirtualMachine) error
	BootEnvs         *bootenv.Manager
}

func NewServer(cfg ServerConfig) *Server {
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.Recover())
	e.Use(middleware.RequestID())
	e.Use(middleware.CORS())

	s := &Server{
		echo:             e,
		machines:         cfg.Machines,
		powerExecutor:    cfg.PowerExecutor,
		subnets:          cfg.Subnets,
		authStore:        cfg.AuthStore,
		authService:      cfg.AuthService,
		discovery:        cfg.Discovery,
		sshkeys:          cfg.SSHKeys,
		hwinfo:           cfg.HWInfo,
		hypervisors:      cfg.Hypervisors,
		vms:              cfg.VMs,
		cloudInits:       cfg.CloudInits,
		osimages:         cfg.OSImages,
		leaseStore:       cfg.LeaseStore,
		agentTokenStore:  cfg.AgentTokenStore,
		filesDir:         cfg.FilesDir,
		imageStorageDir:  cfg.ImageStorageDir,
		provisionTimeout: cfg.ProvisionTimeout,
		startTime:        time.Now(),
		vmDeployer:       cfg.VMDeployer,
		vmRuntimeSyncer:  cfg.VMRuntimeSyncer,
		vmMigrator:       cfg.VMMigrator,
		vmRuntimeDeleter: cfg.VMRuntimeDeleter,
		bootenvs:         cfg.BootEnvs,
		catalogInstalls:  map[string]struct{}{},
	}
	if s.provisionTimeout <= 0 {
		s.provisionTimeout = 30 * time.Minute
	}

	healthFn := cfg.HealthCheck
	e.GET("/healthz", func(c echo.Context) error {
		if healthFn != nil {
			if err := healthFn(c.Request().Context()); err != nil {
				return c.JSON(gohttp.StatusServiceUnavailable, map[string]string{"status": "unhealthy", "error": err.Error()})
			}
		}
		return c.JSON(gohttp.StatusOK, map[string]string{"status": "ok"})
	})

	v1 := e.Group("/api/v1")
	v1.POST("/auth/login", s.Login)
	v1.GET("/setup/status", s.SetupStatus)
	v1.POST("/setup/admin", s.SetupAdmin)
	v1.POST("/machines/:name/hardware", s.ReportHardwareInfo)              // unauthenticated for PXE agents
	v1.POST("/machines/:name/power-events", s.ReportMachinePowerEvent)     // authenticated by WoL HMAC signature
	v1.POST("/hypervisors/register", s.RegisterHypervisor)                 // unauthenticated for hypervisor self-registration
	v1.GET("/hypervisors/setup-and-register.sh", s.SetupAndRegisterScript) // public script

	// Static file routes are public so that setup scripts can download agent binaries.
	e.GET("/files/*", s.ServeFile)

	// PXE routes are intentionally public to allow netboot clients to fetch assets.
	pxeH := pxehttp.NewHandler(pxehttp.Config{
		Machines:         cfg.Machines,
		VMs:              cfg.VMs,
		Subnets:          cfg.Subnets,
		CloudInits:       cfg.CloudInits,
		HWInfo:           cfg.HWInfo,
		OSImages:         cfg.OSImages,
		SSHKeys:          cfg.SSHKeys,
		LeaseStore:       cfg.LeaseStore,
		AuthStore:        cfg.AuthStore,
		Hypervisors:      cfg.Hypervisors,
		PowerExecutor:    cfg.PowerExecutor,
		PXEHTTPBaseURL:   cfg.PXEHTTPBaseURL,
		PXEFilesDir:      cfg.FilesDir,
		PXETFTPRoot:      cfg.PXETFTPRoot,
		ProvisionTimeout: cfg.ProvisionTimeout,
		VMRuntimeSyncer:  cfg.VMRuntimeSyncer,
	})
	e.GET("/pxe/boot.ipxe", pxeH.PXEBootScript)
	e.GET("/pxe/preseed.cfg", pxeH.PXEPreseed)
	e.GET("/pxe/nocloud/:mac/user-data", pxeH.PXENocloudUserData)
	e.GET("/pxe/nocloud/:mac/meta-data", pxeH.PXENocloudMetaData)
	e.GET("/pxe/nocloud/:mac/vendor-data", pxeH.PXENocloudVendorData)
	e.GET("/pxe/nocloud/:mac/network-config", pxeH.PXENocloudNetworkConfig)
	e.POST("/pxe/inventory", pxeH.PXEInventory)
	e.GET("/pxe/curtin-config", pxeH.PXECurtinConfig)
	e.POST("/pxe/deploy-events", pxeH.PXEDeployEvents)
	e.POST("/pxe/install-complete", pxeH.PXEInstallComplete)
	e.GET("/pxe/install-complete", pxeH.PXEInstallComplete)
	e.GET("/pxe/artifacts/os-images/:name/*", pxeH.PXEArtifact)
	e.GET("/pxe/files/*", pxeH.PXEFile)

	authed := v1.Group("", s.AuthMiddleware())
	authed.POST("/auth/logout", s.Logout)
	authed.GET("/me", s.Me)
	authed.GET("/system-info", s.SystemInfo)

	// Writer group: requires admin or operator role.
	writer := v1.Group("", s.AuthMiddleware(), RequireWriter())

	// Admin group: requires admin role.
	admin := v1.Group("", s.AuthMiddleware(), RequireAdmin())

	// Machine routes — reads are for all authenticated users, writes for operator+.
	authed.GET("/machines", s.ListMachines)
	authed.GET("/machines/:name", s.GetMachine)
	authed.GET("/machines/:name/hardware", s.GetHardwareInfo)
	authed.GET("/machines/:name/vnc", s.VNCProxy)
	writer.POST("/machines", s.CreateMachine)
	writer.POST("/machines/discover", s.DiscoverMachine)
	writer.DELETE("/machines/:name", s.DeleteMachine)
	writer.POST("/machines/:name/actions/redeploy", s.RedeployMachine)
	writer.POST("/machines/:name/actions/reinstall", s.ReinstallMachine)
	writer.POST("/machines/:name/actions/power-on", s.PowerOnMachine)
	writer.POST("/machines/:name/actions/power-off", s.PowerOffMachine)
	writer.PATCH("/machines/:name/settings", s.UpdateMachineSettings)
	writer.PATCH("/machines/:name/network", s.UpdateMachineNetwork)

	// Audit events — all authenticated users can read.
	authed.GET("/audit-events", s.ListAuditEvents)

	// Subnet routes — reads for all, writes for operator+.
	authed.GET("/subnets", s.ListSubnets)
	authed.GET("/subnets/:name", s.GetSubnet)
	writer.POST("/subnets", s.CreateSubnet)
	writer.PUT("/subnets/:name", s.UpdateSubnet)
	writer.DELETE("/subnets/:name", s.DeleteSubnet)

	// SSH key routes — reads for all (sanitized), writes for admin only (handles secrets).
	authed.GET("/ssh-keys", s.ListSSHKeys)
	authed.GET("/ssh-keys/:name", s.GetSSHKey)
	admin.POST("/ssh-keys", s.CreateSSHKey)
	admin.DELETE("/ssh-keys/:name", s.DeleteSSHKey)

	// User routes — admin only.
	admin.POST("/users", s.CreateUser)

	// Hypervisor routes — reads for all, management for admin only.
	authed.GET("/hypervisors", s.ListHypervisors)
	authed.GET("/hypervisors/:name", s.GetHypervisor)
	admin.POST("/hypervisors", s.CreateHypervisor)
	admin.POST("/hypervisors/registration-tokens", s.CreateRegistrationToken)
	admin.POST("/hypervisors/:name/agent-token", s.CreateAgentToken)
	admin.DELETE("/hypervisors/:name", s.DeleteHypervisor)

	// VirtualMachine routes — reads for all, writes for operator+.
	authed.GET("/virtual-machines", s.ListVirtualMachines)
	authed.GET("/virtual-machines/:name", s.GetVirtualMachine)
	writer.POST("/virtual-machines", s.CreateVirtualMachine)
	writer.DELETE("/virtual-machines/:name", s.DeleteVirtualMachine)
	writer.POST("/virtual-machines/:name/actions/power-on", s.PowerOnVM)
	writer.POST("/virtual-machines/:name/actions/power-off", s.PowerOffVM)
	writer.POST("/virtual-machines/:name/actions/reinstall", s.ReinstallVM)
	authed.GET("/virtual-machines/:name/vnc", s.VMVNCProxy)
	writer.POST("/virtual-machines/:name/actions/migrate", s.MigrateVM)
	writer.POST("/virtual-machines/:name/actions/redeploy", s.RedeployVM)

	// CloudInitTemplate routes — reads for all, writes for operator+.
	authed.GET("/cloud-init-templates", s.ListCloudInitTemplates)
	authed.GET("/cloud-init-templates/:name", s.GetCloudInitTemplate)
	writer.POST("/cloud-init-templates", s.CreateCloudInitTemplate)
	writer.PUT("/cloud-init-templates/:name", s.UpdateCloudInitTemplate)
	writer.DELETE("/cloud-init-templates/:name", s.DeleteCloudInitTemplate)

	// OSImage routes — reads for all, writes for operator+.
	authed.GET("/os-catalog", s.ListOSCatalog)
	writer.POST("/os-catalog/:name/install", s.InstallOSCatalogEntry)
	authed.GET("/boot-environments", s.ListBootEnvironments)
	authed.GET("/boot-environments/:name/logs", s.GetBootEnvironmentLogs)
	writer.POST("/boot-environments/:name/rebuild", s.RebuildBootEnvironment)
	authed.GET("/os-images", s.ListOSImages)
	authed.GET("/os-images/:name", s.GetOSImage)
	writer.POST("/os-images", s.CreateOSImage)
	writer.POST("/os-images/:name/upload", s.UploadOSImage)
	writer.PATCH("/os-images/:name/status", s.UpdateOSImageStatus)
	writer.DELETE("/os-images/:name", s.DeleteOSImage)
	authed.GET("/os-images/:name/download", s.DownloadOSImage)

	// DHCP Lease routes — reads for all authenticated users.
	authed.GET("/dhcp-leases", s.ListDHCPLeases)

	// Serve embedded frontend (SPA fallback)
	if cfg.FrontendFS != nil {
		fileHandler := gohttp.FileServer(gohttp.FS(cfg.FrontendFS))
		e.GET("/*", echo.WrapHandler(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
			// Try serving the requested file first.
			path := r.URL.Path
			if path == "/" {
				path = "index.html"
			}
			if _, err := fs.Stat(cfg.FrontendFS, path[1:]); err == nil {
				fileHandler.ServeHTTP(w, r)
				return
			}
			// Fall back to index.html for SPA client-side routing.
			r.URL.Path = "/"
			fileHandler.ServeHTTP(w, r)
		})))
	}

	return s
}

func (s *Server) Start(addr string) error {
	return s.echo.Start(addr)
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.echo.Shutdown(ctx)
}

func (s *Server) Echo() *echo.Echo {
	return s.echo
}
