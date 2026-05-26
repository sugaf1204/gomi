package api_test

import (
	"context"
	"net/http"
	"testing"
)

func TestRedeployMachine_UpdatesSpecAndNetwork(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", map[string]any{
		"name":      "ubuntu-machine",
		"osFamily":  "ubuntu",
		"osVersion": "24.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/os-images", map[string]any{
		"name":      "debian-machine",
		"osFamily":  "debian",
		"osVersion": "13",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/subnets", map[string]any{
		"name": "subnet-old",
		"spec": map[string]any{
			"cidr":       "192.168.10.0/24",
			"dnsServers": []string{"8.8.8.8"},
			"domainName": "old.example",
		},
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/subnets", map[string]any{
		"name": "subnet-new",
		"spec": map[string]any{
			"cidr":       "192.168.20.0/24",
			"dnsServers": []string{"8.8.8.8"},
			"domainName": "new.example",
		},
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", map[string]any{
		"name":     "ci-machine-new",
		"userData": "#cloud-config\nhostname: machine-new\n",
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines", map[string]any{
		"name":         "machine-redeploy",
		"hostname":     "machine-redeploy",
		"mac":          "52:54:00:de:ad:01",
		"arch":         "amd64",
		"firmware":     "uefi",
		"power":        map[string]any{"type": "manual"},
		"subnetRef":    "subnet-old",
		"ipAssignment": "static",
		"ip":           "192.168.10.25",
		"network": map[string]any{
			"domain": "old.example",
		},
		"osPreset": map[string]any{
			"family":   "ubuntu",
			"version":  "24.04",
			"imageRef": "ubuntu-machine",
		},
		"cloudInitRefs": []string{"ci-old"},
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines/machine-redeploy/actions/redeploy", map[string]any{
		"confirm":  "machine-redeploy",
		"hostname": "machine-redeploy-new",
		"mac":      "52:54:00:de:ad:02",
		"arch":     "arm64",
		"firmware": "bios",
		"power": map[string]any{
			"type": "webhook",
			"webhook": map[string]any{
				"powerOnURL":  "https://power/on",
				"powerOffURL": "https://power/off",
			},
		},
		"osPreset": map[string]any{
			"imageRef": "debian-machine",
		},
		"cloudInitRefs": []string{"ci-machine-new"},
		"subnetRef":     "subnet-new",
		"ipAssignment":  "dhcp",
		"role":          "hypervisor",
		"bridgeName":    "br-edge",
		"network": map[string]any{
			"domain": "redeploy.example",
		},
	}, env.token)
	requireStatus(t, rec, http.StatusAccepted)

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/machines/machine-redeploy", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)

	if body["hostname"] != "machine-redeploy-new" {
		t.Fatalf("expected hostname=machine-redeploy-new, got %v", body["hostname"])
	}
	if body["mac"] != "52:54:00:de:ad:02" {
		t.Fatalf("expected mac=52:54:00:de:ad:02, got %v", body["mac"])
	}
	if body["arch"] != "arm64" {
		t.Fatalf("expected arch=arm64, got %v", body["arch"])
	}
	if body["firmware"] != "bios" {
		t.Fatalf("expected firmware=bios, got %v", body["firmware"])
	}
	osPreset, _ := body["osPreset"].(map[string]any)
	if osPreset["family"] != "debian" || osPreset["version"] != "13" || osPreset["imageRef"] != "debian-machine" {
		t.Fatalf("expected redeploy to update osPreset from OS image, got %v", osPreset)
	}
	if body["subnetRef"] != "subnet-new" {
		t.Fatalf("expected subnetRef=subnet-new, got %v", body["subnetRef"])
	}
	if body["ipAssignment"] != "dhcp" {
		t.Fatalf("expected ipAssignment=dhcp, got %v", body["ipAssignment"])
	}
	if ip, _ := body["ip"].(string); ip != "" {
		t.Fatalf("expected static IP to be cleared, got %q", ip)
	}
	network, _ := body["network"].(map[string]any)
	if network["domain"] != "redeploy.example" {
		t.Fatalf("expected domain=redeploy.example, got %v", network["domain"])
	}
	cloudInitRefs, _ := body["cloudInitRefs"].([]any)
	if len(cloudInitRefs) != 1 || cloudInitRefs[0] != "ci-machine-new" {
		t.Fatalf("expected cloudInitRefs=[ci-machine-new], got %v", cloudInitRefs)
	}
	if body["lastDeployedCloudInitRef"] != "ci-machine-new" {
		t.Fatalf("expected lastDeployedCloudInitRef=ci-machine-new, got %v", body["lastDeployedCloudInitRef"])
	}
	powerBody, _ := body["power"].(map[string]any)
	if powerBody["type"] != "webhook" {
		t.Fatalf("expected power.type=webhook, got %v", powerBody["type"])
	}
	if body["role"] != "hypervisor" {
		t.Fatalf("expected role=hypervisor, got %v", body["role"])
	}
	if body["bridgeName"] != "br-edge" {
		t.Fatalf("expected bridgeName=br-edge, got %v", body["bridgeName"])
	}
	if body["phase"] != "Provisioning" {
		t.Fatalf("expected phase=Provisioning, got %v", body["phase"])
	}
}

