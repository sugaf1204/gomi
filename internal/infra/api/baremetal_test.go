package api_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"errors"
	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/baremetal"
	infraapi "github.com/sugaf1204/gomi/internal/infra/api"
	infrasql "github.com/sugaf1204/gomi/internal/infra/sql"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/power"
)

type bareMetalPowerExecutor struct {
	actions []power.Action
}

func (e *bareMetalPowerExecutor) Execute(_ context.Context, _ power.MachineInfo, action power.Action) error {
	e.actions = append(e.actions, action)
	return nil
}

func (e *bareMetalPowerExecutor) CheckStatus(context.Context, power.MachineInfo) (power.PowerState, error) {
	return power.PowerStateUnknown, nil
}

func (e *bareMetalPowerExecutor) ConfigureBootOrder(context.Context, power.MachineInfo, power.BootOrder) error {
	return nil
}

func TestBareMetalAPIEnrollmentAndServiceAccount(t *testing.T) {
	ctx := context.Background()
	key, err := os.ReadFile("../../baremetal/testdata/host-public.pem")
	if err != nil {
		t.Fatal(err)
	}
	b, err := infrasql.New("sqlite", filepath.Join(t.TempDir(), "gomi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if err = b.Migrate(); err != nil {
		t.Fatal(err)
	}
	ms := machine.NewService(b.Machines())
	err = b.Machines().Upsert(ctx, machine.Machine{Name: "node1", Hostname: "node1", MAC: "00:11:22:33:44:55", Arch: "amd64", Firmware: machine.FirmwareUEFI, Power: power.PowerConfig{Type: power.PowerTypeWebhook, Webhook: &power.WebhookConfig{PowerOnURL: "http://power.test/on", PowerOffURL: "http://power.test/off"}}})
	if err != nil {
		t.Fatal(err)
	}
	createUser(t, b.Auth(), "admin", "password", auth.RoleAdmin)
	admin := createSession(t, b.Auth(), "admin")
	powerExecutor := &bareMetalPowerExecutor{}
	e := infraapi.NewServer(infraapi.ServerConfig{Machines: ms, PowerExecutor: powerExecutor, AuthStore: b.Auth(), AuthService: infraapi.NewAuthService(b.Auth(), time.Hour), BareMetal: b.BareMetal()}).Echo()
	for _, name := range []string{"manual", "hypervisor", "arm64"} {
		m, err := b.Machines().Get(ctx, "node1")
		if err != nil {
			t.Fatal(err)
		}
		m.Name = name
		m.MAC = ""
		switch name {
		case "manual":
			m.Power = power.PowerConfig{Type: power.PowerTypeManual}
		case "hypervisor":
			m.Role = machine.RoleHypervisor
		case "arm64":
			m.Arch = "arm64"
		}
		if err := b.Machines().Upsert(ctx, m); err != nil {
			t.Fatal(err)
		}
		rec := doRequest(e, http.MethodPut, "/api/v1/bare-metal-hosts/"+name, map[string]any{"pool": "k8scluster02", "publicKey": string(key), "targetDisk": "/dev/vda"}, admin)
		requireStatus(t, rec, http.StatusBadRequest)
		if _, err := b.BareMetal().Get(ctx, name); !errors.Is(err, baremetal.ErrNotFound) {
			t.Fatalf("ineligible host was enrolled: %v", err)
		}
	}
	rec := doRequest(e, http.MethodPost, "/api/v1/service-accounts", map[string]any{"name": "capi", "role": "operator"}, admin)
	requireStatus(t, rec, http.StatusCreated)
	token := parseBody(t, rec)["token"].(string)
	rec = doRequest(e, http.MethodPut, "/api/v1/bare-metal-hosts/node1", map[string]any{"pool": "k8scluster02", "publicKey": string(key), "targetDisk": "/dev/nvme0n1"}, token)
	requireStatus(t, rec, http.StatusForbidden)
	rec = doRequest(e, http.MethodPost, "/api/v1/bare-metal-claims", map[string]any{"pool": "k8scluster02", "owner": "capi-test"}, token)
	requireStatus(t, rec, http.StatusConflict)
	rec = doRequest(e, http.MethodPut, "/api/v1/bare-metal-hosts/node1", map[string]any{"pool": "k8scluster02", "publicKey": string(key), "targetDisk": "/dev/nvme0n1"}, admin)
	requireStatus(t, rec, http.StatusOK)
	enrolled := parseBody(t, rec)
	revision := int64(enrolled["revision"].(float64))
	rec = doRequest(e, http.MethodPatch, "/api/v1/bare-metal-hosts/node1", map[string]any{"pool": "cilium-cluster", "revision": revision}, token)
	requireStatus(t, rec, http.StatusForbidden)
	rec = doRequest(e, http.MethodPatch, "/api/v1/bare-metal-hosts/node1", map[string]any{"pool": "cilium-cluster", "revision": revision}, admin)
	requireStatus(t, rec, http.StatusOK)
	updated := parseBody(t, rec)
	if updated["pool"] != "cilium-cluster" {
		t.Fatalf("pool was not updated: %v", updated)
	}
	rec = doRequest(e, http.MethodPatch, "/api/v1/bare-metal-hosts/node1", map[string]any{"pool": "stale", "revision": revision}, admin)
	requireStatus(t, rec, http.StatusConflict)
	revision = int64(updated["revision"].(float64))
	rec = doRequest(e, http.MethodPatch, "/api/v1/bare-metal-hosts/node1", map[string]any{"pool": "k8scluster02", "revision": revision}, admin)
	requireStatus(t, rec, http.StatusOK)
	for i := 0; i < 2; i++ {
		rec = doRequest(e, http.MethodPost, "/api/v1/bare-metal-claims", map[string]any{"pool": "k8scluster02", "owner": "capi-test"}, token)
		requireStatus(t, rec, http.StatusOK)
		body := parseBody(t, rec)
		if body["name"] != "node1" || body["state"] != "Claimed" {
			t.Fatalf("bad claim: %v", body)
		}
	}
	rec = doRequest(e, http.MethodGet, "/api/v1/bare-metal-claims/capi-test", nil, token)
	requireStatus(t, rec, http.StatusOK)
	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/machines/node1:redeploy"},
		{http.MethodPatch, "/api/v1/machines/node1/network"},
		{http.MethodPatch, "/api/v1/machines/node1/settings"},
		{http.MethodDelete, "/api/v1/machines/node1"},
	} {
		rec = doRequest(e, route.method, route.path, nil, admin)
		requireStatus(t, rec, http.StatusConflict)
	}
	for _, route := range []struct {
		path   string
		action power.Action
	}{
		{path: "/api/v1/machines/node1:powerOff", action: power.ActionPowerOff},
		{path: "/api/v1/machines/node1:powerOn", action: power.ActionPowerOn},
	} {
		rec = doRequest(e, http.MethodPost, route.path, nil, admin)
		requireStatus(t, rec, http.StatusOK)
		if got := powerExecutor.actions[len(powerExecutor.actions)-1]; got != route.action {
			t.Fatalf("expected %s, got %s", route.action, got)
		}
	}
	rec = doRequest(e, http.MethodPost, "/api/v1/service-accounts", map[string]any{"name": "reader", "role": "viewer"}, admin)
	requireStatus(t, rec, http.StatusCreated)
	viewer := parseBody(t, rec)["token"].(string)
	rec = doRequest(e, http.MethodPost, "/api/v1/bare-metal-claims", map[string]any{"pool": "k8scluster02", "owner": "capi-viewer"}, viewer)
	requireStatus(t, rec, http.StatusForbidden)
	rec = doRequest(e, http.MethodGet, "/api/v1/bare-metal-hosts/node1", nil, "")
	requireStatus(t, rec, http.StatusUnauthorized)
}
