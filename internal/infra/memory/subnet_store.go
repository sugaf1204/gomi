package memory

import (
	"context"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/subnet"
	"sort"
	"sync"
)

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
