package vm

import "context"

type Store interface {
	Upsert(ctx context.Context, v VirtualMachine) error
	Get(ctx context.Context, name string) (VirtualMachine, error)
	List(ctx context.Context) ([]VirtualMachine, error)
	ListByHypervisor(ctx context.Context, hypervisorName string) ([]VirtualMachine, error)
	Delete(ctx context.Context, name string) error
}

// Inserter is optionally implemented by Store backends that can write a VM row
// only when its name is unused, returning resource.ErrAlreadyExists otherwise.
// Create uses it so two concurrent requests for the same name cannot both
// succeed with one silently overwriting the other, which plain Upsert allows.
type Inserter interface {
	Insert(ctx context.Context, v VirtualMachine) error
}

// ExistingUpdater is optionally implemented by Store backends that can write
// a VM row only if it still exists, reporting whether a row was written. The
// runtime sync loop uses it so a status write computed from a snapshot cannot
// resurrect a record deleted concurrently.
type ExistingUpdater interface {
	UpdateExisting(ctx context.Context, v VirtualMachine) (bool, error)
}

// CreatedDeleter is optionally implemented by Store backends that can delete a
// VM row only while it still carries the given provisioning completion token,
// reporting whether a row was deleted. A create rolling back after its insert
// uses it so the ownership check and the delete are one statement: a
// delete-and-recreate landing in between must not let the rollback remove a
// replacement that owns the name under a different token.
type CreatedDeleter interface {
	DeleteCreatedToken(ctx context.Context, name, completionToken string) (bool, error)
}

// OwnedDeleter is optionally implemented by Store backends that can delete a
// VM row only while it still references the given hypervisor, reporting
// whether a row was deleted. The record-only cascade uses it so a concurrent
// migration moving the VM to another hypervisor cannot lose the record.
type OwnedDeleter interface {
	DeleteOwned(ctx context.Context, name, hypervisorRef string) (bool, error)
}

// writeExisting persists v without recreating a concurrently deleted row when
// the store supports existence-checked updates. It reports whether a row was
// written; with plain Upsert backends it always reports true.
func writeExisting(ctx context.Context, store Store, v VirtualMachine) (bool, error) {
	if updater, ok := store.(ExistingUpdater); ok {
		return updater.UpdateExisting(ctx, v)
	}
	if err := store.Upsert(ctx, v); err != nil {
		return false, err
	}
	return true, nil
}

// PageLister is optionally implemented by Store backends that can return a
// single page of virtual machines (ordered by name) together with the total
// count, without materializing the whole collection. Backends that do not
// implement it fall back to List + in-memory pagination.
type PageLister interface {
	ListPage(ctx context.Context, offset, limit int) (items []VirtualMachine, total int, err error)
}

// ChangeNotifier is optionally implemented by Store backends that support
// push-based change notification (e.g. the SQL backend).
type ChangeNotifier interface {
	Subscribe(fn func())
}
