package memory

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/pxe"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/sshkey"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
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

// --- MachineStore ---

type MachineStore struct {
	b         *Backend
	mu        sync.Mutex
	listeners []func()
}

var _ machine.Store = (*MachineStore)(nil)
var _ machine.ChangeNotifier = (*MachineStore)(nil)
var _ machine.PowerActionStatusUpdater = (*MachineStore)(nil)
var _ machine.PowerStateStatusUpdater = (*MachineStore)(nil)
var _ machine.IPAddressUpdater = (*MachineStore)(nil)

func (s *MachineStore) Subscribe(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, fn)
}

func (s *MachineStore) notify() {
	s.mu.Lock()
	fns := make([]func(), len(s.listeners))
	copy(fns, s.listeners)
	s.mu.Unlock()
	for _, fn := range fns {
		go fn()
	}
}

func (s *MachineStore) Upsert(_ context.Context, m machine.Machine) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.machines[m.Name] = m
	s.notify()
	return nil
}

func (s *MachineStore) UpdatePowerActionStatus(_ context.Context, name string, action power.Action, lastError *string, updatedAt time.Time) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	m, ok := s.b.machines[name]
	if !ok {
		return resource.ErrNotFound
	}
	m.LastPowerAction = string(action)
	if lastError != nil {
		m.LastError = *lastError
	}
	m.UpdatedAt = updatedAt
	s.b.machines[name] = m
	s.notify()
	return nil
}

func (s *MachineStore) UpdatePowerStateStatus(_ context.Context, name string, state power.PowerState, stateAt time.Time, updatedAt time.Time) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	m, ok := s.b.machines[name]
	if !ok {
		return resource.ErrNotFound
	}
	m.PowerState = state
	m.PowerStateAt = &stateAt
	m.UpdatedAt = updatedAt
	s.b.machines[name] = m
	s.notify()
	return nil
}

func (s *MachineStore) UpdateDynamicIPAddress(_ context.Context, name string, expectedMAC string, ip string, updatedAt time.Time) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	m, ok := s.b.machines[name]
	if !ok {
		return resource.ErrNotFound
	}
	if strings.ToLower(m.MAC) != strings.ToLower(expectedMAC) || m.IPAssignment == machine.IPAssignmentModeStatic {
		return nil
	}
	m.IP = ip
	m.UpdatedAt = updatedAt
	s.b.machines[name] = m
	s.notify()
	return nil
}

func (s *MachineStore) Get(_ context.Context, name string) (machine.Machine, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	m, ok := s.b.machines[name]
	if !ok {
		return machine.Machine{}, resource.ErrNotFound
	}
	return m, nil
}

func (s *MachineStore) List(_ context.Context) ([]machine.Machine, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	out := make([]machine.Machine, 0, len(s.b.machines))
	for _, m := range s.b.machines {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *MachineStore) GetByMAC(_ context.Context, mac string) (machine.Machine, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	normalizedMAC := strings.ToLower(mac)
	for _, m := range s.b.machines {
		if strings.ToLower(m.MAC) == normalizedMAC {
			return m, nil
		}
	}
	return machine.Machine{}, resource.ErrNotFound
}

func (s *MachineStore) Delete(_ context.Context, name string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	if _, ok := s.b.machines[name]; !ok {
		return resource.ErrNotFound
	}
	delete(s.b.machines, name)
	s.notify()
	return nil
}

// --- SubnetStore ---

type SubnetStore struct {
	b         *Backend
	mu        sync.Mutex
	listeners []func()
}

var _ subnet.Store = (*SubnetStore)(nil)
var _ subnet.ChangeNotifier = (*SubnetStore)(nil)

func (s *SubnetStore) Subscribe(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, fn)
}

func (s *SubnetStore) notify() {
	s.mu.Lock()
	fns := make([]func(), len(s.listeners))
	copy(fns, s.listeners)
	s.mu.Unlock()
	for _, fn := range fns {
		go fn()
	}
}

func (s *SubnetStore) Upsert(_ context.Context, sub subnet.Subnet) error {
	s.b.mu.Lock()
	s.b.subnets[sub.Name] = sub
	s.b.mu.Unlock()
	s.notify()
	return nil
}

