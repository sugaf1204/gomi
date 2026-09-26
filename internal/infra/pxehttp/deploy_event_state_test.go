package pxehttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
)

func TestLateRestorationEventIsFencedAndDoesNotReactivate(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		sealed, cleanup       bool
		event, token, attempt string
		want                  int
	}{
		{"sealed-timeout", true, false, "image_applied", "token", "current", http.StatusOK},
		{"cleanup-timeout", true, true, "image_applied", "token", "current", http.StatusOK},
		{"ordinary-timeout", false, false, "image_applied", "token", "current", http.StatusConflict},
		{"inactive-progress", true, false, "progress", "token", "current", http.StatusConflict},
		{"inactive-failure", true, false, "failed", "token", "current", http.StatusConflict},
		{"old-attempt", true, false, "image_applied", "token", "old", http.StatusConflict},
		{"old-token", true, false, "image_applied", "old-token", "current", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			backend := memory.New()
			m := machine.Machine{Name: "node", Phase: machine.PhaseError, LastError: "provisioning timeout",
				Provision: &machine.ProvisionProgress{AttemptID: "current", CompletionToken: "token"}}
			if tc.sealed {
				m.SealedBootstrap = &machine.SealedBootstrap{Owner: "capi-owner", Cleanup: tc.cleanup}
			}
			if err := backend.Machines().Upsert(ctx, m); err != nil {
				t.Fatal(err)
			}
			h := &Handler{machines: machine.NewService(backend.Machines())}
			// The second request models an acknowledgement lost after persistence.
			for i := 0; i < 2; i++ {
				req := httptest.NewRequest(http.MethodPost, "/pxe/deploy-events?token="+tc.token+"&attempt_id="+tc.attempt, strings.NewReader(`{"type":"`+tc.event+`"}`))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				if err := h.PXEDeployEvents(echo.New().NewContext(req, rec)); err != nil {
					t.Fatal(err)
				}
				if rec.Code != tc.want {
					t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
				}
			}
			saved, err := backend.Machines().Get(ctx, m.Name)
			if err != nil {
				t.Fatal(err)
			}
			if (saved.Provision.Artifacts["imageApplied"] == "true") != (tc.want == http.StatusOK) {
				t.Fatal("unexpected restoration evidence")
			}
			if saved.Provision.Active || saved.Phase != machine.PhaseError || saved.LastError != m.LastError || saved.Provision.AttemptID != "current" {
				t.Fatal("late event changed deployment lifecycle")
			}
		})
	}
}

func TestEventRechecksAttemptBeforeUpdating(t *testing.T) {
	ctx := context.Background()
	backend := memory.New()
	expected := machine.Machine{Name: "node", Provision: &machine.ProvisionProgress{Active: true, AttemptID: "old", CompletionToken: "old-token"}}
	fresh := expected
	fresh.Provision = &machine.ProvisionProgress{Active: true, AttemptID: "new", CompletionToken: "new-token"}
	if err := backend.Machines().Upsert(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	h := &Handler{machines: machine.NewService(backend.Machines())}
	called := false
	err := h.updateProvisionEvent(ctx, &expected, deployEventImageApplied, func(*machine.Machine) { called = true })
	if err == nil || called {
		t.Fatal("stale event reached new attempt")
	}
}
