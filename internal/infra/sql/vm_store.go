package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/vm"
)

type VMStore struct {
	b         *Backend
	mu        sync.Mutex
	listeners []func()
}

var _ vm.Store = (*VMStore)(nil)
var _ vm.ChangeNotifier = (*VMStore)(nil)

// Subscribe registers a callback that fires after Upsert or Delete.
func (s *VMStore) Subscribe(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, fn)
}

func (s *VMStore) notify() {
	s.mu.Lock()
	fns := make([]func(), len(s.listeners))
	copy(fns, s.listeners)
	s.mu.Unlock()
	for _, fn := range fns {
		go fn()
	}
}

// vmSpecJSON is the internal JSON shape stored in the spec column.
type vmSpecJSON struct {
	Resources          vm.ResourceSpec       `json:"resources"`
	OSImageRef         string                `json:"osImageRef,omitempty"`
	CloudInitRef       string                `json:"cloudInitRef,omitempty"`
	CloudInitRefs      []string              `json:"cloudInitRefs,omitempty"`
	Network            []vm.NetworkInterface `json:"network,omitempty"`
	IPAssignment       vm.IPAssignmentMode   `json:"ipAssignment,omitempty"`
	SubnetRef          string                `json:"subnetRef,omitempty"`
	Domain             string                `json:"domain,omitempty"`
	InstallCfg         *vm.InstallConfig     `json:"installConfig,omitempty"`
	PowerControlMethod vm.PowerControlMethod `json:"powerControlMethod"`
	AdvancedOptions    *vm.AdvancedOptions   `json:"advancedOptions,omitempty"`
	SSHKeyRefs         []string              `json:"sshKeyRefs,omitempty"`
	LoginUser          *vm.LoginUserSpec     `json:"loginUser,omitempty"`
}

// vmStatusJSON is the internal JSON shape stored in the status column.
type vmStatusJSON struct {
	Phase                    vm.Phase                    `json:"phase"`
	LibvirtDomain            string                      `json:"libvirtDomain,omitempty"`
	HypervisorName           string                      `json:"hypervisorName,omitempty"`
	IPAddresses              []string                    `json:"ipAddresses,omitempty"`
	NetworkInterfaces        []vm.NetworkInterfaceStatus `json:"networkInterfaces,omitempty"`
	Provisioning             vm.ProvisioningStatus       `json:"provisioning,omitempty"`
	LastPowerAction          string                      `json:"lastPowerAction,omitempty"`
	LastDeployedCloudInitRef string                      `json:"lastDeployedCloudInitRef,omitempty"`
	LastError                string                      `json:"lastError,omitempty"`
	MissingSince             *time.Time                  `json:"missingSince,omitempty"`
	CreatedOnHost            string                      `json:"createdOnHost,omitempty"`
}

func marshalVMColumns(v vm.VirtualMachine) (specJSON, statusJSON string, err error) {
	specJSON, err = marshalJSON(vmSpecJSON{
		Resources:          v.Resources,
		OSImageRef:         v.OSImageRef,
		CloudInitRef:       v.CloudInitRef,
		CloudInitRefs:      v.CloudInitRefs,
		Network:            v.Network,
		IPAssignment:       v.IPAssignment,
		SubnetRef:          v.SubnetRef,
		Domain:             v.Domain,
		InstallCfg:         v.InstallCfg,
		PowerControlMethod: v.PowerControlMethod,
		AdvancedOptions:    v.AdvancedOptions,
		SSHKeyRefs:         v.SSHKeyRefs,
		LoginUser:          v.LoginUser,
	})
	if err != nil {
		return "", "", err
	}
	statusJSON, err = marshalJSON(vmStatusJSON{
		Phase:                    v.Phase,
		LibvirtDomain:            v.LibvirtDomain,
		HypervisorName:           v.HypervisorName,
		IPAddresses:              v.IPAddresses,
		NetworkInterfaces:        v.NetworkInterfaces,
		Provisioning:             v.Provisioning,
		LastPowerAction:          v.LastPowerAction,
		LastDeployedCloudInitRef: v.LastDeployedCloudInitRef,
		LastError:                v.LastError,
		MissingSince:             v.MissingSince,
		CreatedOnHost:            v.CreatedOnHost,
	})
	if err != nil {
		return "", "", err
	}
	return specJSON, statusJSON, nil
}

