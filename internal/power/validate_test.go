package power

import (
	"testing"
)

func TestValidatePowerConfig_IPMI_Valid(t *testing.T) {
	cfg := PowerConfig{
		Type: PowerTypeIPMI,
		IPMI: &IPMIConfig{Host: "10.0.0.1", Username: "admin", Password: "pass"},
	}
	if err := ValidatePowerConfig(cfg); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidatePowerConfig_IPMI_MissingConfig(t *testing.T) {
	cfg := PowerConfig{Type: PowerTypeIPMI}
	if err := ValidatePowerConfig(cfg); err == nil {
		t.Fatal("expected error for missing IPMI config")
	}
}

func TestValidatePowerConfig_Webhook_Valid(t *testing.T) {
	cfg := PowerConfig{
		Type: PowerTypeWebhook,
		Webhook: &WebhookConfig{
			PowerOnURL:  "http://example.com/on",
			PowerOffURL: "http://example.com/off",
		},
	}
	if err := ValidatePowerConfig(cfg); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidatePowerConfig_Webhook_MissingConfig(t *testing.T) {
	cfg := PowerConfig{Type: PowerTypeWebhook}
	if err := ValidatePowerConfig(cfg); err == nil {
		t.Fatal("expected error for missing Webhook config")
	}
}

func TestValidatePowerConfig_WoL_Valid(t *testing.T) {
	cfg := PowerConfig{
		Type: PowerTypeWoL,
		WoL:  &WoLConfig{WakeMAC: "aa:bb:cc:dd:ee:01"},
	}
	if err := ValidatePowerConfig(cfg); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidatePowerConfig_WoL_WithShutdown(t *testing.T) {
	cfg := PowerConfig{
		Type: PowerTypeWoL,
		WoL: &WoLConfig{
			WakeMAC:    "aa:bb:cc:dd:ee:01",
			HMACSecret: "secret",
			Token:      "token123",
		},
	}
	if err := ValidatePowerConfig(cfg); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidatePowerConfig_WoL_MissingConfig(t *testing.T) {
	cfg := PowerConfig{Type: PowerTypeWoL}
	if err := ValidatePowerConfig(cfg); err == nil {
		t.Fatal("expected error for missing WoL config")
	}
}

func TestValidatePowerConfig_WoL_MissingWakeMAC(t *testing.T) {
	cfg := PowerConfig{
		Type: PowerTypeWoL,
		WoL:  &WoLConfig{},
	}
	if err := ValidatePowerConfig(cfg); err == nil {
		t.Fatal("expected error for missing wakeMAC")
	}
}

func TestFillWoLDefaults_GeneratesToken(t *testing.T) {
	cfg := PowerConfig{
		Type: PowerTypeWoL,
		WoL:  &WoLConfig{WakeMAC: "aa:bb:cc:dd:ee:01"},
	}
	if err := FillWoLDefaults(&cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WoL.Token == "" {
		t.Fatal("expected token to be auto-generated")
	}
	if len(cfg.WoL.Token) != 32 { // 16 bytes = 32 hex chars
		t.Fatalf("expected 32-char hex token, got %d chars: %s", len(cfg.WoL.Token), cfg.WoL.Token)
	}
	if cfg.WoL.HMACSecret == "" {
		t.Fatal("expected hmacSecret to be auto-generated")
	}
	if len(cfg.WoL.HMACSecret) != 64 { // 32 bytes = 64 hex chars
		t.Fatalf("expected 64-char hex hmacSecret, got %d chars: %s", len(cfg.WoL.HMACSecret), cfg.WoL.HMACSecret)
	}
	if cfg.WoL.BroadcastIP != "255.255.255.255" {
		t.Fatalf("expected default broadcastIP, got %s", cfg.WoL.BroadcastIP)
	}
	if cfg.WoL.Port != 9 {
		t.Fatalf("expected default port 9, got %d", cfg.WoL.Port)
	}
	if cfg.WoL.ShutdownUDPPort != DefaultWoLShutdownUDPPort {
		t.Fatalf("expected default shutdown port %d, got %d", DefaultWoLShutdownUDPPort, cfg.WoL.ShutdownUDPPort)
	}
	if cfg.WoL.TokenTTLSeconds != DefaultWoLShutdownTTLSeconds {
		t.Fatalf("expected default token ttl %d, got %d", DefaultWoLShutdownTTLSeconds, cfg.WoL.TokenTTLSeconds)
	}
}

func TestFillWoLDefaults_PreservesExistingToken(t *testing.T) {
	cfg := PowerConfig{
		Type: PowerTypeWoL,
		WoL:  &WoLConfig{WakeMAC: "aa:bb:cc:dd:ee:01", Token: "my-custom-token", HMACSecret: "my-secret", BroadcastIP: "10.0.0.255", Port: 7, ShutdownUDPPort: 40001, TokenTTLSeconds: 120},
	}
	if err := FillWoLDefaults(&cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WoL.Token != "my-custom-token" {
		t.Fatalf("expected existing token to be preserved, got %s", cfg.WoL.Token)
	}
	if cfg.WoL.HMACSecret != "my-secret" {
		t.Fatalf("expected existing hmacSecret to be preserved, got %s", cfg.WoL.HMACSecret)
	}
	if cfg.WoL.BroadcastIP != "10.0.0.255" {
		t.Fatalf("expected existing broadcastIP to be preserved, got %s", cfg.WoL.BroadcastIP)
	}
	if cfg.WoL.Port != 7 {
		t.Fatalf("expected existing port to be preserved, got %d", cfg.WoL.Port)
	}
	if cfg.WoL.ShutdownUDPPort != 40001 {
		t.Fatalf("expected existing shutdown port to be preserved, got %d", cfg.WoL.ShutdownUDPPort)
	}
	if cfg.WoL.TokenTTLSeconds != 120 {
		t.Fatalf("expected existing ttl to be preserved, got %d", cfg.WoL.TokenTTLSeconds)
	}
}

func TestFillWoLDefaults_NonWoLType(t *testing.T) {
	cfg := PowerConfig{Type: PowerTypeIPMI, IPMI: &IPMIConfig{Host: "10.0.0.1"}}
	if err := FillWoLDefaults(&cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// should be a no-op
	if cfg.IPMI.Host != "10.0.0.1" {
		t.Fatal("non-wol config should not be modified")
	}
}

func TestValidatePowerConfig_Manual(t *testing.T) {
	cfg := PowerConfig{Type: PowerTypeManual}
	if err := ValidatePowerConfig(cfg); err != nil {
		t.Fatalf("expected no error for manual type, got %v", err)
	}
}
