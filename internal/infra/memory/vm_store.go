package memory

import (
	"context"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
	"sort"
	"sync"
)

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

// ListPage returns one page of virtual machines (ordered by name) plus the
// total count. It implements vm.PageLister.
func (s *VMStore) ListPage(ctx context.Context, offset, limit int) ([]vm.VirtualMachine, int, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, 0, err
	}
	total := len(all)
	if limit <= 0 || offset >= total {
		return []vm.VirtualMachine{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
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
