package vm_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	golibvirt "github.com/digitalocean/go-libvirt"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/libvirt"
	"github.com/sugaf1204/gomi/internal/vm"
)

func TestRuntimeSyncerSyncAllUsesOneExecutorPerHypervisor(t *testing.T) {
	backend := memory.New()
	hypervisors := hypervisor.NewService(backend.Hypervisors(), backend.HypervisorTokens(), backend.AgentTokens())
	vms := vm.NewService(backend.VMs())
	ctx := context.Background()

	if _, err := hypervisors.Create(ctx, hypervisor.Hypervisor{
		Name: "hv-sync",
		Connection: hypervisor.ConnectionSpec{
			Type: hypervisor.ConnectionTCP,
			Host: "192.0.2.10",
			Port: 16509,
		},
		Phase: hypervisor.PhaseRegistered,
	}); err != nil {
		t.Fatalf("create hypervisor: %v", err)
	}
	for _, name := range []string{"vm-sync-a", "vm-sync-b"} {
		if _, err := vms.Create(ctx, vm.VirtualMachine{
			Name:          name,
			HypervisorRef: "hv-sync",
			Resources:     vm.ResourceSpec{CPUCores: 1, MemoryMB: 1024, DiskGB: 8},
			OSImageRef:    "ubuntu-test",
		}); err != nil {
			t.Fatalf("create vm %s: %v", name, err)
		}
	}

	var factoryCalls atomic.Int32
	syncer := &vm.RuntimeSyncer{
		Hypervisors: hypervisors,
		VMs:         vms,
		ExecutorFactory: func(context.Context, libvirt.LibvirtConfig) (libvirt.Executor, error) {
			factoryCalls.Add(1)
			return &fakeLibvirtExecutor{
				domains: map[string]*libvirt.DomainInfo{
					"vm-sync-a": {Name: "vm-sync-a", State: libvirt.StateRunning},
					"vm-sync-b": {Name: "vm-sync-b", State: libvirt.StateShutoff},
				},
			}, nil
		},
	}

	if err := syncer.SyncAll(ctx, map[string]string{}); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("expected one executor for the hypervisor, got %d", got)
	}

	vmA, err := vms.Get(ctx, "vm-sync-a")
	if err != nil {
		t.Fatalf("get vm-sync-a: %v", err)
	}
	if vmA.Phase != vm.PhaseRunning || vmA.HypervisorName != "hv-sync" {
		t.Fatalf("unexpected vm-sync-a status: phase=%s hypervisor=%s", vmA.Phase, vmA.HypervisorName)
	}
	vmB, err := vms.Get(ctx, "vm-sync-b")
	if err != nil {
		t.Fatalf("get vm-sync-b: %v", err)
	}
	if vmB.Phase != vm.PhaseStopped || vmB.HypervisorName != "hv-sync" {
		t.Fatalf("unexpected vm-sync-b status: phase=%s hypervisor=%s", vmB.Phase, vmB.HypervisorName)
	}
	hv, err := hypervisors.Get(ctx, "hv-sync")
	if err != nil {
		t.Fatalf("get hypervisor: %v", err)
	}
	if hv.Phase != hypervisor.PhaseReady || hv.LastError != "" {
		t.Fatalf("unexpected hypervisor status: phase=%s lastError=%q", hv.Phase, hv.LastError)
	}
}

