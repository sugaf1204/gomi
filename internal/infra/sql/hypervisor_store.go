package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/resource"
)

// --- HypervisorStore ---

type HypervisorStore struct{ b *Backend }

var _ hypervisor.Store = (*HypervisorStore)(nil)

// hypervisorSpecJSON is the internal JSON shape stored in the spec column.
type hypervisorSpecJSON struct {
	Connection hypervisor.ConnectionSpec `json:"connection"`
	Labels     map[string]string         `json:"labels,omitempty"`
	MachineRef string                    `json:"machineRef,omitempty"`
	BridgeName string                    `json:"bridgeName,omitempty"`
}

// hypervisorStatusJSON is the internal JSON shape stored in the status column.
type hypervisorStatusJSON struct {
	Phase         hypervisor.Phase          `json:"phase"`
	Capacity      *hypervisor.ResourceInfo  `json:"capacity,omitempty"`
	Used          *hypervisor.ResourceUsage `json:"used,omitempty"`
	VMCount       int                       `json:"vmCount"`
	LibvirtURI    string                    `json:"libvirtURI,omitempty"`
	LastHeartbeat *time.Time                `json:"lastHeartbeat,omitempty"`
	LastError     string                    `json:"lastError,omitempty"`
}

func (s *HypervisorStore) Upsert(ctx context.Context, h hypervisor.Hypervisor) error {
	specJSON, err := marshalJSON(hypervisorSpecJSON{
		Connection: h.Connection,
		Labels:     h.Labels,
		MachineRef: h.MachineRef,
		BridgeName: h.BridgeName,
	})
	if err != nil {
		return err
	}
	statusJSON, err := marshalJSON(hypervisorStatusJSON{
		Phase:         h.Phase,
		Capacity:      h.Capacity,
		Used:          h.Used,
		VMCount:       h.VMCount,
		LibvirtURI:    h.LibvirtURI,
		LastHeartbeat: h.LastHeartbeat,
		LastError:     h.LastError,
	})
	if err != nil {
		return err
	}

	_, err = s.b.exec(ctx, `
		INSERT INTO hypervisors (name, spec, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (name) DO UPDATE SET
			spec = EXCLUDED.spec,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at`,
		h.Name,
		specJSON, statusJSON,
		h.CreatedAt, h.UpdatedAt,
	)
	return err
}

func (s *HypervisorStore) Get(ctx context.Context, name string) (hypervisor.Hypervisor, error) {
	row := s.b.queryRow(ctx, `
		SELECT name, spec, status, created_at, updated_at
		FROM hypervisors WHERE name = ?`,
		name,
	)
	h, err := scanHypervisorRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return hypervisor.Hypervisor{}, resource.ErrNotFound
	}
	return h, err
}

