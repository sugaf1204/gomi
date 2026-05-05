package machine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
)

type Service struct {
	store            Store
	provisionTimeout time.Duration
}

func NewService(store Store, opts ...ServiceOption) *Service {
	s := &Service{store: store, provisionTimeout: 30 * time.Minute}
	for _, o := range opts {
		o(s)
	}
	return s
}

type ServiceOption func(*Service)

func WithProvisionTimeout(d time.Duration) ServiceOption {
	return func(s *Service) {
		if d > 0 {
			s.provisionTimeout = d
		}
	}
}

func (s *Service) Create(ctx context.Context, m Machine) (Machine, error) {
	now := time.Now().UTC()
	m.CreatedAt = now
	m.UpdatedAt = now
	if m.Phase == "" {
		m.Phase = PhaseReady
	}

	normalizeMachineCloudInitRefs(&m)
	if err := power.FillWoLDefaults(&m.Power); err != nil {
		return Machine{}, err
	}
	if err := ValidateMachine(m); err != nil {
		return Machine{}, err
	}
	m.Arch = CanonicalArch(m.Arch)
	if len(m.CloudInitRefs) > 0 {
		m.LastDeployedCloudInitRef = m.CloudInitRefs[0]
	}
	if err := s.store.Upsert(ctx, m); err != nil {
		return Machine{}, err
	}
	return m, nil
}

func (s *Service) Get(ctx context.Context, name string) (Machine, error) {
	return s.store.Get(ctx, name)
}

func (s *Service) List(ctx context.Context) ([]Machine, error) {
	return s.store.List(ctx)
}

func (s *Service) UpdateSettings(ctx context.Context, name string, powerCfg power.PowerConfig) (Machine, error) {
	m, err := s.store.Get(ctx, name)
	if err != nil {
		return Machine{}, err
	}
	if err := power.FillWoLDefaults(&powerCfg); err != nil {
		return Machine{}, err
	}
	m.Power = powerCfg
	now := time.Now().UTC()
	m.UpdatedAt = now
	if err := s.store.Upsert(ctx, m); err != nil {
		return Machine{}, err
	}
	return m, nil
}

func (s *Service) UpdateNetwork(ctx context.Context, name string, ip string, ipAssignment IPAssignmentMode, subnetRef string, network NetworkConfig) (Machine, error) {
	m, err := s.store.Get(ctx, name)
	if err != nil {
		return Machine{}, err
	}
	// IPAssignment cannot be changed by UpdateNetwork; use redeploy instead.
	if ipAssignment != "" && ipAssignment != m.IPAssignment {
		return Machine{}, fmt.Errorf("ipAssignment cannot be changed via network update; use redeploy instead")
	}
	m.IP = ip
	m.SubnetRef = subnetRef
	m.Network = network
	now := time.Now().UTC()
	m.UpdatedAt = now
	if err := s.store.Upsert(ctx, m); err != nil {
		return Machine{}, err
	}
	return m, nil
}

// ReinstallOptions allows overriding IPAssignment and IP during redeploy.
type ReinstallOptions struct {
	Hostname      *string
	MAC           *string
	Arch          *string
	Firmware      *Firmware
	Power         *power.PowerConfig
	OSPreset      *OSPreset
	TargetDisk    *string
	Network       *NetworkConfig
	CloudInitRef  *string
	CloudInitRefs *[]string
	IPAssignment  *IPAssignmentMode
	IP            *string
	SubnetRef     *string
	Role          *Role
	BridgeName    *string
	SSHKeyRefs    *[]string
	LoginUser     *LoginUserSpec
}

