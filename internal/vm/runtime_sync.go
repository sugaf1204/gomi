package vm

import (
	"context"
	"errors"
	"fmt"
	"log"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/libvirt"
)

const (
	defaultLibvirtOperationTimeout = 5 * time.Second
	defaultHypervisorRetryInterval = 30 * time.Second
)

type RuntimeSyncer struct {
	Hypervisors      *hypervisor.Service
	VMs              *Service
	ExecutorFactory  ExecutorFactory
	OperationTimeout time.Duration
	RetryInterval    time.Duration
}

// hypervisorGetter resolves a hypervisor by reference. It lets a list sync
// memoize lookups so a hypervisor shared by many VMs is read from the store
// once per request instead of once per VM.
type hypervisorGetter func(ctx context.Context, ref string) (hypervisor.Hypervisor, error)
type ExecutorFactory func(context.Context, libvirt.LibvirtConfig) (libvirt.Executor, error)

func (s *RuntimeSyncer) Sync(ctx context.Context, v VirtualMachine, leaseIPByMAC map[string]string) (VirtualMachine, error) {
	return s.sync(ctx, v, leaseIPByMAC, s.Hypervisors.Get)
}

// SyncList runtime-syncs a page of VMs, memoizing hypervisor lookups across the
// page. Listing a page of VMs that share a handful of hypervisors otherwise
// reads the same hypervisor rows from the store once per VM. The returned slice
// mirrors items with synced state applied; a VM that fails to sync keeps its
// stored state (matching the per-VM Sync behavior).
func (s *RuntimeSyncer) SyncList(ctx context.Context, items []VirtualMachine, leaseIPByMAC map[string]string) []VirtualMachine {
	cache := make(map[string]hypervisor.Hypervisor)
	get := func(ctx context.Context, ref string) (hypervisor.Hypervisor, error) {
		if hv, ok := cache[ref]; ok {
			return hv, nil
		}
		hv, err := s.Hypervisors.Get(ctx, ref)
		if err != nil {
			return hypervisor.Hypervisor{}, err
		}
		cache[ref] = hv
		return hv, nil
	}
	out := make([]VirtualMachine, len(items))
	copy(out, items)
	for i := range out {
		synced, err := s.sync(ctx, out[i], leaseIPByMAC, get)
		if err != nil {
			log.Printf("vm-sync: list %s: %v", out[i].Name, err)
			continue
		}
		out[i] = synced
	}
	return out
}

func (s *RuntimeSyncer) sync(ctx context.Context, v VirtualMachine, leaseIPByMAC map[string]string, getHypervisor hypervisorGetter) (VirtualMachine, error) {
	if strings.TrimSpace(v.HypervisorRef) == "" {
		return v, nil
	}

	hv, err := getHypervisor(ctx, v.HypervisorRef)
	if err != nil {
		return v, fmt.Errorf("get hypervisor %s: %w", v.HypervisorRef, err)
	}

	exec, err := s.connectExecutor(ctx, hv)
	if err != nil {
		return v, fmt.Errorf("connect hypervisor %s: %w", hv.Name, err)
	}
	defer exec.Close()

	return s.syncVMWithTimeout(ctx, hv, exec, v, leaseIPByMAC)
}

func (s *RuntimeSyncer) SyncAll(ctx context.Context, leaseIPByMAC map[string]string) error {
	if s == nil || s.Hypervisors == nil || s.VMs == nil {
		return nil
	}
	items, err := s.VMs.List(ctx)
	if err != nil {
		return fmt.Errorf("list virtual machines: %w", err)
	}
	hypervisors, err := s.Hypervisors.List(ctx)
	if err != nil {
		return fmt.Errorf("list hypervisors: %w", err)
	}

	byHypervisor := make(map[string][]VirtualMachine)
	for _, v := range items {
		ref := strings.TrimSpace(v.HypervisorRef)
		if ref == "" {
			continue
		}
		byHypervisor[ref] = append(byHypervisor[ref], v)
	}
	if len(byHypervisor) == 0 {
		return nil
	}

	hypervisorByName := make(map[string]hypervisor.Hypervisor, len(hypervisors))
	for _, hv := range hypervisors {
		hypervisorByName[hv.Name] = hv
	}

	names := make([]string, 0, len(byHypervisor))
	for name := range byHypervisor {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		hv, ok := hypervisorByName[name]
		if !ok {
			log.Printf("vm-sync: hypervisor %s referenced by VM but not found", name)
			continue
		}
		if s.skipUnreachableHypervisor(hv, time.Now().UTC()) {
			continue
		}
		if err := s.syncHypervisor(ctx, hv, byHypervisor[name], leaseIPByMAC); err != nil {
			if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
				return ctx.Err()
			}
			if changed := s.markHypervisorUnreachable(ctx, hv, err); changed {
				log.Printf("vm-sync: hypervisor %s unreachable: %v", hv.Name, err)
			}
		}
	}
	return nil
}

