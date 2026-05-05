package osimage

import "context"

type Store interface {
	Upsert(ctx context.Context, img OSImage) error
	Get(ctx context.Context, name string) (OSImage, error)
	List(ctx context.Context) ([]OSImage, error)
	Delete(ctx context.Context, name string) error
}
