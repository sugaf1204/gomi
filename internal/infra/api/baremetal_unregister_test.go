package api_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
	infraapi "github.com/sugaf1204/gomi/internal/infra/api"
	infrasql "github.com/sugaf1204/gomi/internal/infra/sql"
	"github.com/sugaf1204/gomi/internal/machine"
)

func TestBareMetalUnregisterAPIProtectsClaimsAndPreservesMachine(t *testing.T) {
	ctx := context.Background()
	b, err := infrasql.New("sqlite", filepath.Join(t.TempDir(), "gomi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err := b.Migrate(); err != nil {
		t.Fatal(err)
	}
	if err := b.Machines().Upsert(ctx, machine.Machine{Name: "node1", MAC: "00:11:22:33:44:55"}); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile("../../baremetal/testdata/host-public.pem")
	if err != nil {
		t.Fatal(err)
	}
	h, err := b.BareMetal().Register(ctx, "node1", "pool", string(key), "/dev/nvme0n1")
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []auth.Role{auth.RoleAdmin, auth.RoleOperator, auth.RoleViewer} {
		createUser(t, b.Auth(), string(role), "password", role)
	}
	e := infraapi.NewServer(infraapi.ServerConfig{Machines: machine.NewService(b.Machines()), AuthStore: b.Auth(), AuthService: infraapi.NewAuthService(b.Auth(), time.Hour), BareMetal: b.BareMetal()}).Echo()
	path := "/api/v1/bare-metal-hosts/node1"
	body := map[string]any{"revision": h.Revision}
	for _, role := range []string{"operator", "viewer"} {
		requireStatus(t, doRequest(e, http.MethodDelete, path, body, createSession(t, b.Auth(), role)), http.StatusForbidden)
	}
	admin := createSession(t, b.Auth(), "admin")
	requireStatus(t, doRequest(e, http.MethodDelete, path, map[string]any{"revision": 0}, admin), http.StatusBadRequest)
	requireStatus(t, doRequest(e, http.MethodDelete, path, map[string]any{"revision": h.Revision + 1}, admin), http.StatusConflict)
	claimed, err := b.BareMetal().Acquire(ctx, "pool", "capi-owner")
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, doRequest(e, http.MethodDelete, path, map[string]any{"revision": claimed.Revision}, admin), http.StatusConflict)
	if _, err := b.BareMetal().FindOwner(ctx, "capi-owner"); err != nil {
		t.Fatal("claim was lost", err)
	}
	// A separate unallocated host can be retired, and repeated deletion is safe.
	if err := b.Machines().Upsert(ctx, machine.Machine{Name: "node2", MAC: "00:11:22:33:44:66"}); err != nil {
		t.Fatal(err)
	}
	available, err := b.BareMetal().Register(ctx, "node2", "pool", string(key), "/dev/vda")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		requireStatus(t, doRequest(e, http.MethodDelete, "/api/v1/bare-metal-hosts/node2", map[string]any{"revision": available.Revision}, admin), http.StatusNoContent)
	}
	if _, err := b.Machines().Get(ctx, "node2"); err != nil {
		t.Fatal("machine inventory was deleted", err)
	}
}
