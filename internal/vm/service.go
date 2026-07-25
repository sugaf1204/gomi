package vm

import (
	"context"
	"errors"
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
	v, err := prepareForCreate(v)
	if err != nil {
		return VirtualMachine{}, err
	}
	if err := s.store.Upsert(ctx, v); err != nil {
		return VirtualMachine{}, err
	}
	return v, nil
}

// CreateExclusive stores the VM only if its name is unused, returning
// resource.ErrAlreadyExists otherwise. Callers that must not overwrite an
// existing VM use this instead of Create, whose Upsert silently replaces a
// same-named record. Backends without vm.Inserter fall back to Create.
func (s *Service) CreateExclusive(ctx context.Context, v VirtualMachine) (VirtualMachine, error) {
	inserter, ok := s.store.(Inserter)
	if !ok {
		return s.Create(ctx, v)
	}
	v, err := prepareForCreate(v)
	if err != nil {
		return VirtualMachine{}, err
	}
	if err := inserter.Insert(ctx, v); err != nil {
		return VirtualMachine{}, err
	}
	return v, nil
}

// prepareForCreate applies the defaults and validation both create paths share,
// so they cannot drift apart.
func prepareForCreate(v VirtualMachine) (VirtualMachine, error) {
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
	written, err := writeExisting(ctx, s.store, v)
	if err != nil {
		return VirtualMachine{}, err
	}
	if !written {
		return VirtualMachine{}, resource.ErrNotFound
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
	// A window the sync loop has already ended stays ended when ending it was
	// the right call: the domain was removed again after this deploy defined
	// it (Missing mark postdating the observation) or the install timed out
	// (Error). Other inactive states are the define-gap fallout — Missing
	// marked before the domain existed, possibly already recovered by a sync
	// that observed the new domain — and the deploy completing re-arms them.
	if !v.Provisioning.Active {
		removedAfterObservation := v.Phase == PhaseMissing &&
			v.Provisioning.DomainObservedAt != nil && v.MissingSince != nil &&
			v.MissingSince.After(*v.Provisioning.DomainObservedAt)
		if removedAfterObservation || v.Phase == PhaseError {
			return v, nil
		}
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
	written, err := writeExisting(ctx, s.store, v)
	if err != nil {
		return VirtualMachine{}, err
	}
	if !written {
		return VirtualMachine{}, resource.ErrNotFound
	}
	return v, nil
}

// MarkMissing records that the VM's libvirt domain is confirmed absent from
// the host (e.g. a power action hit a typed not-found). Mirroring the runtime
// sync's Missing marking, it ends any in-flight provisioning so PXE stops
// resolving install config for the absent domain, and stamps MissingSince on
// the transition into Missing.
func (s *Service) MarkMissing(ctx context.Context, name, lastAction, lastErr string) (VirtualMachine, error) {
	v, err := s.store.Get(ctx, name)
	if err != nil {
		return VirtualMachine{}, err
	}
	// A deploy is still preparing this window's domain (same predicate the
	// sync loop uses): the not-found the caller saw belongs to the define
	// gap, and marking Missing here would clobber the active deploy.
	if vmDeployInFlight(v, time.Now().UTC()) {
		return v, nil
	}
	if v.Phase != PhaseMissing || v.MissingSince == nil {
		now := time.Now().UTC()
		v.MissingSince = &now
	}
	v.Phase = PhaseMissing
	v.LastPowerAction = lastAction
	v.LastError = lastErr
	v.Provisioning.Active = false
	// Runtime addresses belong to the absent domain; embedded DNS would keep
	// publishing them otherwise.
	v.IPAddresses = nil
	v.NetworkInterfaces = nil
	v.UpdatedAt = time.Now().UTC()
	written, err := writeExisting(ctx, s.store, v)
	if err != nil {
		return VirtualMachine{}, err
	}
	if !written {
		return VirtualMachine{}, resource.ErrNotFound
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
	written, err := writeExisting(ctx, s.store, v)
	if err != nil {
		return VirtualMachine{}, err
	}
	if !written {
		return VirtualMachine{}, resource.ErrNotFound
	}
	return v, nil
}

// UpdateExisting persists v only while its row still exists, reporting
// whether a row was written. Callers use it for status writes that must not
// resurrect a record removed by a concurrent delete or cascade.
func (s *Service) UpdateExisting(ctx context.Context, v VirtualMachine) (bool, error) {
	return writeExisting(ctx, s.store, v)
}

func (s *Service) Delete(ctx context.Context, name string) error {
	return s.store.Delete(ctx, name)
}

// DeleteOwned removes the VM record only while it still references the given
// hypervisor, reporting whether it was deleted. A false result means the
// record is already gone or was moved to another hypervisor (e.g. by a
// concurrent migration) and must survive a cascade.
func (s *Service) DeleteOwned(ctx context.Context, name, hypervisorRef string) (bool, error) {
	if deleter, ok := s.store.(OwnedDeleter); ok {
		return deleter.DeleteOwned(ctx, name, hypervisorRef)
	}
	v, err := s.store.Get(ctx, name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	if v.HypervisorRef != hypervisorRef {
		return false, nil
	}
	if err := s.store.Delete(ctx, name); err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *Service) Store() Store {
	return s.store
}

func normalizeCloudInitRefs(v *VirtualMachine) {
	v.CloudInitRefs = resource.NormalizeCloudInitRefs(v.CloudInitRef, v.CloudInitRefs)
	v.CloudInitRef = ""
}
