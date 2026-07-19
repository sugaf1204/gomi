package api

import (
	"context"
	"sort"
	"testing"

	"github.com/sugaf1204/gomi/internal/pxe"
	"github.com/sugaf1204/gomi/internal/vm"
)

type fakeLeaseStore struct {
	deleted map[string]bool
}

func (f *fakeLeaseStore) Upsert(_ context.Context, _ pxe.DHCPLease) error { return nil }
func (f *fakeLeaseStore) List(_ context.Context) ([]pxe.DHCPLease, error)  { return nil, nil }
func (f *fakeLeaseStore) Delete(_ context.Context, mac string) error {
	f.deleted[mac] = true
	return nil
}

func TestVMLeaseMACs_DedupesSpecAndStatus(t *testing.T) {
	v := vm.VirtualMachine{
		Network: []vm.NetworkInterface{
			{MAC: "AA:BB:CC:DD:EE:01"},
			{MAC: " "},
		},
		NetworkInterfaces: []vm.NetworkInterfaceStatus{
			{MAC: "aa:bb:cc:dd:ee:01"}, // duplicate of spec (case-insensitive)
			{MAC: "aa:bb:cc:dd:ee:02"},
		},
	}

	got := vmLeaseMACs(v)
	sort.Strings(got)
	want := []string{"aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestReleaseVMLeases_UsesReleaserForEachMAC(t *testing.T) {
	released := map[string]bool{}
	s := &Server{
		leaseReleaser: func(_ context.Context, mac string) error {
			released[mac] = true
			return nil
		},
	}

	v := vm.VirtualMachine{
		Name:    "vm1",
		Network: []vm.NetworkInterface{{MAC: "aa:bb:cc:dd:ee:01"}},
		NetworkInterfaces: []vm.NetworkInterfaceStatus{
			{MAC: "aa:bb:cc:dd:ee:02"},
		},
	}

	s.releaseVMLeases(context.Background(), v)

	if !released["aa:bb:cc:dd:ee:01"] || !released["aa:bb:cc:dd:ee:02"] {
		t.Fatalf("expected both MACs released, got %v", released)
	}
}

func TestReleaseLease_FallsBackToStore(t *testing.T) {
	store := &fakeLeaseStore{deleted: map[string]bool{}}
	s := &Server{leaseStore: store}

	if err := s.releaseLease(context.Background(), "aa:bb:cc:dd:ee:03"); err != nil {
		t.Fatalf("releaseLease: %v", err)
	}
	if !store.deleted["aa:bb:cc:dd:ee:03"] {
		t.Fatal("expected lease deleted from store when no releaser is set")
	}
}
