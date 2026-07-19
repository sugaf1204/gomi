package vm

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
)

type fakeCloudImageStorage struct {
	exists            bool
	existsByName      map[string]bool
	createdName       string
	format            string
	sizeBytes         int64
	data              []byte
	deletedName       string
	deleteErr         error
	createHadDeadline bool
}

type concurrentCloudImageStorage struct {
	mu          sync.Mutex
	volumes     map[string]bool
	inflight    map[string]bool
	createCalls int
}

func (s *concurrentCloudImageStorage) VolumeExists(_ context.Context, name string, format string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.volumes[name+"."+format], nil
}

func (s *concurrentCloudImageStorage) CreateVolumeFromReader(_ context.Context, name string, sizeBytes int64, format string, r io.Reader) error {
	key := name + "." + format
	s.mu.Lock()
	if s.volumes[key] || s.inflight[key] {
		s.mu.Unlock()
		return errors.New("duplicate backing volume create")
	}
	s.inflight[key] = true
	s.mu.Unlock()

	if _, err := io.Copy(io.Discard, r); err != nil {
		return err
	}

	s.mu.Lock()
	delete(s.inflight, key)
	s.volumes[key] = true
	s.createCalls++
	s.mu.Unlock()
	return nil
}

func (s *concurrentCloudImageStorage) DeleteVolume(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.volumes, name+".qcow2")
	return nil
}

func (s *fakeCloudImageStorage) VolumeExists(_ context.Context, name string, format string) (bool, error) {
	s.createdName = name
	s.format = format
	if s.existsByName != nil {
		return s.existsByName[cloudImageVolumeName(name, format)], nil
	}
	return s.exists, nil
}

func (s *fakeCloudImageStorage) CreateVolumeFromReader(ctx context.Context, name string, sizeBytes int64, format string, r io.Reader) error {
	s.createdName = name
	s.format = format
	s.sizeBytes = sizeBytes
	_, s.createHadDeadline = ctx.Deadline()
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.data = data
	return nil
}

func (s *fakeCloudImageStorage) DeleteVolume(_ context.Context, name string) error {
	s.deletedName = name
	return s.deleteErr
}

type testOSImageStore struct {
	items map[string]osimage.OSImage
}

func (s *testOSImageStore) Upsert(_ context.Context, img osimage.OSImage) error {
	if s.items == nil {
		s.items = map[string]osimage.OSImage{}
	}
	s.items[img.Name] = img
	return nil
}

func (s *testOSImageStore) Get(_ context.Context, name string) (osimage.OSImage, error) {
	img, ok := s.items[name]
	if !ok {
		return osimage.OSImage{}, resource.ErrNotFound
	}
	return img, nil
}

func (s *testOSImageStore) List(context.Context) ([]osimage.OSImage, error) {
	out := make([]osimage.OSImage, 0, len(s.items))
	for _, img := range s.items {
		out = append(out, img)
	}
	return out, nil
}

func (s *testOSImageStore) Delete(_ context.Context, name string) error {
	if _, ok := s.items[name]; !ok {
		return resource.ErrNotFound
	}
	delete(s.items, name)
	return nil
}

type mapVMStore struct {
	items map[string]VirtualMachine
}

func (s *mapVMStore) Upsert(_ context.Context, v VirtualMachine) error {
	s.items[v.Name] = v
	return nil
}

func (s *mapVMStore) Get(_ context.Context, name string) (VirtualMachine, error) {
	v, ok := s.items[name]
	if !ok {
		return VirtualMachine{}, resource.ErrNotFound
	}
	return v, nil
}

func (s *mapVMStore) List(_ context.Context) ([]VirtualMachine, error) {
	out := make([]VirtualMachine, 0, len(s.items))
	for _, v := range s.items {
		out = append(out, v)
	}
	return out, nil
}

func (s *mapVMStore) ListByHypervisor(_ context.Context, hypervisorName string) ([]VirtualMachine, error) {
	out := []VirtualMachine{}
	for _, v := range s.items {
		if v.HypervisorRef == hypervisorName {
			out = append(out, v)
		}
	}
	return out, nil
}

func (s *mapVMStore) Delete(_ context.Context, name string) error {
	if _, ok := s.items[name]; !ok {
		return resource.ErrNotFound
	}
	delete(s.items, name)
	return nil
}

