package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/resource"
)

type OSImageStore struct{ b *Backend }

var _ osimage.Store = (*OSImageStore)(nil)

// osimageSpecJSON is the internal JSON shape stored in the spec column.
type osimageSpecJSON struct {
	OSFamily    string              `json:"osFamily"`
	OSVersion   string              `json:"osVersion"`
	Arch        string              `json:"arch"`
	Format      osimage.ImageFormat `json:"format"`
	Source      osimage.SourceType  `json:"source"`
	Variant     osimage.Variant     `json:"variant,omitempty"`
	URL         string              `json:"url,omitempty"`
	Checksum    string              `json:"checksum,omitempty"`
	SizeBytes   int64               `json:"sizeBytes,omitempty"`
	Description string              `json:"description,omitempty"`
	Manifest    *osimage.Manifest   `json:"manifest,omitempty"`
}

// osimageStatusJSON is the internal JSON shape stored in the status column.
type osimageStatusJSON struct {
	Ready     bool   `json:"ready"`
	LocalPath string `json:"localPath,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (s *OSImageStore) Upsert(ctx context.Context, img osimage.OSImage) error {
	specJSON, err := marshalJSON(osimageSpecJSON{
		OSFamily:    img.OSFamily,
		OSVersion:   img.OSVersion,
		Arch:        img.Arch,
		Format:      img.Format,
		Source:      img.Source,
		Variant:     img.Variant,
		URL:         img.URL,
		Checksum:    img.Checksum,
		SizeBytes:   img.SizeBytes,
		Description: img.Description,
		Manifest:    img.Manifest,
	})
	if err != nil {
		return err
	}
	statusJSON, err := marshalJSON(osimageStatusJSON{
		Ready:     img.Ready,
		LocalPath: img.LocalPath,
		Error:     img.Error,
	})
	if err != nil {
		return err
	}

	_, err = s.b.exec(ctx, `
		INSERT INTO os_images (name, spec, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET
			spec = EXCLUDED.spec,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at`,
		img.Name,
		specJSON, statusJSON,
		img.CreatedAt, img.UpdatedAt,
	)
	return err
}

func (s *OSImageStore) Get(ctx context.Context, name string) (osimage.OSImage, error) {
	row := s.b.queryRow(ctx, `
		SELECT name, spec, status, created_at, updated_at
		FROM os_images WHERE name = ?`,
		name,
	)
	img, err := scanOSImageRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return osimage.OSImage{}, resource.ErrNotFound
	}
	return img, err
}

func (s *OSImageStore) List(ctx context.Context) ([]osimage.OSImage, error) {
	rows, err := s.b.query(ctx,
		`SELECT name, spec, status, created_at, updated_at
		 FROM os_images ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []osimage.OSImage
	for rows.Next() {
		img, err := scanOSImageRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	return out, rows.Err()
}

func (s *OSImageStore) Delete(ctx context.Context, name string) error {
	result, err := s.b.exec(ctx,
		`DELETE FROM os_images WHERE name = ?`,
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

func scanOSImageRow(row scanner) (osimage.OSImage, error) {
	var img osimage.OSImage
	var specJSON, statusJSON string

	err := row.Scan(
		&img.Name,
		&specJSON, &statusJSON,
		&img.CreatedAt, &img.UpdatedAt,
	)
	if err != nil {
		return img, err
	}

	var spec osimageSpecJSON
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		return img, err
	}
	img.OSFamily = spec.OSFamily
	img.OSVersion = spec.OSVersion
	img.Arch = spec.Arch
	img.Format = spec.Format
	img.Source = spec.Source
	img.Variant = spec.Variant
	img.URL = spec.URL
	img.Checksum = spec.Checksum
	img.SizeBytes = spec.SizeBytes
	img.Description = spec.Description
	img.Manifest = spec.Manifest

	var status osimageStatusJSON
	if err := json.Unmarshal([]byte(statusJSON), &status); err != nil {
		return img, err
	}
	img.Ready = status.Ready
	img.LocalPath = status.LocalPath
	img.Error = status.Error

	return img, nil
}
