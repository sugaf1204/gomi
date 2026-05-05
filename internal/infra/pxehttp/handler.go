package pxehttp

import (
	"context"
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/pxe"
	"github.com/sugaf1204/gomi/internal/sshkey"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
)

type PowerExecutor interface {
	ConfigureBootOrder(ctx context.Context, m power.MachineInfo, order power.BootOrder) error
}

type Handler struct {
	machines         *machine.Service
	vms              *vm.Service
	subnets          subnet.Store
	cloudInits       *cloudinit.Service
	hwinfo           *hwinfo.Service
	osimages         *osimage.Service
	sshkeys          *sshkey.Service
	leaseStore       pxe.LeaseStore
	authStore        auth.Store
	hypervisors      *hypervisor.Service
	powerExecutor    PowerExecutor
	pxeHTTPBaseURL   string
	pxeFilesDir      string
	pxeTFTPRoot      string
	provisionTimeout time.Duration
	vmRuntimeSyncer  *vm.RuntimeSyncer
}

type Config struct {
	Machines         *machine.Service
	VMs              *vm.Service
	Subnets          subnet.Store
	CloudInits       *cloudinit.Service
	HWInfo           *hwinfo.Service
	OSImages         *osimage.Service
	SSHKeys          *sshkey.Service
	LeaseStore       pxe.LeaseStore
	AuthStore        auth.Store
	Hypervisors      *hypervisor.Service
	PowerExecutor    PowerExecutor
	PXEHTTPBaseURL   string
	PXEFilesDir      string
	PXETFTPRoot      string
	ProvisionTimeout time.Duration
	VMRuntimeSyncer  *vm.RuntimeSyncer
}

func NewHandler(cfg Config) *Handler {
	h := &Handler{
		machines:         cfg.Machines,
		vms:              cfg.VMs,
		subnets:          cfg.Subnets,
		cloudInits:       cfg.CloudInits,
		hwinfo:           cfg.HWInfo,
		osimages:         cfg.OSImages,
		sshkeys:          cfg.SSHKeys,
		leaseStore:       cfg.LeaseStore,
		authStore:        cfg.AuthStore,
		hypervisors:      cfg.Hypervisors,
		powerExecutor:    cfg.PowerExecutor,
		pxeHTTPBaseURL:   cfg.PXEHTTPBaseURL,
		pxeFilesDir:      cfg.PXEFilesDir,
		pxeTFTPRoot:      cfg.PXETFTPRoot,
		provisionTimeout: cfg.ProvisionTimeout,
		vmRuntimeSyncer:  cfg.VMRuntimeSyncer,
	}
	if h.provisionTimeout <= 0 {
		h.provisionTimeout = 30 * time.Minute
	}
	return h
}
