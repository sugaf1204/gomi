package cloudinit

import "context"

type Store interface {
	Upsert(ctx context.Context, t CloudInitTemplate) error
	Get(ctx context.Context, name string) (CloudInitTemplate, error)
	List(ctx context.Context) ([]CloudInitTemplate, error)
	Delete(ctx context.Context, name string) error
}
