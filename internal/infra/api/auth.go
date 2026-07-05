package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	gohttp "net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/infra/httputil"
	"github.com/sugaf1204/gomi/internal/resource"
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

type createServiceAccountRequest struct {
	Name string    `json:"name"`
	Role auth.Role `json:"role"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

type serviceAccountResponse struct {
	Name       string     `json:"name"`
	Role       auth.Role  `json:"role"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

type createServiceAccountResponse struct {
	ServiceAccount serviceAccountResponse `json:"serviceAccount"`
	Token          string                 `json:"token"`
}

type listServiceAccountsResponse struct {
	ServiceAccounts []serviceAccountResponse `json:"serviceAccounts"`
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

func (s *Server) ListServiceAccounts(c echo.Context) error {
	accounts, err := s.authStore.ListServiceAccounts(c.Request().Context())
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	items := make([]serviceAccountResponse, 0, len(accounts))
	for _, account := range accounts {
		items = append(items, serviceAccountToResponse(account))
	}
	return c.JSON(gohttp.StatusOK, listServiceAccountsResponse{ServiceAccounts: items})
}

func (s *Server) CreateServiceAccount(c echo.Context) error {
	var req createServiceAccountRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return c.JSON(gohttp.StatusBadRequest, jsonError("name is required"))
	}
	role, err := validateUserRole(req.Role)
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
	}
	token, err := generateServiceAccountToken()
	if err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	account := auth.ServiceAccount{
		Name:      name,
		TokenHash: serviceAccountTokenHash(token),
		Role:      role,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.authStore.UpsertServiceAccount(c.Request().Context(), account); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, "", "create-service-account", "success", "service account token issued", map[string]string{
		"name": name,
		"role": string(role),
	})
	return c.JSON(gohttp.StatusCreated, createServiceAccountResponse{
		ServiceAccount: serviceAccountToResponse(account),
		Token:          token,
	})
}

func (s *Server) DeleteServiceAccount(c echo.Context) error {
	name := strings.TrimSpace(c.Param("name"))
	if name == "" {
		return c.JSON(gohttp.StatusBadRequest, jsonError("name is required"))
	}
	if err := s.authStore.DeleteServiceAccount(c.Request().Context(), name); err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return c.JSON(gohttp.StatusNotFound, jsonError("service account not found"))
		}
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, "", "delete-service-account", "success", "service account deleted", map[string]string{
		"name": name,
	})
	return c.NoContent(gohttp.StatusNoContent)
}

func (s *Server) ChangeMyPassword(c echo.Context) error {
	user, ok := httputil.UserFromContext(c)
	if !ok {
		return c.JSON(gohttp.StatusUnauthorized, jsonError("auth required"))
	}
	if httputil.AuthMethodFromContext(c) != httputil.AuthMethodSession {
		return c.JSON(gohttp.StatusForbidden, jsonError("session user required"))
	}

	var req changePasswordRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid body"))
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		return c.JSON(gohttp.StatusBadRequest, jsonError("currentPassword/newPassword required"))
	}
	if len(req.NewPassword) < 8 {
		return c.JSON(gohttp.StatusBadRequest, jsonError("new password must be at least 8 characters"))
	}

	stored, err := s.authStore.GetUser(c.Request().Context(), user.Username)
	if err != nil {
		return c.JSON(gohttp.StatusUnauthorized, jsonError("invalid session user"))
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte(req.CurrentPassword)); err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonError("invalid current password"))
	}
	if err := s.createUser(c.Request().Context(), stored.Username, req.NewPassword, stored.Role); err != nil {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
	httputil.CreateAudit(c, s.authStore, "", "change-password", "success", "password changed", nil)
	return c.NoContent(gohttp.StatusNoContent)
}

func serviceAccountToResponse(account auth.ServiceAccount) serviceAccountResponse {
	return serviceAccountResponse{
		Name:       account.Name,
		Role:       account.Role,
		CreatedAt:  account.CreatedAt,
		LastUsedAt: account.LastUsedAt,
	}
}

func generateServiceAccountToken() (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	return "gomi_sa_" + hex.EncodeToString(tokenBytes), nil
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
