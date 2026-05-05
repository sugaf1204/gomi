package hwinfo

import "context"

type Store interface {
	Upsert(ctx context.Context, info HardwareInfo) error
	Get(ctx context.Context, machineName string) (HardwareInfo, error)
	Delete(ctx context.Context, machineName string) error
}
