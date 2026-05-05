package vm

import (
	"fmt"
	"log"
	"reflect"
	"sort"
	"strings"
	"time"

	"context"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/libvirt"
)

type RuntimeSyncer struct {
	Hypervisors *hypervisor.Service
	VMs         *Service
}

func (s *RuntimeSyncer) Sync(ctx context.Context, v VirtualMachine, leaseIPByMAC map[string]string) (VirtualMachine, error) {
	if strings.TrimSpace(v.HypervisorRef) == "" {
		return v, nil
	}

	hv, err := s.Hypervisors.Get(ctx, v.HypervisorRef)
	if err != nil {
		return v, fmt.Errorf("get hypervisor %s: %w", v.HypervisorRef, err)
	}

	cfg := BuildLibvirtConfig(hv)
	exec, err := libvirt.NewExecutor(cfg)
	if err != nil {
		return v, fmt.Errorf("connect hypervisor %s: %w", hv.Name, err)
	}
	defer exec.Close()

	domainName := strings.TrimSpace(v.LibvirtDomain)
	if domainName == "" {
		domainName = v.Name
	}

	info, err := exec.DomainInfo(ctx, domainName)
	if err != nil {
		return v, fmt.Errorf("domain info %s: %w", domainName, err)
	}

	interfaces, err := exec.DomainInterfaces(ctx, domainName)
	if err != nil {
		log.Printf("vm-sync: %s interface query failed: %v", v.Name, err)
	}

	networkStatuses, ipAddresses := ConvertRuntimeInterfaces(interfaces)
	networkStatuses, ipAddresses = MergeRuntimeLeaseIPs(v, networkStatuses, ipAddresses, leaseIPByMAC)

	updated := v
	now := time.Now().UTC()
	if IsProvisioningTimedOut(updated.Provisioning, now) {
		updated.Provisioning.Active = false
		updated.Phase = PhaseError
		updated.LastError = "provisioning timed out waiting for install completion signal"
	} else {
		updated.Phase = MapVMPhaseFromDomainState(info.State, v.Phase, v.Provisioning)
	}
	updated.HypervisorName = hv.Name
	updated.CreatedOnHost = hv.Name
	updated.LibvirtDomain = domainName
	updated.IPAddresses = ipAddresses
	updated.NetworkInterfaces = networkStatuses

	if !VMStatusChanged(v, updated) {
		return v, nil
	}

	updated.UpdatedAt = now
	if err := s.VMs.Store().Upsert(ctx, updated); err != nil {
		return v, fmt.Errorf("persist synced vm status: %w", err)
	}
	return updated, nil
}

func IsProvisioningTimedOut(status ProvisioningStatus, now time.Time) bool {
	if !status.Active || status.CompletedAt != nil || status.DeadlineAt == nil {
		return false
	}
	return now.After(*status.DeadlineAt)
}

func MapVMPhaseFromDomainState(state libvirt.DomainState, current Phase, provisioning ProvisioningStatus) Phase {
	if provisioning.Active && provisioning.CompletedAt == nil {
		if state == libvirt.StateCrashed {
			return PhaseError
		}
		return PhaseProvisioning
	}
	switch state {
	case libvirt.StateRunning:
		return PhaseRunning
	case libvirt.StateShutoff:
		return PhaseStopped
	case libvirt.StatePaused:
		return PhaseStopped
	case libvirt.StateCrashed:
		return PhaseError
	default:
		return current
	}
}

