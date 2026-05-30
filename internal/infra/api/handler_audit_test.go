package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
)

func TestListAuditEvents_PaginatesWithoutLoadingAllRows(t *testing.T) {
	env := setupTestEnv(t)
	base := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		err := env.authStore.CreateAuditEvent(context.Background(), auth.AuditEvent{
			ID:        fmt.Sprintf("audit-%d", i),
			Machine:   "node-audit",
			Action:    fmt.Sprintf("action-%d", i),
			Actor:     "test",
			Result:    "success",
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
		if err != nil {
			t.Fatalf("create audit event: %v", err)
		}
	}

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/audit-events?machine=node-audit&pageSize=2", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	if body["totalSize"] != float64(3) {
		t.Fatalf("expected totalSize 3, got %v", body["totalSize"])
	}
	items := listValues(t, body)
	if len(items) != 2 {
		t.Fatalf("expected 2 events, got %d", len(items))
	}
	if first := items[0].(map[string]any)["action"]; first != "action-2" {
		t.Fatalf("expected newest event first, got %v", first)
	}
	token, _ := body["nextPageToken"].(string)
	if token == "" {
		t.Fatal("expected nextPageToken")
	}

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/audit-events?machine=node-audit&pageSize=2&pageToken="+token, nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	body = parseBody(t, rec)
	items = listValues(t, body)
	if len(items) != 1 {
		t.Fatalf("expected 1 event on second page, got %d", len(items))
	}
	if token, ok := body["nextPageToken"].(string); ok && token != "" {
		t.Fatalf("expected no nextPageToken on last page, got %v", body["nextPageToken"])
	}
}