func TestUpdateMachineSettings_PreservesWoLGeneratedFields(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", map[string]any{
		"name":      "ubuntu-wol-settings",
		"osFamily":  "ubuntu",
		"osVersion": "24.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines", map[string]any{
		"name":     "machine-wol-settings",
		"hostname": "machine-wol-settings",
		"mac":      "52:54:00:de:ad:12",
		"arch":     "amd64",
		"firmware": "uefi",
		"power": map[string]any{
			"type": "wol",
			"wol": map[string]any{
				"wakeMAC":         "52:54:00:de:ad:12",
				"broadcastIP":     "192.0.2.255",
				"port":            7,
				"shutdownTarget":  "192.0.2.30",
				"shutdownUDPPort": 40100,
				"hmacSecret":      "existing-secret",
				"token":           "existing-token",
				"tokenTTLSeconds": 120,
			},
		},
		"osPreset": map[string]any{
			"family":   "ubuntu",
			"version":  "24.04",
			"imageRef": "ubuntu-wol-settings",
		},
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPatch, "/api/v1/machines/machine-wol-settings/settings", map[string]any{
		"power": map[string]any{
			"type": "wol",
			"wol": map[string]any{
				"wakeMAC": "52:54:00:de:ad:12",
			},
		},
	}, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	powerBody, _ := body["power"].(map[string]any)
	wol, _ := powerBody["wol"].(map[string]any)
	if _, ok := wol["hmacSecret"]; ok {
		t.Fatalf("expected WoL hmacSecret to be redacted, got %v", wol)
	}
	if _, ok := wol["token"]; ok {
		t.Fatalf("expected WoL token to be redacted, got %v", wol)
	}
	if wol["hmacSecretConfigured"] != true || wol["tokenConfigured"] != true {
		t.Fatalf("expected WoL configured flags to be true, got %v", wol)
	}
	if wol["shutdownTarget"] != "192.0.2.30" || int(wol["shutdownUDPPort"].(float64)) != 40100 {
		t.Fatalf("expected existing WoL shutdown endpoint to be preserved, got %v", wol)
	}
	if wol["broadcastIP"] != "192.0.2.255" || int(wol["port"].(float64)) != 7 || int(wol["tokenTTLSeconds"].(float64)) != 120 {
		t.Fatalf("expected existing WoL transport defaults to be preserved, got %v", wol)
	}
	stored, err := env.machines.Get(context.Background(), "machine-wol-settings")
	if err != nil {
		t.Fatalf("get stored machine: %v", err)
	}
	if stored.Power.WoL == nil || stored.Power.WoL.HMACSecret != "existing-secret" || stored.Power.WoL.Token != "existing-token" {
		t.Fatalf("expected stored WoL secret/token to be preserved, got %#v", stored.Power.WoL)
	}
}

func TestRedeployMachine_PreservesWoLGeneratedFieldsOnPowerOverride(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", map[string]any{
		"name":      "ubuntu-wol-preserve",
		"osFamily":  "ubuntu",
		"osVersion": "24.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines", map[string]any{
		"name":     "machine-wol-preserve",
		"hostname": "machine-wol-preserve",
		"mac":      "52:54:00:de:ad:13",
		"arch":     "amd64",
		"firmware": "uefi",
		"power": map[string]any{
			"type": "wol",
			"wol": map[string]any{
				"wakeMAC":         "52:54:00:de:ad:13",
				"shutdownTarget":  "192.0.2.31",
				"shutdownUDPPort": 40101,
				"hmacSecret":      "redeploy-secret",
				"token":           "redeploy-token",
			},
		},
		"osPreset": map[string]any{
			"family":   "ubuntu",
			"version":  "24.04",
			"imageRef": "ubuntu-wol-preserve",
		},
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines/machine-wol-preserve/actions/redeploy", map[string]any{
		"confirm": "machine-wol-preserve",
		"power": map[string]any{
			"type": "wol",
			"wol": map[string]any{
				"wakeMAC": "52:54:00:de:ad:13",
			},
		},
	}, env.token)
	requireStatus(t, rec, http.StatusAccepted)

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/machines/machine-wol-preserve", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	powerBody, _ := body["power"].(map[string]any)
	wol, _ := powerBody["wol"].(map[string]any)
	if _, ok := wol["hmacSecret"]; ok {
		t.Fatalf("expected WoL hmacSecret to be redacted, got %v", wol)
	}
	if _, ok := wol["token"]; ok {
		t.Fatalf("expected WoL token to be redacted, got %v", wol)
	}
	if wol["hmacSecretConfigured"] != true || wol["tokenConfigured"] != true {
		t.Fatalf("expected WoL configured flags to be true, got %v", wol)
	}
	if wol["shutdownTarget"] != "192.0.2.31" || int(wol["shutdownUDPPort"].(float64)) != 40101 {
		t.Fatalf("expected redeploy to preserve existing WoL shutdown endpoint, got %v", wol)
	}
	stored, err := env.machines.Get(context.Background(), "machine-wol-preserve")
	if err != nil {
		t.Fatalf("get stored machine: %v", err)
	}
	if stored.Power.WoL == nil || stored.Power.WoL.HMACSecret != "redeploy-secret" || stored.Power.WoL.Token != "redeploy-token" {
		t.Fatalf("expected stored WoL secret/token to be preserved, got %#v", stored.Power.WoL)
	}
}

