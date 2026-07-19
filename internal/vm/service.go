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

// ListPage returns a single page of virtual machines (ordered by name) plus the
// total count. ok reports whether the underlying store supports paged reads;
// when it is false callers should fall back to List.
func (s *Service) ListPage(ctx context.Context, offset, limit int) (items []VirtualMachine, total int, ok bool, err error) {
	pl, ok := s.store.(PageLister)
	if !ok {
		return nil, 0, false, nil
	}
	items, total, err = pl.ListPage(ctx, offset, limit)
	return items, total, true, err
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

// UpdateDeployStatus persists the outcome of a completed deploy step and
// restores the provisioning state armed by the API handler. The runtime sync
// loop may have marked the record Missing (ending provisioning) while the
// domain was not defined yet; the deploy completing proves the domain exists,
// so the provisioning window must be re-armed for PXE resolution.
func (s *Service) UpdateDeployStatus(ctx context.Context, name string, phase Phase, lastAction string, provisioning ProvisioningStatus) (VirtualMachine, error) {
	v, err := s.store.Get(ctx, name)
	if err != nil {
		return VirtualMachine{}, err
	}
	// A different stored completion token means a newer deploy has armed its
	// own provisioning window since this deploy started; the stale snapshot
	// must not clobber it.
	if v.Provisioning.CompletionToken != provisioning.CompletionToken {
		return v, nil
	}
	// An install-complete callback may have already finished this
	// provisioning window while the deploy was unwinding; keep the completed
	// state instead of re-arming the stale window.
	if v.Provisioning.CompletedAt != nil {
		return v, nil
	}
	// The sync loop observed this window's domain and then saw it removed
	// (the Missing mark postdates the observation); the deploy finishing
	// later must not resurrect the window for a domain that is gone again.
	// A Missing mark that predates the observation is the opposite case: the
	// deploy defined the domain after a timed-out define gap, so the restore
	// below re-arms the install.
	if v.Phase == PhaseMissing && v.Provisioning.DomainObservedAt != nil &&
		v.MissingSince != nil && v.MissingSince.After(*v.Provisioning.DomainObservedAt) {
		return v, nil
	}
	v.MissingSince = nil
	// The domain-defined marker (and the deadline renewed with it) may have
	// been recorded for this window after the caller captured its snapshot;
	// restoring the snapshot must not erase them, or a later domain removal
	// would look like the define gap and a slow define gap would leave the
	// install with an already-expired deadline.
	if provisioning.DomainObservedAt == nil {
		provisioning.DomainObservedAt = v.Provisioning.DomainObservedAt
	}
	if v.Provisioning.DeadlineAt != nil && (provisioning.DeadlineAt == nil || v.Provisioning.DeadlineAt.After(*provisioning.DeadlineAt)) {
		provisioning.DeadlineAt = v.Provisioning.DeadlineAt
	}
	v.Phase = phase
	v.LastPowerAction = lastAction
	v.LastError = ""
	v.Provisioning = provisioning
	v.UpdatedAt = time.Now().UTC()
	if err := s.store.Upsert(ctx, v); err != nil {
		return VirtualMachine{}, err
	}
	return v, nil
}

// FailDeploy records a deploy failure and ends the provisioning window that
// deploy armed, so PXE resolution stops serving install config for a deploy
// that is already over. The record is only touched while it still belongs to
// the failed deploy (matching completion token), so a newer redeploy's window
// survives a stale failure report.
func (s *Service) FailDeploy(ctx context.Context, name, lastAction, lastErr, completionToken string) (VirtualMachine, error) {
	v, err := s.store.Get(ctx, name)
	if err != nil {
		return VirtualMachine{}, err
	}
	if v.Provisioning.CompletionToken != completionToken {
		return v, nil
	}
	v.Phase = PhaseError
	v.LastPowerAction = lastAction
	v.LastError = lastErr
	if v.Provisioning.CompletedAt == nil {
		v.Provisioning.Active = false
	}
	v.UpdatedAt = time.Now().UTC()
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