func (s *SubnetStore) Get(_ context.Context, name string) (subnet.Subnet, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	sub, ok := s.b.subnets[name]
	if !ok {
		return subnet.Subnet{}, resource.ErrNotFound
	}
	return sub, nil
}

func (s *SubnetStore) List(_ context.Context) ([]subnet.Subnet, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	out := make([]subnet.Subnet, 0, len(s.b.subnets))
	for _, sub := range s.b.subnets {
		out = append(out, sub)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *SubnetStore) Delete(_ context.Context, name string) error {
	s.b.mu.Lock()
	if _, ok := s.b.subnets[name]; !ok {
		s.b.mu.Unlock()
		return resource.ErrNotFound
	}
	delete(s.b.subnets, name)
	s.b.mu.Unlock()
	s.notify()
	return nil
}

// --- AuthStore ---

type AuthStore struct{ b *Backend }

var _ auth.Store = (*AuthStore)(nil)

func (s *AuthStore) UpsertUser(_ context.Context, user auth.User) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.users[user.Username] = user
	return nil
}

func (s *AuthStore) GetUser(_ context.Context, username string) (auth.User, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	u, ok := s.b.users[username]
	if !ok {
		return auth.User{}, resource.ErrNotFound
	}
	return u, nil
}

func (s *AuthStore) CountUsers(_ context.Context) (int, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	return len(s.b.users), nil
}

func (s *AuthStore) CreateSession(_ context.Context, session auth.Session) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.sessions[session.Token] = session
	return nil
}

func (s *AuthStore) GetSession(_ context.Context, token string) (auth.Session, error) {
	s.b.mu.RLock()
	sess, ok := s.b.sessions[token]
	s.b.mu.RUnlock()
	if !ok {
		return auth.Session{}, resource.ErrNotFound
	}
	if time.Now().After(sess.ExpiresAt) {
		s.b.mu.Lock()
		delete(s.b.sessions, token)
		s.b.mu.Unlock()
		return auth.Session{}, resource.ErrNotFound
	}
	return sess, nil
}

func (s *AuthStore) DeleteSession(_ context.Context, token string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	delete(s.b.sessions, token)
	return nil
}

func (s *AuthStore) CreateAuditEvent(_ context.Context, event auth.AuditEvent) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.auditEvents[event.ID] = event
	return nil
}

func (s *AuthStore) ListAuditEvents(_ context.Context, machineName string, limit int) ([]auth.AuditEvent, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	out := make([]auth.AuditEvent, 0)
	for _, event := range s.b.auditEvents {
		if machineName != "" && !strings.EqualFold(event.Machine, machineName) {
			continue
		}
		out = append(out, event)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		return out[:limit], nil
	}
	return out, nil
}

// --- SSHKeyStore ---

type SSHKeyStore struct{ b *Backend }

var _ sshkey.Store = (*SSHKeyStore)(nil)

func (s *SSHKeyStore) Upsert(_ context.Context, k sshkey.SSHKey) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.sshkeys[k.Name] = k
	return nil
}

func (s *SSHKeyStore) Get(_ context.Context, name string) (sshkey.SSHKey, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	k, ok := s.b.sshkeys[name]
	if !ok {
		return sshkey.SSHKey{}, resource.ErrNotFound
	}
	return k, nil
}

func (s *SSHKeyStore) List(_ context.Context) ([]sshkey.SSHKey, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	out := make([]sshkey.SSHKey, 0, len(s.b.sshkeys))
	for _, sk := range s.b.sshkeys {
		out = append(out, sk)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *SSHKeyStore) Delete(_ context.Context, name string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	if _, ok := s.b.sshkeys[name]; !ok {
		return resource.ErrNotFound
	}
	delete(s.b.sshkeys, name)
	return nil
}

// --- HWInfoStore ---

type HWInfoStore struct{ b *Backend }

var _ hwinfo.Store = (*HWInfoStore)(nil)

func (s *HWInfoStore) Upsert(_ context.Context, info hwinfo.HardwareInfo) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.hwinfos[info.MachineName] = info
	return nil
}

