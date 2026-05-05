package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sugaf1204/gomi/internal/power"
)

var daemonVersion = "dev"

type daemonConfig struct {
	ListenAddr    string
	HMACSecret    string
	ExpectedToken string
	TTL           time.Duration
	Command       string
	EnvFile       string
	ServerURL     string
	MachineName   string
}

func main() {
	cfg, err := resolveDaemonConfig(os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	httpClient := &http.Client{Timeout: 5 * time.Second}
	if err := runDaemon(ctx, cfg, httpClient); err != nil {
		log.Fatalf("wol-daemon failed: %v", err)
	}
}

func runDaemon(ctx context.Context, cfg daemonConfig, httpClient *http.Client) error {
	return Run(ctx, Config{
		ListenAddr:    cfg.ListenAddr,
		HMACSecret:    cfg.HMACSecret,
		ExpectedToken: cfg.ExpectedToken,
		TTL:           cfg.TTL,
		Logf:          log.Printf,
	}, func(packet power.ShutdownPacket) error {
		if err := postPowerEvent(ctx, httpClient, cfg, packet, "accepted", "shutdown command accepted"); err != nil {
			log.Printf("power event callback failed: %v", err)
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", cfg.Command)
		out, err := cmd.CombinedOutput()
		if err != nil {
			if cbErr := postPowerEvent(ctx, httpClient, cfg, packet, "command_failed", err.Error()); cbErr != nil {
				log.Printf("power event callback failed: %v", cbErr)
			}
			return err
		}
		if len(out) > 0 {
			log.Printf("shutdown command output: %s", string(out))
		}
		return nil
	})
}

func resolveDaemonConfig(args []string) (daemonConfig, error) {
	cfg := daemonConfig{
		ListenAddr: ":40000",
		TTL:        60 * time.Second,
		Command:    "poweroff",
	}

	fs := flag.NewFlagSet("gomi-wol-daemon", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.ListenAddr, "listen", cfg.ListenAddr, "udp listen address")
	fs.StringVar(&cfg.HMACSecret, "secret", "", "hmac secret")
	fs.StringVar(&cfg.ExpectedToken, "token", "", "expected token")
	fs.DurationVar(&cfg.TTL, "ttl", cfg.TTL, "packet ttl")
	fs.StringVar(&cfg.Command, "command", cfg.Command, "shutdown command")
	fs.StringVar(&cfg.EnvFile, "env-file", "", "path to daemon environment file")
	fs.StringVar(&cfg.ServerURL, "server-url", "", "GOMI API server URL for power event callbacks")
	fs.StringVar(&cfg.MachineName, "machine-name", "", "machine name for power event callbacks")
	if err := fs.Parse(args); err != nil {
		return daemonConfig{}, err
	}

	explicit := map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		explicit[f.Name] = true
	})

	if cfg.EnvFile != "" {
		env, err := readEnvFile(cfg.EnvFile)
		if err != nil {
			return daemonConfig{}, err
		}
		if !explicit["listen"] && strings.TrimSpace(env["GOMI_WOL_LISTEN"]) != "" {
			cfg.ListenAddr = strings.TrimSpace(env["GOMI_WOL_LISTEN"])
		}
		if !explicit["secret"] && strings.TrimSpace(env["GOMI_WOL_SECRET"]) != "" {
			cfg.HMACSecret = strings.TrimSpace(env["GOMI_WOL_SECRET"])
		}
		if !explicit["token"] && strings.TrimSpace(env["GOMI_WOL_TOKEN"]) != "" {
			cfg.ExpectedToken = strings.TrimSpace(env["GOMI_WOL_TOKEN"])
		}
		if !explicit["ttl"] && strings.TrimSpace(env["GOMI_WOL_TTL"]) != "" {
			ttl, err := time.ParseDuration(strings.TrimSpace(env["GOMI_WOL_TTL"]))
			if err != nil {
				return daemonConfig{}, fmt.Errorf("invalid GOMI_WOL_TTL: %w", err)
			}
			cfg.TTL = ttl
		}
		if !explicit["server-url"] && strings.TrimSpace(env["GOMI_SERVER_URL"]) != "" {
			cfg.ServerURL = strings.TrimSpace(env["GOMI_SERVER_URL"])
		}
		if !explicit["machine-name"] && strings.TrimSpace(env["GOMI_MACHINE_NAME"]) != "" {
			cfg.MachineName = strings.TrimSpace(env["GOMI_MACHINE_NAME"])
		}
	}

	if strings.TrimSpace(cfg.HMACSecret) == "" {
		return daemonConfig{}, fmt.Errorf("--secret or GOMI_WOL_SECRET is required")
	}
	return cfg, nil
}

func readEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	env := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		env[key] = parseEnvValue(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return env, nil
}

func parseEnvValue(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
		(strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
		unquoted, err := strconv.Unquote(value)
		if err == nil {
			return unquoted
		}
		return strings.Trim(value, `"'`)
	}
	if idx := strings.Index(value, " #"); idx >= 0 {
		value = value[:idx]
	}
	return strings.TrimSpace(value)
}

type powerEventCallback struct {
	RequestID     string `json:"requestID"`
	Stage         string `json:"stage"`
	Message       string `json:"message,omitempty"`
	DaemonVersion string `json:"daemonVersion"`
	CreatedAt     string `json:"createdAt"`
}

func postPowerEvent(ctx context.Context, client *http.Client, cfg daemonConfig, packet power.ShutdownPacket, stage, message string) error {
	serverURL := strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")
	if serverURL == "" {
		return nil
	}
	machineName := strings.TrimSpace(cfg.MachineName)
	if machineName == "" {
		machineName = strings.TrimSpace(packet.MachineName)
	}
	if machineName == "" {
		return nil
	}
	body, err := json.Marshal(powerEventCallback{
		RequestID:     packet.RequestID(),
		Stage:         stage,
		Message:       message,
		DaemonVersion: daemonVersion,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return err
	}
	target := serverURL + "/api/v1/machines/" + url.PathEscape(machineName) + "/power-events"
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GOMI-WOL-Signature", signPowerEvent(body, cfg.HMACSecret))
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("callback returned status %d", resp.StatusCode)
	}
	return nil
}

func signPowerEvent(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
