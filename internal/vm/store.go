package vm

import "context"

type Store interface {
	Upsert(ctx context.Context, v VirtualMachine) error
	Get(ctx context.Context, name string) (VirtualMachine, error)
	List(ctx context.Context) ([]VirtualMachine, error)
	ListByHypervisor(ctx context.Context, hypervisorName string) ([]VirtualMachine, error)
	Delete(ctx context.Context, name string) error
}

// ChangeNotifier is optionally implemented by Store backends that support
// push-based change notification (e.g. the SQL backend).
type ChangeNotifier interface {
	Subscribe(fn func())
}
