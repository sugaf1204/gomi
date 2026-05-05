package subnet

import "context"

type Store interface {
	Upsert(ctx context.Context, s Subnet) error
	Get(ctx context.Context, name string) (Subnet, error)
	List(ctx context.Context) ([]Subnet, error)
	Delete(ctx context.Context, name string) error
}

// ChangeNotifier is optionally implemented by Store backends that support
// push-based change notification (e.g. the SQL backend).
type ChangeNotifier interface {
	Subscribe(fn func())
}