func (s *HWInfoStore) Get(_ context.Context, machineName string) (hwinfo.HardwareInfo, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	info, ok := s.b.hwinfos[machineName]
	if !ok {
		return hwinfo.HardwareInfo{}, resource.ErrNotFound
	}
	return info, nil
}

func (s *HWInfoStore) Delete(_ context.Context, machineName string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	if _, ok := s.b.hwinfos[machineName]; !ok {
		return resource.ErrNotFound
	}
	delete(s.b.hwinfos, machineName)
	return nil
}

// --- HypervisorStore ---

type HypervisorStore struct{ b *Backend }

var _ hypervisor.Store = (*HypervisorStore)(nil)

func (s *HypervisorStore) Upsert(_ context.Context, h hypervisor.Hypervisor) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.hypervisors[h.Name] = h
	return nil
}

func (s *HypervisorStore) Get(_ context.Context, name string) (hypervisor.Hypervisor, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	h, ok := s.b.hypervisors[name]
	if !ok {
		return hypervisor.Hypervisor{}, resource.ErrNotFound
	}
	return h, nil
}

func (s *HypervisorStore) List(_ context.Context) ([]hypervisor.Hypervisor, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	out := make([]hypervisor.Hypervisor, 0, len(s.b.hypervisors))
	for _, h := range s.b.hypervisors {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *HypervisorStore) Delete(_ context.Context, name string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	if _, ok := s.b.hypervisors[name]; !ok {
		return resource.ErrNotFound
	}
	delete(s.b.hypervisors, name)
	return nil
}

// --- RegTokenStore ---

type RegTokenStore struct{ b *Backend }

var _ hypervisor.TokenStore = (*RegTokenStore)(nil)

func (s *RegTokenStore) Create(_ context.Context, token hypervisor.RegistrationToken) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.regTokens[token.Token] = token
	return nil
}

func (s *RegTokenStore) Get(_ context.Context, tokenValue string) (hypervisor.RegistrationToken, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	t, ok := s.b.regTokens[tokenValue]
	if !ok {
		return hypervisor.RegistrationToken{}, resource.ErrNotFound
	}
	return t, nil
}

func (s *RegTokenStore) MarkUsed(_ context.Context, tokenValue, usedBy string) (hypervisor.RegistrationToken, error) {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	t, ok := s.b.regTokens[tokenValue]
	if !ok {
		return hypervisor.RegistrationToken{}, resource.ErrNotFound
	}
	if t.Used {
		return hypervisor.RegistrationToken{}, errors.New("token already used")
	}
	if time.Now().After(t.ExpiresAt) {
		return hypervisor.RegistrationToken{}, errors.New("token expired")
	}
	t.Used = true
	t.UsedBy = usedBy
	s.b.regTokens[tokenValue] = t
	return t, nil
}

func (s *RegTokenStore) List(_ context.Context) ([]hypervisor.RegistrationToken, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	out := make([]hypervisor.RegistrationToken, 0, len(s.b.regTokens))
	for _, t := range s.b.regTokens {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

// --- AgentTokenStore ---

type AgentTokenStore struct{ b *Backend }

var _ hypervisor.AgentTokenStore = (*AgentTokenStore)(nil)

func (s *AgentTokenStore) Create(_ context.Context, token hypervisor.AgentToken) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.agentTokens[token.Token] = token
	return nil
}

func (s *AgentTokenStore) GetByToken(_ context.Context, tokenValue string) (hypervisor.AgentToken, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	t, ok := s.b.agentTokens[tokenValue]
	if !ok {
		return hypervisor.AgentToken{}, resource.ErrNotFound
	}
	return t, nil
}

func (s *AgentTokenStore) DeleteByHypervisor(_ context.Context, hypervisorName string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	for k, t := range s.b.agentTokens {
		if t.HypervisorName == hypervisorName {
			delete(s.b.agentTokens, k)
		}
	}
	return nil
}

// --- VMStore ---

type VMStore struct {
	b         *Backend
	mu        sync.Mutex
	listeners []func()
}

var _ vm.Store = (*VMStore)(nil)
var _ vm.ChangeNotifier = (*VMStore)(nil)

func (s *VMStore) Subscribe(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, fn)
}

