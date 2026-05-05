package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
)

type MachineStore struct {
	b         *Backend
	mu        sync.Mutex
	listeners []func()
}

var _ machine.Store = (*MachineStore)(nil)
var _ machine.ChangeNotifier = (*MachineStore)(nil)
var _ machine.PowerActionStatusUpdater = (*MachineStore)(nil)
var _ machine.PowerStateStatusUpdater = (*MachineStore)(nil)
var _ machine.IPAddressUpdater = (*MachineStore)(nil)

// Subscribe registers a callback that fires after Upsert or Delete.
func (s *MachineStore) Subscribe(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, fn)
}

func (s *MachineStore) notify() {
	s.mu.Lock()
	fns := make([]func(), len(s.listeners))
	copy(fns, s.listeners)
	s.mu.Unlock()
	for _, fn := range fns {
		go fn()
	}
}

// machineSpecJSON is the internal JSON shape stored in the spec column.
type machineSpecJSON struct {
	Power         power.PowerConfig        `json:"power"`
	Network       machine.NetworkConfig    `json:"network"`
	OSPreset      machine.OSPreset         `json:"osPreset"`
	CloudInitRef  string                   `json:"cloudInitRef,omitempty"`
	CloudInitRefs []string                 `json:"cloudInitRefs,omitempty"`
	IPAssignment  machine.IPAssignmentMode `json:"ipAssignment,omitempty"`
	SubnetRef     string                   `json:"subnetRef,omitempty"`
	Role          machine.Role             `json:"role,omitempty"`
	BridgeName    string                   `json:"bridgeName,omitempty"`
	SSHKeyRefs    []string                 `json:"sshKeyRefs,omitempty"`
	LoginUser     *machine.LoginUserSpec   `json:"loginUser,omitempty"`
}

// machineStatusJSON is the internal JSON shape stored in the status column.
type machineStatusJSON struct {
	Phase                    machine.Phase              `json:"phase"`
	Provision                *machine.ProvisionProgress `json:"provision,omitempty"`
	LastPowerAction          string                     `json:"lastPowerAction,omitempty"`
	LastDeployedCloudInitRef string                     `json:"lastDeployedCloudInitRef,omitempty"`
	LastError                string                     `json:"lastError,omitempty"`
	PowerState               power.PowerState           `json:"powerState,omitempty"`
	PowerStateAt             *time.Time                 `json:"powerStateAt,omitempty"`
}

func (s *MachineStore) Upsert(ctx context.Context, m machine.Machine) error {
	specJSON, err := marshalJSON(machineSpecJSON{
		Power:         m.Power,
		Network:       m.Network,
		OSPreset:      m.OSPreset,
		CloudInitRef:  m.CloudInitRef,
		CloudInitRefs: m.CloudInitRefs,
		IPAssignment:  m.IPAssignment,
		SubnetRef:     m.SubnetRef,
		Role:          m.Role,
		BridgeName:    m.BridgeName,
		SSHKeyRefs:    m.SSHKeyRefs,
		LoginUser:     m.LoginUser,
	})
	if err != nil {
		return err
	}
	statusJSON, err := marshalJSON(machineStatusJSON{
		Phase:                    m.Phase,
		Provision:                m.Provision,
		LastPowerAction:          m.LastPowerAction,
		LastDeployedCloudInitRef: m.LastDeployedCloudInitRef,
		LastError:                m.LastError,
		PowerState:               m.PowerState,
		PowerStateAt:             m.PowerStateAt,
	})
	if err != nil {
		return err
	}

	_, err = s.b.exec(ctx, `
		INSERT INTO machines (name, hostname, mac, ip, arch, firmware, spec, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET
			hostname = EXCLUDED.hostname,
			mac = EXCLUDED.mac,
			ip = EXCLUDED.ip,
			arch = EXCLUDED.arch,
			firmware = EXCLUDED.firmware,
			spec = EXCLUDED.spec,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at`,
		m.Name,
		m.Hostname, m.MAC, m.IP,
		string(m.Arch), string(m.Firmware),
		specJSON, statusJSON,
		m.CreatedAt, m.UpdatedAt,
	)
	if err == nil {
		s.notify()
	}
	return err
}

func (s *MachineStore) UpdatePowerActionStatus(ctx context.Context, name string, action power.Action, lastError *string, updatedAt time.Time) error {
	return s.updateMachineStatus(ctx, name, updatedAt, func(status *machineStatusJSON) {
		status.LastPowerAction = string(action)
		if lastError != nil {
			status.LastError = *lastError
		}
	})
}

func (s *MachineStore) UpdatePowerStateStatus(ctx context.Context, name string, state power.PowerState, stateAt time.Time, updatedAt time.Time) error {
	return s.updateMachineStatus(ctx, name, updatedAt, func(status *machineStatusJSON) {
		status.PowerState = state
		status.PowerStateAt = &stateAt
	})
}

