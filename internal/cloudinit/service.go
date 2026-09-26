package cloudinit

import (
	"context"
	"errors"
	"github.com/sugaf1204/gomi/internal/resource"
	"time"
)

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Create(ctx context.Context, t CloudInitTemplate) (CloudInitTemplate, error) {
	existing, err := s.store.Get(ctx, t.Name)
	if err != nil && !errors.Is(err, resource.ErrNotFound) {
		return CloudInitTemplate{}, err
	}
	if err == nil && existing.DeliveryMode == DeliveryVMSeed {
		t.DeliveryMode = DeliveryVMSeed
	}
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now

	if err := ValidateCloudInitTemplate(t); err != nil {
		return CloudInitTemplate{}, err
	}
	if err := s.store.Upsert(ctx, t); err != nil {
		return CloudInitTemplate{}, err
	}
	return t, nil
}

func (s *Service) Get(ctx context.Context, name string) (CloudInitTemplate, error) {
	return s.store.Get(ctx, name)
}

func (s *Service) List(ctx context.Context) ([]CloudInitTemplate, error) {
	return s.store.List(ctx)
}

func (s *Service) Update(ctx context.Context, t CloudInitTemplate) (CloudInitTemplate, error) {
	existing, err := s.store.Get(ctx, t.Name)
	if err != nil {
		return CloudInitTemplate{}, err
	}
	if existing.DeliveryMode == DeliveryVMSeed {
		t.DeliveryMode = DeliveryVMSeed
	}
	t.CreatedAt = existing.CreatedAt
	t.UpdatedAt = time.Now().UTC()

	if err := ValidateCloudInitTemplate(t); err != nil {
		return CloudInitTemplate{}, err
	}
	if err := s.store.Upsert(ctx, t); err != nil {
		return CloudInitTemplate{}, err
	}
	return t, nil
}

func (s *Service) Delete(ctx context.Context, name string) error {
	return s.store.Delete(ctx, name)
}
