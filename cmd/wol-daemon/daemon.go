package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/sugaf1204/gomi/internal/power"
)

type Config struct {
	ListenAddr               string
	HMACSecret               string
	ExpectedToken            string
	TTL                      time.Duration
	InvalidPacketLogInterval time.Duration
	Logf                     func(format string, args ...any)
}

func Run(ctx context.Context, cfg Config, onShutdown func(packet power.ShutdownPacket) error) error {
	if cfg.TTL <= 0 {
		cfg.TTL = 60 * time.Second
	}
	if cfg.InvalidPacketLogInterval <= 0 {
		cfg.InvalidPacketLogInterval = 30 * time.Second
	}
	if cfg.Logf == nil {
		cfg.Logf = log.Printf
	}
	pc, err := net.ListenPacket("udp", cfg.ListenAddr)
	if err != nil {
		return err
	}
	defer pc.Close()

	buf := make([]byte, 2048)
	var lastInvalidLog time.Time
	logInvalid := func(format string, args ...any) {
		now := time.Now()
		if !lastInvalidLog.IsZero() && now.Sub(lastInvalidLog) < cfg.InvalidPacketLogInterval {
			return
		}
		lastInvalidLog = now
		cfg.Logf(format, args...)
	}
	for {
		_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					return nil
				default:
					continue
				}
			}
			return err
		}
		packet, err := power.ParseAndVerifyShutdownPacket(buf[:n], cfg.HMACSecret, time.Now().UTC(), cfg.TTL)
		if err != nil {
			logInvalid("invalid WoL shutdown packet: %v", err)
			continue
		}
		if cfg.ExpectedToken != "" && packet.Token != cfg.ExpectedToken {
			logInvalid("invalid WoL shutdown packet: token mismatch for machine %s", packet.MachineName)
			continue
		}
		if err := onShutdown(packet); err != nil {
			return fmt.Errorf("shutdown callback failed: %w", err)
		}
	}
}