func (s *VMStore) Upsert(ctx context.Context, v vm.VirtualMachine) error {
	specJSON, statusJSON, err := marshalVMColumns(v)
	if err != nil {
		return err
	}

	_, err = s.b.exec(ctx, `
		INSERT INTO virtual_machines (name, hypervisor_ref, spec, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET
			hypervisor_ref = EXCLUDED.hypervisor_ref,
			spec = EXCLUDED.spec,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at`,
		v.Name,
		v.HypervisorRef,
		specJSON, statusJSON,
		v.CreatedAt, v.UpdatedAt,
	)
	if err == nil {
		s.notify()
	}
	return err
}

// UpdateExisting writes the VM only if its row still exists. It implements
// vm.ExistingUpdater.
// Insert writes the VM only if its name is unused, letting the primary key
// reject a duplicate. It implements vm.Inserter. A handler-side existence
// check could not do this: two concurrent requests can both find the name free
// and then both write, which is exactly what Upsert's ON CONFLICT DO UPDATE
// turns into a silent overwrite.
func (s *VMStore) Insert(ctx context.Context, v vm.VirtualMachine) error {
	specJSON, statusJSON, err := marshalVMColumns(v)
	if err != nil {
		return err
	}

	_, err = s.b.exec(ctx, `
		INSERT INTO virtual_machines (name, hypervisor_ref, spec, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		v.Name,
		v.HypervisorRef,
		specJSON, statusJSON,
		v.CreatedAt, v.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return resource.ErrAlreadyExists
		}
		return err
	}
	s.notify()
	return nil
}

func (s *VMStore) UpdateExisting(ctx context.Context, v vm.VirtualMachine) (bool, error) {
	specJSON, statusJSON, err := marshalVMColumns(v)
	if err != nil {
		return false, err
	}

	res, err := s.b.exec(ctx, `
		UPDATE virtual_machines
		SET hypervisor_ref = ?, spec = ?, status = ?, updated_at = ?
		WHERE name = ?`,
		v.HypervisorRef, specJSON, statusJSON, v.UpdatedAt, v.Name,
	)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows > 0 {
		s.notify()
	}
	return rows > 0, nil
}

func (s *VMStore) Get(ctx context.Context, name string) (vm.VirtualMachine, error) {
	row := s.b.queryRow(ctx, `
		SELECT name, hypervisor_ref, spec, status, created_at, updated_at
		FROM virtual_machines WHERE name = ?`,
		name,
	)
	v, err := scanVMRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return vm.VirtualMachine{}, resource.ErrNotFound
	}
	return v, err
}

func (s *VMStore) List(ctx context.Context) ([]vm.VirtualMachine, error) {
	rows, err := s.b.query(ctx,
		`SELECT name, hypervisor_ref, spec, status, created_at, updated_at
		 FROM virtual_machines ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []vm.VirtualMachine
	for rows.Next() {
		v, err := scanVMRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ListPage returns one page of virtual machines (ordered by name) plus the
// total row count, using SQL LIMIT/OFFSET so only the requested rows are
// scanned and decoded. It implements vm.PageLister.
func (s *VMStore) ListPage(ctx context.Context, offset, limit int) ([]vm.VirtualMachine, int, error) {
	var total int
	if err := s.b.queryRow(ctx, `SELECT COUNT(*) FROM virtual_machines`).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 || offset >= total {
		return []vm.VirtualMachine{}, total, nil
	}
	rows, err := s.b.query(ctx,
		`SELECT name, hypervisor_ref, spec, status, created_at, updated_at
		 FROM virtual_machines ORDER BY name LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]vm.VirtualMachine, 0, limit)
	for rows.Next() {
		v, err := scanVMRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

func (s *VMStore) ListByHypervisor(ctx context.Context, hypervisorName string) ([]vm.VirtualMachine, error) {
	rows, err := s.b.query(ctx,
		`SELECT name, hypervisor_ref, spec, status, created_at, updated_at
		 FROM virtual_machines WHERE hypervisor_ref = ? ORDER BY name`,
		hypervisorName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []vm.VirtualMachine
	for rows.Next() {
		v, err := scanVMRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// DeleteOwned deletes the VM row only while it still references the given
// hypervisor. It implements vm.OwnedDeleter.
// DeleteCreatedToken removes the row only while it still carries the given
// provisioning completion token, reporting whether it was deleted. It
// implements vm.CreatedDeleter. The comparison is part of the DELETE so a
// delete-and-recreate landing between a separate lookup and the delete cannot
// make this remove a replacement that owns the name under a different token.
func (s *VMStore) DeleteCreatedToken(ctx context.Context, name, completionToken string) (bool, error) {
	query := `DELETE FROM virtual_machines WHERE name = ? AND `
	if s.b.dialect == DialectPostgres {
		query += `COALESCE(status::jsonb -> 'provisioning' ->> 'completionToken', '') = ?`
	} else {
		query += `COALESCE(json_extract(status, '$.provisioning.completionToken'), '') = ?`
	}

	res, err := s.b.exec(ctx, query, name, completionToken)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows > 0 {
		s.notify()
	}
	return rows > 0, nil
}

func (s *VMStore) DeleteOwned(ctx context.Context, name, hypervisorRef string) (bool, error) {
	res, err := s.b.exec(ctx,
		`DELETE FROM virtual_machines WHERE name = ? AND hypervisor_ref = ?`,
		name, hypervisorRef,
	)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if rows > 0 {
		s.notify()
	}
	return rows > 0, nil
}

func (s *VMStore) Delete(ctx context.Context, name string) error {
	result, err := s.b.exec(ctx,
		`DELETE FROM virtual_machines WHERE name = ?`,
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

func scanVMRow(row scanner) (vm.VirtualMachine, error) {
	var v vm.VirtualMachine
	var hypervisorRef, specJSON, statusJSON string

	err := row.Scan(
		&v.Name, &hypervisorRef,
		&specJSON, &statusJSON,
		&v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		return v, err
	}

	v.HypervisorRef = hypervisorRef

	var spec vmSpecJSON
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		return v, err
	}
	v.Resources = spec.Resources
	v.OSImageRef = spec.OSImageRef
	v.CloudInitRef = spec.CloudInitRef
	v.CloudInitRefs = spec.CloudInitRefs
	v.Network = spec.Network
	v.IPAssignment = spec.IPAssignment
	v.SubnetRef = spec.SubnetRef
	v.Domain = spec.Domain
	v.InstallCfg = spec.InstallCfg
	v.PowerControlMethod = spec.PowerControlMethod
	v.AdvancedOptions = spec.AdvancedOptions
	v.SSHKeyRefs = spec.SSHKeyRefs
	v.LoginUser = spec.LoginUser

	var status vmStatusJSON
	if err := json.Unmarshal([]byte(statusJSON), &status); err != nil {
		return v, err
	}
	v.Phase = status.Phase
	v.LibvirtDomain = status.LibvirtDomain
	v.HypervisorName = status.HypervisorName
	v.IPAddresses = status.IPAddresses
	v.NetworkInterfaces = status.NetworkInterfaces
	v.Provisioning = status.Provisioning
	v.LastPowerAction = status.LastPowerAction
	v.LastDeployedCloudInitRef = status.LastDeployedCloudInitRef
	v.LastError = status.LastError
	v.MissingSince = status.MissingSince
	v.CreatedOnHost = status.CreatedOnHost

	return v, nil
}
