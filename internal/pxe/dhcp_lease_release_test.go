package pxe

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/subnet"
)

func fullModeSpec(ttlSeconds int) subnet.SubnetSpec {
	return subnet.SubnetSpec{
		CIDR:            "10.0.0.0/24",
		PXEAddressRange: &subnet.AddressRange{Start: "10.0.0.10", End: "10.0.0.10"},
		LeaseTime:       ttlSeconds,
	}
}

func TestServer_ReleaseLease_FreesPoolAndStore(t *testing.T) {
	store := &testLeaseStore{}
	s := NewServer("full", "eth0", net.ParseIP("10.0.0.1"), fullModeSpec(0), BootConfig{}, store)

	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:01")
	ip := s.leases.Allocate(mac, "", false)
	if ip == nil {
		t.Fatal("expected an allocated IP")
	}

	if err := s.ReleaseLease(context.Background(), "aa:bb:cc:dd:ee:01"); err != nil {
		t.Fatalf("ReleaseLease: %v", err)
	}

	// After release the single-address pool is free again for another MAC.
	mac2, _ := net.ParseMAC("aa:bb:cc:dd:ee:02")
	if ip := s.leases.Allocate(mac2, "", false); ip == nil {
		t.Fatal("expected pool freed after release")
	}
}

func TestServer_ReleaseLease_InvalidMAC(t *testing.T) {
	s := NewServer("full", "eth0", net.ParseIP("10.0.0.1"), fullModeSpec(0), BootConfig{}, nil)
	if err := s.ReleaseLease(context.Background(), "not-a-mac"); err == nil {
		t.Fatal("expected error for invalid MAC")
	}
}

func TestServer_Reconfigure_UpdatesTTLInPlace(t *testing.T) {
	s := NewServer("full", "eth0", net.ParseIP("10.0.0.1"), fullModeSpec(0), BootConfig{}, nil)
	pool := s.leases
	if pool.ttl != 0 {
		t.Fatalf("expected initial TTL 0, got %s", pool.ttl)
	}

	// Same range, new lease time: TTL updates without rebuilding the pool.
	s.Reconfigure(fullModeSpec(3600))
	if s.leases != pool {
		t.Fatal("expected pool preserved when only lease time changes")
	}
	if pool.ttl != time.Hour {
		t.Fatalf("expected TTL updated to 1h, got %s", pool.ttl)
	}
}
