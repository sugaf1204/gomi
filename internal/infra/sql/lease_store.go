package sql

import (
	"context"

	"github.com/sugaf1204/gomi/internal/pxe"
)

type DHCPLeaseStore struct{ b *Backend }

var _ pxe.LeaseStore = (*DHCPLeaseStore)(nil)

func (s *DHCPLeaseStore) Upsert(ctx context.Context, lease pxe.DHCPLease) error {
	_, err := s.b.exec(ctx, `
		INSERT INTO dhcp_leases (mac, ip, hostname, pxe_client, leased_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (mac) DO UPDATE SET
			ip = EXCLUDED.ip,
			hostname = EXCLUDED.hostname,
			pxe_client = EXCLUDED.pxe_client,
			leased_at = EXCLUDED.leased_at`,
		lease.MAC, lease.IP, lease.Hostname,
		boolToInt(lease.PXEClient), lease.LeasedAt,
	)
	return err
}

func (s *DHCPLeaseStore) List(ctx context.Context) ([]pxe.DHCPLease, error) {
	rows, err := s.b.query(ctx,
		`SELECT mac, ip, hostname, pxe_client, leased_at
		 FROM dhcp_leases ORDER BY mac`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []pxe.DHCPLease
	for rows.Next() {
		var l pxe.DHCPLease
		var pxeClient int
		if err := rows.Scan(&l.MAC, &l.IP, &l.Hostname, &pxeClient, &l.LeasedAt); err != nil {
			return nil, err
		}
		l.PXEClient = pxeClient != 0
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *DHCPLeaseStore) Delete(ctx context.Context, mac string) error {
	_, err := s.b.exec(ctx,
		`DELETE FROM dhcp_leases WHERE mac = ?`,
		mac,
	)
	return err
}
