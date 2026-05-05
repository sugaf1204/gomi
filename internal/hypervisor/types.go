package hypervisor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

type Phase string

const (
	PhasePending     Phase = "Pending"
	PhaseRegistered  Phase = "Registered"
	PhaseReady       Phase = "Ready"
	PhaseNotReady    Phase = "NotReady"
	PhaseUnreachable Phase = "Unreachable"
)

type ConnectionType string

const (
	ConnectionSSH ConnectionType = "ssh"
	ConnectionTCP ConnectionType = "tcp"
)

type ConnectionSpec struct {
	Type    ConnectionType `json:"type"`
	Host    string         `json:"host"`
	Port    int            `json:"port,omitempty"`
	User    string         `json:"user,omitempty"`
	KeyRef  string         `json:"keyRef,omitempty"`
	HostKey string         `json:"hostKey,omitempty"`
}

type ResourceInfo struct {
	CPUCores  int   `json:"cpuCores"`
	MemoryMB  int64 `json:"memoryMB"`
	StorageGB int64 `json:"storageGB,omitempty"`
}

type ResourceUsage struct {
	CPUUsedCores  int   `json:"cpuUsedCores"`
	MemoryUsedMB  int64 `json:"memoryUsedMB"`
	StorageUsedGB int64 `json:"storageUsedGB,omitempty"`
}

type Hypervisor struct {
	Name string `json:"name"`

	// Spec fields
	Connection ConnectionSpec    `json:"connection"`
	Labels     map[string]string `json:"labels,omitempty"`
	MachineRef string            `json:"machineRef,omitempty"`
	BridgeName string            `json:"bridgeName,omitempty"`

	// Status fields
	Phase         Phase          `json:"phase"`
	Capacity      *ResourceInfo  `json:"capacity,omitempty"`
	Used          *ResourceUsage `json:"used,omitempty"`
	VMCount       int            `json:"vmCount"`
	LibvirtURI    string         `json:"libvirtURI,omitempty"`
	LastHeartbeat *time.Time     `json:"lastHeartbeat,omitempty"`
	LastError     string         `json:"lastError,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// RegistrationToken is a one-time-use token for hypervisor self-registration.
type RegistrationToken struct {
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	Used      bool      `json:"used"`
	UsedBy    string    `json:"usedBy,omitempty"`
}

// AgentToken is a long-lived token used by the hypervisor agent for API authentication.
type AgentToken struct {
	Token          string    `json:"token"`
	HypervisorName string    `json:"hypervisorName"`
	CreatedAt      time.Time `json:"createdAt"`
}

// RegisterResponse wraps the created Hypervisor and includes an agent token.
type RegisterResponse struct {
	Hypervisor Hypervisor `json:"hypervisor"`
	AgentToken string     `json:"agentToken"`
}

// RegisterRequest is the payload sent by a hypervisor during self-registration.
type RegisterRequest struct {
	Token      string         `json:"token"`
	Hostname   string         `json:"hostname"`
	Connection ConnectionSpec `json:"connection"`
	Capacity   ResourceInfo   `json:"capacity"`
}

type Store interface {
	Upsert(ctx context.Context, h Hypervisor) error
	Get(ctx context.Context, name string) (Hypervisor, error)
	List(ctx context.Context) ([]Hypervisor, error)
	Delete(ctx context.Context, name string) error
}

type TokenStore interface {
	Create(ctx context.Context, token RegistrationToken) error
	Get(ctx context.Context, tokenValue string) (RegistrationToken, error)
	// MarkUsed atomically validates a token (exists, unused, unexpired) and marks
	// it as used. It returns the token data on success or an error if the token
	// is missing, already used, or expired.
	MarkUsed(ctx context.Context, tokenValue, usedBy string) (RegistrationToken, error)
	List(ctx context.Context) ([]RegistrationToken, error)
}

// AgentTokenStore manages long-lived agent tokens for hypervisor API authentication.
type AgentTokenStore interface {
	Create(ctx context.Context, token AgentToken) error
	GetByToken(ctx context.Context, tokenValue string) (AgentToken, error)
	DeleteByHypervisor(ctx context.Context, hypervisorName string) error
}

var (
	ErrInvalidName = errors.New("name is required")
	ErrInvalidHost = errors.New("connection.host is required")
)

func ValidateHypervisor(h Hypervisor) error {
	if strings.TrimSpace(h.Name) == "" {
		return ErrInvalidName
	}
	if strings.TrimSpace(h.Connection.Host) == "" {
		return ErrInvalidHost
	}
	if h.Connection.Type != "" && h.Connection.Type != ConnectionSSH && h.Connection.Type != ConnectionTCP {
		return fmt.Errorf("unsupported connection type: %s", h.Connection.Type)
	}
	if host := h.Connection.Host; host != "" {
		if net.ParseIP(host) == nil {
			if _, err := net.LookupHost(host); err != nil {
				// Accept hostnames that may resolve later; only reject obviously invalid.
				if strings.ContainsAny(host, " \t\n") {
					return fmt.Errorf("invalid host: %s", host)
				}
			}
		}
	}
	return nil
}

func ValidateRegisterRequest(req RegisterRequest) error {
	if strings.TrimSpace(req.Token) == "" {
		return errors.New("token is required")
	}
	if strings.TrimSpace(req.Hostname) == "" {
		return errors.New("hostname is required")
	}
	if strings.TrimSpace(req.Connection.Host) == "" {
		return errors.New("connection.host is required")
	}
	if req.Capacity.CPUCores <= 0 {
		return errors.New("capacity.cpuCores must be positive")
	}
	if req.Capacity.MemoryMB <= 0 {
		return errors.New("capacity.memoryMB must be positive")
	}
	return nil
}