func (s *RuntimeSyncer) executorFactory() ExecutorFactory {
	if s.ExecutorFactory != nil {
		return s.ExecutorFactory
	}
	return libvirt.NewExecutorContext
}

func (s *RuntimeSyncer) operationTimeout() time.Duration {
	if s.OperationTimeout > 0 {
		return s.OperationTimeout
	}
	return defaultLibvirtOperationTimeout
}

func (s *RuntimeSyncer) retryInterval() time.Duration {
	if s.RetryInterval > 0 {
		return s.RetryInterval
	}
	return defaultHypervisorRetryInterval
}

func (s *RuntimeSyncer) skipUnreachableHypervisor(hv hypervisor.Hypervisor, now time.Time) bool {
	if hv.Phase != hypervisor.PhaseUnreachable || strings.TrimSpace(hv.LastError) == "" {
		return false
	}
	return now.Sub(hv.UpdatedAt) < s.retryInterval()
}

func (s *RuntimeSyncer) syncHypervisor(ctx context.Context, hv hypervisor.Hypervisor, items []VirtualMachine, leaseIPByMAC map[string]string) error {
	exec, err := s.connectExecutor(ctx, hv)
	if err != nil {
		return err
	}
	defer exec.Close()

	successCount := 0
	var firstErr error
	for _, v := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, syncErr := s.syncVMWithTimeout(ctx, hv, exec, v, leaseIPByMAC); syncErr != nil {
			if errors.Is(syncErr, context.Canceled) || errors.Is(syncErr, context.DeadlineExceeded) {
				return syncErr
			}
			if firstErr == nil {
				firstErr = syncErr
			}
			log.Printf("vm-sync: %s: %v", v.Name, syncErr)
			continue
		}
		successCount++
	}
	if successCount == 0 && firstErr != nil {
		return fmt.Errorf("all VM runtime queries failed: %w", firstErr)
	}
	if successCount > 0 {
		s.markHypervisorReachable(ctx, hv)
	}
	return nil
}

func (s *RuntimeSyncer) connectExecutor(ctx context.Context, hv hypervisor.Hypervisor) (libvirt.Executor, error) {
	timeout := s.operationTimeout()
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	exec, err := s.executorFactory()(connectCtx, BuildLibvirtConfig(hv))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("libvirt connect timed out after %s: %w", timeout, err)
		}
		return nil, err
	}
	return exec, nil
}

type vmSyncResult struct {
	vm  VirtualMachine
	err error
}

func (s *RuntimeSyncer) syncVMWithTimeout(ctx context.Context, hv hypervisor.Hypervisor, exec libvirt.Executor, v VirtualMachine, leaseIPByMAC map[string]string) (VirtualMachine, error) {
	timeout := s.operationTimeout()
	opCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan vmSyncResult, 1)
	go func() {
		updated, err := s.syncWithExecutor(opCtx, hv, exec, v, leaseIPByMAC)
		done <- vmSyncResult{vm: updated, err: err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result := <-done:
		return result.vm, result.err
	case <-ctx.Done():
		_ = exec.Close()
		return v, ctx.Err()
	case <-timer.C:
		_ = exec.Close()
		return v, fmt.Errorf("runtime query timed out after %s: %w", timeout, context.DeadlineExceeded)
	}
}

func (s *RuntimeSyncer) syncWithExecutor(ctx context.Context, hv hypervisor.Hypervisor, exec libvirt.Executor, v VirtualMachine, leaseIPByMAC map[string]string) (VirtualMachine, error) {
	domainName := strings.TrimSpace(v.LibvirtDomain)
	if domainName == "" {
		domainName = v.Name
	}

	info, err := exec.DomainInfo(ctx, domainName)
	if err != nil {
		if libvirt.IsDomainNotFoundError(err) {
			if vmDeployInFlight(v, time.Now().UTC()) {
				return v, nil
			}
			return s.markVMMissing(ctx, hv, v, domainName)
		}
		return v, fmt.Errorf("domain info %s: %w", domainName, err)
	}

	interfaces, err := exec.DomainInterfaces(ctx, domainName)
	if err != nil {
		log.Printf("vm-sync: %s interface query failed: %v", v.Name, err)
	}

	networkStatuses, ipAddresses := ConvertRuntimeInterfaces(interfaces)
	networkStatuses, ipAddresses = MergeRuntimeLeaseIPs(v, networkStatuses, ipAddresses, leaseIPByMAC)

	updated := v
	if v.Phase == PhaseMissing {
		// The domain exists again; the Missing-specific state no longer
		// applies regardless of what phase the live state maps to below.
		updated.LastError = ""
		updated.MissingSince = nil
	}
	now := time.Now().UTC()
	if IsProvisioningTimedOut(updated.Provisioning, now) {
		updated.Provisioning.Active = false
		updated.Phase = PhaseError
		updated.LastError = "provisioning timed out waiting for install completion signal"
	} else {
		updated.Phase = MapVMPhaseFromDomainState(info.State, v.Phase, v.Provisioning)
	}
	// The domain exists, so the record must leave Missing even when the
	// domain state maps to no specific phase; otherwise delete would keep
	// skipping runtime teardown and orphan a live domain.
	if updated.Phase == PhaseMissing {
		updated.Phase = PhaseStopped
	}
	updated.HypervisorName = hv.Name
	updated.CreatedOnHost = hv.Name
	updated.LibvirtDomain = domainName
	updated.IPAddresses = ipAddresses
	updated.NetworkInterfaces = networkStatuses

	return s.persistSyncedVM(ctx, v, updated)
}

