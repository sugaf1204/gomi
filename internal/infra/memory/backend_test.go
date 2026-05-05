package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/subnet"
	"github.com/sugaf1204/gomi/internal/vm"
)

func TestSessionExpiry(t *testing.T) {
	b := memory.New()
	store := b.Auth()
	ctx := context.Background()

	session := auth.Session{
		Token:     "expired-token",
		Username:  "admin",
		CreatedAt: time.Now().Add(-2 * time.Hour),
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	_, err := store.GetSession(ctx, "expired-token")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for expired session, got %v", err)
	}
}

func TestAuditCaseInsensitiveFilter(t *testing.T) {
	b := memory.New()
	store := b.Auth()
	ctx := context.Background()

	event := auth.AuditEvent{
		ID:        "evt-1",
		Machine:   "Node-01",
		Action:    "create-machine",
		Actor:     "admin",
		Result:    "success",
		CreatedAt: time.Now(),
	}
	if err := store.CreateAuditEvent(ctx, event); err != nil {
		t.Fatalf("CreateAuditEvent: %v", err)
	}

	events, err := store.ListAuditEvents(ctx, "node-01", 0)
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event with case-insensitive filter, got %d", len(events))
	}
	if events[0].ID != "evt-1" {
		t.Fatalf("expected event evt-1, got %s", events[0].ID)
	}
}

// --- Backend stores initialization ---

func TestBackendStoresInitialized(t *testing.T) {
	b := memory.New()

	if b.Hypervisors() == nil {
		t.Fatal("expected non-nil HypervisorStore")
	}
	if b.HypervisorTokens() == nil {
		t.Fatal("expected non-nil RegTokenStore")
	}
	if b.VMs() == nil {
		t.Fatal("expected non-nil VMStore")
	}
	if b.CloudInits() == nil {
		t.Fatal("expected non-nil CloudInitStore")
	}
	if b.OSImages() == nil {
		t.Fatal("expected non-nil OSImageStore")
	}
}

