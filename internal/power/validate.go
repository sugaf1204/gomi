package power

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// FillWoLDefaults populates auto-generated fields for WoL config.
// Call this before ValidatePowerConfig. The cfg pointer is modified in place.
func FillWoLDefaults(cfg *PowerConfig) error {
	if cfg.Type != PowerTypeWoL || cfg.WoL == nil {
		return nil
	}
	if strings.TrimSpace(cfg.WoL.Token) == "" {
		token, err := generateToken(16)
		if err != nil {
			return fmt.Errorf("failed to generate WoL token: %w", err)
		}
		cfg.WoL.Token = token
	}
	if strings.TrimSpace(cfg.WoL.HMACSecret) == "" {
		secret, err := generateToken(32)
		if err != nil {
			return fmt.Errorf("failed to generate WoL hmac secret: %w", err)
		}
		cfg.WoL.HMACSecret = secret
	}
	if cfg.WoL.BroadcastIP == "" {
		cfg.WoL.BroadcastIP = "255.255.255.255"
	}
	if cfg.WoL.Port == 0 {
		cfg.WoL.Port = 9
	}
	if cfg.WoL.ShutdownUDPPort == 0 {
		cfg.WoL.ShutdownUDPPort = DefaultWoLShutdownUDPPort
	}
	if cfg.WoL.TokenTTLSeconds == 0 {
		cfg.WoL.TokenTTLSeconds = DefaultWoLShutdownTTLSeconds
	}
	return nil
}

func generateToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func ValidatePowerConfig(cfg PowerConfig) error {
	switch cfg.Type {
	case PowerTypeIPMI:
		if cfg.IPMI == nil {
			return errors.New("power.ipmi is required for ipmi type")
		}
		if strings.TrimSpace(cfg.IPMI.Host) == "" {
			return errors.New("power.ipmi.host is required")
		}
	case PowerTypeWebhook:
		if cfg.Webhook == nil {
			return errors.New("power.webhook is required for webhook type")
		}
		if strings.TrimSpace(cfg.Webhook.PowerOnURL) == "" || strings.TrimSpace(cfg.Webhook.PowerOffURL) == "" {
			return errors.New("power.webhook.powerOnURL and powerOffURL are required")
		}
	case PowerTypeWoL:
		if cfg.WoL == nil {
			return errors.New("power.wol is required for wol type")
		}
		if strings.TrimSpace(cfg.WoL.WakeMAC) == "" {
			return errors.New("power.wol.wakeMAC is required: set the MAC address of the target machine for Wake-on-LAN")
		}
	case PowerTypeManual:
		// no additional config required
	default:
		return fmt.Errorf("unsupported power type: %s", cfg.Type)
	}
	return nil
}
