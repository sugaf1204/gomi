package pxe

import (
	"net"
	"testing"
)

func TestLeasePool_Allocate(t *testing.T) {
	p := newLeasePool("10.0.0.10", "10.0.0.12", nil)

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
	p := newLeasePool("10.0.0.10", "10.0.0.10", nil)

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