func (s *MachineStore) updateMachineStatus(ctx context.Context, name string, updatedAt time.Time, mutate func(*machineStatusJSON)) error {
	tx, err := s.b.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `SELECT status FROM machines WHERE name = ?`
	if s.b.dialect == DialectPostgres {
		query += ` FOR UPDATE`
	}
	row := tx.QueryRowContext(ctx, s.b.dialect.Rebind(query), name)

	var statusJSON string
	if err := row.Scan(&statusJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return resource.ErrNotFound
		}
		return err
	}

	var status machineStatusJSON
	if err := json.Unmarshal([]byte(statusJSON), &status); err != nil {
		return err
	}
	mutate(&status)

	nextStatusJSON, err := marshalJSON(status)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, s.b.dialect.Rebind(`
		UPDATE machines SET status = ?, updated_at = ? WHERE name = ?`),
		nextStatusJSON, updatedAt, name,
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
	if err := tx.Commit(); err != nil {
		return err
	}
	s.notify()
	return nil
}

func (s *MachineStore) UpdateDynamicIPAddress(ctx context.Context, name string, expectedMAC string, ip string, updatedAt time.Time) error {
	query := `
		UPDATE machines
		SET ip = ?, updated_at = ?
		WHERE name = ?
		  AND LOWER(mac) = ?`
	if s.b.dialect == DialectPostgres {
		query += ` AND COALESCE(spec::jsonb ->> 'ipAssignment', '') != ?`
	} else {
		query += ` AND COALESCE(json_extract(spec, '$.ipAssignment'), '') != ?`
	}
	result, err := s.b.exec(ctx, query, ip, updatedAt, name, strings.ToLower(expectedMAC), string(machine.IPAssignmentModeStatic))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		s.notify()
		return nil
	}

	if _, err := s.Get(ctx, name); err != nil {
		if !errors.Is(err, resource.ErrNotFound) {
			return err
		}
		return resource.ErrNotFound
	}
	return nil
}

func (s *MachineStore) Get(ctx context.Context, name string) (machine.Machine, error) {
	row := s.b.queryRow(ctx, `
		SELECT name, hostname, mac, ip, arch, firmware, spec, status, created_at, updated_at
		FROM machines WHERE name = ?`,
		name,
	)
	m, err := scanMachineRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return machine.Machine{}, resource.ErrNotFound
	}
	return m, err
}

func (s *MachineStore) List(ctx context.Context) ([]machine.Machine, error) {
	rows, err := s.b.query(ctx,
		`SELECT name, hostname, mac, ip, arch, firmware, spec, status, created_at, updated_at
		 FROM machines ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []machine.Machine
	for rows.Next() {
		m, err := scanMachineRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *MachineStore) GetByMAC(ctx context.Context, mac string) (machine.Machine, error) {
	row := s.b.queryRow(ctx, `
		SELECT name, hostname, mac, ip, arch, firmware, spec, status, created_at, updated_at
		FROM machines WHERE LOWER(mac) = ?`,
		strings.ToLower(mac),
	)
	m, err := scanMachineRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return machine.Machine{}, resource.ErrNotFound
	}
	return m, err
}

func (s *MachineStore) Delete(ctx context.Context, name string) error {
	result, err := s.b.exec(ctx,
		`DELETE FROM machines WHERE name = ?`,
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

type scanner interface {
	Scan(dest ...any) error
}

func scanMachineRow(row scanner) (machine.Machine, error) {
	var m machine.Machine
	var specJSON, statusJSON string

	err := row.Scan(
		&m.Name,
		&m.Hostname, &m.MAC, &m.IP,
		&m.Arch, &m.Firmware,
		&specJSON, &statusJSON,
		&m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return m, err
	}

	var spec machineSpecJSON
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		return m, err
	}
	m.Power = spec.Power
	m.Network = spec.Network
	m.OSPreset = spec.OSPreset
	m.CloudInitRef = spec.CloudInitRef
	m.CloudInitRefs = spec.CloudInitRefs
	m.IPAssignment = spec.IPAssignment
	m.SubnetRef = spec.SubnetRef
	m.Role = spec.Role
	m.BridgeName = spec.BridgeName
	m.SSHKeyRefs = spec.SSHKeyRefs
	m.LoginUser = spec.LoginUser

	var status machineStatusJSON
	if err := json.Unmarshal([]byte(statusJSON), &status); err != nil {
		return m, err
	}
	m.Phase = status.Phase
	m.Provision = status.Provision
	m.LastPowerAction = status.LastPowerAction
	m.LastDeployedCloudInitRef = status.LastDeployedCloudInitRef
	m.LastError = status.LastError
	m.PowerState = status.PowerState
	m.PowerStateAt = status.PowerStateAt

	return m, nil
}
