package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReportMachinePowerEvent_StoresSignedAuditEvent(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", map[string]any{
		"name":      "ubuntu-power-event",
		"osFamily":  "ubuntu",
		"osVersion": "24.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines", map[string]any{
		"name":     "machine-power-event",
		"hostname": "machine-power-event",
		"mac":      "52:54:00:aa:bb:dd",
		"arch":     "amd64",
		"firmware": "uefi",
		"power": map[string]any{
			"type": "wol",
			"wol": map[string]any{
				"wakeMAC":    "52:54:00:aa:bb:dd",
				"hmacSecret": "event-secret",
				"token":      "event-token",
			},
		},
		"osPreset": map[string]any{
			"family":   "ubuntu",
			"version":  "24.04",
			"imageRef": "ubuntu-power-event",
		},
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	payload := map[string]any{
		"requestID":     "001122aabbcc",
		"stage":         "accepted",
		"message":       "shutdown command accepted",
		"daemonVersion": "test",
		"createdAt":     "2026-04-30T00:00:00Z",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/machines/machine-power-event/power-events", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GOMI-WOL-Signature", signTestPowerEvent(raw, "event-secret"))
	rec = httptest.NewRecorder()
	env.echo.ServeHTTP(rec, req)
	requireStatus(t, rec, http.StatusOK)

	events, err := env.authStore.ListAuditEvents(context.Background(), "machine-power-event", 20)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	var found bool
	for _, event := range events {
		if event.Action != "wol-power-event" {
			continue
		}
		found = true
		if event.Result != "success" || event.Actor != "wol-daemon" {
			t.Fatalf("unexpected audit event: %#v", event)
		}
		if event.Details["requestID"] != "001122aabbcc" || event.Details["stage"] != "accepted" || event.Details["daemonVersion"] != "test" {
			t.Fatalf("unexpected audit details: %#v", event.Details)
		}
	}
	if !found {
		t.Fatalf("expected wol-power-event audit entry, got %#v", events)
	}
}

func TestReportMachinePowerEvent_RejectsSignatureMismatch(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", map[string]any{
		"name":      "ubuntu-power-event-bad",
		"osFamily":  "ubuntu",
		"osVersion": "24.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines", map[string]any{
		"name":     "machine-power-event-bad",
		"hostname": "machine-power-event-bad",
		"mac":      "52:54:00:aa:bb:ee",
		"arch":     "amd64",
		"firmware": "uefi",
		"power": map[string]any{
			"type": "wol",
			"wol": map[string]any{
				"wakeMAC":    "52:54:00:aa:bb:ee",
				"hmacSecret": "good-secret",
				"token":      "event-token",
			},
		},
		"osPreset": map[string]any{
			"family":   "ubuntu",
			"version":  "24.04",
			"imageRef": "ubuntu-power-event-bad",
		},
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	raw := []byte(`{"requestID":"bad","stage":"accepted","createdAt":"2026-04-30T00:00:00Z"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/machines/machine-power-event-bad/power-events", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GOMI-WOL-Signature", signTestPowerEvent(raw, "wrong-secret"))
	rec = httptest.NewRecorder()
	env.echo.ServeHTTP(rec, req)
	requireStatus(t, rec, http.StatusUnauthorized)
}
