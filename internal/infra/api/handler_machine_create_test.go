package api_test

import (
	"context"
	"github.com/sugaf1204/gomi/internal/machine"
	"net/http"
	"strings"
	"testing"
)

func TestCreateMachine_ResolveOSPresetFromImage(t *testing.T) {
	env := setupTestEnv(t)

	// Create an OS image with osFamily=debian, osVersion=13.
	imgBody := map[string]any{
		"name":      "debian-13-amd64",
		"osFamily":  "debian",
		"osVersion": "13",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
		"manifest":  bareMetalQCOW2Manifest(),
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", imgBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// Create a machine that references debian-13-amd64 but sends WRONG family=ubuntu.
	// The backend must override family/version from the OS image.
	machineBody := map[string]any{
		"name":     "test-resolve",
		"hostname": "test-resolve",
		"mac":      "52:54:00:ab:cd:ef",
		"arch":     "amd64",
		"firmware": "uefi",
		"power":    map[string]any{"type": "manual"},
		"osPreset": map[string]any{
			"family":   "ubuntu",
			"version":  "24.04",
			"imageRef": "debian-13-amd64",
		},
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines", machineBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	result := parseBody(t, rec)
	osPreset, ok := result["osPreset"].(map[string]any)
	if !ok {
		t.Fatalf("expected osPreset in response, got: %v", result)
	}
	if osPreset["family"] != "debian" {
		t.Fatalf("expected family=debian (from OS image), got: %v", osPreset["family"])
	}
	if osPreset["version"] != "13" {
		t.Fatalf("expected version=13 (from OS image), got: %v", osPreset["version"])
	}
	if osPreset["imageRef"] != "osImages/debian-13-amd64" {
		t.Fatalf("expected imageRef=debian-13-amd64, got: %v", osPreset["imageRef"])
	}
	provision, ok := result["provision"].(map[string]any)
	if !ok {
		t.Fatalf("expected provision in response, got: %v", result)
	}
	if attemptID, _ := provision["attemptId"].(string); strings.TrimSpace(attemptID) == "" {
		t.Fatalf("expected provision.attemptId to be set, got: %v", provision["attemptId"])
	}
	if _, ok := provision["completionToken"]; ok {
		t.Fatalf("expected provision.completionToken to be redacted, got: %v", provision)
	}
}

func TestCreateMachine_RejectsImageWithoutBareMetalSupport(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", map[string]any{
		"name":      "ubuntu-cloud-only",
		"osFamily":  "ubuntu",
		"osVersion": "24.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
		"variant":   "cloud",
		"manifest": map[string]any{
			"capabilities": map[string]any{
				"deployTargets": []string{"vm"},
			},
		},
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines", map[string]any{
		"name":     "machine-cloud-only",
		"hostname": "machine-cloud-only",
		"mac":      "52:54:00:aa:cc:01",
		"arch":     "amd64",
		"firmware": "uefi",
		"power":    map[string]any{"type": "manual"},
		"osPreset": map[string]any{
			"family":   "ubuntu",
			"version":  "24.04",
			"imageRef": "ubuntu-cloud-only",
		},
	}, env.token)
	requireStatus(t, rec, http.StatusBadRequest)

	body := parseBody(t, rec)
	errMsg, _ := body["error"].(string)
	if !strings.Contains(errMsg, "does not support bare-metal deployment") {
		t.Fatalf("expected bare-metal support error, got: %s", errMsg)
	}
}

