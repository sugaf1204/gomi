package osimage

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

func (s *Service) Create(ctx context.Context, img OSImage) (OSImage, error) {
	now := time.Now().UTC()
	img.CreatedAt = now
	img.UpdatedAt = now
	if img.Format == "" {
		img.Format = FormatQCOW2
	}
	if img.Source == "" {
		img.Source = SourceUpload
	}

	if err := ValidateOSImage(img); err != nil {
		return OSImage{}, err
	}
	if err := s.store.Upsert(ctx, img); err != nil {
		return OSImage{}, err
	}
	return img, nil
}

func (s *Service) Get(ctx context.Context, name string) (OSImage, error) {
	return s.store.Get(ctx, name)
}

func (s *Service) List(ctx context.Context) ([]OSImage, error) {
	return s.store.List(ctx)
}

func (s *Service) UpdateStatus(ctx context.Context, name string, ready bool, localPath, errMsg string) (OSImage, error) {
	img, err := s.store.Get(ctx, name)
	if err != nil {
		return OSImage{}, err
	}
	now := time.Now().UTC()
	img.Ready = ready
	img.LocalPath = localPath
	img.Error = errMsg
	img.UpdatedAt = now
	if err := s.store.Upsert(ctx, img); err != nil {
		return OSImage{}, err
	}
	return img, nil
}

func (s *Service) UpdateChecksum(ctx context.Context, name, checksum string) (OSImage, error) {
	img, err := s.store.Get(ctx, name)
	if err != nil {
		return OSImage{}, err
	}
	img.Checksum = checksum
	img.UpdatedAt = time.Now().UTC()
	if err := s.store.Upsert(ctx, img); err != nil {
		return OSImage{}, err
	}
	return img, nil
}

func (s *Service) Delete(ctx context.Context, name string) error {
	return s.store.Delete(ctx, name)
}
