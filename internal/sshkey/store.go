package sshkey

import "context"

type Store interface {
	Upsert(ctx context.Context, key SSHKey) error
	Get(ctx context.Context, name string) (SSHKey, error)
	List(ctx context.Context) ([]SSHKey, error)
	Delete(ctx context.Context, name string) error
}