// vmDeployInFlight reports whether the VM is inside a create or redeploy
// window where the libvirt domain may legitimately not exist yet: both flows
// upsert the record with an armed provisioning window before DefineDomain
// runs, and redeploy undefines the old domain before defining the new one.
// While DomainObservedAt is nil the deploy is still preparing the domain;
// once it is set a later not-found means the domain was removed from the
// host. The deadline still bounds the pre-domain window so a deploy
// abandoned before DefineDomain (crashed server, killed worker) does not
// keep PXE armed forever — a slow-but-alive deploy self-heals, because
// markDomainDefined re-arms and renews the window at definition time.
func vmDeployInFlight(v VirtualMachine, now time.Time) bool {
	if !v.Provisioning.Active || IsProvisioningTimedOut(v.Provisioning, now) {
		return false
	}
	return v.Provisioning.DomainObservedAt == nil
}

// markVMMissing records that the VM's libvirt domain no longer exists on the
// hypervisor while keeping the GOMI record. It returns a nil error because the
// libvirt connection worked; the missing domain is VM-level state, not a
// hypervisor failure.
func (s *RuntimeSyncer) markVMMissing(ctx context.Context, hv hypervisor.Hypervisor, v VirtualMachine, domainName string) (VirtualMachine, error) {
	updated := v
	updated.Phase = PhaseMissing
	updated.LastError = fmt.Sprintf("libvirt domain %s not found on hypervisor %s", domainName, hv.Name)
	updated.HypervisorName = hv.Name
	updated.LibvirtDomain = domainName
	updated.IPAddresses = nil
	updated.NetworkInterfaces = nil
	// The domain is gone, so any in-flight provisioning can never complete;
	// end it so the machine's PXE config stops resolving to this VM.
	updated.Provisioning.Active = false
	// Refresh the timestamp on every transition into Missing (a record that
	// left Missing, e.g. via redeploy, may still carry the old value); keep
	// it stable while the record stays Missing.
	if v.Phase != PhaseMissing || updated.MissingSince == nil {
		now := time.Now().UTC()
		updated.MissingSince = &now
	}
	return s.persistSyncedVM(ctx, v, updated)
}

func (s *RuntimeSyncer) persistSyncedVM(ctx context.Context, before, updated VirtualMachine) (VirtualMachine, error) {
	if !VMStatusChanged(before, updated) {
		return before, nil
	}
	updated.UpdatedAt = time.Now().UTC()
	// The sync worked from a snapshot; writing through an existence-checked
	// update keeps a concurrent delete from being resurrected by this write.
	written, err := writeExisting(ctx, s.VMs.Store(), updated)
	if err != nil {
		return before, fmt.Errorf("persist synced vm status: %w", err)
	}
	if !written {
		return before, nil
	}
	return updated, nil
}

func (s *RuntimeSyncer) markHypervisorReachable(ctx context.Context, hv hypervisor.Hypervisor) {
	if s.Hypervisors == nil || (hv.Phase == hypervisor.PhaseReady && strings.TrimSpace(hv.LastError) == "") {
		return
	}
	if _, err := s.Hypervisors.UpdateStatus(ctx, hv.Name, hypervisor.PhaseReady, nil, nil, "", ""); err != nil {
		log.Printf("vm-sync: update hypervisor %s reachable status: %v", hv.Name, err)
	}
}

func (s *RuntimeSyncer) markHypervisorUnreachable(ctx context.Context, hv hypervisor.Hypervisor, cause error) bool {
	if s.Hypervisors == nil {
		return false
	}
	changed := hv.Phase != hypervisor.PhaseUnreachable || hv.LastError != cause.Error()
	if _, err := s.Hypervisors.UpdateStatus(ctx, hv.Name, hypervisor.PhaseUnreachable, nil, nil, "", cause.Error()); err != nil {
		log.Printf("vm-sync: update hypervisor %s unreachable status: %v", hv.Name, err)
		return false
	}
	return changed
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

func equalTimePtr(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func VMStatusChanged(before VirtualMachine, after VirtualMachine) bool {
	if before.Phase != after.Phase {
		return true
	}
	if before.LastError != after.LastError {
		return true
	}
	if !equalTimePtr(before.MissingSince, after.MissingSince) {
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