func TestMachineAPIResponsesRedactSensitiveFields(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", map[string]any{
		"name":      "ubuntu-redact",
		"osFamily":  "ubuntu",
		"osVersion": "24.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
		"manifest":  bareMetalQCOW2Manifest(),
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines", map[string]any{
		"name":     "machine-redact",
		"hostname": "machine-redact",
		"mac":      "52:54:00:aa:bb:cc",
		"arch":     "amd64",
		"firmware": "uefi",
		"power": map[string]any{
			"type": "wol",
			"wol": map[string]any{
				"wakeMAC":    "52:54:00:aa:bb:cc",
				"hmacSecret": "wol-secret",
				"token":      "wol-token",
			},
		},
		"osPreset": map[string]any{
			"family":   "ubuntu",
			"version":  "24.04",
			"imageRef": "ubuntu-redact",
		},
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	body := parseBody(t, rec)
	provision, _ := body["provision"].(map[string]any)
	if _, ok := provision["completionToken"]; ok {
		t.Fatalf("expected completionToken to be redacted, got %v", provision)
	}
	powerBody, _ := body["power"].(map[string]any)
	wol, _ := powerBody["wol"].(map[string]any)
	if _, ok := wol["hmacSecret"]; ok {
		t.Fatalf("expected hmacSecret to be redacted, got %v", wol)
	}
	if _, ok := wol["token"]; ok {
		t.Fatalf("expected token to be redacted, got %v", wol)
	}
	if wol["hmacSecretConfigured"] != true || wol["tokenConfigured"] != true {
		t.Fatalf("expected WoL configured flags, got %v", wol)
	}

	stored, err := env.machines.Get(context.Background(), "machine-redact")
	if err != nil {
		t.Fatalf("get stored machine: %v", err)
	}
	if stored.Provision == nil || strings.TrimSpace(stored.Provision.CompletionToken) == "" {
		t.Fatalf("expected stored completion token to remain, got %#v", stored.Provision)
	}
	if stored.Power.WoL == nil || stored.Power.WoL.HMACSecret != "wol-secret" || stored.Power.WoL.Token != "wol-token" {
		t.Fatalf("expected stored WoL secret/token to remain, got %#v", stored.Power.WoL)
	}

	rec = doRequest(env.echo, http.MethodPatch, "/api/v1/machines/machine-redact/settings", map[string]any{
		"power": map[string]any{
			"type": "ipmi",
			"ipmi": map[string]any{
				"host":     "192.0.2.10",
				"username": "admin",
				"password": "ipmi-password",
			},
		},
	}, env.token)
	requireStatus(t, rec, http.StatusOK)
	body = parseBody(t, rec)
	powerBody, _ = body["power"].(map[string]any)
	ipmi, _ := powerBody["ipmi"].(map[string]any)
	if _, ok := ipmi["password"]; ok {
		t.Fatalf("expected IPMI password to be redacted, got %v", ipmi)
	}
	if ipmi["passwordConfigured"] != true {
		t.Fatalf("expected IPMI passwordConfigured=true, got %v", ipmi)
	}

	rec = doRequest(env.echo, http.MethodPatch, "/api/v1/machines/machine-redact/settings", map[string]any{
		"power": map[string]any{
			"type": "webhook",
			"webhook": map[string]any{
				"powerOnURL":  "https://power.example/on",
				"powerOffURL": "https://power.example/off",
				"headers": map[string]any{
					"Authorization": "Bearer webhook-secret",
				},
				"bodyExtras": map[string]any{
					"secret": "webhook-body-secret",
				},
			},
		},
	}, env.token)
	requireStatus(t, rec, http.StatusOK)
	body = parseBody(t, rec)
	powerBody, _ = body["power"].(map[string]any)
	webhook, _ := powerBody["webhook"].(map[string]any)
	if _, ok := webhook["headers"]; ok {
		t.Fatalf("expected webhook headers to be redacted, got %v", webhook)
	}
	if _, ok := webhook["bodyExtras"]; ok {
		t.Fatalf("expected webhook bodyExtras to be redacted, got %v", webhook)
	}
	if webhook["headersConfigured"] != true || webhook["bodyExtrasConfigured"] != true {
		t.Fatalf("expected webhook configured flags, got %v", webhook)
	}
}

func TestCreateMachine_HypervisorIssuesPrivateRegistrationToken(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/machines", map[string]any{
		"name":       "node3",
		"hostname":   "node3",
		"mac":        "52:54:00:aa:bb:cc",
		"arch":       "amd64",
		"firmware":   "uefi",
		"power":      map[string]any{"type": "manual"},
		"role":       "hypervisor",
		"bridgeName": "br0",
		"osPreset": map[string]any{
			"family":  "ubuntu",
			"version": "24.04",
		},
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	stored, err := env.machines.Get(context.Background(), "node3")
	if err != nil {
		t.Fatalf("get stored machine: %v", err)
	}
	if stored.Provision == nil || strings.TrimSpace(stored.Provision.Artifacts[machine.ProvisionArtifactHypervisorRegistrationToken]) == "" {
		t.Fatalf("expected stored hypervisor registration token artifact, got %#v", stored.Provision)
	}

	body := parseBody(t, rec)
	provision, ok := body["provision"].(map[string]any)
	if !ok {
		t.Fatalf("expected provision response, got %v", body["provision"])
	}
	artifacts, _ := provision["artifacts"].(map[string]any)
	if _, leaked := artifacts[machine.ProvisionArtifactHypervisorRegistrationToken]; leaked {
		t.Fatalf("registration token must not be exposed in machine response: %v", artifacts)
	}
	if _, leaked := artifacts[machine.ProvisionArtifactHypervisorRegistrationTokenExpiresAt]; leaked {
		t.Fatalf("registration token expiry must not be exposed in machine response: %v", artifacts)
	}
}

func TestCreateMachine_InvalidImageRef(t *testing.T) {
	env := setupTestEnv(t)

	machineBody := map[string]any{
		"name":     "test-badimg",
		"hostname": "test-badimg",
		"mac":      "52:54:00:ba:d1:00",
		"arch":     "amd64",
		"firmware": "uefi",
		"power":    map[string]any{"type": "manual"},
		"osPreset": map[string]any{
			"family":   "ubuntu",
			"imageRef": "nonexistent-image",
		},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/machines", machineBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)

	body := parseBody(t, rec)
	errMsg, _ := body["error"].(string)
	if !strings.Contains(errMsg, "not found") {
		t.Fatalf("expected 'not found' error for invalid imageRef, got: %s", errMsg)
	}
}