func (s *VMStore) notify() {
	s.mu.Lock()
	fns := make([]func(), len(s.listeners))
	copy(fns, s.listeners)
	s.mu.Unlock()
	for _, fn := range fns {
		go fn()
	}
}

func (s *VMStore) Upsert(_ context.Context, v vm.VirtualMachine) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.vms[v.Name] = v
	s.notify()
	return nil
}

func (s *VMStore) Get(_ context.Context, name string) (vm.VirtualMachine, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	v, ok := s.b.vms[name]
	if !ok {
		return vm.VirtualMachine{}, resource.ErrNotFound
	}
	return v, nil
}

func (s *VMStore) List(_ context.Context) ([]vm.VirtualMachine, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	out := make([]vm.VirtualMachine, 0, len(s.b.vms))
	for _, v := range s.b.vms {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *VMStore) ListByHypervisor(_ context.Context, hypervisorName string) ([]vm.VirtualMachine, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	out := make([]vm.VirtualMachine, 0)
	for _, v := range s.b.vms {
		if v.HypervisorRef == hypervisorName {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *VMStore) Delete(_ context.Context, name string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	if _, ok := s.b.vms[name]; !ok {
		return resource.ErrNotFound
	}
	delete(s.b.vms, name)
	s.notify()
	return nil
}

// --- CloudInitStore ---

type CloudInitStore struct{ b *Backend }

var _ cloudinit.Store = (*CloudInitStore)(nil)

func (s *CloudInitStore) Upsert(_ context.Context, t cloudinit.CloudInitTemplate) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.cloudInits[t.Name] = t
	return nil
}

func (s *CloudInitStore) Get(_ context.Context, name string) (cloudinit.CloudInitTemplate, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	t, ok := s.b.cloudInits[name]
	if !ok {
		return cloudinit.CloudInitTemplate{}, resource.ErrNotFound
	}
	return t, nil
}

func (s *CloudInitStore) List(_ context.Context) ([]cloudinit.CloudInitTemplate, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	out := make([]cloudinit.CloudInitTemplate, 0, len(s.b.cloudInits))
	for _, t := range s.b.cloudInits {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *CloudInitStore) Delete(_ context.Context, name string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	if _, ok := s.b.cloudInits[name]; !ok {
		return resource.ErrNotFound
	}
	delete(s.b.cloudInits, name)
	return nil
}

// --- OSImageStore ---

type OSImageStore struct{ b *Backend }

var _ osimage.Store = (*OSImageStore)(nil)

func (s *OSImageStore) Upsert(_ context.Context, img osimage.OSImage) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.osimages[img.Name] = img
	return nil
}

func (s *OSImageStore) Get(_ context.Context, name string) (osimage.OSImage, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	img, ok := s.b.osimages[name]
	if !ok {
		return osimage.OSImage{}, resource.ErrNotFound
	}
	return img, nil
}

func (s *OSImageStore) List(_ context.Context) ([]osimage.OSImage, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	out := make([]osimage.OSImage, 0, len(s.b.osimages))
	for _, img := range s.b.osimages {
		out = append(out, img)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *OSImageStore) Delete(_ context.Context, name string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	if _, ok := s.b.osimages[name]; !ok {
		return resource.ErrNotFound
	}
	delete(s.b.osimages, name)
	return nil
}

// --- DHCPLeaseStore ---

type DHCPLeaseStore struct{ b *Backend }

var _ pxe.LeaseStore = (*DHCPLeaseStore)(nil)

func (s *DHCPLeaseStore) Upsert(_ context.Context, lease pxe.DHCPLease) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.dhcpLeases[lease.MAC] = lease
	return nil
}

func (s *DHCPLeaseStore) List(_ context.Context) ([]pxe.DHCPLease, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	out := make([]pxe.DHCPLease, 0, len(s.b.dhcpLeases))
	for _, l := range s.b.dhcpLeases {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].MAC < out[j].MAC
	})
	return out, nil
}

func (s *DHCPLeaseStore) Delete(_ context.Context, mac string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	delete(s.b.dhcpLeases, mac)
	return nil
}
