package machine

import (
	"context"
	"time"

	"github.com/sugaf1204/gomi/internal/power"
)

type Store interface {
	Upsert(ctx context.Context, m Machine) error
	Get(ctx context.Context, name string) (Machine, error)
	List(ctx context.Context) ([]Machine, error)
	GetByMAC(ctx context.Context, mac string) (Machine, error)
	Delete(ctx context.Context, name string) error
}

// PageLister is optionally implemented by Store backends that can return a
// single page of machines (ordered by name) together with the total count,
// without materializing the whole collection. Backends that do not implement
// it fall back to List + in-memory pagination.
type PageLister interface {
	ListPage(ctx context.Context, offset, limit int) (items []Machine, total int, err error)
}

// PowerActionStatusUpdater updates only power-action status fields. Store
// backends should implement this to avoid overwriting provisioning state from
// stale Machine snapshots.
type PowerActionStatusUpdater interface {
	UpdatePowerActionStatus(ctx context.Context, name string, action power.Action, lastError *string, updatedAt time.Time) error
}

// PowerStateStatusUpdater updates only polled power-state fields.
type PowerStateStatusUpdater interface {
	UpdatePowerStateStatus(ctx context.Context, name string, state power.PowerState, stateAt time.Time, updatedAt time.Time) error
}

// IPAddressUpdater updates only the machine's current IP address.
type IPAddressUpdater interface {
	UpdateDynamicIPAddress(ctx context.Context, name string, expectedMAC string, ip string, updatedAt time.Time) error
}

// ChangeNotifier is optionally implemented by Store backends that support
// push-based change notification (e.g. the SQL backend).
type ChangeNotifier interface {
	Subscribe(fn func())
}
