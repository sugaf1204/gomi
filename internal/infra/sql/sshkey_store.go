package sql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/sshkey"
)

type SSHKeyStore struct{ b *Backend }

var _ sshkey.Store = (*SSHKeyStore)(nil)

func (s *SSHKeyStore) Upsert(ctx context.Context, k sshkey.SSHKey) error {
	_, err := s.b.exec(ctx, `
		INSERT INTO ssh_keys (name, public_key, private_key, comment, key_type, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET
			public_key = EXCLUDED.public_key,
			private_key = EXCLUDED.private_key,
			comment = EXCLUDED.comment,
			key_type = EXCLUDED.key_type,
			updated_at = EXCLUDED.updated_at`,
		k.Name,
		k.PublicKey, k.PrivateKey,
		k.Comment, k.KeyType,
		k.CreatedAt, k.UpdatedAt,
	)
	return err
}

func (s *SSHKeyStore) Get(ctx context.Context, name string) (sshkey.SSHKey, error) {
	row := s.b.queryRow(ctx, `
		SELECT name, public_key, private_key, comment, key_type, created_at, updated_at
		FROM ssh_keys WHERE name = ?`,
		name,
	)
	k, err := scanSSHKeyRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return sshkey.SSHKey{}, resource.ErrNotFound
	}
	return k, err
}

func (s *SSHKeyStore) List(ctx context.Context) ([]sshkey.SSHKey, error) {
	rows, err := s.b.query(ctx,
		`SELECT name, public_key, private_key, comment, key_type, created_at, updated_at
		 FROM ssh_keys ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []sshkey.SSHKey
	for rows.Next() {
		k, err := scanSSHKeyRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *SSHKeyStore) Delete(ctx context.Context, name string) error {
	result, err := s.b.exec(ctx,
		`DELETE FROM ssh_keys WHERE name = ?`,
		name,
	)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return resource.ErrNotFound
	}
	return nil
}

func scanSSHKeyRow(row scanner) (sshkey.SSHKey, error) {
	var k sshkey.SSHKey

	err := row.Scan(
		&k.Name,
		&k.PublicKey, &k.PrivateKey,
		&k.Comment, &k.KeyType,
		&k.CreatedAt, &k.UpdatedAt,
	)
	if err != nil {
		return k, err
	}
	return k, nil
}
