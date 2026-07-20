package pxe

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

func TestLeasePool_Allocate(t *testing.T) {
	p := newLeasePool("10.0.0.10", "10.0.0.12", nil, 0)

	mac1, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")
	mac2, _ := net.ParseMAC("aa:bb:cc:dd:ee:02")
	mac3, _ := net.ParseMAC("aa:bb:cc:dd:ee:03")
	mac4, _ := net.ParseMAC("aa:bb:cc:dd:ee:04")

	ip1 := p.Allocate(mac1, "", false)
	if ip1.String() != "10.0.0.10" {
		t.Fatalf("expected 10.0.0.10, got %s", ip1)
	}

	// Same MAC should return the same IP.
	ip1b := p.Allocate(mac1, "", false)
	if !ip1.Equal(ip1b) {
		t.Fatalf("expected same IP for same MAC, got %s vs %s", ip1, ip1b)
	}

	ip2 := p.Allocate(mac2, "", false)
	if ip2.String() != "10.0.0.11" {
		t.Fatalf("expected 10.0.0.11, got %s", ip2)
	}

	ip3 := p.Allocate(mac3, "", false)
	if ip3.String() != "10.0.0.12" {
		t.Fatalf("expected 10.0.0.12, got %s", ip3)
	}

	// Pool should be exhausted.
	ip4 := p.Allocate(mac4, "", false)
	if ip4 != nil {
		t.Fatalf("expected nil (pool exhausted), got %s", ip4)
	}
}

func TestLeasePool_Release(t *testing.T) {
	p := newLeasePool("10.0.0.10", "10.0.0.10", nil, 0)

	mac1, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")
	mac2, _ := net.ParseMAC("aa:bb:cc:dd:ee:02")

	ip1 := p.Allocate(mac1, "", false)
	if ip1 == nil {
		t.Fatal("expected an IP")
	}

	// Pool is full.
	if ip := p.Allocate(mac2, "", false); ip != nil {
		t.Fatalf("expected nil, got %s", ip)
	}

	// Release and re-allocate.
	p.Release(mac1)
	ip2 := p.Allocate(mac2, "", false)
	if ip2.String() != "10.0.0.10" {
		t.Fatalf("expected 10.0.0.10 after release, got %s", ip2)
	}
}

func TestLeasePool_RestoreSkipsLeasesOutsideRange(t *testing.T) {
	store := &testLeaseStore{
		leases: map[string]DHCPLease{
			"aa:bb:cc:dd:ee:01": {
				MAC:      "aa:bb:cc:dd:ee:01",
				IP:       "10.0.0.10",
				LeasedAt: time.Now(),
			},
			"aa:bb:cc:dd:ee:02": {
				MAC:      "aa:bb:cc:dd:ee:02",
				IP:       "10.0.0.99",
				LeasedAt: time.Now(),
			},
		},
	}

	p := newLeasePool("10.0.0.10", "10.0.0.10", store, 0)

	mac1, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")
	if ip := p.Allocate(mac1, "", false); ip == nil || ip.String() != "10.0.0.10" {
		t.Fatalf("expected restored in-range lease, got %v", ip)
	}

	mac2, _ := net.ParseMAC("aa:bb:cc:dd:ee:02")
	if ip := p.Allocate(mac2, "", false); ip != nil {
		t.Fatalf("expected stale out-of-range lease to be skipped and pool exhausted, got %s", ip)
	}
	if !store.wasDeleted("aa:bb:cc:dd:ee:02") {
		t.Fatal("expected stale lease to be deleted")
	}
}

