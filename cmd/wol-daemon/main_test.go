package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/power"
)

func TestResolveDaemonConfigEnvFile(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "wol-daemon.env")
	if err := os.WriteFile(envPath, []byte(`
# comment
GOMI_WOL_LISTEN=":41000"
GOMI_WOL_SECRET='secret-from-file'
GOMI_WOL_TOKEN=token-from-file # trailing comment
GOMI_WOL_TTL="90s"
GOMI_SERVER_URL="http://127.0.0.1:8080"
GOMI_MACHINE_NAME=node-01
`), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	cfg, err := resolveDaemonConfig([]string{"--env-file", envPath})
	if err != nil {
		t.Fatalf("resolve config: %v", err)
	}
	if cfg.ListenAddr != ":41000" {
		t.Fatalf("expected listen from env file, got %q", cfg.ListenAddr)
	}
	if cfg.HMACSecret != "secret-from-file" {
		t.Fatalf("expected secret from env file, got %q", cfg.HMACSecret)
	}
	if cfg.ExpectedToken != "token-from-file" {
		t.Fatalf("expected token from env file, got %q", cfg.ExpectedToken)
	}
	if cfg.TTL != 90*time.Second {
		t.Fatalf("expected ttl=90s, got %s", cfg.TTL)
	}
	if cfg.ServerURL != "http://127.0.0.1:8080" || cfg.MachineName != "node-01" {
		t.Fatalf("expected callback env, got server=%q machine=%q", cfg.ServerURL, cfg.MachineName)
	}
}

func TestResolveDaemonConfigCLIOverridesEnvFile(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "wol-daemon.env")
	if err := os.WriteFile(envPath, []byte(`
GOMI_WOL_LISTEN=":41000"
GOMI_WOL_SECRET="secret-from-file"
GOMI_WOL_TOKEN="token-from-file"
GOMI_WOL_TTL="90s"
GOMI_SERVER_URL="http://file.example"
GOMI_MACHINE_NAME=file-node
`), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	cfg, err := resolveDaemonConfig([]string{
		"--env-file", envPath,
		"--listen", ":42000",
		"--secret", "secret-cli",
		"--token", "token-cli",
		"--ttl", "2m",
		"--server-url", "http://cli.example",
		"--machine-name", "cli-node",
	})
	if err != nil {
		t.Fatalf("resolve config: %v", err)
	}
	if cfg.ListenAddr != ":42000" || cfg.HMACSecret != "secret-cli" || cfg.ExpectedToken != "token-cli" || cfg.TTL != 2*time.Minute {
		t.Fatalf("expected CLI overrides, got %#v", cfg)
	}
	if cfg.ServerURL != "http://cli.example" || cfg.MachineName != "cli-node" {
		t.Fatalf("expected callback CLI overrides, got server=%q machine=%q", cfg.ServerURL, cfg.MachineName)
	}
}

func TestResolveDaemonConfigRequiresSecret(t *testing.T) {
	if _, err := resolveDaemonConfig(nil); err == nil {
		t.Fatal("expected missing secret error")
	}
}

func TestPostPowerEventSignsCallback(t *testing.T) {
	var sawRequest bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/machines/node-01/power-events" {
			t.Fatalf("unexpected callback path: %s", r.URL.Path)
		}
		var payload powerEventCallback
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read callback body: %v", err)
		}
		if got, want := r.Header.Get("X-GOMI-WOL-Signature"), signPowerEvent(raw, "callback-secret"); got != want {
			t.Fatalf("unexpected signature: got %q want %q", got, want)
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("unmarshal callback: %v", err)
		}
		if payload.RequestID != "000102030405060708090a0b" || payload.Stage != "accepted" {
			t.Fatalf("unexpected callback payload: %#v", payload)
		}
		sawRequest = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	packet := power.ShutdownPacket{
		MachineName: "node-01",
		Nonce:       []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
	}
	err := postPowerEvent(context.Background(), server.Client(), daemonConfig{
		HMACSecret:  "callback-secret",
		ServerURL:   server.URL,
		MachineName: "node-01",
	}, packet, "accepted", "ok")
	if err != nil {
		t.Fatalf("post callback: %v", err)
	}
	if !sawRequest {
		t.Fatal("expected callback request")
	}
}