func TestRuntimeSyncerSyncAllMarksHypervisorUnreachableOnConnectError(t *testing.T) {
	backend := memory.New()
	hypervisors := hypervisor.NewService(backend.Hypervisors(), backend.HypervisorTokens(), backend.AgentTokens())
	vms := vm.NewService(backend.VMs())
	ctx := context.Background()

	if _, err := hypervisors.Create(ctx, hypervisor.Hypervisor{
		Name: "hv-down",
		Connection: hypervisor.ConnectionSpec{
			Type: hypervisor.ConnectionTCP,
			Host: "192.0.2.20",
			Port: 16509,
		},
		Phase: hypervisor.PhaseReady,
	}); err != nil {
		t.Fatalf("create hypervisor: %v", err)
	}
	if _, err := vms.Create(ctx, vm.VirtualMachine{
		Name:          "vm-down",
		HypervisorRef: "hv-down",
		Resources:     vm.ResourceSpec{CPUCores: 1, MemoryMB: 1024, DiskGB: 8},
		OSImageRef:    "ubuntu-test",
	}); err != nil {
		t.Fatalf("create vm: %v", err)
	}

	connectErr := errors.New("dial libvirtd 192.0.2.20:16509: no route to host")
	syncer := &vm.RuntimeSyncer{
		Hypervisors: hypervisors,
		VMs:         vms,
		ExecutorFactory: func(context.Context, libvirt.LibvirtConfig) (libvirt.Executor, error) {
			return nil, connectErr
		},
	}
	if err := syncer.SyncAll(ctx, nil); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	hv, err := hypervisors.Get(ctx, "hv-down")
	if err != nil {
		t.Fatalf("get hypervisor: %v", err)
	}
	if hv.Phase != hypervisor.PhaseUnreachable {
		t.Fatalf("expected hypervisor phase Unreachable, got %s", hv.Phase)
	}
	if hv.LastError != connectErr.Error() {
		t.Fatalf("expected lastError %q, got %q", connectErr.Error(), hv.LastError)
	}
}

func TestRuntimeSyncerSyncAllSkipsRecentlyUnreachableHypervisor(t *testing.T) {
	backend := memory.New()
	hypervisors := hypervisor.NewService(backend.Hypervisors(), backend.HypervisorTokens(), backend.AgentTokens())
	vms := vm.NewService(backend.VMs())
	ctx := context.Background()

	if _, err := hypervisors.Create(ctx, hypervisor.Hypervisor{
		Name: "hv-circuit-open",
		Connection: hypervisor.ConnectionSpec{
			Type: hypervisor.ConnectionTCP,
			Host: "192.0.2.30",
			Port: 16509,
		},
		Phase: hypervisor.PhaseUnreachable,
	}); err != nil {
		t.Fatalf("create hypervisor: %v", err)
	}
	if _, err := hypervisors.UpdateStatus(ctx, "hv-circuit-open", hypervisor.PhaseUnreachable, nil, nil, "", "previous connection failure"); err != nil {
		t.Fatalf("mark unreachable: %v", err)
	}
	if _, err := vms.Create(ctx, vm.VirtualMachine{
		Name:          "vm-circuit-open",
		HypervisorRef: "hv-circuit-open",
		Resources:     vm.ResourceSpec{CPUCores: 1, MemoryMB: 1024, DiskGB: 8},
		OSImageRef:    "ubuntu-test",
	}); err != nil {
		t.Fatalf("create vm: %v", err)
	}

	var factoryCalls atomic.Int32
	syncer := &vm.RuntimeSyncer{
		Hypervisors:   hypervisors,
		VMs:           vms,
		RetryInterval: time.Hour,
		ExecutorFactory: func(context.Context, libvirt.LibvirtConfig) (libvirt.Executor, error) {
			factoryCalls.Add(1)
			return nil, errors.New("should not retry while circuit is open")
		},
	}
	if err := syncer.SyncAll(ctx, nil); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	if got := factoryCalls.Load(); got != 0 {
		t.Fatalf("expected circuit breaker to skip executor creation, got %d calls", got)
	}
}

