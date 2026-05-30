package memory

import (
	"context"
	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/resource"
	"sort"
	"strings"
	"time"
)

type AuthStore struct{ b *Backend }

var _ auth.Store = (*AuthStore)(nil)

func (s *AuthStore) UpsertUser(_ context.Context, user auth.User) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.users[user.Username] = user
	return nil
}

func (s *AuthStore) GetUser(_ context.Context, username string) (auth.User, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	u, ok := s.b.users[username]
	if !ok {
		return auth.User{}, resource.ErrNotFound
	}
	return u, nil
}

func (s *AuthStore) CountUsers(_ context.Context) (int, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	return len(s.b.users), nil
}

func (s *AuthStore) CreateSession(_ context.Context, session auth.Session) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.sessions[session.Token] = session
	return nil
}

func (s *AuthStore) GetSession(_ context.Context, token string) (auth.Session, error) {
	s.b.mu.RLock()
	sess, ok := s.b.sessions[token]
	s.b.mu.RUnlock()
	if !ok {
		return auth.Session{}, resource.ErrNotFound
	}
	if time.Now().After(sess.ExpiresAt) {
		s.b.mu.Lock()
		delete(s.b.sessions, token)
		s.b.mu.Unlock()
		return auth.Session{}, resource.ErrNotFound
	}
	return sess, nil
}

func (s *AuthStore) DeleteSession(_ context.Context, token string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	delete(s.b.sessions, token)
	return nil
}

func (s *AuthStore) CreateAuditEvent(_ context.Context, event auth.AuditEvent) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.auditEvents[event.ID] = event
	return nil
}

func (s *AuthStore) ListAuditEvents(_ context.Context, machineName string, limit int) ([]auth.AuditEvent, error) {
	out, _, err := s.listAuditEvents(machineName, 0, limit)
	return out, err
}

func (s *AuthStore) ListAuditEventsPage(_ context.Context, machineName string, offset, limit int) ([]auth.AuditEvent, int, error) {
	return s.listAuditEvents(machineName, offset, limit)
}

func (s *AuthStore) listAuditEvents(machineName string, offset, limit int) ([]auth.AuditEvent, int, error) {
	s.b.mu.RLock()
	defer s.b.mu.RUnlock()
	out := make([]auth.AuditEvent, 0)
	for _, event := range s.b.auditEvents {
		if machineName != "" && !strings.EqualFold(event.Machine, machineName) {
			continue
		}
		out = append(out, event)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	total := len(out)
	if offset > total {
		return []auth.AuditEvent{}, total, nil
	}
	out = out[offset:]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

// --- SSHKeyStore ---
