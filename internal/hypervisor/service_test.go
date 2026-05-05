package hypervisor_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/resource"
)

func newTestService() (*hypervisor.Service, *memory.Backend) {
	b := memory.New()
	return hypervisor.NewService(b.Hypervisors(), b.HypervisorTokens(), b.AgentTokens()), b
}

func testHypervisor() hypervisor.Hypervisor {
	return hypervisor.Hypervisor{
		Name: "hv-test-01",
		Connection: hypervisor.ConnectionSpec{
			Type: hypervisor.ConnectionTCP,
			Host: "192.168.1.100",
			Port: 16509,
		},
	}
}

func TestServiceCreate(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	h, err := svc.Create(ctx, testHypervisor())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if h.Phase != hypervisor.PhasePending {
		t.Fatalf("expected phase Pending, got %s", h.Phase)
	}
}

func TestServiceGetAndList(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testHypervisor())

	got, err := svc.Get(ctx, "hv-test-01")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "hv-test-01" {
		t.Fatalf("expected name hv-test-01, got %s", got.Name)
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 hypervisor, got %d", len(list))
	}
}

func TestServiceDelete(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()
	_, _ = svc.Create(ctx, testHypervisor())

	if err := svc.Delete(ctx, "hv-test-01"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := svc.Get(ctx, "hv-test-01")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestServiceCreateTokenAndRegister(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	token, err := svc.CreateToken(ctx)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if token.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if token.Used {
		t.Fatal("expected token to not be used")
	}

	req := hypervisor.RegisterRequest{
		Token:    token.Token,
		Hostname: "hv-reg-01",
		Connection: hypervisor.ConnectionSpec{
			Type: hypervisor.ConnectionTCP,
			Host: "10.0.0.50",
			Port: 16509,
		},
		Capacity: hypervisor.ResourceInfo{CPUCores: 16, MemoryMB: 65536},
	}
	h, agentToken, err := svc.Register(ctx, req)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if h.Phase != hypervisor.PhaseRegistered {
		t.Fatalf("expected phase Registered, got %s", h.Phase)
	}
	if h.LibvirtURI == "" {
		t.Fatal("expected libvirtURI to be set")
	}
	if agentToken == "" {
		t.Fatal("expected non-empty agent token")
	}

	// Token should be single-use.
	_, _, err = svc.Register(ctx, req)
	if err == nil {
		t.Fatal("expected error when reusing token")
	}
}

func TestServiceRegisterPreservesMachineOwnedFields(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	existing := hypervisor.Hypervisor{
		Name: "node3",
		Connection: hypervisor.ConnectionSpec{
			Type: hypervisor.ConnectionTCP,
			Host: "192.168.2.102",
			Port: 16509,
		},
		MachineRef: "node3",
		BridgeName: "br0",
		Phase:      hypervisor.PhasePending,
	}
	if _, err := svc.Create(ctx, existing); err != nil {
		t.Fatalf("Create existing hypervisor: %v", err)
	}
	token, err := svc.CreateToken(ctx)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	h, _, err := svc.Register(ctx, hypervisor.RegisterRequest{
		Token:    token.Token,
		Hostname: "node3",
		Connection: hypervisor.ConnectionSpec{
			Type: hypervisor.ConnectionTCP,
			Host: "192.168.2.102",
			Port: 16509,
		},
		Capacity: hypervisor.ResourceInfo{CPUCores: 8, MemoryMB: 32768},
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if h.MachineRef != "node3" {
		t.Fatalf("expected machineRef to be preserved, got %q", h.MachineRef)
	}
	if h.BridgeName != "br0" {
		t.Fatalf("expected bridgeName to be preserved, got %q", h.BridgeName)
	}
	if h.Phase != hypervisor.PhaseRegistered {
		t.Fatalf("expected phase Registered, got %s", h.Phase)
	}
}

func TestServiceRegisterWithExpiredToken(t *testing.T) {
	svc, b := newTestService()
	ctx := context.Background()

	token, err := svc.CreateToken(ctx)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	// Manually expire the token by overwriting it in the store with a past ExpiresAt.
	expiredToken := hypervisor.RegistrationToken{
		Token:     token.Token,
		CreatedAt: token.CreatedAt,
		ExpiresAt: time.Now().Add(-1 * time.Hour),
		Used:      false,
	}
	if err := b.HypervisorTokens().Create(ctx, expiredToken); err != nil {
		t.Fatalf("overwrite token: %v", err)
	}

	req := hypervisor.RegisterRequest{
		Token:    token.Token,
		Hostname: "hv-expired",
		Connection: hypervisor.ConnectionSpec{
			Host: "10.0.0.99",
			Port: 16509,
		},
		Capacity: hypervisor.ResourceInfo{CPUCores: 8, MemoryMB: 32768},
	}
	_, _, err = svc.Register(ctx, req)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

func TestServiceRegisterWithInvalidToken(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	req := hypervisor.RegisterRequest{
		Token:    "non-existent-token-value",
		Hostname: "hv-invalid",
		Connection: hypervisor.ConnectionSpec{
			Host: "10.0.0.88",
			Port: 16509,
		},
		Capacity: hypervisor.ResourceInfo{CPUCores: 4, MemoryMB: 16384},
	}
	_, _, err := svc.Register(ctx, req)
	if err == nil {
		t.Fatal("expected error for non-existent token, got nil")
	}
}

func TestServiceCreateDuplicateHypervisor(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	_, err := svc.Create(ctx, testHypervisor())
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}

	// Since the store uses Upsert, creating a duplicate overwrites the existing one.
	// This verifies the upsert behavior (no error on duplicate).
	h2, err := svc.Create(ctx, testHypervisor())
	if err != nil {
		t.Fatalf("second Create (upsert): %v", err)
	}
	if h2.Name != "hv-test-01" {
		t.Fatalf("expected name hv-test-01, got %s", h2.Name)
	}

	// Should still be 1 hypervisor, not 2.
	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 hypervisor after upsert, got %d", len(list))
	}
}

func TestServiceUpdateStatus(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	_, err := svc.Create(ctx, testHypervisor())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	capacity := &hypervisor.ResourceInfo{
		CPUCores:  32,
		MemoryMB:  131072,
		StorageGB: 2000,
	}
	used := &hypervisor.ResourceUsage{
		CPUUsedCores:  8,
		MemoryUsedMB:  32768,
		StorageUsedGB: 500,
	}
	updated, err := svc.UpdateStatus(ctx, "hv-test-01", hypervisor.PhaseReady, capacity, used, "qemu+tcp://192.168.1.100/system", "")
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if updated.Phase != hypervisor.PhaseReady {
		t.Fatalf("expected phase Ready, got %s", updated.Phase)
	}
	if updated.VMCount != 0 {
		t.Fatalf("expected vmCount 0 (not set by UpdateStatus), got %d", updated.VMCount)
	}
	if updated.Capacity == nil || updated.Capacity.CPUCores != 32 {
		t.Fatal("expected capacity cpuCores 32")
	}
	if updated.Used == nil || updated.Used.CPUUsedCores != 8 {
		t.Fatal("expected used cpuUsedCores 8")
	}
	if updated.LibvirtURI != "qemu+tcp://192.168.1.100/system" {
		t.Fatalf("expected libvirtURI, got %s", updated.LibvirtURI)
	}
}

func TestServiceUpdateStatusWithError(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	_, err := svc.Create(ctx, testHypervisor())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := svc.UpdateStatus(ctx, "hv-test-01", hypervisor.PhaseUnreachable, nil, nil, "", "connection refused")
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if updated.Phase != hypervisor.PhaseUnreachable {
		t.Fatalf("expected phase Unreachable, got %s", updated.Phase)
	}
	if updated.LastError != "connection refused" {
		t.Fatalf("expected lastError 'connection refused', got %s", updated.LastError)
	}
}

func TestServiceUpdateStatusNonExistent(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	_, err := svc.UpdateStatus(ctx, "non-existent", hypervisor.PhaseReady, nil, nil, "", "")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestServiceListEmpty(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 hypervisors, got %d", len(list))
	}
}

func TestServiceDeleteNonExistent(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	err := svc.Delete(ctx, "non-existent-hv")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for deleting non-existent, got %v", err)
	}
}

func TestServiceGetNonExistent(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	_, err := svc.Get(ctx, "ghost-hv")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestServiceCreateSetsDefaults(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	h := hypervisor.Hypervisor{
		Name: "hv-defaults",
		Connection: hypervisor.ConnectionSpec{
			Host: "10.0.0.1",
		},
	}
	created, err := svc.Create(ctx, h)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Connection.Type != hypervisor.ConnectionTCP {
		t.Fatalf("expected default connection type tcp, got %s", created.Connection.Type)
	}
	if created.CreatedAt.IsZero() {
		t.Fatal("expected non-zero createdAt")
	}
	if created.UpdatedAt.IsZero() {
		t.Fatal("expected non-zero updatedAt")
	}
}

func TestServiceRegisterSetsLibvirtURI(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	token, err := svc.CreateToken(ctx)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	req := hypervisor.RegisterRequest{
		Token:    token.Token,
		Hostname: "hv-uri-test",
		Connection: hypervisor.ConnectionSpec{
			Host: "172.16.0.10",
			Port: 16509,
		},
		Capacity: hypervisor.ResourceInfo{CPUCores: 4, MemoryMB: 8192},
	}
	h, _, err := svc.Register(ctx, req)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	expectedURI := "qemu+tcp://172.16.0.10/system"
	if h.LibvirtURI != expectedURI {
		t.Fatalf("expected libvirtURI %s, got %s", expectedURI, h.LibvirtURI)
	}
	if h.Capacity == nil || h.Capacity.CPUCores != 4 {
		t.Fatal("expected capacity to be set from request")
	}
}

func TestServiceListMultipleHypervisors(t *testing.T) {
	svc, _ := newTestService()
	ctx := context.Background()

	for _, name := range []string{"hv-a", "hv-b", "hv-c"} {
		h := testHypervisor()
		h.Name = name
		if _, err := svc.Create(ctx, h); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}

	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 hypervisors, got %d", len(list))
	}
}
