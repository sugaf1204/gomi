package memory

import (
	"context"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
	"sort"
	"strings"
	"sync"
	"time"
)

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

// ListPage returns one page of machines (ordered by name) plus the total count.
// It implements machine.PageLister.
func (s *MachineStore) ListPage(ctx context.Context, offset, limit int) ([]machine.Machine, int, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, 0, err
	}
	total := len(all)
	if limit <= 0 || offset >= total {
		return []machine.Machine{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
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
