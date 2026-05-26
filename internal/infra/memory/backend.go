package memory

import (
	"context"
	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/pxe"
	"github.com/sugaf1204/gomi/internal/sshkey"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
	"sync"
)

type Backend struct {
	mu          sync.RWMutex
	machines    map[string]machine.Machine
	subnets     map[string]subnet.Subnet
	users       map[string]auth.User
	sessions    map[string]auth.Session
	auditEvents map[string]auth.AuditEvent
	sshkeys     map[string]sshkey.SSHKey
	hwinfos     map[string]hwinfo.HardwareInfo
	hypervisors map[string]hypervisor.Hypervisor
	regTokens   map[string]hypervisor.RegistrationToken
	agentTokens map[string]hypervisor.AgentToken // key = token value
	vms         map[string]vm.VirtualMachine
	cloudInits  map[string]cloudinit.CloudInitTemplate
	osimages    map[string]osimage.OSImage
	dhcpLeases  map[string]pxe.DHCPLease
}

func New() *Backend {
	return &Backend{
		machines:    make(map[string]machine.Machine),
		subnets:     make(map[string]subnet.Subnet),
		users:       make(map[string]auth.User),
		sessions:    make(map[string]auth.Session),
		auditEvents: make(map[string]auth.AuditEvent),
		sshkeys:     make(map[string]sshkey.SSHKey),
		hwinfos:     make(map[string]hwinfo.HardwareInfo),
		hypervisors: make(map[string]hypervisor.Hypervisor),
		regTokens:   make(map[string]hypervisor.RegistrationToken),
		agentTokens: make(map[string]hypervisor.AgentToken),
		vms:         make(map[string]vm.VirtualMachine),
		cloudInits:  make(map[string]cloudinit.CloudInitTemplate),
		osimages:    make(map[string]osimage.OSImage),
		dhcpLeases:  make(map[string]pxe.DHCPLease),
	}
}

// Machines returns a machine.Store implementation.
func (b *Backend) Machines() *MachineStore { return &MachineStore{b: b} }

// Subnets returns a subnet.Store implementation.
func (b *Backend) Subnets() *SubnetStore { return &SubnetStore{b: b} }

// Auth returns an auth.Store implementation.
func (b *Backend) Auth() *AuthStore { return &AuthStore{b: b} }

// SSHKeys returns a sshkey.Store implementation.
func (b *Backend) SSHKeys() *SSHKeyStore { return &SSHKeyStore{b: b} }

// HWInfo returns a hwinfo.Store implementation.
func (b *Backend) HWInfo() *HWInfoStore { return &HWInfoStore{b: b} }

// Hypervisors returns a hypervisor.Store implementation.
func (b *Backend) Hypervisors() *HypervisorStore { return &HypervisorStore{b: b} }

// HypervisorTokens returns a hypervisor.TokenStore implementation.
func (b *Backend) HypervisorTokens() *RegTokenStore { return &RegTokenStore{b: b} }

// AgentTokens returns a hypervisor.AgentTokenStore implementation.
func (b *Backend) AgentTokens() *AgentTokenStore { return &AgentTokenStore{b: b} }

// VMs returns a vm.Store implementation.
func (b *Backend) VMs() *VMStore { return &VMStore{b: b} }

// CloudInits returns a cloudinit.Store implementation.
func (b *Backend) CloudInits() *CloudInitStore { return &CloudInitStore{b: b} }

// OSImages returns an osimage.Store implementation.
func (b *Backend) OSImages() *OSImageStore { return &OSImageStore{b: b} }

// DHCPLeases returns a pxe.LeaseStore implementation.
func (b *Backend) DHCPLeases() *DHCPLeaseStore { return &DHCPLeaseStore{b: b} }

func (b *Backend) Health(_ context.Context) error { return nil }
func (b *Backend) Close() error                   { return nil }