func TestRuntimeSyncerSyncAllDoesNotMarkReadyWhenAllVMQueriesFail(t *testing.T) {
	backend := memory.New()
	hypervisors := hypervisor.NewService(backend.Hypervisors(), backend.HypervisorTokens(), backend.AgentTokens())
	vms := vm.NewService(backend.VMs())
	ctx := context.Background()

	if _, err := hypervisors.Create(ctx, hypervisor.Hypervisor{
		Name: "hv-query-fail",
		Connection: hypervisor.ConnectionSpec{
			Type: hypervisor.ConnectionTCP,
			Host: "192.0.2.35",
			Port: 16509,
		},
		Phase: hypervisor.PhaseReady,
	}); err != nil {
		t.Fatalf("create hypervisor: %v", err)
	}
	if _, err := hypervisors.UpdateStatus(ctx, "hv-query-fail", hypervisor.PhaseUnreachable, nil, nil, "", "previous connection failure"); err != nil {
		t.Fatalf("mark unreachable: %v", err)
	}
	time.Sleep(time.Millisecond)

	for _, name := range []string{"vm-query-fail-a", "vm-query-fail-b"} {
		if _, err := vms.Create(ctx, vm.VirtualMachine{
			Name:          name,
			HypervisorRef: "hv-query-fail",
			Resources:     vm.ResourceSpec{CPUCores: 1, MemoryMB: 1024, DiskGB: 8},
			OSImageRef:    "ubuntu-test",
		}); err != nil {
			t.Fatalf("create vm %s: %v", name, err)
		}
	}

	syncer := &vm.RuntimeSyncer{
		Hypervisors:   hypervisors,
		VMs:           vms,
		RetryInterval: time.Nanosecond,
		ExecutorFactory: func(context.Context, libvirt.LibvirtConfig) (libvirt.Executor, error) {
			return &fakeLibvirtExecutor{
				domainErrors: map[string]error{
					"vm-query-fail-a": errors.New("libvirt session reset"),
					"vm-query-fail-b": errors.New("libvirt session reset"),
				},
			}, nil
		},
	}

	if err := syncer.SyncAll(ctx, nil); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	hv, err := hypervisors.Get(ctx, "hv-query-fail")
	if err != nil {
		t.Fatalf("get hypervisor: %v", err)
	}
	if hv.Phase == hypervisor.PhaseReady {
		t.Fatalf("hypervisor must not be marked Ready after all VM queries fail")
	}
	if !strings.Contains(hv.LastError, "all VM runtime queries failed") {
		t.Fatalf("expected all-query-failed lastError, got %q", hv.LastError)
	}
}

func TestRuntimeSyncerSyncAllDoesNotUseOneTimeoutForWholeBatch(t *testing.T) {
	backend := memory.New()
	hypervisors := hypervisor.NewService(backend.Hypervisors(), backend.HypervisorTokens(), backend.AgentTokens())
	vms := vm.NewService(backend.VMs())
	ctx := context.Background()

	if _, err := hypervisors.Create(ctx, hypervisor.Hypervisor{
		Name: "hv-many-slow",
		Connection: hypervisor.ConnectionSpec{
			Type: hypervisor.ConnectionTCP,
			Host: "192.0.2.36",
			Port: 16509,
		},
		Phase: hypervisor.PhaseRegistered,
	}); err != nil {
		t.Fatalf("create hypervisor: %v", err)
	}

	domains := map[string]*libvirt.DomainInfo{}
	for _, name := range []string{"vm-many-slow-a", "vm-many-slow-b", "vm-many-slow-c", "vm-many-slow-d"} {
		if _, err := vms.Create(ctx, vm.VirtualMachine{
			Name:          name,
			HypervisorRef: "hv-many-slow",
			Resources:     vm.ResourceSpec{CPUCores: 1, MemoryMB: 1024, DiskGB: 8},
			OSImageRef:    "ubuntu-test",
		}); err != nil {
			t.Fatalf("create vm %s: %v", name, err)
		}
		domains[name] = &libvirt.DomainInfo{Name: name, State: libvirt.StateRunning}
	}

	syncer := &vm.RuntimeSyncer{
		Hypervisors:      hypervisors,
		VMs:              vms,
		OperationTimeout: 50 * time.Millisecond,
		ExecutorFactory: func(context.Context, libvirt.LibvirtConfig) (libvirt.Executor, error) {
			return &fakeLibvirtExecutor{
				domains:         domains,
				domainInfoDelay: 20 * time.Millisecond,
			}, nil
		},
	}

	if err := syncer.SyncAll(ctx, nil); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	hv, err := hypervisors.Get(ctx, "hv-many-slow")
	if err != nil {
		t.Fatalf("get hypervisor: %v", err)
	}
	if hv.Phase != hypervisor.PhaseReady || hv.LastError != "" {
		t.Fatalf("unexpected hypervisor status: phase=%s lastError=%q", hv.Phase, hv.LastError)
	}
}

