package power

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestExecuteWithResult_WoLPowerOffReturnsRequestID(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer pc.Close()

	port := pc.LocalAddr().(*net.UDPAddr).Port
	exec := NewExecutor()
	result, err := exec.ExecuteWithResult(context.Background(), MachineInfo{
		Name: "node-01",
		IP:   "127.0.0.1",
		Power: PowerConfig{
			Type: PowerTypeWoL,
			WoL: &WoLConfig{
				WakeMAC:         "52:54:00:aa:bb:cc",
				ShutdownTarget:  "127.0.0.1",
				ShutdownUDPPort: port,
				HMACSecret:      "result-secret",
				Token:           "result-token",
			},
		},
	}, ActionPowerOff)
	if err != nil {
		t.Fatalf("ExecuteWithResult: %v", err)
	}
	if len(result.RequestID) != 24 {
		t.Fatalf("expected 24-char requestID, got %q", result.RequestID)
	}

	buf := make([]byte, 2048)
	_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read shutdown packet: %v", err)
	}
	packet, err := ParseAndVerifyShutdownPacket(buf[:n], "result-secret", time.Now().UTC(), 60*time.Second)
	if err != nil {
		t.Fatalf("parse shutdown packet: %v", err)
	}
	if packet.RequestID() != result.RequestID {
		t.Fatalf("expected requestID %s from nonce, got %s", result.RequestID, packet.RequestID())
	}
}

func TestConfigureBootOrder_Webhook(t *testing.T) {
	var got struct {
		Action    string       `json:"action"`
		MachineID string       `json:"machineId"`
		Hostname  string       `json:"hostname"`
		MAC       string       `json:"mac"`
		IP        string       `json:"ip"`
		BootOrder []BootDevice `json:"bootOrder"`
	}

	exec := &Executor{
		httpClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodPost {
					t.Fatalf("expected POST, got %s", r.Method)
				}
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(http.NoBody),
					Header:     make(http.Header),
				}, nil
			}),
		},
	}
	err := exec.ConfigureBootOrder(context.Background(), MachineInfo{
		Name:     "node-01",
		Hostname: "node-01",
		MAC:      "52:54:00:aa:bb:cc",
		IP:       "192.168.2.10",
		Power: PowerConfig{
			Type: PowerTypeWebhook,
			Webhook: &WebhookConfig{
				PowerOnURL:   "https://power.example/on",
				PowerOffURL:  "https://power.example/off",
				BootOrderURL: "https://power.example/boot-order",
			},
		},
	}, DefaultBIOSBootOrder)
	if err != nil {
		t.Fatalf("ConfigureBootOrder: %v", err)
	}

	if got.Action != "set-boot-order" {
		t.Fatalf("expected action set-boot-order, got %s", got.Action)
	}
	if got.MachineID != "node-01" {
		t.Fatalf("expected machineId node-01, got %s", got.MachineID)
	}
	if got.Hostname != "node-01" {
		t.Fatalf("expected hostname node-01, got %s", got.Hostname)
	}
	if got.MAC != "52:54:00:aa:bb:cc" {
		t.Fatalf("expected mac 52:54:00:aa:bb:cc, got %s", got.MAC)
	}
	if got.IP != "192.168.2.10" {
		t.Fatalf("expected ip 192.168.2.10, got %s", got.IP)
	}
	if len(got.BootOrder) != len(DefaultBIOSBootOrder) {
		t.Fatalf("expected boot order length %d, got %d", len(DefaultBIOSBootOrder), len(got.BootOrder))
	}
	for i, item := range DefaultBIOSBootOrder {
		if got.BootOrder[i] != item {
			t.Fatalf("expected bootOrder[%d]=%s, got %s", i, item, got.BootOrder[i])
		}
	}
}
