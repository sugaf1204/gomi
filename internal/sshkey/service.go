package sshkey

import (
	"context"
	"strings"
	"time"
)

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Create(ctx context.Context, k SSHKey) (SSHKey, error) {
	now := time.Now().UTC()
	k.CreatedAt = now
	k.UpdatedAt = now

	// Auto-detect key type from public key prefix.
	if k.KeyType == "" {
		k.KeyType = detectKeyType(k.PublicKey)
	}

	if err := ValidateSSHKey(k); err != nil {
		return SSHKey{}, err
	}
	if err := s.store.Upsert(ctx, k); err != nil {
		return SSHKey{}, err
	}
	return k, nil
}

func (s *Service) Get(ctx context.Context, name string) (SSHKey, error) {
	return s.store.Get(ctx, name)
}

func (s *Service) List(ctx context.Context) ([]SSHKey, error) {
	return s.store.List(ctx)
}

func (s *Service) Delete(ctx context.Context, name string) error {
	return s.store.Delete(ctx, name)
}

func detectKeyType(pubKey string) string {
	pubKey = strings.TrimSpace(pubKey)
	if idx := strings.IndexByte(pubKey, ' '); idx > 0 {
		return pubKey[:idx]
	}
	return ""
}