func TestMarkDomainDefinedRecordsMarkerForActiveWindow(t *testing.T) {
	vms := NewService(&mapVMStore{items: map[string]VirtualMachine{}})
	d := &Deployer{VMs: vms}
	ctx := context.Background()

	created, err := vms.Create(ctx, VirtualMachine{
		Name:          "vm-define-marker",
		HypervisorRef: "hv-01",
		Resources:     ResourceSpec{CPUCores: 1, MemoryMB: 1024, DiskGB: 8},
		OSImageRef:    "ubuntu-test",
	})
	if err != nil {
		t.Fatalf("create vm: %v", err)
	}
	started := time.Now().UTC()
	deadline := started.Add(time.Hour)
	created.Provisioning = ProvisioningStatus{Active: true, StartedAt: &started, DeadlineAt: &deadline, CompletionToken: "tok-define"}
	if err := vms.Store().Upsert(ctx, created); err != nil {
		t.Fatalf("arm provisioning: %v", err)
	}

	d.markDomainDefined(ctx, &created)

	stored, err := vms.Get(ctx, "vm-define-marker")
	if err != nil {
		t.Fatalf("get vm: %v", err)
	}
	if stored.Provisioning.DomainObservedAt == nil {
		t.Fatal("expected the domain-defined marker to be persisted")
	}
	if created.Provisioning.DomainObservedAt == nil {
		t.Fatal("expected the caller's snapshot to carry the marker")
	}

	// A window deactivated by a Missing mark after a define gap that already
	// consumed its deadline still belongs to this deploy; the marker must be
	// recorded and the install deadline renewed from definition time.
	expiredStart := time.Now().UTC().Add(-2 * time.Hour)
	expiredDeadline := expiredStart.Add(time.Hour)
	deactivated := stored
	deactivated.Provisioning.Active = false
	deactivated.Provisioning.DomainObservedAt = nil
	deactivated.Provisioning.StartedAt = &expiredStart
	deactivated.Provisioning.DeadlineAt = &expiredDeadline
	if err := vms.Store().Upsert(ctx, deactivated); err != nil {
		t.Fatalf("seed deactivated window: %v", err)
	}
	d.markDomainDefined(ctx, &deactivated)
	stored, err = vms.Get(ctx, "vm-define-marker")
	if err != nil {
		t.Fatalf("get vm after deactivated: %v", err)
	}
	if stored.Provisioning.DomainObservedAt == nil {
		t.Fatal("expected deactivated but incomplete window to be marked")
	}
	if stored.Provisioning.DeadlineAt == nil || !stored.Provisioning.DeadlineAt.After(time.Now().UTC()) {
		t.Fatalf("expected install deadline to be renewed from definition time, got %v", stored.Provisioning.DeadlineAt)
	}
	if deactivated.Provisioning.DeadlineAt == nil || !deactivated.Provisioning.DeadlineAt.Equal(*stored.Provisioning.DeadlineAt) {
		t.Fatal("expected the caller's snapshot to carry the renewed deadline")
	}

	// A completed window is left untouched.
	now := time.Now().UTC()
	completed := stored
	completed.Provisioning.CompletedAt = &now
	completed.Provisioning.DomainObservedAt = nil
	if err := vms.Store().Upsert(ctx, completed); err != nil {
		t.Fatalf("seed completed window: %v", err)
	}
	d.markDomainDefined(ctx, &completed)
	stored, err = vms.Get(ctx, "vm-define-marker")
	if err != nil {
		t.Fatalf("get vm after completed: %v", err)
	}
	if stored.Provisioning.DomainObservedAt != nil {
		t.Fatal("expected completed provisioning window to stay unmarked")
	}

	// A stored window armed by a newer redeploy (different token) must not be
	// stamped by the older deploy.
	newer := stored
	newer.Provisioning = ProvisioningStatus{Active: true, StartedAt: &started, DeadlineAt: &deadline, CompletionToken: "tok-newer"}
	if err := vms.Store().Upsert(ctx, newer); err != nil {
		t.Fatalf("seed newer window: %v", err)
	}
	stale := newer
	stale.Provisioning.CompletionToken = "tok-define"
	d.markDomainDefined(ctx, &stale)
	stored, err = vms.Get(ctx, "vm-define-marker")
	if err != nil {
		t.Fatalf("get vm after stale deploy: %v", err)
	}
	if stored.Provisioning.DomainObservedAt != nil {
		t.Fatal("expected newer window to stay unmarked by a stale deploy")
	}
}
