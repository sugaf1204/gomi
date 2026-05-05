package power

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	gohttp "net/http"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// Executor dispatches power actions based on machine's inline power config.
type Executor struct {
	httpClient *gohttp.Client
}

func NewExecutor() *Executor {
	return &Executor{httpClient: &gohttp.Client{Timeout: 10 * time.Second}}
}

func (e *Executor) ConfigureBootOrder(ctx context.Context, m MachineInfo, order BootOrder) error {
	cfg := m.Power
	switch cfg.Type {
	case PowerTypeWebhook:
		if cfg.Webhook == nil {
			return fmt.Errorf("webhook config missing")
		}
		return e.configureWebhookBootOrder(ctx, m, *cfg.Webhook, order)
	case PowerTypeIPMI, PowerTypeWoL, PowerTypeManual:
		return fmt.Errorf("power type %s does not support boot order automation", cfg.Type)
	default:
		return fmt.Errorf("unsupported power type %s", cfg.Type)
	}
}

func (e *Executor) Execute(ctx context.Context, m MachineInfo, action Action) error {
	_, err := e.ExecuteWithResult(ctx, m, action)
	return err
}

func (e *Executor) ExecuteWithResult(ctx context.Context, m MachineInfo, action Action) (ActionResult, error) {
	cfg := m.Power
	switch cfg.Type {
	case PowerTypeIPMI:
		if cfg.IPMI == nil {
			return ActionResult{}, fmt.Errorf("ipmi config missing")
		}
		return ActionResult{}, e.executeIPMI(ctx, *cfg.IPMI, action)
	case PowerTypeWebhook:
		if cfg.Webhook == nil {
			return ActionResult{}, fmt.Errorf("webhook config missing")
		}
		return ActionResult{}, e.executeWebhook(ctx, m, *cfg.Webhook, action)
	case PowerTypeWoL:
		if cfg.WoL == nil {
			return ActionResult{}, fmt.Errorf("wol config missing")
		}
		return e.executeWoL(ctx, m, *cfg.WoL, action)
	case PowerTypeManual:
		return ActionResult{}, fmt.Errorf("manual power type does not support automated %s", action)
	default:
		return ActionResult{}, fmt.Errorf("unsupported power type %s", cfg.Type)
	}
}

// CheckStatus probes the current power state of a machine.
func (e *Executor) CheckStatus(ctx context.Context, m MachineInfo) (PowerState, error) {
	cfg := m.Power
	switch cfg.Type {
	case PowerTypeIPMI:
		if cfg.IPMI == nil {
			return PowerStateUnknown, fmt.Errorf("ipmi config missing")
		}
		return e.checkIPMIStatus(ctx, *cfg.IPMI)
	case PowerTypeWebhook:
		if cfg.Webhook != nil && strings.TrimSpace(cfg.Webhook.StatusURL) != "" {
			return e.checkWebhookStatus(ctx, *cfg.Webhook)
		}
		return e.checkICMP(ctx, m.IP)
	case PowerTypeWoL, PowerTypeManual:
		return e.checkICMP(ctx, m.IP)
	default:
		return PowerStateUnknown, nil
	}
}

func (e *Executor) checkIPMIStatus(ctx context.Context, cfg IPMIConfig) (PowerState, error) {
	iface := cfg.Interface
	if strings.TrimSpace(iface) == "" {
		iface = "lanplus"
	}
	cmd := exec.CommandContext(ctx, "ipmitool", "-I", iface, "-H", cfg.Host, "-U", cfg.Username, "-P", cfg.Password, "chassis", "power", "status")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return PowerStateUnknown, fmt.Errorf("ipmitool status failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	out := strings.ToLower(strings.TrimSpace(string(output)))
	if strings.Contains(out, "is on") {
		return PowerStateRunning, nil
	}
	if strings.Contains(out, "is off") {
		return PowerStateStopped, nil
	}
	return PowerStateUnknown, nil
}

func (e *Executor) checkWebhookStatus(ctx context.Context, cfg WebhookConfig) (PowerState, error) {
	req, err := gohttp.NewRequestWithContext(ctx, gohttp.MethodGet, cfg.StatusURL, nil)
	if err != nil {
		return PowerStateUnknown, err
	}
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return PowerStateUnknown, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return PowerStateUnknown, err
	}
	var result struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return PowerStateUnknown, err
	}
	switch strings.ToLower(result.Status) {
	case "running":
		return PowerStateRunning, nil
	case "stopped":
		return PowerStateStopped, nil
	default:
		return PowerStateUnknown, nil
	}
}

