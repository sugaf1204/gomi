package sql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/resource"
)

type CloudInitStore struct{ b *Backend }

var _ cloudinit.Store = (*CloudInitStore)(nil)

func (s *CloudInitStore) Upsert(ctx context.Context, t cloudinit.CloudInitTemplate) error {
	_, err := s.b.exec(ctx, `
		INSERT INTO cloud_init_templates (name, user_data, network_config, metadata_tmpl, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET
			user_data = EXCLUDED.user_data,
			network_config = EXCLUDED.network_config,
			metadata_tmpl = EXCLUDED.metadata_tmpl,
			description = EXCLUDED.description,
			updated_at = EXCLUDED.updated_at`,
		t.Name,
		t.UserData, t.NetworkConfig,
		t.MetadataTemplate, t.Description,
		t.CreatedAt, t.UpdatedAt,
	)
	return err
}

func (s *CloudInitStore) Get(ctx context.Context, name string) (cloudinit.CloudInitTemplate, error) {
	row := s.b.queryRow(ctx, `
		SELECT name, user_data, network_config, metadata_tmpl, description, created_at, updated_at
		FROM cloud_init_templates WHERE name = ?`,
		name,
	)
	t, err := scanCloudInitRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return cloudinit.CloudInitTemplate{}, resource.ErrNotFound
	}
	return t, err
}

func (s *CloudInitStore) List(ctx context.Context) ([]cloudinit.CloudInitTemplate, error) {
	rows, err := s.b.query(ctx,
		`SELECT name, user_data, network_config, metadata_tmpl, description, created_at, updated_at
		 FROM cloud_init_templates ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []cloudinit.CloudInitTemplate
	for rows.Next() {
		t, err := scanCloudInitRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *CloudInitStore) Delete(ctx context.Context, name string) error {
	result, err := s.b.exec(ctx,
		`DELETE FROM cloud_init_templates WHERE name = ?`,
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

func scanCloudInitRow(row scanner) (cloudinit.CloudInitTemplate, error) {
	var t cloudinit.CloudInitTemplate

	err := row.Scan(
		&t.Name,
		&t.UserData, &t.NetworkConfig,
		&t.MetadataTemplate, &t.Description,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return t, err
	}
	return t, nil
}
