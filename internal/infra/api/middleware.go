package api

import (
	"context"
	"errors"
	gohttp "net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/resource"
)

func (s *Server) AuthMiddleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			authz := c.Request().Header.Get("Authorization")
			parts := strings.SplitN(authz, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				return c.JSON(gohttp.StatusUnauthorized, jsonError("missing bearer token"))
			}
			tokenValue := strings.TrimSpace(parts[1])

			// Try session-based authentication first.
			user, err := s.authenticate(c.Request().Context(), tokenValue)
			if err == nil {
				c.Set(httputil.UserContextKey, user)
				return next(c)
			}

			// Fallback to agent token authentication.
			if s.agentTokenStore != nil {
				agentToken, atErr := s.agentTokenStore.GetByToken(c.Request().Context(), tokenValue)
				if atErr == nil {
					c.Set(httputil.UserContextKey, auth.User{
						Username: "agent:" + agentToken.HypervisorName,
						Role:     auth.RoleViewer,
					})
					return next(c)
				}
			}

			return c.JSON(gohttp.StatusUnauthorized, jsonError("invalid session"))
		}
	}
}

func (s *Server) authenticate(ctx context.Context, token string) (auth.User, error) {
	session, err := s.authStore.GetSession(ctx, token)
	if err != nil {
		return auth.User{}, err
	}
	if time.Now().UTC().After(session.ExpiresAt) {
		_ = s.authStore.DeleteSession(ctx, token)
		return auth.User{}, resource.ErrNotFound
	}
	return s.authStore.GetUser(ctx, session.Username)
}

func (s *Server) login(ctx context.Context, username, password string) (auth.Session, auth.User, error) {
	user, err := s.authStore.GetUser(ctx, username)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return auth.Session{}, auth.User{}, errors.New("invalid credentials")
		}
		return auth.Session{}, auth.User{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return auth.Session{}, auth.User{}, errors.New("invalid credentials")
	}
	now := time.Now().UTC()
	session := auth.Session{
		Token:     uuid.NewString(),
		Username:  user.Username,
		CreatedAt: now,
		ExpiresAt: now.Add(s.authService.sessionTTL),
	}
	if err := s.authStore.CreateSession(ctx, session); err != nil {
		return auth.Session{}, auth.User{}, err
	}
	return session, user, nil
}

// RequireAdmin restricts access to admin users only.
func RequireAdmin() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user, ok := httputil.UserFromContext(c)
			if !ok {
				return c.JSON(gohttp.StatusUnauthorized, jsonError("not authenticated"))
			}
			if !user.Role.IsAdmin() {
				return c.JSON(gohttp.StatusForbidden, jsonError("admin access required"))
			}
			return next(c)
		}
	}
}

// RequireWriter restricts access to admin and operator users (excludes viewer).
func RequireWriter() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user, ok := httputil.UserFromContext(c)
			if !ok {
				return c.JSON(gohttp.StatusUnauthorized, jsonError("not authenticated"))
			}
			if !user.Role.CanWrite() {
				return c.JSON(gohttp.StatusForbidden, jsonError("write access required"))
			}
			return next(c)
		}
	}
}