func (s *HypervisorStore) List(ctx context.Context) ([]hypervisor.Hypervisor, error) {
	rows, err := s.b.query(ctx,
		`SELECT name, spec, status, created_at, updated_at
		 FROM hypervisors ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []hypervisor.Hypervisor
	for rows.Next() {
		h, err := scanHypervisorRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *HypervisorStore) Delete(ctx context.Context, name string) error {
	result, err := s.b.exec(ctx,
		`DELETE FROM hypervisors WHERE name = ?`,
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

func scanHypervisorRow(row scanner) (hypervisor.Hypervisor, error) {
	var h hypervisor.Hypervisor
	var specJSON, statusJSON string

	err := row.Scan(
		&h.Name,
		&specJSON, &statusJSON,
		&h.CreatedAt, &h.UpdatedAt,
	)
	if err != nil {
		return h, err
	}

	var spec hypervisorSpecJSON
	if err := json.Unmarshal([]byte(specJSON), &spec); err != nil {
		return h, err
	}
	h.Connection = spec.Connection
	h.Labels = spec.Labels
	h.MachineRef = spec.MachineRef
	h.BridgeName = spec.BridgeName

	var status hypervisorStatusJSON
	if err := json.Unmarshal([]byte(statusJSON), &status); err != nil {
		return h, err
	}
	h.Phase = status.Phase
	h.Capacity = status.Capacity
	h.Used = status.Used
	h.VMCount = status.VMCount
	h.LibvirtURI = status.LibvirtURI
	h.LastHeartbeat = status.LastHeartbeat
	h.LastError = status.LastError

	return h, nil
}

// --- RegTokenStore ---

type RegTokenStore struct{ b *Backend }

var _ hypervisor.TokenStore = (*RegTokenStore)(nil)

func (s *RegTokenStore) Create(ctx context.Context, token hypervisor.RegistrationToken) error {
	_, err := s.b.exec(ctx, `
		INSERT INTO registration_tokens (token, used, used_by, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?)`,
		token.Token,
		boolToInt(token.Used), token.UsedBy,
		token.CreatedAt, token.ExpiresAt,
	)
	return err
}

func (s *RegTokenStore) Get(ctx context.Context, tokenValue string) (hypervisor.RegistrationToken, error) {
	var t hypervisor.RegistrationToken
	var used int
	err := s.b.queryRow(ctx,
		`SELECT token, used, used_by, created_at, expires_at
		 FROM registration_tokens WHERE token = ?`,
		tokenValue,
	).Scan(&t.Token, &used, &t.UsedBy, &t.CreatedAt, &t.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return hypervisor.RegistrationToken{}, resource.ErrNotFound
	}
	if err != nil {
		return hypervisor.RegistrationToken{}, err
	}
	t.Used = used != 0
	return t, nil
}

func (s *RegTokenStore) MarkUsed(ctx context.Context, tokenValue, usedBy string) (hypervisor.RegistrationToken, error) {
	now := time.Now().UTC()
	result, err := s.b.exec(ctx, `
		UPDATE registration_tokens SET used = 1, used_by = ?
		WHERE token = ? AND used = 0 AND expires_at > ?`,
		usedBy, tokenValue, now,
	)
	if err != nil {
		return hypervisor.RegistrationToken{}, err
	}

	n, err := result.RowsAffected()
	if err != nil {
		return hypervisor.RegistrationToken{}, err
	}
	if n == 0 {
		token, err := s.Get(ctx, tokenValue)
		if err != nil {
			return hypervisor.RegistrationToken{}, resource.ErrNotFound
		}
		if token.Used {
			return hypervisor.RegistrationToken{}, errors.New("token already used")
		}
		return hypervisor.RegistrationToken{}, errors.New("token expired")
	}

	return s.Get(ctx, tokenValue)
}

func (s *RegTokenStore) List(ctx context.Context) ([]hypervisor.RegistrationToken, error) {
	rows, err := s.b.query(ctx,
		`SELECT token, used, used_by, created_at, expires_at
		 FROM registration_tokens ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []hypervisor.RegistrationToken
	for rows.Next() {
		var t hypervisor.RegistrationToken
		var used int
		err := rows.Scan(&t.Token, &used, &t.UsedBy, &t.CreatedAt, &t.ExpiresAt)
		if err != nil {
			return nil, err
		}
		t.Used = used != 0
		out = append(out, t)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// --- AgentTokenStore ---

type AgentTokenStore struct{ b *Backend }

var _ hypervisor.AgentTokenStore = (*AgentTokenStore)(nil)

func (s *AgentTokenStore) Create(ctx context.Context, token hypervisor.AgentToken) error {
	_, err := s.b.exec(ctx, `
		INSERT INTO agent_tokens (token, hypervisor_name, created_at)
		VALUES (?, ?, ?)`,
		token.Token, token.HypervisorName, token.CreatedAt,
	)
	return err
}

func (s *AgentTokenStore) GetByToken(ctx context.Context, tokenValue string) (hypervisor.AgentToken, error) {
	var t hypervisor.AgentToken
	err := s.b.queryRow(ctx,
		`SELECT token, hypervisor_name, created_at
		 FROM agent_tokens WHERE token = ?`,
		tokenValue,
	).Scan(&t.Token, &t.HypervisorName, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return hypervisor.AgentToken{}, resource.ErrNotFound
	}
	return t, err
}

func (s *AgentTokenStore) DeleteByHypervisor(ctx context.Context, hypervisorName string) error {
	_, err := s.b.exec(ctx,
		`DELETE FROM agent_tokens WHERE hypervisor_name = ?`,
		hypervisorName,
	)
	return err
}