func (e *Executor) checkICMP(ctx context.Context, ip string) (PowerState, error) {
	if strings.TrimSpace(ip) == "" {
		return PowerStateUnknown, nil
	}
	cmd := exec.CommandContext(ctx, "ping", "-c", "3", "-W", "2", ip)
	if err := cmd.Run(); err != nil {
		return PowerStateStopped, nil
	}
	return PowerStateRunning, nil
}

func (e *Executor) executeIPMI(ctx context.Context, cfg IPMIConfig, action Action) error {
	iface := cfg.Interface
	if strings.TrimSpace(iface) == "" {
		iface = "lanplus"
	}
	var cmdAction string
	switch action {
	case ActionPowerOn:
		cmdAction = "on"
	case ActionPowerOff:
		cmdAction = "off"
	default:
		return fmt.Errorf("unsupported ipmi action: %s", action)
	}
	cmd := exec.CommandContext(ctx, "ipmitool", "-I", iface, "-H", cfg.Host, "-U", cfg.Username, "-P", cfg.Password, "chassis", "power", cmdAction)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ipmitool failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (e *Executor) executeWebhook(ctx context.Context, m MachineInfo, cfg WebhookConfig, action Action) error {
	var target string
	switch action {
	case ActionPowerOn:
		target = cfg.PowerOnURL
	case ActionPowerOff:
		target = cfg.PowerOffURL
	default:
		return fmt.Errorf("unsupported webhook action: %s", action)
	}
	return e.postWebhookJSON(ctx, target, cfg, map[string]any{
		"action":    action,
		"machineId": m.Name,
		"hostname":  m.Hostname,
		"mac":       m.MAC,
		"ip":        m.IP,
		"requestId": uuid.NewString(),
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (e *Executor) configureWebhookBootOrder(ctx context.Context, m MachineInfo, cfg WebhookConfig, order BootOrder) error {
	target := strings.TrimSpace(cfg.BootOrderURL)
	if target == "" {
		return fmt.Errorf("webhook bootOrderURL is required for boot order automation")
	}
	if len(order) == 0 {
		return fmt.Errorf("boot order must not be empty")
	}
	return e.postWebhookJSON(ctx, target, cfg, map[string]any{
		"action":    "set-boot-order",
		"machineId": m.Name,
		"hostname":  m.Hostname,
		"mac":       m.MAC,
		"ip":        m.IP,
		"bootOrder": order,
		"requestId": uuid.NewString(),
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func (e *Executor) postWebhookJSON(ctx context.Context, target string, cfg WebhookConfig, payload map[string]any) error {
	merged := make(map[string]any, len(payload)+len(cfg.BodyExtras))
	for k, v := range payload {
		merged[k] = v
	}
	for k, v := range cfg.BodyExtras {
		if _, exists := merged[k]; !exists {
			merged[k] = v
		}
	}
	raw, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	req, err := gohttp.NewRequestWithContext(ctx, gohttp.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}

const shutdownMagic = "GOMI-SHUTDOWN"

var wolPowerOnRetryDelays = []time.Duration{
	0,
	1 * time.Second,
	2 * time.Second,
	4 * time.Second,
	8 * time.Second,
}

func (e *Executor) executeWoL(ctx context.Context, m MachineInfo, cfg WoLConfig, action Action) (ActionResult, error) {
	switch action {
	case ActionPowerOn:
		packet, err := buildWakePacket(cfg.WakeMAC)
		if err != nil {
			return ActionResult{}, err
		}
		addr := net.JoinHostPort(cfg.BroadcastIP, strconv.Itoa(cfg.Port))
		return ActionResult{}, sendUDPWithDelays(ctx, addr, packet, wolPowerOnRetryDelays)
	case ActionPowerOff:
		if strings.TrimSpace(cfg.HMACSecret) == "" || strings.TrimSpace(cfg.Token) == "" {
			return ActionResult{}, fmt.Errorf("WoL power-off requires hmacSecret and token; set power.wol.hmacSecret and power.wol.token in the machine config to enable remote shutdown")
		}
		target := strings.TrimSpace(cfg.ShutdownTarget)
		if target == "" {
			target = strings.TrimSpace(m.IP)
		}
		if target == "" {
			return ActionResult{}, fmt.Errorf("WoL power-off requires shutdownTarget; set power.wol.shutdownTarget to the IP/hostname of the target machine's shutdown agent")
		}
		packet, requestID, err := buildShutdownPacketWithRequestID(m.Name, cfg.Token, cfg.HMACSecret, time.Now().UTC())
		if err != nil {
			return ActionResult{}, err
		}
		port := cfg.ShutdownUDPPort
		if port == 0 {
			port = DefaultWoLShutdownUDPPort
		}
		addr := net.JoinHostPort(target, strconv.Itoa(port))
		return ActionResult{RequestID: requestID}, sendUDP(ctx, addr, packet)
	default:
		return ActionResult{}, fmt.Errorf("unsupported wol action: %s", action)
	}
}

func sendUDPWithDelays(ctx context.Context, target string, payload []byte, delays []time.Duration) error {
	if len(delays) == 0 {
		return sendUDP(ctx, target, payload)
	}
	var lastErr error
	sent := false
	for _, delay := range delays {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				if sent {
					return nil
				}
				return ctx.Err()
			case <-timer.C:
			}
		}
		if err := sendUDP(ctx, target, payload); err != nil {
			lastErr = err
			continue
		}
		sent = true
	}
	if sent {
		return nil
	}
	return lastErr
}

func buildWakePacket(macAddress string) ([]byte, error) {
	mac, err := net.ParseMAC(macAddress)
	if err != nil {
		return nil, fmt.Errorf("parse mac: %w", err)
	}
	packet := make([]byte, 6+16*len(mac))
	for i := 0; i < 6; i++ {
		packet[i] = 0xff
	}
	for i := 6; i < len(packet); i += len(mac) {
		copy(packet[i:], mac)
	}
	return packet, nil
}

func buildShutdownPacket(machineName, token, secret string, now time.Time) ([]byte, error) {
	packet, _, err := buildShutdownPacketWithRequestID(machineName, token, secret, now)
	return packet, err
}

func buildShutdownPacketWithRequestID(machineName, token, secret string, now time.Time) ([]byte, string, error) {
	if len(machineName) > 255 || len(token) > 255 {
		return nil, "", fmt.Errorf("machineName/token too long")
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return nil, "", err
	}

	magic := "GOMI"
	body := make([]byte, 0, 4+8+12+1+len(machineName)+1+len(token))
	body = append(body, []byte(magic)...)
	timestamp := make([]byte, 8)
	binary.BigEndian.PutUint64(timestamp, uint64(now.Unix()))
	body = append(body, timestamp...)
	body = append(body, nonce...)
	body = append(body, byte(len(machineName)))
	body = append(body, []byte(machineName)...)
	body = append(body, byte(len(token)))
	body = append(body, []byte(token)...)

	mac := hmac.New(sha256.New, []byte(secret))
	if _, err := mac.Write(body); err != nil {
		return nil, "", err
	}
	signature := mac.Sum(nil)
	return append(body, signature...), hex.EncodeToString(nonce), nil
}

func sendUDP(ctx context.Context, target string, payload []byte) error {
	dialer := net.Dialer{
		Control: func(network, address string, conn syscall.RawConn) error {
			var controlErr error
			if strings.HasPrefix(network, "udp") {
				if err := conn.Control(func(fd uintptr) {
					controlErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
				}); err != nil {
					return err
				}
			}
			return controlErr
		},
	}
	conn, err := dialer.DialContext(ctx, "udp", target)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.Write(payload); err != nil {
		return err
	}
	return nil
}

func NormalizeAction(raw string) (Action, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "on", "power-on", "power_on":
		return ActionPowerOn, nil
	case "off", "power-off", "power_off":
		return ActionPowerOff, nil
	default:
		return "", fmt.Errorf("unknown action: %s", raw)
	}
}
