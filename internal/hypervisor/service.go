package hypervisor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

const (
	defaultTokenTTL = 1 * time.Hour
)

type Service struct {
	store           Store
	tokenStore      TokenStore
	agentTokenStore AgentTokenStore
}

func NewService(store Store, tokenStore TokenStore, agentTokenStore AgentTokenStore) *Service {
	return &Service{store: store, tokenStore: tokenStore, agentTokenStore: agentTokenStore}
}

func (s *Service) Create(ctx context.Context, h Hypervisor) (Hypervisor, error) {
	now := time.Now().UTC()
	h.CreatedAt = now
	h.UpdatedAt = now
	if h.Phase == "" {
		h.Phase = PhasePending
	}
	if h.Connection.Type == "" {
		h.Connection.Type = ConnectionTCP
	}

	if err := ValidateHypervisor(h); err != nil {
		return Hypervisor{}, err
	}
	if err := s.store.Upsert(ctx, h); err != nil {
		return Hypervisor{}, err
	}
	return h, nil
}

func (s *Service) Get(ctx context.Context, name string) (Hypervisor, error) {
	return s.store.Get(ctx, name)
}

func (s *Service) List(ctx context.Context) ([]Hypervisor, error) {
	return s.store.List(ctx)
}

func (s *Service) Delete(ctx context.Context, name string) error {
	if err := s.store.Delete(ctx, name); err != nil {
		return err
	}
	_ = s.agentTokenStore.DeleteByHypervisor(ctx, name)
	return nil
}

func (s *Service) UpdateStatus(ctx context.Context, name string, phase Phase, capacity *ResourceInfo, used *ResourceUsage, libvirtURI, lastErr string) (Hypervisor, error) {
	h, err := s.store.Get(ctx, name)
	if err != nil {
		return Hypervisor{}, err
	}
	h.Phase = phase
	if capacity != nil {
		h.Capacity = capacity
	}
	if used != nil {
		h.Used = used
	}
	if libvirtURI != "" {
		h.LibvirtURI = libvirtURI
	}
	h.LastError = lastErr
	now := time.Now().UTC()
	h.UpdatedAt = now
	if err := s.store.Upsert(ctx, h); err != nil {
		return Hypervisor{}, err
	}
	return h, nil
}

// CreateToken generates a single-use registration token.
func (s *Service) CreateToken(ctx context.Context) (RegistrationToken, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return RegistrationToken{}, err
	}
	now := time.Now().UTC()
	token := RegistrationToken{
		Token:     hex.EncodeToString(tokenBytes),
		CreatedAt: now,
		ExpiresAt: now.Add(defaultTokenTTL),
	}
	if err := s.tokenStore.Create(ctx, token); err != nil {
		return RegistrationToken{}, err
	}
	return token, nil
}

// Register processes a self-registration request from a hypervisor.
// It returns the created Hypervisor and a generated agent token string.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (Hypervisor, string, error) {
	if err := ValidateRegisterRequest(req); err != nil {
		return Hypervisor{}, "", err
	}

	// Atomic check-and-mark: validates token exists, is unused, and unexpired.
	_, err := s.tokenStore.MarkUsed(ctx, req.Token, req.Hostname)
	if err != nil {
		return Hypervisor{}, "", fmt.Errorf("token validation: %w", err)
	}

	now := time.Now().UTC()
	existing, existingErr := s.store.Get(ctx, req.Hostname)
	conn := req.Connection
	if conn.Type == "" {
		conn.Type = ConnectionTCP
	}
	h := Hypervisor{
		Name:       req.Hostname,
		Connection: conn,
		Phase:      PhaseRegistered,
		Capacity:   &req.Capacity,
		LibvirtURI: "qemu+tcp://" + req.Connection.Host + "/system",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if existingErr == nil {
		h.CreatedAt = existing.CreatedAt
		h.Labels = existing.Labels
		h.MachineRef = existing.MachineRef
		h.BridgeName = existing.BridgeName
	}

	if err := s.store.Upsert(ctx, h); err != nil {
		return Hypervisor{}, "", err
	}

	agentToken, err := s.CreateAgentToken(ctx, h.Name)
	if err != nil {
		return Hypervisor{}, "", fmt.Errorf("create agent token: %w", err)
	}

	return h, agentToken, nil
}

// CreateAgentToken generates a long-lived agent token for the given hypervisor.
// Any existing tokens for the hypervisor are deleted first.
func (s *Service) CreateAgentToken(ctx context.Context, hypervisorName string) (string, error) {
	// Verify hypervisor exists.
	if _, err := s.store.Get(ctx, hypervisorName); err != nil {
		return "", err
	}

	// Delete existing tokens for this hypervisor.
	_ = s.agentTokenStore.DeleteByHypervisor(ctx, hypervisorName)

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	tokenValue := hex.EncodeToString(tokenBytes)

	token := AgentToken{
		Token:          tokenValue,
		HypervisorName: hypervisorName,
		CreatedAt:      time.Now().UTC(),
	}
	if err := s.agentTokenStore.Create(ctx, token); err != nil {
		return "", err
	}
	return tokenValue, nil
}

// ValidateAgentToken checks a token value and returns the associated AgentToken.
func (s *Service) ValidateAgentToken(ctx context.Context, tokenValue string) (AgentToken, error) {
	return s.agentTokenStore.GetByToken(ctx, tokenValue)
}

func (s *Service) Store() Store {
	return s.store
}
