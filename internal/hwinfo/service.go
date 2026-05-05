package hwinfo

import (
	"context"
	"time"
)

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Upsert(ctx context.Context, info HardwareInfo) (HardwareInfo, error) {
	now := time.Now().UTC()
	if info.CreatedAt.IsZero() {
		info.CreatedAt = now
	}
	info.UpdatedAt = now
	if err := s.store.Upsert(ctx, info); err != nil {
		return HardwareInfo{}, err
	}
	return info, nil
}

func (s *Service) Get(ctx context.Context, machineName string) (HardwareInfo, error) {
	return s.store.Get(ctx, machineName)
}

func (s *Service) Delete(ctx context.Context, machineName string) error {
	return s.store.Delete(ctx, machineName)
}