func ConvertRuntimeInterfaces(interfaces []libvirt.InterfaceInfo) ([]NetworkInterfaceStatus, []string) {
	if len(interfaces) == 0 {
		return nil, nil
	}

	statuses := make([]NetworkInterfaceStatus, 0, len(interfaces))
	ipSet := map[string]struct{}{}
	for _, iface := range interfaces {
		ips := append([]string(nil), iface.IPAddresses...)
		sort.Strings(ips)
		for _, ip := range ips {
			if strings.TrimSpace(ip) == "" {
				continue
			}
			ipSet[ip] = struct{}{}
		}
		statuses = append(statuses, NetworkInterfaceStatus{
			Name:        iface.Name,
			MAC:         strings.ToLower(strings.TrimSpace(iface.MAC)),
			IPAddresses: ips,
		})
	}
	sort.Slice(statuses, func(i, j int) bool {
		if statuses[i].Name != statuses[j].Name {
			return statuses[i].Name < statuses[j].Name
		}
		return statuses[i].MAC < statuses[j].MAC
	})

	ips := make([]string, 0, len(ipSet))
	for ip := range ipSet {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	return statuses, ips
}

func MergeRuntimeLeaseIPs(v VirtualMachine, statuses []NetworkInterfaceStatus, ipAddresses []string, leaseIPByMAC map[string]string) ([]NetworkInterfaceStatus, []string) {
	if len(leaseIPByMAC) == 0 {
		return statuses, ipAddresses
	}

	if len(statuses) == 0 {
		statuses = bootstrapNetworkStatusesFromVM(v)
	}

	ipSet := map[string]struct{}{}
	for _, ip := range ipAddresses {
		if trimmed := strings.TrimSpace(ip); trimmed != "" {
			ipSet[trimmed] = struct{}{}
		}
	}

	for i := range statuses {
		mac := strings.ToLower(strings.TrimSpace(statuses[i].MAC))
		if mac == "" {
			continue
		}
		leaseIP := strings.TrimSpace(leaseIPByMAC[mac])
		if leaseIP == "" {
			continue
		}
		statuses[i].IPAddresses = prependUniqueIP(leaseIP, statuses[i].IPAddresses)
		ipSet[leaseIP] = struct{}{}
	}

	ips := make([]string, 0, len(ipSet))
	for ip := range ipSet {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	return statuses, ips
}

func bootstrapNetworkStatusesFromVM(v VirtualMachine) []NetworkInterfaceStatus {
	if len(v.Network) > 0 {
		statuses := make([]NetworkInterfaceStatus, 0, len(v.Network))
		for _, nic := range v.Network {
			statuses = append(statuses, NetworkInterfaceStatus{
				Name: nic.Name,
				MAC:  strings.ToLower(strings.TrimSpace(nic.MAC)),
			})
		}
		return statuses
	}
	if len(v.NetworkInterfaces) > 0 {
		statuses := make([]NetworkInterfaceStatus, 0, len(v.NetworkInterfaces))
		for _, nic := range v.NetworkInterfaces {
			statuses = append(statuses, NetworkInterfaceStatus{
				Name: nic.Name,
				MAC:  strings.ToLower(strings.TrimSpace(nic.MAC)),
			})
		}
		return statuses
	}
	return nil
}

func prependUniqueIP(primary string, ips []string) []string {
	normalized := strings.TrimSpace(primary)
	if normalized == "" {
		return ips
	}
	out := make([]string, 0, len(ips)+1)
	out = append(out, normalized)
	for _, ip := range ips {
		trimmed := strings.TrimSpace(ip)
		if trimmed == "" || trimmed == normalized {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func VMStatusChanged(before VirtualMachine, after VirtualMachine) bool {
	if before.Phase != after.Phase {
		return true
	}
	if before.HypervisorName != after.HypervisorName {
		return true
	}
	if before.CreatedOnHost != after.CreatedOnHost {
		return true
	}
	if before.LibvirtDomain != after.LibvirtDomain {
		return true
	}
	if !reflect.DeepEqual(before.IPAddresses, after.IPAddresses) {
		return true
	}
	if !reflect.DeepEqual(before.NetworkInterfaces, after.NetworkInterfaces) {
		return true
	}
	if !reflect.DeepEqual(before.Provisioning, after.Provisioning) {
		return true
	}
	return false
}
