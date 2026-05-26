package memory

import (
	"context"
	"errors"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/sshkey"
	"sort"
	"time"
)

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
