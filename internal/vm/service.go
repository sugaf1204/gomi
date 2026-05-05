package vm

import (
	"context"
	"time"

	"github.com/sugaf1204/gomi/internal/resource"
)

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Create(ctx context.Context, v VirtualMachine) (VirtualMachine, error) {
	now := time.Now().UTC()
	v.CreatedAt = now
	v.UpdatedAt = now
	if v.Phase == "" {
		v.Phase = PhasePending
	}
	// VM power control is always libvirt.
	v.PowerControlMethod = PowerControlLibvirt

	normalizeCloudInitRefs(&v)
	if err := ValidateVirtualMachine(v); err != nil {
		return VirtualMachine{}, err
	}
	if len(v.CloudInitRefs) > 0 {
		v.LastDeployedCloudInitRef = v.CloudInitRefs[0]
	}
	if err := s.store.Upsert(ctx, v); err != nil {
		return VirtualMachine{}, err
	}
	return v, nil
}

func (s *Service) Get(ctx context.Context, name string) (VirtualMachine, error) {
	return s.store.Get(ctx, name)
}

func (s *Service) List(ctx context.Context) ([]VirtualMachine, error) {
	return s.store.List(ctx)
}

func (s *Service) ListByHypervisor(ctx context.Context, hypervisorName string) ([]VirtualMachine, error) {
	return s.store.ListByHypervisor(ctx, hypervisorName)
}

func (s *Service) UpdateStatus(ctx context.Context, name string, phase Phase, lastAction, lastErr string) (VirtualMachine, error) {
	v, err := s.store.Get(ctx, name)
	if err != nil {
		return VirtualMachine{}, err
	}
	now := time.Now().UTC()
	v.Phase = phase
	v.LastPowerAction = lastAction
	v.LastError = lastErr
	v.UpdatedAt = now
	if err := s.store.Upsert(ctx, v); err != nil {
		return VirtualMachine{}, err
	}
	return v, nil
}

func (s *Service) Delete(ctx context.Context, name string) error {
	return s.store.Delete(ctx, name)
}

func (s *Service) Store() Store {
	return s.store
}

func normalizeCloudInitRefs(v *VirtualMachine) {
	v.CloudInitRefs = resource.NormalizeCloudInitRefs(v.CloudInitRef, v.CloudInitRefs)
	v.CloudInitRef = ""
}
