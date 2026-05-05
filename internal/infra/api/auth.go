package api

import (
	"context"
	"errors"
	gohttp "net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) Login(c echo.Context) error {
	var req loginRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	session, user, err := s.login(c.Request().Context(), strings.TrimSpace(req.Username), req.Password)
	if err != nil {
		return c.JSON(gohttp.StatusUnauthorized, map[string]string{"error": "invalid credential"})
	}
	return c.JSON(gohttp.StatusOK, map[string]any{
		"token":   session.Token,
		"expires": session.ExpiresAt,
		"user": map[string]any{
			"username": user.Username,
			"role":     user.Role,
		},
	})
}

func (s *Server) Logout(c echo.Context) error {
	authz := c.Request().Header.Get("Authorization")
	parts := strings.SplitN(authz, " ", 2)
	if len(parts) != 2 {
		return c.NoContent(gohttp.StatusNoContent)
	}
	_ = s.authStore.DeleteSession(c.Request().Context(), strings.TrimSpace(parts[1]))
	return c.NoContent(gohttp.StatusNoContent)
}

func (s *Server) Me(c echo.Context) error {
	user, ok := httputil.UserFromContext(c)
	if !ok {
		return c.JSON(gohttp.StatusUnauthorized, map[string]string{"error": "auth required"})
	}
	return c.JSON(gohttp.StatusOK, map[string]any{
		"username": user.Username,
		"role":     user.Role,
	})
}

type createUserRequest struct {
	Username string    `json:"username"`
	Password string    `json:"password"`
	Role     auth.Role `json:"role"`
}

func (s *Server) CreateUser(c echo.Context) error {
	var req createUserRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "invalid body"})
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": "username/password required"})
	}
	role, err := validateUserRole(req.Role)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if err := s.createUser(c.Request().Context(), username, req.Password, role); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(gohttp.StatusCreated, map[string]string{"status": "created"})
}

type setupStatusResponse struct {
	Required bool `json:"required"`
}

func (s *Server) SetupStatus(c echo.Context) error {
	required, err := s.setupRequired(c.Request().Context())
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	return c.JSON(gohttp.StatusOK, setupStatusResponse{Required: required})
}

func (s *Server) SetupAdmin(c echo.Context) error {
	return c.JSON(gohttp.StatusForbidden, map[string]string{"error": "create the first admin with `gomi setup admin` on the server"})
}

func (s *Server) setupRequired(ctx context.Context) (bool, error) {
	count, err := s.authStore.CountUsers(ctx)
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

func validateUserRole(role auth.Role) (auth.Role, error) {
	switch role {
	case auth.RoleAdmin, auth.RoleOperator, auth.RoleViewer:
		return role, nil
	default:
		return "", errors.New("role must be admin, operator, or viewer")
	}
}

func (s *Server) createUser(ctx context.Context, username, password string, role auth.Role) error {
	username = strings.TrimSpace(username)
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.authStore.UpsertUser(ctx, auth.User{
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
		CreatedAt:    time.Now().UTC(),
	})
}
