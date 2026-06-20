package vm

import "context"

type Store interface {
	Upsert(ctx context.Context, v VirtualMachine) error
	Get(ctx context.Context, name string) (VirtualMachine, error)
	List(ctx context.Context) ([]VirtualMachine, error)
	ListByHypervisor(ctx context.Context, hypervisorName string) ([]VirtualMachine, error)
	Delete(ctx context.Context, name string) error
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
