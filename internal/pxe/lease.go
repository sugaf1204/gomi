package pxe

import (
	"context"
	"encoding/binary"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

// leasePool manages IP address allocation from an address range.
type leasePool struct {
	mu           sync.Mutex
	start        net.IP
	end          net.IP
	leases       map[string]net.IP // MAC -> IP
	reservations map[string]net.IP // MAC -> reserved IP
	store        LeaseStore
}

func newLeasePool(start, end string, store LeaseStore) *leasePool {
	p := &leasePool{
		start:        net.ParseIP(start).To4(),
		end:          net.ParseIP(end).To4(),
		leases:       make(map[string]net.IP),
		reservations: make(map[string]net.IP),
		store:        store,
	}
	p.restore()
	return p
}

// restore loads existing leases from the store on startup.
func (p *leasePool) restore() {
	if p.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	leases, err := p.store.List(ctx)
	if err != nil {
		log.Printf("dhcp: failed to restore leases: %v", err)
		return
	}
	restored := 0
	skipped := 0
	for _, l := range leases {
		key := strings.ToLower(strings.TrimSpace(l.MAC))
		ip := net.ParseIP(l.IP).To4()
		if key == "" || ip == nil || !p.inRange(ip) {
			skipped++
			if key != "" {
				if err := p.store.Delete(ctx, key); err != nil {
					log.Printf("dhcp: failed to delete stale lease %s: %v", key, err)
				}
			}
			continue
		}
		p.leases[key] = ip
		restored++
	}
	if restored > 0 || skipped > 0 {
		log.Printf("dhcp: restored %d leases from store, skipped %d stale leases", restored, skipped)
	}
}

// UpdateReservations replaces the current set of static DHCP reservations.
func (p *leasePool) UpdateReservations(reservations map[string]net.IP) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reservations = reservations
}

// Allocate returns an IP for the given MAC, reusing a previous lease if one
// exists or picking the next free address from the pool.
// Static reservations take priority over dynamic allocation.
func (p *leasePool) Allocate(mac net.HardwareAddr, hostname string, pxeClient bool) net.IP {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := mac.String()

	// Check static reservations first.
	if reservedIP, ok := p.reservations[key]; ok {
		p.leases[key] = reservedIP
		p.persistAsync(key, reservedIP, hostname, pxeClient)
		return reservedIP
	}

	if ip, ok := p.leases[key]; ok {
		if p.inRange(ip) {
			p.persistAsync(key, ip, hostname, pxeClient)
			return ip
		}
		delete(p.leases, key)
		p.deleteLeaseAsync(key)
	}

	startN := binary.BigEndian.Uint32(p.start)
	endN := binary.BigEndian.Uint32(p.end)

	used := make(map[uint32]bool, len(p.leases))
	for _, ip := range p.leases {
		used[binary.BigEndian.Uint32(ip.To4())] = true
	}
	// Also mark reserved IPs as used to avoid conflicts.
	for _, ip := range p.reservations {
		used[binary.BigEndian.Uint32(ip.To4())] = true
	}

	for n := startN; n <= endN; n++ {
		if used[n] {
			continue
		}
		ip := make(net.IP, 4)
		binary.BigEndian.PutUint32(ip, n)
		p.leases[key] = ip
		p.persistAsync(key, ip, hostname, pxeClient)
		return ip
	}

	return nil // pool exhausted
}

// persistAsync writes the lease to the store in a background goroutine.
func (p *leasePool) persistAsync(mac string, ip net.IP, hostname string, pxeClient bool) {
	if p.store == nil {
		return
	}
	lease := DHCPLease{
		MAC:       mac,
		IP:        ip.String(),
		Hostname:  hostname,
		PXEClient: pxeClient,
		LeasedAt:  time.Now(),
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := p.store.Upsert(ctx, lease); err != nil {
			log.Printf("dhcp: failed to persist lease %s -> %s: %v", mac, ip, err)
		}
	}()
}

func (p *leasePool) deleteLeaseAsync(mac string) {
	if p.store == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := p.store.Delete(ctx, mac); err != nil {
			log.Printf("dhcp: failed to delete lease %s: %v", mac, err)
		}
	}()
}

func (p *leasePool) inRange(ip net.IP) bool {
	ip = ip.To4()
	if ip == nil || p.start == nil || p.end == nil {
		return false
	}
	n := binary.BigEndian.Uint32(ip)
	return n >= binary.BigEndian.Uint32(p.start) && n <= binary.BigEndian.Uint32(p.end)
}

// Release frees the lease for the given MAC address.
func (p *leasePool) Release(mac net.HardwareAddr) {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := mac.String()
	delete(p.leases, key)

	p.deleteLeaseAsync(key)
}
