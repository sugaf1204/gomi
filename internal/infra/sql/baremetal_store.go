package sql

import (
	"context"
	"database/sql"
	"errors"

	"github.com/sugaf1204/gomi/internal/baremetal"
	"github.com/sugaf1204/gomi/internal/machine"
)

type BareMetalStore struct{ b *Backend }

var _ baremetal.Store = (*BareMetalStore)(nil)

func (b *Backend) BareMetal() *BareMetalStore { return &BareMetalStore{b: b} }

const hostColumns = "name, pool, COALESCE(owner, ''), state, revision, attempt_id, public_key, target_disk"

func scanHost(row *sql.Row) (baremetal.Host, error) {
	var h baremetal.Host
	err := row.Scan(&h.Name, &h.Pool, &h.Owner, &h.State, &h.Revision, &h.AttemptID, &h.PublicKey, &h.TargetDisk)
	if errors.Is(err, sql.ErrNoRows) {
		err = baremetal.ErrNotFound
	}
	return h, err
}
func (s *BareMetalStore) Get(ctx context.Context, name string) (baremetal.Host, error) {
	return scanHost(s.b.queryRow(ctx, "SELECT "+hostColumns+" FROM bare_metal_hosts WHERE name = ?", name))
}
func (s *BareMetalStore) FindOwner(ctx context.Context, owner string) (baremetal.Host, error) {
	return scanHost(s.b.queryRow(ctx, "SELECT "+hostColumns+" FROM bare_metal_hosts WHERE owner = ?", owner))
}
func (s *BareMetalStore) Register(ctx context.Context, name, pool, publicKey, targetDisk string) (baremetal.Host, error) {
	if err := baremetal.ValidateRegistration(name, pool); err != nil {
		return baremetal.Host{}, err
	}
	if !machine.IsWholeDiskPath(targetDisk) {
		return baremetal.Host{}, baremetal.ErrConflict
	}
	if _, err := baremetal.EnrollmentFingerprint(publicKey); err != nil {
		return baremetal.Host{}, err
	}
	_, err := s.b.exec(ctx, "INSERT INTO bare_metal_hosts (name,pool,public_key,target_disk,state) VALUES (?,?,?,?,'Available') ON CONFLICT(name) DO NOTHING", name, pool, publicKey, targetDisk)
	if err != nil {
		return baremetal.Host{}, err
	}
	h, err := s.Get(ctx, name)
	if err == nil && (h.Pool != pool || h.PublicKey != publicKey || h.TargetDisk != targetDisk) {
		return baremetal.Host{}, baremetal.ErrConflict
	}
	return h, err
}
func (s *BareMetalStore) UpdatePool(ctx context.Context, name, pool string, revision int64) (baremetal.Host, error) {
	if err := baremetal.ValidateRegistration(name, pool); err != nil || revision <= 0 {
		return baremetal.Host{}, baremetal.ErrConflict
	}
	updated, err := scanHost(s.b.queryRow(ctx, `UPDATE bare_metal_hosts SET pool=?,revision=revision+1
 WHERE name=? AND revision=? AND owner IS NULL AND state='Available' RETURNING `+hostColumns,
		pool, name, revision))
	if errors.Is(err, baremetal.ErrNotFound) {
		err = baremetal.ErrConflict
	}
	return updated, err
}
func (s *BareMetalStore) Acquire(ctx context.Context, pool, owner string) (baremetal.Host, error) {
	if err := baremetal.ValidateAcquire(pool, owner); err != nil {
		return baremetal.Host{}, err
	}
	h, err := s.FindOwner(ctx, owner)
	if err == nil {
		if h.Pool != pool {
			return baremetal.Host{}, baremetal.ErrConflict
		}
		return h, nil
	}
	if !errors.Is(err, baremetal.ErrNotFound) {
		return h, err
	}
	// The guarded UPDATE and unique owner constraint fence independent server
	// processes as well as concurrent requests. No read-modify-write window.
	h, err = scanHost(s.b.queryRow(ctx, `UPDATE bare_metal_hosts SET owner=?,state='Claimed',revision=revision+1
 WHERE name=(SELECT name FROM bare_metal_hosts WHERE pool=? AND owner IS NULL AND state='Available' ORDER BY name LIMIT 1)
 AND owner IS NULL AND state='Available' RETURNING `+hostColumns, owner, pool))
	if err == nil {
		return h, nil
	}
	// Another request with this owner may have committed while we waited.
	if existing, e := s.FindOwner(ctx, owner); e == nil {
		if existing.Pool != pool {
			return baremetal.Host{}, baremetal.ErrConflict
		}
		return existing, nil
	}
	if errors.Is(err, baremetal.ErrNotFound) {
		return baremetal.Host{}, baremetal.ErrCapacity
	}
	return baremetal.Host{}, err
}
func (s *BareMetalStore) Transition(ctx context.Context, h baremetal.Host, next baremetal.State, attempt string) (baremetal.Host, error) {
	if err := baremetal.ValidateTransition(h, next, attempt); err != nil {
		return baremetal.Host{}, err
	}
	updated, err := scanHost(s.b.queryRow(ctx, `UPDATE bare_metal_hosts SET state=?,attempt_id=?,revision=revision+1
 WHERE name=? AND owner=? AND revision=? AND state=? AND attempt_id=? RETURNING `+hostColumns,
		next, attempt, h.Name, h.Owner, h.Revision, h.State, h.AttemptID))
	if errors.Is(err, baremetal.ErrNotFound) {
		err = baremetal.ErrConflict
	}
	return updated, err
}

// CompleteRelease is called only after deprovisioning confirms that the host no
// longer contains the previous workload or its credentials.
func (s *BareMetalStore) CompleteRelease(ctx context.Context, h baremetal.Host) (baremetal.Host, error) {
	if h.State != baremetal.Releasing || h.Owner == "" {
		return baremetal.Host{}, baremetal.ErrConflict
	}
	updated, err := scanHost(s.b.queryRow(ctx, `UPDATE bare_metal_hosts SET owner=NULL,state='Available',attempt_id='',revision=revision+1
 WHERE name=? AND owner=? AND revision=? AND state='Releasing' RETURNING `+hostColumns, h.Name, h.Owner, h.Revision))
	if errors.Is(err, baremetal.ErrNotFound) {
		err = baremetal.ErrConflict
	}
	return updated, err
}
