package auth

import "time"

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

// CanWrite returns true if the role has write access (admin or operator).
func (r Role) CanWrite() bool {
	return r == RoleAdmin || r == RoleOperator
}

// IsAdmin returns true if the role is admin.
func (r Role) IsAdmin() bool {
	return r == RoleAdmin
}

type User struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"passwordHash"`
	Role         Role      `json:"role"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Session struct {
	Token     string    `json:"token"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type AuditEvent struct {
	ID        string            `json:"id"`
	Machine   string            `json:"machine"`
	Action    string            `json:"action"`
	Actor     string            `json:"actor"`
	Result    string            `json:"result"`
	Message   string            `json:"message,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
	CreatedAt time.Time         `json:"createdAt"`
}
