package api

import (
	"context"
	"log"
	"strings"

	"github.com/sugaf1204/gomi/internal/vm"
)

// releaseVMLeases frees the DHCP lease(s) held by a deleted VM so its
// address(es) return to the pool immediately instead of lingering until the
// lease TTL expires. It is best-effort: failures are logged but never block
// the delete, which has already removed the VM record by the time this runs.
//
// A VM may declare MACs on its spec interfaces and observe more on its status
// interfaces (e.g. after cloud-init assigns them); both are released. VMs that
// use static IPs still receive no dynamic lease, so releasing their MAC is a
// harmless no-op.
func (s *Server) releaseVMLeases(ctx context.Context, v vm.VirtualMachine) {
	for _, mac := range vmLeaseMACs(v) {
		if err := s.releaseLease(ctx, mac); err != nil {
			log.Printf("delete vm %s: release lease for %s: %v", v.Name, mac, err)
		}
	}
}

// releaseLease frees a single MAC's lease, preferring the live DHCP server's
// releaser (which clears both the in-memory pool and the store) and falling
// back to deleting the persisted lease record directly.
func (s *Server) releaseLease(ctx context.Context, mac string) error {
	if s.leaseReleaser != nil {
		return s.leaseReleaser(ctx, mac)
	}
	if s.leaseStore != nil {
		return s.leaseStore.Delete(ctx, mac)
	}
	return nil
}

// vmLeaseMACs returns the de-duplicated, normalized MAC addresses associated
// with a VM across its spec and status network interfaces.
func vmLeaseMACs(v vm.VirtualMachine) []string {
	seen := make(map[string]struct{})
	var macs []string
	add := func(raw string) {
		mac := strings.ToLower(strings.TrimSpace(raw))
		if mac == "" {
			return
		}
		if _, ok := seen[mac]; ok {
			return
		}
		seen[mac] = struct{}{}
		macs = append(macs, mac)
	}
	for _, ni := range v.Network {
		add(ni.MAC)
	}
	for _, ni := range v.NetworkInterfaces {
		add(ni.MAC)
	}
	return macs
}
