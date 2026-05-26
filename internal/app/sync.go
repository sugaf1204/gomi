package app

import (
	"context"
	"github.com/sugaf1204/gomi/internal/infra/config"
	"github.com/sugaf1204/gomi/internal/infra/dns"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/provision"
	"github.com/sugaf1204/gomi/internal/pxe"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
	"log"
	"net"
	"strings"
	"time"
)

func (r *Runtime) runSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.syncProvisioningStates(ctx)
		}
	}
}

func (r *Runtime) syncProvisioningStates(ctx context.Context) {
	machines, err := r.machineStore.List(ctx)
	if err != nil {
		log.Printf("sync: list error: %v", err)
		return
	}

	for _, m := range machines {
		// Provisioning timeout check
		if m.Provision != nil && m.Provision.Active && m.Provision.DeadlineAt != nil {
			if time.Now().UTC().After(*m.Provision.DeadlineAt) {
				m.Provision.Active = false
				m.Phase = machine.PhaseError
				m.LastError = "provisioning timed out"
				m.UpdatedAt = time.Now().UTC()
				if err := r.machineStore.Upsert(ctx, m); err != nil {
					log.Printf("sync: timeout save error: %v", err)
				}
				continue
			}
		}

		if m.Phase != machine.PhaseProvisioning {
			continue
		}
		if m.Provision != nil && m.Provision.FinishedAt != nil {
			continue
		}
		sshKeys, _ := r.sshkeyStore.List(ctx)
		artifacts, cfg, buildErr := provision.BuildArtifacts(m, r.currentBootHTTPBaseURL(), sshKeys)
		result := machine.SyncState(m, artifacts, cfg, buildErr)
		if result.NeedsSave {
			if err := r.machineStore.Upsert(ctx, result.Machine); err != nil {
				log.Printf("sync: save error: %v", err)
			}
		}
		if result.NeedsDNS {
			r.syncDNS(ctx)
		}
	}
}

func (r *Runtime) startDNS(ctx context.Context) {
	if r.dnsController == nil {
		return
	}

	if notifier, ok := r.machineStore.(machine.ChangeNotifier); ok {
		notifier.Subscribe(func() {
			r.syncDNS(ctx)
		})
	}
	if r.Config.DNSMode == config.DNSModeEmbedded || r.Config.DNSMode == config.DNSModeRFC2136 {
		r.subscribeEmbeddedDNSChanges(ctx)
	}
	go r.runDNSSyncLoop(ctx)

	if err := r.dnsController.Start(ctx); err != nil {
		log.Printf("dns: controller error: %v", err)
	}
}

func (r *Runtime) configureDNSController() {
	switch r.Config.DNSMode {
	case config.DNSModeEmbedded:
		r.dnsController = dns.NewEmbeddedServer(dns.EmbeddedConfig{
			Addr:               r.resolveDNSEmbeddedAddr(),
			TTL:                r.Config.DNSTTL,
			DynamicRecordsPath: r.Config.DataDir + "/dns-records.json",
			Machines:           r.machineStore,
			VMs:                r.vmStore,
			Subnets:            r.subnetStore,
		})
	case config.DNSModePowerDNS:
		r.dnsController = dns.NewPowerDNSController(r.dnsClient, r.machineStore)
	case config.DNSModeRFC2136:
		r.dnsController = dns.NewRFC2136Controller(dns.RFC2136Config{
			Server:        r.Config.RFC2136Server,
			Zone:          r.Config.RFC2136Zone,
			TTL:           r.Config.DNSTTL,
			TSIGName:      r.Config.RFC2136TSIGName,
			TSIGSecret:    r.Config.RFC2136TSIGSecret,
			TSIGAlgorithm: r.Config.RFC2136TSIGAlgorithm,
			Transport:     r.Config.RFC2136Transport,
			Machines:      r.machineStore,
			VMs:           r.vmStore,
			Subnets:       r.subnetStore,
		})
	default:
		r.dnsController = nil
	}
}

