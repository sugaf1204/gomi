package api_test

import (
	"github.com/sugaf1204/gomi/internal/power"
	"net/http"
	"testing"
	"time"
)

func TestRedeployMachine_PowerCyclesPoweredMachine(t *testing.T) {
	exec := newRecordingPowerExecutor()
	env := setupTestEnvWithPowerExecutor(t, exec)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", map[string]any{
		"name":      "ubuntu-powered",
		"osFamily":  "ubuntu",
		"osVersion": "24.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
		"manifest":  bareMetalQCOW2Manifest(),
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines", map[string]any{
		"name":         "machine-powered-redeploy",
		"hostname":     "machine-powered-redeploy",
		"mac":          "52:54:00:de:ad:10",
		"arch":         "amd64",
		"firmware":     "uefi",
		"ipAssignment": "static",
		"ip":           "192.0.2.10",
		"power": map[string]any{
			"type": "webhook",
			"webhook": map[string]any{
				"powerOnURL":  "https://power.example/on",
				"powerOffURL": "https://power.example/off",
			},
		},
		"osPreset": map[string]any{
			"family":   "ubuntu",
			"version":  "24.04",
			"imageRef": "ubuntu-powered",
		},
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines/machine-powered-redeploy:redeploy", map[string]any{
		"confirm": "machine-powered-redeploy",
	}, env.token)
	requireStatus(t, rec, http.StatusAccepted)

	if got := waitPowerAction(t, exec.calls); got != power.ActionPowerOff {
		t.Fatalf("expected first redeploy power action power-off, got %s", got)
	}
	if got := waitPowerAction(t, exec.calls); got != power.ActionPowerOn {
		t.Fatalf("expected second redeploy power action power-on, got %s", got)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		rec = doRequest(env.echo, http.MethodGet, "/api/v1/machines/machine-powered-redeploy", nil, env.token)
		requireStatus(t, rec, http.StatusOK)
		body := parseBody(t, rec)
		if body["lastPowerAction"] == string(power.ActionPowerOn) && body["lastError"] == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected final lastPowerAction=power-on and empty lastError, got %v", body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRedeployMachine_PowerCycleUsesPreviousIPWhenRedeployClearsIP(t *testing.T) {
	exec := newRecordingPowerExecutor()
	env := setupTestEnvWithPowerExecutor(t, exec)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", map[string]any{
		"name":      "ubuntu-dhcp-redeploy",
		"osFamily":  "ubuntu",
		"osVersion": "24.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
		"manifest":  bareMetalQCOW2Manifest(),
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines", map[string]any{
		"name":         "machine-dhcp-redeploy",
		"hostname":     "machine-dhcp-redeploy",
		"mac":          "52:54:00:de:ad:11",
		"arch":         "amd64",
		"firmware":     "uefi",
		"ipAssignment": "static",
		"ip":           "192.0.2.25",
		"power": map[string]any{
			"type": "webhook",
			"webhook": map[string]any{
				"powerOnURL":  "https://power.example/on",
				"powerOffURL": "https://power.example/off",
			},
		},
		"osPreset": map[string]any{
			"family":   "ubuntu",
			"version":  "24.04",
			"imageRef": "ubuntu-dhcp-redeploy",
		},
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines/machine-dhcp-redeploy:redeploy", map[string]any{
		"confirm":      "machine-dhcp-redeploy",
		"ipAssignment": "dhcp",
	}, env.token)
	requireStatus(t, rec, http.StatusAccepted)

	if got := waitPowerAction(t, exec.calls); got != power.ActionPowerOff {
		t.Fatalf("expected first redeploy power action power-off, got %s", got)
	}
	if info := waitPowerInfo(t, exec.infos); info.IP != "192.0.2.25" {
		t.Fatalf("expected power-off to use previous IP, got %q", info.IP)
	}
	if got := waitPowerAction(t, exec.calls); got != power.ActionPowerOn {
		t.Fatalf("expected second redeploy power action power-on, got %s", got)
	}
	if info := waitPowerInfo(t, exec.infos); info.IP != "192.0.2.25" {
		t.Fatalf("expected power-on to use previous IP, got %q", info.IP)
	}

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/machines/machine-dhcp-redeploy", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	if body["ipAssignment"] != "dhcp" {
		t.Fatalf("expected redeploy to switch machine to dhcp, got %v", body["ipAssignment"])
	}
	if ip, _ := body["ip"].(string); ip != "" {
		t.Fatalf("expected redeployed machine IP to be cleared, got %q", ip)
	}
}
