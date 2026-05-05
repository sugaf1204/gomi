package sql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/resource"
)

type AuthStore struct{ b *Backend }

var _ auth.Store = (*AuthStore)(nil)

func (s *AuthStore) UpsertUser(ctx context.Context, user auth.User) error {
	_, err := s.b.exec(ctx, `
		INSERT INTO users (username, password_hash, role, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (username) DO UPDATE SET
			password_hash = EXCLUDED.password_hash,
			role = EXCLUDED.role`,
		user.Username, user.PasswordHash, string(user.Role), user.CreatedAt,
	)
	return err
}

func (s *AuthStore) GetUser(ctx context.Context, username string) (auth.User, error) {
	var u auth.User
	var role string
	err := s.b.queryRow(ctx,
		`SELECT username, password_hash, role, created_at FROM users WHERE username = ?`,
		username,
	).Scan(&u.Username, &u.PasswordHash, &role, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.User{}, resource.ErrNotFound
	}
	if err != nil {
		return auth.User{}, err
	}
	u.Role = auth.Role(role)
	return u, nil
}

func (s *AuthStore) CountUsers(ctx context.Context) (int, error) {
	var count int
	if err := s.b.queryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *AuthStore) CreateSession(ctx context.Context, session auth.Session) error {
	_, err := s.b.exec(ctx, `
		INSERT INTO sessions (token, username, created_at, expires_at)
		VALUES (?, ?, ?, ?)`,
		session.Token, session.Username, session.CreatedAt, session.ExpiresAt,
	)
	return err
}

func (s *AuthStore) GetSession(ctx context.Context, token string) (auth.Session, error) {
	var sess auth.Session
	err := s.b.queryRow(ctx,
		`SELECT token, username, created_at, expires_at FROM sessions WHERE token = ?`,
		token,
	).Scan(&sess.Token, &sess.Username, &sess.CreatedAt, &sess.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return auth.Session{}, resource.ErrNotFound
	}
	if err != nil {
		return auth.Session{}, err
	}
	if time.Now().After(sess.ExpiresAt) {
		_ = s.DeleteSession(ctx, token)
		return auth.Session{}, resource.ErrNotFound
	}
	return sess, nil
}

func (s *AuthStore) DeleteSession(ctx context.Context, token string) error {
	_, err := s.b.exec(ctx, `DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func (s *AuthStore) CreateAuditEvent(ctx context.Context, event auth.AuditEvent) error {
	detailsJSON := ""
	if len(event.Details) > 0 {
		b, _ := json.Marshal(event.Details)
		detailsJSON = string(b)
	}

	_, err := s.b.exec(ctx, `
		INSERT INTO audit_events (id, machine, action, actor, result, message, details, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.Machine,
		event.Action, event.Actor, event.Result,
		event.Message, detailsJSON, event.CreatedAt,
	)
	return err
}

func (s *AuthStore) ListAuditEvents(ctx context.Context, machineName string, limit int) ([]auth.AuditEvent, error) {
	query := `SELECT id, machine, action, actor, result, message, COALESCE(details, ''), created_at FROM audit_events`
	var args []any
	var conditions []string

	if machineName != "" {
		conditions = append(conditions, "LOWER(machine) = LOWER(?)")
		args = append(args, machineName)
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY created_at DESC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.b.query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []auth.AuditEvent
	for rows.Next() {
		var e auth.AuditEvent
		var detailsJSON string
		err := rows.Scan(
			&e.ID, &e.Machine,
			&e.Action, &e.Actor, &e.Result,
			&e.Message, &detailsJSON, &e.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		if detailsJSON != "" {
			_ = json.Unmarshal([]byte(detailsJSON), &e.Details)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