func (r *Runtime) resolveDNSEmbeddedAddr() string {
	if strings.TrimSpace(r.Config.DNSEmbeddedAddr) != "" {
		return strings.TrimSpace(r.Config.DNSEmbeddedAddr)
	}
	if iface := strings.TrimSpace(r.Config.DHCPIface); iface != "" {
		if ip := detectIfaceIP(iface); ip != nil {
			return net.JoinHostPort(ip.String(), "53")
		}
	}
	if iface := detectDefaultIface(); iface != "" {
		if ip := detectIfaceIP(iface); ip != nil {
			return net.JoinHostPort(ip.String(), "53")
		}
	}
	return ":53"
}

func (r *Runtime) subscribeEmbeddedDNSChanges(ctx context.Context) {
	if notifier, ok := r.subnetStore.(subnet.ChangeNotifier); ok {
		notifier.Subscribe(func() {
			r.syncDNS(ctx)
		})
	}
	if notifier, ok := r.vmStore.(vm.ChangeNotifier); ok {
		notifier.Subscribe(func() {
			r.syncDNS(ctx)
		})
	}
}

func (r *Runtime) runDNSSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.syncDNS(ctx)
		}
	}
}

func (r *Runtime) syncDNS(ctx context.Context) {
	if r.dnsController == nil {
		return
	}
	if err := r.dnsController.Sync(ctx); err != nil {
		log.Printf("dns: sync error: %v", err)
	}
}

// runPowerPollLoop periodically checks power state for all machines and
// synchronizes DHCP lease IPs.
func (r *Runtime) runPowerPollLoop(ctx context.Context) {
	leaseTicker := time.NewTicker(30 * time.Second)
	powerTicker := time.NewTicker(2 * time.Second)
	defer leaseTicker.Stop()
	defer powerTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-leaseTicker.C:
			r.syncLeaseIPs(ctx)
		case <-powerTicker.C:
			r.pollPowerStates(ctx)
		}
	}
}

// syncLeaseIPs synchronizes DHCP lease IPs to matching machines.
func (r *Runtime) syncLeaseIPs(ctx context.Context) {
	ipUpdater, ok := r.machineStore.(machine.IPAddressUpdater)
	if !ok {
		log.Printf("lease-sync: machine store does not support partial IP updates")
		return
	}
	machines, err := r.machineStore.List(ctx)
	if err != nil {
		return
	}
	leases, err := r.leaseStore.List(ctx)
	if err != nil {
		return
	}

	leaseByMAC := make(map[string]pxe.DHCPLease, len(leases))
	for _, l := range leases {
		leaseByMAC[strings.ToLower(l.MAC)] = l
	}

	for _, m := range machines {
		// Static IP machines manage their own IP; don't overwrite from DHCP leases.
		if m.IPAssignment == machine.IPAssignmentModeStatic {
			continue
		}
		mac := strings.ToLower(m.MAC)
		lease, ok := leaseByMAC[mac]
		if !ok || lease.IP == "" {
			continue
		}
		if m.IP == lease.IP {
			continue
		}
		if err := ipUpdater.UpdateDynamicIPAddress(ctx, m.Name, m.MAC, lease.IP, time.Now().UTC()); err != nil {
			log.Printf("lease-sync: failed to update IP for %s: %v", m.Name, err)
		}
	}
}

// pollPowerStates checks the power state of each machine and updates status.
func (r *Runtime) pollPowerStates(ctx context.Context) {
	stateUpdater, ok := r.machineStore.(machine.PowerStateStatusUpdater)
	if !ok {
		log.Printf("power-state: machine store does not support partial power-state updates")
		return
	}
	machines, err := r.machineStore.List(ctx)
	if err != nil {
		return
	}

	for _, m := range machines {
		mi := power.MachineInfo{
			Name:     m.Name,
			Hostname: m.Hostname,
			MAC:      m.MAC,
			IP:       m.IP,
			Power:    m.Power,
		}
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		state, err := r.executor.CheckStatus(checkCtx, mi)
		cancel()
		if err != nil {
			continue
		}
		if state == m.PowerState {
			continue
		}
		now := time.Now().UTC()
		if err := stateUpdater.UpdatePowerStateStatus(ctx, m.Name, state, now, now); err != nil {
			log.Printf("power-state: failed to update %s: %v", m.Name, err)
		}
	}
}