func TestUpdateMachineSettings_PreservesIPMIPasswordWhenOmitted(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", map[string]any{
		"name":      "ubuntu-ipmi-preserve",
		"osFamily":  "ubuntu",
		"osVersion": "24.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines", map[string]any{
		"name":     "machine-ipmi-preserve",
		"hostname": "machine-ipmi-preserve",
		"mac":      "52:54:00:de:ad:14",
		"arch":     "amd64",
		"firmware": "uefi",
		"power": map[string]any{
			"type": "ipmi",
			"ipmi": map[string]any{
				"host":     "192.0.2.20",
				"username": "admin",
				"password": "existing-password",
			},
		},
		"osPreset": map[string]any{
			"family":   "ubuntu",
			"version":  "24.04",
			"imageRef": "ubuntu-ipmi-preserve",
		},
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPatch, "/api/v1/machines/machine-ipmi-preserve/settings", map[string]any{
		"power": map[string]any{
			"type": "ipmi",
			"ipmi": map[string]any{
				"host":     "192.0.2.21",
				"username": "admin2",
			},
		},
	}, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	powerBody, _ := body["power"].(map[string]any)
	ipmi, _ := powerBody["ipmi"].(map[string]any)
	if _, ok := ipmi["password"]; ok {
		t.Fatalf("expected IPMI password to be redacted, got %v", ipmi)
	}
	if ipmi["passwordConfigured"] != true {
		t.Fatalf("expected passwordConfigured=true, got %v", ipmi)
	}

	stored, err := env.machines.Get(context.Background(), "machine-ipmi-preserve")
	if err != nil {
		t.Fatalf("get stored machine: %v", err)
	}
	if stored.Power.IPMI == nil || stored.Power.IPMI.Password != "existing-password" {
		t.Fatalf("expected IPMI password to be preserved, got %#v", stored.Power.IPMI)
	}
}

func TestUpdateMachineSettings_PreservesAndClearsWebhookHiddenMaps(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", map[string]any{
		"name":      "ubuntu-webhook-preserve",
		"osFamily":  "ubuntu",
		"osVersion": "24.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines", map[string]any{
		"name":     "machine-webhook-preserve",
		"hostname": "machine-webhook-preserve",
		"mac":      "52:54:00:de:ad:15",
		"arch":     "amd64",
		"firmware": "uefi",
		"power": map[string]any{
			"type": "webhook",
			"webhook": map[string]any{
				"powerOnURL":  "https://power.example/on",
				"powerOffURL": "https://power.example/off",
				"headers": map[string]any{
					"Authorization": "Bearer existing",
				},
				"bodyExtras": map[string]any{
					"site": "lab-a",
				},
			},
		},
		"osPreset": map[string]any{
			"family":   "ubuntu",
			"version":  "24.04",
			"imageRef": "ubuntu-webhook-preserve",
		},
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPatch, "/api/v1/machines/machine-webhook-preserve/settings", map[string]any{
		"power": map[string]any{
			"type": "webhook",
			"webhook": map[string]any{
				"powerOnURL":  "https://power.example/on2",
				"powerOffURL": "https://power.example/off2",
			},
		},
	}, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	powerBody, _ := body["power"].(map[string]any)
	webhook, _ := powerBody["webhook"].(map[string]any)
	if webhook["headersConfigured"] != true || webhook["bodyExtrasConfigured"] != true {
		t.Fatalf("expected webhook configured flags after omitted maps, got %v", webhook)
	}
	stored, err := env.machines.Get(context.Background(), "machine-webhook-preserve")
	if err != nil {
		t.Fatalf("get stored machine: %v", err)
	}
	if stored.Power.Webhook == nil || stored.Power.Webhook.Headers["Authorization"] != "Bearer existing" || stored.Power.Webhook.BodyExtras["site"] != "lab-a" {
		t.Fatalf("expected webhook hidden maps to be preserved, got %#v", stored.Power.Webhook)
	}

	rec = doRequest(env.echo, http.MethodPatch, "/api/v1/machines/machine-webhook-preserve/settings", map[string]any{
		"power": map[string]any{
			"type": "webhook",
			"webhook": map[string]any{
				"powerOnURL":  "https://power.example/on3",
				"powerOffURL": "https://power.example/off3",
				"headers":     map[string]any{},
				"bodyExtras":  map[string]any{},
			},
		},
	}, env.token)
	requireStatus(t, rec, http.StatusOK)
	body = parseBody(t, rec)
	powerBody, _ = body["power"].(map[string]any)
	webhook, _ = powerBody["webhook"].(map[string]any)
	if webhook["headersConfigured"] != false || webhook["bodyExtrasConfigured"] != false {
		t.Fatalf("expected webhook configured flags false after explicit clear, got %v", webhook)
	}
	stored, err = env.machines.Get(context.Background(), "machine-webhook-preserve")
	if err != nil {
		t.Fatalf("get stored machine: %v", err)
	}
	if stored.Power.Webhook == nil || len(stored.Power.Webhook.Headers) != 0 || len(stored.Power.Webhook.BodyExtras) != 0 {
		t.Fatalf("expected webhook hidden maps to be cleared, got %#v", stored.Power.Webhook)
	}
}
