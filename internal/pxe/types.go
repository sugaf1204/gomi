package pxe

import (
	"context"
	"time"
)

// DHCPLease represents a DHCP lease assigned to a client.
type DHCPLease struct {
	MAC       string    `json:"mac"`
	IP        string    `json:"ip"`
	Hostname  string    `json:"hostname,omitempty"`
	PXEClient bool      `json:"pxeClient"`
	LeasedAt  time.Time `json:"leasedAt"`
}

// BootConfig controls which bootfile is advertised to PXE clients.
type BootConfig struct {
	BIOSBootFile      string
	UEFIBootFile      string
	UEFILocalBootFile string
	IPXEScript        string
}

// LeaseStore persists DHCP lease information.
type LeaseStore interface {
	Upsert(ctx context.Context, lease DHCPLease) error
	List(ctx context.Context) ([]DHCPLease, error)
	Delete(ctx context.Context, mac string) error
}
