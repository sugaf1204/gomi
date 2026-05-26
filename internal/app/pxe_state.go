package app

import (
	"context"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/node"
	"github.com/sugaf1204/gomi/internal/pxe"
	"github.com/sugaf1204/gomi/internal/resource"
	"log"
	"net"
	"strings"
	"time"
)

func (r *Runtime) currentPXEState() (*pxe.Server, pxeRuntimeState) {
	r.pxeMu.Lock()
	defer r.pxeMu.Unlock()
	return r.dhcpServer, r.pxeState
}

func (r *Runtime) stopPXE(reason string) {
	r.pxeMu.Lock()
	cancel := r.pxeCancel
	done := r.pxeDone
	r.dhcpServer = nil
	r.tftpServer = nil
	r.pxeCancel = nil
	r.pxeDone = nil
	r.pxeState = pxeRuntimeState{}
	r.pxeMu.Unlock()

	if cancel != nil {
		log.Printf("dhcp: stopping PXE services: %s", reason)
		cancel()
		if done != nil {
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				log.Printf("dhcp: timed out waiting for PXE services to stop")
			}
		}
	}
}

func (r *Runtime) clearPXEIfCurrent(server *pxe.Server, cancel context.CancelFunc) {
	r.pxeMu.Lock()
	if r.dhcpServer != server {
		r.pxeMu.Unlock()
		return
	}
	r.dhcpServer = nil
	r.tftpServer = nil
	r.pxeCancel = nil
	r.pxeDone = nil
	r.pxeState = pxeRuntimeState{}
	r.pxeMu.Unlock()
	cancel()
}

// addStaticReservation is a shared helper for building DHCP reservation maps.
func addStaticReservation(reservations map[string]net.IP, h node.Node) {
	if h.GetIPAssignment() != resource.IPAssignmentStatic {
		return
	}
	mac := h.PrimaryMAC()
	ip := net.ParseIP(strings.TrimSpace(h.StaticIP()))
	if mac != "" && ip != nil {
		reservations[mac] = ip.To4()
	}
}

func addLocalBootMAC(localBootMACs map[string]struct{}, h node.Node) {
	if h == nil || !shouldDirectLocalBoot(h) {
		return
	}
	for _, raw := range h.AllMACs() {
		mac := strings.ToLower(strings.TrimSpace(raw))
		if mac != "" {
			localBootMACs[mac] = struct{}{}
		}
	}
}

func addActiveProvisioningMAC(provisioningMACs map[string]struct{}, h node.Node) {
	if h == nil || !h.IsProvisioningActive() || shouldDirectLocalBoot(h) {
		return
	}
	for _, raw := range h.AllMACs() {
		mac := strings.ToLower(strings.TrimSpace(raw))
		if mac != "" {
			provisioningMACs[mac] = struct{}{}
		}
	}
}

func removeProvisioningLocalBootMACs(localBootMACs, provisioningMACs map[string]struct{}) {
	for mac := range provisioningMACs {
		delete(localBootMACs, mac)
	}
}

func shouldDirectLocalBoot(h node.Node) bool {
	if !h.IsProvisioningActive() {
		return true
	}
	m, ok := h.(*machine.Machine)
	if !ok || m.Provision == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(m.Provision.Artifacts["imageApplied"]), "true")
}

// syncDHCPReservations builds a MAC→IP reservation map from subnet manual
// reservations and static-IP machines/VMs, then pushes it to the DHCP server.
func (r *Runtime) syncDHCPReservations(ctx context.Context) {
	dhcpSrv, _ := r.currentPXEState()
	if dhcpSrv == nil {
		return
	}

	reservations := make(map[string]net.IP)
	localBootMACs := make(map[string]struct{})
	provisioningMACs := make(map[string]struct{})

	// 1. Subnet manual reservations
	subnets, err := r.subnetStore.List(ctx)
	if err == nil && len(subnets) > 0 {
		for _, res := range subnets[0].Spec.Reservations {
			mac := strings.ToLower(strings.TrimSpace(res.MAC))
			ip := net.ParseIP(strings.TrimSpace(res.IP))
			if mac != "" && ip != nil {
				reservations[mac] = ip.To4()
			}
		}
	}

	// 2. Static machines
	machines, err := r.machineStore.List(ctx)
	if err == nil {
		for i := range machines {
			addStaticReservation(reservations, &machines[i])
			addLocalBootMAC(localBootMACs, &machines[i])
			addActiveProvisioningMAC(provisioningMACs, &machines[i])
		}
	}

	// 3. Static VMs
	vms, vmErr := r.vmStore.List(ctx)
	if vmErr == nil {
		for i := range vms {
			addStaticReservation(reservations, &vms[i])
			addLocalBootMAC(localBootMACs, &vms[i])
			addActiveProvisioningMAC(provisioningMACs, &vms[i])
		}
	}

	removeProvisioningLocalBootMACs(localBootMACs, provisioningMACs)
	dhcpSrv.UpdateReservations(reservations)
	dhcpSrv.UpdateLocalBootMACs(localBootMACs)
	log.Printf("dhcp: reservation sync: %d reservations, %d direct local boot macs", len(reservations), len(localBootMACs))
}
