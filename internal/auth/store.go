package auth

import "context"

type Store interface {
	UpsertUser(ctx context.Context, user User) error
	GetUser(ctx context.Context, username string) (User, error)
	CountUsers(ctx context.Context) (int, error)

	CreateSession(ctx context.Context, session Session) error
	GetSession(ctx context.Context, token string) (Session, error)
	DeleteSession(ctx context.Context, token string) error

	CreateAuditEvent(ctx context.Context, event AuditEvent) error
	ListAuditEvents(ctx context.Context, machine string, limit int) ([]AuditEvent, error)
}
