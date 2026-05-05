package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"

	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/subnet"
)

type SubnetStore struct {
	b         *Backend
	mu        sync.Mutex
	listeners []func()
}

var _ subnet.Store = (*SubnetStore)(nil)
var _ subnet.ChangeNotifier = (*SubnetStore)(nil)

// Subscribe registers a callback that fires after Upsert or Delete.
func (s *SubnetStore) Subscribe(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, fn)
}

func (s *SubnetStore) notify() {
	s.mu.Lock()
	fns := make([]func(), len(s.listeners))
	copy(fns, s.listeners)
	s.mu.Unlock()
	for _, fn := range fns {
		go fn()
	}
}

func (s *SubnetStore) Upsert(ctx context.Context, sub subnet.Subnet) error {
	specJSON, err := marshalJSON(sub.Spec)
	if err != nil {
		return err
	}

	_, err = s.b.exec(ctx, `
		INSERT INTO subnets (name, cidr, spec, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET
			cidr = EXCLUDED.cidr,
			spec = EXCLUDED.spec,
			updated_at = EXCLUDED.updated_at`,
		sub.Name,
		sub.Spec.CIDR, specJSON,
		sub.CreatedAt, sub.UpdatedAt,
	)
	if err == nil {
		s.notify()
	}
	return err
}

func (s *SubnetStore) Get(ctx context.Context, name string) (subnet.Subnet, error) {
	row := s.b.queryRow(ctx, `
		SELECT name, spec, created_at, updated_at
		FROM subnets WHERE name = ?`,
		name,
	)
	sub, err := scanSubnetRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return subnet.Subnet{}, resource.ErrNotFound
	}
	return sub, err
}

func (s *SubnetStore) List(ctx context.Context) ([]subnet.Subnet, error) {
	rows, err := s.b.query(ctx,
		`SELECT name, spec, created_at, updated_at
		 FROM subnets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []subnet.Subnet
	for rows.Next() {
		sub, err := scanSubnetRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

func (s *SubnetStore) Delete(ctx context.Context, name string) error {
	result, err := s.b.exec(ctx,
		`DELETE FROM subnets WHERE name = ?`,
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
	s.notify()
	return nil
}

func scanSubnetRow(row scanner) (subnet.Subnet, error) {
	var sub subnet.Subnet
	var specJSON string

	err := row.Scan(
		&sub.Name,
		&specJSON,
		&sub.CreatedAt, &sub.UpdatedAt,
	)
	if err != nil {
		return sub, err
	}

	if err := json.Unmarshal([]byte(specJSON), &sub.Spec); err != nil {
		return sub, err
	}
	return sub, nil
}
