package libvirt

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	golibvirt "github.com/digitalocean/go-libvirt"
)

const (
	defaultLibvirtPort = 16509
	dialTimeout        = 10 * time.Second
)

// Executor manages libvirt domains on a remote hypervisor.
type Executor interface {
	// DefineDomain creates a new VM domain from config.
	DefineDomain(ctx context.Context, cfg DomainConfig) error
	// CreateVolume creates a storage volume in the default pool.
	CreateVolume(ctx context.Context, name string, sizeGB int, format string) error
	// CreateOverlayVolume creates a qcow2 volume backed by a base image (copy-on-write).
	CreateOverlayVolume(ctx context.Context, name string, sizeGB int, backingPath string, backingFormat string) error
	// VolumeExists reports whether a storage volume exists in the default pool.
	VolumeExists(ctx context.Context, name string, format string) (bool, error)
	// CreateVolumeFromReader creates a storage volume in the default pool and uploads content into it.
	CreateVolumeFromReader(ctx context.Context, name string, sizeBytes int64, format string, r io.Reader) error
	// DeleteVolume removes a storage volume from the default pool, if present.
	DeleteVolume(ctx context.Context, name string) error
	// StartDomain powers on a VM.
	StartDomain(ctx context.Context, name string) error
	// ShutdownDomain gracefully shuts down a VM.
	ShutdownDomain(ctx context.Context, name string) error
	// DestroyDomain forcefully stops a VM.
	DestroyDomain(ctx context.Context, name string) error
	// UndefineDomain removes a VM definition.
	UndefineDomain(ctx context.Context, name string) error
	// SetDomainBootDevice updates persistent boot device (e.g. "hd" or "network").
	SetDomainBootDevice(ctx context.Context, name string, bootDev string) error
	// DomainInfo gets current state of a domain.
	DomainInfo(ctx context.Context, name string) (*DomainInfo, error)
	// DomainInterfaces gets runtime network interface information of a domain.
	DomainInterfaces(ctx context.Context, name string) ([]InterfaceInfo, error)
	// DomainGraphicsInfo returns VNC/SPICE graphics connection info for a running domain.
	DomainGraphicsInfo(ctx context.Context, name string) (*GraphicsInfo, error)
	// MigrateDomain performs a live migration of a domain to a destination URI.
	MigrateDomain(ctx context.Context, name string, destURI string, flags golibvirt.DomainMigrateFlags) error
	// ListDomains lists all domains.
	ListDomains(ctx context.Context) ([]DomainInfo, error)
	// Close releases any underlying connections.
	Close() error
}

// rpcExecutor implements Executor using the libvirt TCP RPC protocol.
type rpcExecutor struct {
	l *golibvirt.Libvirt
}

// NewExecutor creates a new libvirt executor that connects to the hypervisor via TCP RPC.
// The caller should call Close() when done with the executor.
func NewExecutor(cfg LibvirtConfig) (Executor, error) {
	return NewExecutorContext(context.Background(), cfg)
}

// NewExecutorContext creates a new libvirt executor and bounds the TCP dial and
// libvirt connect handshake with ctx.
func NewExecutorContext(ctx context.Context, cfg LibvirtConfig) (Executor, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid libvirt config: %w", err)
	}

	port := cfg.Port
	if port == 0 {
		port = defaultLibvirtPort
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))
	dialer := net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial libvirtd %s: %w", addr, err)
	}

	l := golibvirt.New(conn)
	connectErr := make(chan error, 1)
	go func() {
		connectErr <- l.Connect()
	}()
	select {
	case err := <-connectErr:
		if err == nil {
			return &rpcExecutor{l: l}, nil
		}
		conn.Close()
		return nil, fmt.Errorf("libvirt connect: %w", err)
	case <-ctx.Done():
		conn.Close()
		return nil, fmt.Errorf("libvirt connect: %w", ctx.Err())
	}
}

func (e *rpcExecutor) Close() error {
	return e.l.Disconnect()
}
