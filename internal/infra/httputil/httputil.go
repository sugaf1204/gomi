package httputil

import (
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/resource"
)

const (
	UserContextKey       = "gomi.user"
	AuthMethodContextKey = "gomi.auth_method"
)

const (
	AuthMethodSession = "session"
	AuthMethodAgent   = "agent"
)

func UserFromContext(c echo.Context) (auth.User, bool) {
	v := c.Get(UserContextKey)
	if v == nil {
		return auth.User{}, false
	}
	user, ok := v.(auth.User)
	return user, ok
}

func AuthMethodFromContext(c echo.Context) string {
	v, _ := c.Get(AuthMethodContextKey).(string)
	return v
}

func TimePtr(v time.Time) *time.Time {
	copied := v
	return &copied
}

func GenerateProvisioningToken() (string, error) {
	return resource.GenerateProvisioningToken()
}

func CreateAudit(c echo.Context, authStore auth.Store, machineName, action, result, msg string, details map[string]string) {
	user, ok := UserFromContext(c)
	actor := "anonymous"
	if ok {
		actor = user.Username
	}
	_ = authStore.CreateAuditEvent(c.Request().Context(), auth.AuditEvent{
		ID:        uuid.NewString(),
		Machine:   machineName,
		Action:    action,
		Actor:     actor,
		Result:    result,
		Message:   msg,
		Details:   details,
		CreatedAt: time.Now().UTC(),
	})
}