func TestLeasePool_AllocateReclaimsExpiredLease(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := base
	p := newLeasePool("10.0.0.10", "10.0.0.10", nil, time.Hour)
	p.now = func() time.Time { return clock }

	mac1, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")
	mac2, _ := net.ParseMAC("aa:bb:cc:dd:ee:02")

	if ip := p.Allocate(mac1, "", false); ip == nil || ip.String() != "10.0.0.10" {
		t.Fatalf("expected 10.0.0.10, got %v", ip)
	}

	// Still within TTL: the single-address pool is full for a new MAC.
	clock = base.Add(30 * time.Minute)
	if ip := p.Allocate(mac2, "", false); ip != nil {
		t.Fatalf("expected pool full within TTL, got %s", ip)
	}

	// Past TTL without renewal: mac1's lease is reclaimed for mac2.
	clock = base.Add(2 * time.Hour)
	if ip := p.Allocate(mac2, "", false); ip == nil || ip.String() != "10.0.0.10" {
		t.Fatalf("expected reclaimed 10.0.0.10 after TTL, got %v", ip)
	}
}

func TestLeasePool_AllocateRenewsOwnLeaseBeforeExpiry(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := base
	p := newLeasePool("10.0.0.10", "10.0.0.11", nil, time.Hour)
	p.now = func() time.Time { return clock }

	mac1, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")

	ip1 := p.Allocate(mac1, "", false)

	// Re-allocating just before expiry renews the timestamp and keeps the IP.
	clock = base.Add(59 * time.Minute)
	if ip := p.Allocate(mac1, "", false); !ip.Equal(ip1) {
		t.Fatalf("expected renewed same IP, got %s vs %s", ip, ip1)
	}

	// The renewal moved leasedAt forward, so an hour after the original issue
	// the lease is still valid and keeps its address.
	clock = base.Add(90 * time.Minute)
	if ip := p.Allocate(mac1, "", false); !ip.Equal(ip1) {
		t.Fatalf("expected renewed lease to survive, got %s", ip)
	}
}

func TestLeasePool_RestoreSkipsExpiredLease(t *testing.T) {
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &testLeaseStore{
		leases: map[string]DHCPLease{
			"aa:bb:cc:dd:ee:01": {MAC: "aa:bb:cc:dd:ee:01", IP: "10.0.0.10", LeasedAt: old},
		},
	}

	p := newLeasePool("10.0.0.10", "10.0.0.10", store, time.Hour)

	mac2, _ := net.ParseMAC("aa:bb:cc:dd:ee:02")
	if ip := p.Allocate(mac2, "", false); ip == nil || ip.String() != "10.0.0.10" {
		t.Fatalf("expected expired lease to be reclaimable, got %v", ip)
	}
	if !store.wasDeleted("aa:bb:cc:dd:ee:01") {
		t.Fatal("expected expired lease to be deleted on restore")
	}
}

func TestLeasePool_ZeroTTLNeverExpires(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := base
	p := newLeasePool("10.0.0.10", "10.0.0.10", nil, 0)
	p.now = func() time.Time { return clock }

	mac1, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")
	mac2, _ := net.ParseMAC("aa:bb:cc:dd:ee:02")

	p.Allocate(mac1, "", false)
	clock = base.Add(1000 * time.Hour)
	if ip := p.Allocate(mac2, "", false); ip != nil {
		t.Fatalf("expected no expiry with zero TTL, got %s", ip)
	}
}

type testLeaseStore struct {
	mu      sync.Mutex
	leases  map[string]DHCPLease
	deleted map[string]bool
}

func (s *testLeaseStore) Upsert(_ context.Context, lease DHCPLease) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leases == nil {
		s.leases = map[string]DHCPLease{}
	}
	s.leases[lease.MAC] = lease
	return nil
}

func (s *testLeaseStore) List(_ context.Context) ([]DHCPLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]DHCPLease, 0, len(s.leases))
	for _, lease := range s.leases {
		out = append(out, lease)
	}
	return out, nil
}

func (s *testLeaseStore) Delete(_ context.Context, mac string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleted == nil {
		s.deleted = map[string]bool{}
	}
	s.deleted[mac] = true
	delete(s.leases, mac)
	return nil
}

func (s *testLeaseStore) wasDeleted(mac string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleted[mac]
}
