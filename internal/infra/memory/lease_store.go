package memory

import (
	"context"
	"github.com/sugaf1204/gomi/internal/pxe"
	"sort"
)

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