func TestBackendHealthAndClose(t *testing.T) {
	b := memory.New()
	ctx := context.Background()

	if err := b.Health(ctx); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestSubnetStoreChangeNotifier(t *testing.T) {
	b := memory.New()
	store := b.Subnets()
	ctx := context.Background()
	called := make(chan struct{}, 2)

	store.Subscribe(func() { called <- struct{}{} })

	if err := store.Upsert(ctx, subnet.Subnet{
		Name: "lab",
		Spec: subnet.SubnetSpec{CIDR: "192.168.2.0/24"},
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	waitForSubnetNotification(t, called)

	if err := store.Delete(ctx, "lab"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	waitForSubnetNotification(t, called)
}

func waitForSubnetNotification(t *testing.T, called <-chan struct{}) {
	t.Helper()
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subnet change notification")
	}
}

// --- HypervisorStore ---

func TestHypervisorStoreCRUD(t *testing.T) {
	b := memory.New()
	store := b.Hypervisors()
	ctx := context.Background()

	h := hypervisor.Hypervisor{
		Name: "hv-01",
	}
	if err := store.Upsert(ctx, h); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.Get(ctx, "hv-01")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "hv-01" {
		t.Fatalf("expected hv-01, got %s", got.Name)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}

	if err := store.Delete(ctx, "hv-01"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(ctx, "hv-01")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestHypervisorStoreDeleteNonExistent(t *testing.T) {
	b := memory.New()
	store := b.Hypervisors()
	ctx := context.Background()

	err := store.Delete(ctx, "ghost")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// --- RegTokenStore ---

func TestRegTokenStoreCreateAndGet(t *testing.T) {
	b := memory.New()
	store := b.HypervisorTokens()
	ctx := context.Background()

	token := hypervisor.RegistrationToken{
		Token: "test-token-123",
	}
	if err := store.Create(ctx, token); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.Get(ctx, "test-token-123")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Token != "test-token-123" {
		t.Fatalf("expected token test-token-123, got %s", got.Token)
	}
}

func TestRegTokenStoreGetNonExistent(t *testing.T) {
	b := memory.New()
	store := b.HypervisorTokens()
	ctx := context.Background()

	_, err := store.Get(ctx, "non-existent")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRegTokenStoreMarkUsed(t *testing.T) {
	b := memory.New()
	store := b.HypervisorTokens()
	ctx := context.Background()

	token := hypervisor.RegistrationToken{
		Token:     "use-me-token",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.Create(ctx, token); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.MarkUsed(ctx, "use-me-token", "hv-registered")
	if err != nil {
		t.Fatalf("MarkUsed: %v", err)
	}
	if !got.Used {
		t.Fatal("expected Used=true after MarkUsed")
	}
	if got.UsedBy != "hv-registered" {
		t.Fatalf("expected UsedBy=hv-registered, got %s", got.UsedBy)
	}
}

func TestRegTokenStoreMarkUsedNonExistent(t *testing.T) {
	b := memory.New()
	store := b.HypervisorTokens()
	ctx := context.Background()

	_, err := store.MarkUsed(ctx, "ghost-token", "hv")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRegTokenStoreList(t *testing.T) {
	b := memory.New()
	store := b.HypervisorTokens()
	ctx := context.Background()

	tokens := []hypervisor.RegistrationToken{
		{Token: "token-1", CreatedAt: time.Now()},
		{Token: "token-2", CreatedAt: time.Now().Add(time.Second)},
		{Token: "token-3", CreatedAt: time.Now().Add(2 * time.Second)},
	}
	for _, tok := range tokens {
		if err := store.Create(ctx, tok); err != nil {
			t.Fatalf("Create %s: %v", tok.Token, err)
		}
	}

	all, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 tokens total, got %d", len(all))
	}
}

// --- VMStore ---

func TestVMStoreCRUD(t *testing.T) {
	b := memory.New()
	store := b.VMs()
	ctx := context.Background()

	v := vm.VirtualMachine{
		Name:          "vm-01",
		HypervisorRef: "hv-01",
	}
	if err := store.Upsert(ctx, v); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.Get(ctx, "vm-01")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "vm-01" {
		t.Fatalf("expected vm-01, got %s", got.Name)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}

	if err := store.Delete(ctx, "vm-01"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(ctx, "vm-01")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestVMStoreListByHypervisor(t *testing.T) {
	b := memory.New()
	store := b.VMs()
	ctx := context.Background()

	vms := []vm.VirtualMachine{
		{Name: "vm-a", HypervisorRef: "hv-01"},
		{Name: "vm-b", HypervisorRef: "hv-01"},
		{Name: "vm-c", HypervisorRef: "hv-02"},
	}
	for _, v := range vms {
		if err := store.Upsert(ctx, v); err != nil {
			t.Fatalf("Upsert %s: %v", v.Name, err)
		}
	}

	hv01List, err := store.ListByHypervisor(ctx, "hv-01")
	if err != nil {
		t.Fatalf("ListByHypervisor hv-01: %v", err)
	}
	if len(hv01List) != 2 {
		t.Fatalf("expected 2 VMs on hv-01, got %d", len(hv01List))
	}

	hv02List, err := store.ListByHypervisor(ctx, "hv-02")
	if err != nil {
		t.Fatalf("ListByHypervisor hv-02: %v", err)
	}
	if len(hv02List) != 1 {
		t.Fatalf("expected 1 VM on hv-02, got %d", len(hv02List))
	}

	emptyList, err := store.ListByHypervisor(ctx, "hv-99")
	if err != nil {
		t.Fatalf("ListByHypervisor hv-99: %v", err)
	}
	if len(emptyList) != 0 {
		t.Fatalf("expected 0 VMs on hv-99, got %d", len(emptyList))
	}
}

func TestVMStoreListByHypervisorSorted(t *testing.T) {
	b := memory.New()
	store := b.VMs()
	ctx := context.Background()

	for _, name := range []string{"vm-c", "vm-a", "vm-b"} {
		v := vm.VirtualMachine{
			Name:          name,
			HypervisorRef: "hv-01",
		}
		if err := store.Upsert(ctx, v); err != nil {
			t.Fatalf("Upsert %s: %v", name, err)
		}
	}

	list, err := store.ListByHypervisor(ctx, "hv-01")
	if err != nil {
		t.Fatalf("ListByHypervisor: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
	if list[0].Name != "vm-a" {
		t.Fatalf("expected first vm-a, got %s", list[0].Name)
	}
	if list[1].Name != "vm-b" {
		t.Fatalf("expected second vm-b, got %s", list[1].Name)
	}
	if list[2].Name != "vm-c" {
		t.Fatalf("expected third vm-c, got %s", list[2].Name)
	}
}

func TestVMStoreDeleteNonExistent(t *testing.T) {
	b := memory.New()
	store := b.VMs()
	ctx := context.Background()

	err := store.Delete(ctx, "ghost")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// --- CloudInitStore ---

func TestCloudInitStoreCRUD(t *testing.T) {
	b := memory.New()
	store := b.CloudInits()
	ctx := context.Background()

	tpl := cloudinit.CloudInitTemplate{
		Name:     "tpl-01",
		UserData: "#cloud-config\n",
	}
	if err := store.Upsert(ctx, tpl); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.Get(ctx, "tpl-01")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserData != "#cloud-config\n" {
		t.Fatalf("unexpected userData: %s", got.UserData)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}

	if err := store.Delete(ctx, "tpl-01"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(ctx, "tpl-01")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCloudInitStoreDeleteNonExistent(t *testing.T) {
	b := memory.New()
	store := b.CloudInits()
	ctx := context.Background()

	err := store.Delete(ctx, "ghost")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// --- OSImageStore ---

func TestOSImageStoreCRUD(t *testing.T) {
	b := memory.New()
	store := b.OSImages()
	ctx := context.Background()

	img := osimage.OSImage{
		Name:      "img-01",
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
	}
	if err := store.Upsert(ctx, img); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got, err := store.Get(ctx, "img-01")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.OSFamily != "ubuntu" {
		t.Fatalf("expected ubuntu, got %s", got.OSFamily)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}

	if err := store.Delete(ctx, "img-01"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(ctx, "img-01")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestOSImageStoreDeleteNonExistent(t *testing.T) {
	b := memory.New()
	store := b.OSImages()
	ctx := context.Background()

	err := store.Delete(ctx, "ghost")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// --- Upsert overwrite ---

func TestStoreUpsertOverwrites(t *testing.T) {
	b := memory.New()
	store := b.VMs()
	ctx := context.Background()

	v1 := vm.VirtualMachine{
		Name:          "vm-01",
		HypervisorRef: "hv-01",
		Phase:         vm.PhasePending,
	}
	if err := store.Upsert(ctx, v1); err != nil {
		t.Fatalf("Upsert v1: %v", err)
	}

	v2 := v1
	v2.Phase = vm.PhaseRunning
	v2.HypervisorRef = "hv-02"
	if err := store.Upsert(ctx, v2); err != nil {
		t.Fatalf("Upsert v2: %v", err)
	}

	got, err := store.Get(ctx, "vm-01")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Phase != vm.PhaseRunning {
		t.Fatalf("expected phase Running, got %s", got.Phase)
	}
	if got.HypervisorRef != "hv-02" {
		t.Fatalf("expected hypervisorRef hv-02, got %s", got.HypervisorRef)
	}

	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 VM after upsert, got %d", len(list))
	}
}
