package sql

import (
	"context"
	"github.com/sugaf1204/gomi/internal/baremetal"
	"github.com/sugaf1204/gomi/internal/machine"
)

// CommitDeployment publishes the PXE attempt and its claim fence in one commit.
// A crash cannot leave a reusable host with a pending destructive installation.
func (s *BareMetalStore) CommitDeployment(ctx context.Context, h baremetal.Host, m machine.Machine) (baremetal.Host, error) {
	if h.Owner == "" || m.Name != h.Name || m.Provision == nil || !m.Provision.Active || m.Provision.AttemptID == "" || m.SealedBootstrap == nil || m.SealedBootstrap.Owner != h.Owner {
		return baremetal.Host{}, baremetal.ErrConflict
	}
	next := baremetal.Deploying
	if m.SealedBootstrap.Cleanup {
		if h.State == baremetal.Available || h.State == baremetal.Releasing {
			return baremetal.Host{}, baremetal.ErrConflict
		}
		next = baremetal.Releasing
	} else if h.State != baremetal.Claimed && h.State != baremetal.Failed && h.State != baremetal.Deploying {
		return baremetal.Host{}, baremetal.ErrConflict
	}
	spec, status, err := marshalMachineColumns(m)
	if err != nil {
		return baremetal.Host{}, err
	}
	tx, err := s.b.db.BeginTx(ctx, nil)
	if err != nil {
		return baremetal.Host{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, s.b.dialect.Rebind(`UPDATE bare_metal_hosts SET state=?,attempt_id=?,revision=revision+1
 WHERE name=? AND owner=? AND state=? AND revision=?`), next, m.Provision.AttemptID, h.Name, h.Owner, h.State, h.Revision)
	if err != nil {
		return baremetal.Host{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return baremetal.Host{}, err
	}
	if count != 1 {
		return baremetal.Host{}, baremetal.ErrConflict
	}
	result, err = tx.ExecContext(ctx, s.b.dialect.Rebind(`UPDATE machines SET hostname=?,mac=?,ip=?,arch=?,firmware=?,spec=?,status=?,updated_at=? WHERE name=?`), m.Hostname, m.MAC, m.IP, m.Arch, m.Firmware, spec, status, m.UpdatedAt, m.Name)
	if err != nil {
		return baremetal.Host{}, err
	}
	count, err = result.RowsAffected()
	if err != nil {
		return baremetal.Host{}, err
	}
	if count != 1 {
		return baremetal.Host{}, baremetal.ErrNotFound
	}
	if err = tx.Commit(); err != nil {
		return baremetal.Host{}, err
	}
	h.State = next
	h.AttemptID = m.Provision.AttemptID
	h.Revision++
	return h, nil
}