func (s *Service) Reinstall(ctx context.Context, name, actor string, opts *ReinstallOptions) (Machine, error) {
	m, err := s.store.Get(ctx, name)
	if err != nil {
		return Machine{}, err
	}
	if opts != nil {
		if opts.Hostname != nil {
			m.Hostname = strings.TrimSpace(*opts.Hostname)
		}
		if opts.MAC != nil {
			m.MAC = strings.TrimSpace(*opts.MAC)
		}
		if opts.Arch != nil {
			m.Arch = CanonicalArch(*opts.Arch)
		}
		if opts.Firmware != nil {
			m.Firmware = *opts.Firmware
		}
		if opts.Power != nil {
			m.Power = *opts.Power
		}
		if opts.OSPreset != nil {
			m.OSPreset = *opts.OSPreset
		}
		if opts.TargetDisk != nil {
			m.TargetDisk = strings.TrimSpace(*opts.TargetDisk)
		}
		if opts.Network != nil {
			m.Network = *opts.Network
		}
		if opts.SubnetRef != nil {
			m.SubnetRef = strings.TrimSpace(*opts.SubnetRef)
		}
		if opts.IPAssignment != nil {
			m.IPAssignment = *opts.IPAssignment
			if *opts.IPAssignment != IPAssignmentModeStatic && opts.IP == nil {
				m.IP = ""
			}
		}
		if opts.IP != nil {
			m.IP = strings.TrimSpace(*opts.IP)
		}
		if opts.CloudInitRef != nil || opts.CloudInitRefs != nil {
			legacy := ""
			if opts.CloudInitRef != nil {
				legacy = *opts.CloudInitRef
			}
			var refs []string
			if opts.CloudInitRefs != nil {
				refs = *opts.CloudInitRefs
			}
			m.CloudInitRef = ""
			m.CloudInitRefs = resource.NormalizeCloudInitRefs(legacy, refs)
			if len(m.CloudInitRefs) > 0 {
				m.LastDeployedCloudInitRef = m.CloudInitRefs[0]
			} else {
				m.LastDeployedCloudInitRef = ""
			}
		}
		if opts.Role != nil {
			m.Role = *opts.Role
		}
		if opts.BridgeName != nil {
			m.BridgeName = strings.TrimSpace(*opts.BridgeName)
		}
		if opts.SSHKeyRefs != nil {
			m.SSHKeyRefs = normalizeSSHKeyRefs(*opts.SSHKeyRefs)
		}
		if opts.LoginUser != nil {
			m.LoginUser = opts.LoginUser
		}
	}
	if err := power.FillWoLDefaults(&m.Power); err != nil {
		return Machine{}, err
	}
	if err := ValidateMachine(m); err != nil {
		return Machine{}, err
	}
	m.Arch = CanonicalArch(m.Arch)
	if m.LastDeployedCloudInitRef == "" {
		if ref := resource.ResolveCloudInitRef("", m.CloudInitRef, m.CloudInitRefs); ref != "" {
			m.LastDeployedCloudInitRef = ref
		}
	}
	token, err := resource.GenerateProvisioningToken()
	if err != nil {
		return Machine{}, err
	}
	attemptID, err := resource.GenerateProvisioningAttemptID()
	if err != nil {
		return Machine{}, err
	}
	now := time.Now().UTC()
	deadline := now.Add(s.provisionTimeout)
	m.Phase = PhaseProvisioning
	m.LastError = ""
	m.Provision = &ProvisionProgress{
		Active:          true,
		AttemptID:       attemptID,
		StartedAt:       &now,
		DeadlineAt:      &deadline,
		Trigger:         "redeploy",
		RequestedBy:     actor,
		Message:         "provisioning started",
		CompletionToken: token,
	}
	m.UpdatedAt = now
	if err := s.store.Upsert(ctx, m); err != nil {
		return Machine{}, err
	}
	return m, nil
}

func (s *Service) Delete(ctx context.Context, name string) error {
	return s.store.Delete(ctx, name)
}

func (s *Service) GetByMAC(ctx context.Context, mac string) (Machine, error) {
	return s.store.GetByMAC(ctx, mac)
}

func (s *Service) Store() Store {
	return s.store
}

func normalizeMachineCloudInitRefs(m *Machine) {
	m.CloudInitRefs = resource.NormalizeCloudInitRefs(m.CloudInitRef, m.CloudInitRefs)
	m.CloudInitRef = ""
}

func normalizeSSHKeyRefs(refs []string) []string {
	out := make([]string, 0, len(refs))
	seen := make(map[string]struct{}, len(refs))
	for _, raw := range refs {
		ref := strings.TrimSpace(raw)
		if ref == "" {
			continue
		}
		if _, ok := seen[ref]; ok {
			continue
		}
		seen[ref] = struct{}{}
		out = append(out, ref)
	}
	return out
}
