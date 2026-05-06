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
	"github.com/sugaf1204/gomi/internal/setupadmin"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) Login(c echo.Context) error {
	var req loginRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	session, user, err := s.login(c.Request().Context(), strings.TrimSpace(req.Username), req.Password)
	if err != nil {
		return c.JSON(gohttp.StatusUnauthorized, jsonError("invalid credential"))
	}
	return c.JSON(gohttp.StatusOK, loginResponse{
		Token:   session.Token,
		Expires: session.ExpiresAt,
		User: authUserResponse{
			Username: user.Username,
			Role:     user.Role,
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
		return c.JSON(gohttp.StatusUnauthorized, jsonError("auth required"))
	}
	return c.JSON(gohttp.StatusOK, authUserResponse{
		Username: user.Username,
		Role:     user.Role,
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
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	username := strings.TrimSpace(req.Username)
	if username == "" || req.Password == "" {
		return c.JSON(gohttp.StatusBadRequest, jsonError("username/password required"))
	}
	role, err := validateUserRole(req.Role)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	if err := s.createUser(c.Request().Context(), username, req.Password, role); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusCreated, statusResponse{Status: "created"})
}

type setupStatusResponse struct {
	Required bool `json:"required"`
}

func (s *Server) SetupStatus(c echo.Context) error {
	required, err := s.setupRequired(c.Request().Context())
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusOK, setupStatusResponse{Required: required})
}

func (s *Server) SetupAdmin(c echo.Context) error {
	var req loginRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	err := setupadmin.CreateFirstAdmin(c.Request().Context(), s.authStore, req.Username, req.Password)
	if errors.Is(err, setupadmin.ErrAlreadyConfigured) {
		return c.JSON(gohttp.StatusConflict, jsonError("setup already completed"))
	}
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	return c.JSON(gohttp.StatusCreated, statusResponse{Status: "created"})
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
