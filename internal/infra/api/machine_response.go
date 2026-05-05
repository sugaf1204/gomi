package api

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/resource"
)

type MachineResponse struct {
	Name string `json:"name"`

	Hostname      string                    `json:"hostname"`
	MAC           string                    `json:"mac"`
	IP            string                    `json:"ip,omitempty"`
	Arch          string                    `json:"arch"`
	Firmware      machine.Firmware          `json:"firmware"`
	Power         PowerConfigResponse       `json:"power"`
	Network       machine.NetworkConfig     `json:"network"`
	OSPreset      machine.OSPreset          `json:"osPreset"`
	TargetDisk    string                    `json:"targetDisk,omitempty"`
	CloudInitRef  string                    `json:"cloudInitRef,omitempty"`
	CloudInitRefs []string                  `json:"cloudInitRefs,omitempty"`
	IPAssignment  resource.IPAssignmentMode `json:"ipAssignment,omitempty"`
	SubnetRef     string                    `json:"subnetRef,omitempty"`
	Role          machine.Role              `json:"role,omitempty"`
	BridgeName    string                    `json:"bridgeName,omitempty"`

	Phase                    machine.Phase              `json:"phase"`
	Provision                *ProvisionProgressResponse `json:"provision,omitempty"`
	LastPowerAction          string                     `json:"lastPowerAction,omitempty"`
	LastDeployedCloudInitRef string                     `json:"lastDeployedCloudInitRef,omitempty"`
	LastError                string                     `json:"lastError,omitempty"`
	PowerState               power.PowerState           `json:"powerState,omitempty"`
	PowerStateAt             *time.Time                 `json:"powerStateAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ProvisionProgressResponse struct {
	Active           bool              `json:"active,omitempty"`
	AttemptID        string            `json:"attemptId,omitempty"`
	InventoryID      string            `json:"inventoryId,omitempty"`
	StartedAt        *time.Time        `json:"startedAt,omitempty"`
	DeadlineAt       *time.Time        `json:"deadlineAt,omitempty"`
	FinishedAt       *time.Time        `json:"finishedAt,omitempty"`
	CompletedAt      *time.Time        `json:"completedAt,omitempty"`
	Trigger          string            `json:"trigger,omitempty"`
	RequestedBy      string            `json:"requestedBy,omitempty"`
	Message          string            `json:"message,omitempty"`
	Artifacts        map[string]string `json:"artifacts,omitempty"`
	CompletionSource string            `json:"completionSource,omitempty"`
	LastSignalAt     *time.Time        `json:"lastSignalAt,omitempty"`
	CurtinConfig     json.RawMessage   `json:"curtinConfig,omitempty"`
	FailureReason    string            `json:"failureReason,omitempty"`
	LogURL           string            `json:"logUrl,omitempty"`
}

type PowerConfigResponse struct {
	Type    power.PowerType        `json:"type"`
	IPMI    *IPMIConfigResponse    `json:"ipmi,omitempty"`
	Webhook *WebhookConfigResponse `json:"webhook,omitempty"`
	WoL     *WoLConfigResponse     `json:"wol,omitempty"`
	Manual  *power.ManualConfig    `json:"manual,omitempty"`
}

type IPMIConfigResponse struct {
	Host               string `json:"host"`
	Username           string `json:"username"`
	Interface          string `json:"interface,omitempty"`
	PasswordConfigured bool   `json:"passwordConfigured"`
}

type WebhookConfigResponse struct {
	PowerOnURL           string `json:"powerOnURL"`
	PowerOffURL          string `json:"powerOffURL"`
	StatusURL            string `json:"statusURL,omitempty"`
	BootOrderURL         string `json:"bootOrderURL,omitempty"`
	HeadersConfigured    bool   `json:"headersConfigured"`
	BodyExtrasConfigured bool   `json:"bodyExtrasConfigured"`
}

type WoLConfigResponse struct {
	WakeMAC              string `json:"wakeMAC"`
	BroadcastIP          string `json:"broadcastIP"`
	Port                 int    `json:"port"`
	ShutdownTarget       string `json:"shutdownTarget"`
	ShutdownUDPPort      int    `json:"shutdownUDPPort"`
	TokenTTLSeconds      int    `json:"tokenTTLSeconds"`
	HMACSecretConfigured bool   `json:"hmacSecretConfigured"`
	TokenConfigured      bool   `json:"tokenConfigured"`
}

func machineResponses(machines []machine.Machine) []MachineResponse {
	out := make([]MachineResponse, 0, len(machines))
	for _, m := range machines {
		out = append(out, machineResponse(m))
	}
	return out
}

func machineResponse(m machine.Machine) MachineResponse {
	return MachineResponse{
		Name:                     m.Name,
		Hostname:                 m.Hostname,
		MAC:                      m.MAC,
		IP:                       m.IP,
		Arch:                     m.Arch,
		Firmware:                 m.Firmware,
		Power:                    powerConfigResponse(m.Power),
		Network:                  m.Network,
		OSPreset:                 m.OSPreset,
		TargetDisk:               m.TargetDisk,
		CloudInitRef:             m.CloudInitRef,
		CloudInitRefs:            m.CloudInitRefs,
		IPAssignment:             m.IPAssignment,
		SubnetRef:                m.SubnetRef,
		Role:                     m.Role,
		BridgeName:               m.BridgeName,
		Phase:                    m.Phase,
		Provision:                provisionProgressResponse(m.Provision),
		LastPowerAction:          m.LastPowerAction,
		LastDeployedCloudInitRef: m.LastDeployedCloudInitRef,
		LastError:                m.LastError,
		PowerState:               m.PowerState,
		PowerStateAt:             m.PowerStateAt,
		CreatedAt:                m.CreatedAt,
		UpdatedAt:                m.UpdatedAt,
	}
}

func provisionProgressResponse(p *machine.ProvisionProgress) *ProvisionProgressResponse {
	if p == nil {
		return nil
	}
	return &ProvisionProgressResponse{
		Active:           p.Active,
		AttemptID:        p.AttemptID,
		InventoryID:      p.InventoryID,
		StartedAt:        p.StartedAt,
		DeadlineAt:       p.DeadlineAt,
		FinishedAt:       p.FinishedAt,
		CompletedAt:      p.CompletedAt,
		Trigger:          p.Trigger,
		RequestedBy:      p.RequestedBy,
		Message:          p.Message,
		Artifacts:        provisionArtifactsResponse(p.Artifacts),
		CompletionSource: p.CompletionSource,
		LastSignalAt:     p.LastSignalAt,
		CurtinConfig:     p.CurtinConfig,
		FailureReason:    p.FailureReason,
		LogURL:           p.LogURL,
	}
}

func provisionArtifactsResponse(artifacts map[string]string) map[string]string {
	if len(artifacts) == 0 {
		return nil
	}
	out := make(map[string]string, len(artifacts))
	for k, v := range artifacts {
		switch k {
		case machine.ProvisionArtifactHypervisorRegistrationToken,
			machine.ProvisionArtifactHypervisorRegistrationTokenExpiresAt:
			continue
		default:
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func powerConfigResponse(cfg power.PowerConfig) PowerConfigResponse {
	resp := PowerConfigResponse{Type: cfg.Type}
	if cfg.IPMI != nil {
		resp.IPMI = &IPMIConfigResponse{
			Host:               cfg.IPMI.Host,
			Username:           cfg.IPMI.Username,
			Interface:          cfg.IPMI.Interface,
			PasswordConfigured: strings.TrimSpace(cfg.IPMI.Password) != "",
		}
	}
	if cfg.Webhook != nil {
		resp.Webhook = &WebhookConfigResponse{
			PowerOnURL:           cfg.Webhook.PowerOnURL,
			PowerOffURL:          cfg.Webhook.PowerOffURL,
			StatusURL:            cfg.Webhook.StatusURL,
			BootOrderURL:         cfg.Webhook.BootOrderURL,
			HeadersConfigured:    len(cfg.Webhook.Headers) > 0,
			BodyExtrasConfigured: len(cfg.Webhook.BodyExtras) > 0,
		}
	}
	if cfg.WoL != nil {
		resp.WoL = &WoLConfigResponse{
			WakeMAC:              cfg.WoL.WakeMAC,
			BroadcastIP:          cfg.WoL.BroadcastIP,
			Port:                 cfg.WoL.Port,
			ShutdownTarget:       cfg.WoL.ShutdownTarget,
			ShutdownUDPPort:      cfg.WoL.ShutdownUDPPort,
			TokenTTLSeconds:      cfg.WoL.TokenTTLSeconds,
			HMACSecretConfigured: strings.TrimSpace(cfg.WoL.HMACSecret) != "",
			TokenConfigured:      strings.TrimSpace(cfg.WoL.Token) != "",
		}
	}
	if cfg.Manual != nil {
		resp.Manual = cfg.Manual
	}
	return resp
}