func TestRuntimeSyncerSyncAllTimesOutSlowHypervisor(t *testing.T) {
	backend := memory.New()
	hypervisors := hypervisor.NewService(backend.Hypervisors(), backend.HypervisorTokens(), backend.AgentTokens())
	vms := vm.NewService(backend.VMs())
	ctx := context.Background()

	if _, err := hypervisors.Create(ctx, hypervisor.Hypervisor{
		Name: "hv-slow",
		Connection: hypervisor.ConnectionSpec{
			Type: hypervisor.ConnectionTCP,
			Host: "192.0.2.40",
			Port: 16509,
		},
		Phase: hypervisor.PhaseReady,
	}); err != nil {
		t.Fatalf("create hypervisor: %v", err)
	}
	if _, err := vms.Create(ctx, vm.VirtualMachine{
		Name:          "vm-slow",
		HypervisorRef: "hv-slow",
		Resources:     vm.ResourceSpec{CPUCores: 1, MemoryMB: 1024, DiskGB: 8},
		OSImageRef:    "ubuntu-test",
	}); err != nil {
		t.Fatalf("create vm: %v", err)
	}

	syncer := &vm.RuntimeSyncer{
		Hypervisors:      hypervisors,
		VMs:              vms,
		OperationTimeout: 20 * time.Millisecond,
		ExecutorFactory: func(context.Context, libvirt.LibvirtConfig) (libvirt.Executor, error) {
			return &fakeLibvirtExecutor{blockDomainInfo: true}, nil
		},
	}
	if err := syncer.SyncAll(ctx, nil); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	hv, err := hypervisors.Get(ctx, "hv-slow")
	if err != nil {
		t.Fatalf("get hypervisor: %v", err)
	}
	if hv.Phase != hypervisor.PhaseUnreachable {
		t.Fatalf("expected slow hypervisor to be marked Unreachable, got %s", hv.Phase)
	}
	if !strings.Contains(hv.LastError, "timed out") {
		t.Fatalf("expected timeout lastError, got %q", hv.LastError)
	}
}

type fakeLibvirtExecutor struct {
	domains         map[string]*libvirt.DomainInfo
	domainErrors    map[string]error
	domainInfoDelay time.Duration
	blockDomainInfo bool
}

func (e *fakeLibvirtExecutor) DefineDomain(context.Context, libvirt.DomainConfig) error {
	return nil
}

func (e *fakeLibvirtExecutor) CreateVolume(context.Context, string, int, string) error {
	return nil
}

func (e *fakeLibvirtExecutor) CreateOverlayVolume(context.Context, string, int, string, string) error {
	return nil
}

func (e *fakeLibvirtExecutor) VolumeExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (e *fakeLibvirtExecutor) CreateVolumeFromReader(context.Context, string, int64, string, io.Reader) error {
	return nil
}

func (e *fakeLibvirtExecutor) DeleteVolume(context.Context, string) error {
	return nil
}

func (e *fakeLibvirtExecutor) StartDomain(context.Context, string) error {
	return nil
}

func (e *fakeLibvirtExecutor) ShutdownDomain(context.Context, string) error {
	return nil
}

func (e *fakeLibvirtExecutor) DestroyDomain(context.Context, string) error {
	return nil
}

func (e *fakeLibvirtExecutor) UndefineDomain(context.Context, string) error {
	return nil
}

func (e *fakeLibvirtExecutor) SetDomainBootDevice(context.Context, string, string) error {
	return nil
}

func (e *fakeLibvirtExecutor) DomainInfo(ctx context.Context, name string) (*libvirt.DomainInfo, error) {
	if e.blockDomainInfo {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if e.domainInfoDelay > 0 {
		timer := time.NewTimer(e.domainInfoDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	if err := e.domainErrors[name]; err != nil {
		return nil, err
	}
	info, ok := e.domains[name]
	if !ok {
		return nil, errors.New("domain not found")
	}
	return info, nil
}

func (e *fakeLibvirtExecutor) DomainInterfaces(context.Context, string) ([]libvirt.InterfaceInfo, error) {
	return nil, nil
}

func (e *fakeLibvirtExecutor) DomainGraphicsInfo(context.Context, string) (*libvirt.GraphicsInfo, error) {
	return nil, nil
}

func (e *fakeLibvirtExecutor) MigrateDomain(context.Context, string, string, golibvirt.DomainMigrateFlags) error {
	return nil
}

func (e *fakeLibvirtExecutor) ListDomains(context.Context) ([]libvirt.DomainInfo, error) {
	return nil, nil
}

func (e *fakeLibvirtExecutor) Close() error {
	return nil
}
