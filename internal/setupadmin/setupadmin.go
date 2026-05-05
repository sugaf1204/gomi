package setupadmin

import (
	"context"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/sugaf1204/gomi/internal/auth"
)

var ErrAlreadyConfigured = errors.New("setup already completed")

func CreateFirstAdmin(ctx context.Context, store auth.Store, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return errors.New("username/password required")
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}

	count, err := store.CountUsers(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrAlreadyConfigured
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return store.UpsertUser(ctx, auth.User{
		Username:     username,
		PasswordHash: string(hash),
		Role:         auth.RoleAdmin,
		CreatedAt:    time.Now().UTC(),
	})
}
