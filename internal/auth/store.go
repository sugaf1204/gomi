package auth

import (
	"context"
	"time"
)

type Store interface {
	UpsertUser(ctx context.Context, user User) error
	GetUser(ctx context.Context, username string) (User, error)
	CountUsers(ctx context.Context) (int, error)

	CreateSession(ctx context.Context, session Session) error
	GetSession(ctx context.Context, token string) (Session, error)
	DeleteSession(ctx context.Context, token string) error

	UpsertServiceAccount(ctx context.Context, account ServiceAccount) error
	GetServiceAccount(ctx context.Context, name string) (ServiceAccount, error)
	GetServiceAccountByTokenHash(ctx context.Context, tokenHash string) (ServiceAccount, error)
	ListServiceAccounts(ctx context.Context) ([]ServiceAccount, error)
	DeleteServiceAccount(ctx context.Context, name string) error
	MarkServiceAccountUsed(ctx context.Context, name string, tokenHash string, usedAt time.Time) error

	CreateAuditEvent(ctx context.Context, event AuditEvent) error
	ListAuditEvents(ctx context.Context, machine string, limit int) ([]AuditEvent, error)
	ListAuditEventsPage(ctx context.Context, machine string, offset, limit int) ([]AuditEvent, int, error)
}
