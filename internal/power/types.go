package power

type Action string

const (
	ActionPowerOn  Action = "power-on"
	ActionPowerOff Action = "power-off"
)

type ActionResult struct {
	RequestID string `json:"requestID,omitempty"`
}

type PowerType string

const (
	PowerTypeIPMI    PowerType = "ipmi"
	PowerTypeWebhook PowerType = "webhook"
	PowerTypeWoL     PowerType = "wol"
	PowerTypeManual  PowerType = "manual"
)

type PowerState string

const (
	PowerStateUnknown PowerState = "unknown"
	PowerStateRunning PowerState = "running"
	PowerStateStopped PowerState = "stopped"
)

type IPMIConfig struct {
	Host      string `json:"host"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Interface string `json:"interface,omitempty"`
}

type WebhookConfig struct {
	PowerOnURL   string            `json:"powerOnURL"`
	PowerOffURL  string            `json:"powerOffURL"`
	StatusURL    string            `json:"statusURL,omitempty"`
	BootOrderURL string            `json:"bootOrderURL,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	BodyExtras   map[string]string `json:"bodyExtras,omitempty"`
}

type WoLConfig struct {
	WakeMAC         string `json:"wakeMAC"`
	BroadcastIP     string `json:"broadcastIP"`
	Port            int    `json:"port"`
	ShutdownTarget  string `json:"shutdownTarget"`
	ShutdownUDPPort int    `json:"shutdownUDPPort"`
	HMACSecret      string `json:"hmacSecret"`
	Token           string `json:"token"`
	TokenTTLSeconds int    `json:"tokenTTLSeconds"`
}

const (
	DefaultWoLShutdownUDPPort    = 40000
	DefaultWoLShutdownTTLSeconds = 60
)

type ManualConfig struct{}

type BootDevice string

const (
	BootDevicePXEIPv4 BootDevice = "pxe-ipv4"
	BootDeviceDisk    BootDevice = "disk"
	BootDevicePXEIPv6 BootDevice = "pxe-ipv6"
)

type BootOrder []BootDevice

var DefaultBIOSBootOrder = BootOrder{
	BootDevicePXEIPv4,
	BootDeviceDisk,
}

// PowerConfig is embedded directly in Machine.Spec (replaces the old PolicySpec/PowerPolicy CRD).
type PowerConfig struct {
	Type    PowerType      `json:"type"`
	IPMI    *IPMIConfig    `json:"ipmi,omitempty"`
	Webhook *WebhookConfig `json:"webhook,omitempty"`
	WoL     *WoLConfig     `json:"wol,omitempty"`
	Manual  *ManualConfig  `json:"manual,omitempty"`
}

// MachineInfo carries the minimal machine data needed by the power executor.
// This avoids a circular import between machine and power packages.
type MachineInfo struct {
	Name     string
	Hostname string
	MAC      string
	IP       string
	Power    PowerConfig
}
