package api_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
	infraapi "github.com/sugaf1204/gomi/internal/infra/api"
	"github.com/sugaf1204/gomi/internal/infra/clusterapi"
	"github.com/sugaf1204/gomi/internal/infra/memory"
)

type fakeClusterAPI struct {
	calls  int
	change clusterapi.Change
	err    error
}

func (f *fakeClusterAPI) Snapshot(context.Context) (clusterapi.Snapshot, error) {
	return clusterapi.Snapshot{Configured: true, Resources: []clusterapi.Resource{}}, f.err
}
func (f *fakeClusterAPI) Change(_ context.Context, _, _, _ string, c clusterapi.Change) (clusterapi.Resource, error) {
	f.calls++
	f.change = c
	return clusterapi.Resource{Name: "demo"}, f.err
}

func TestClusterAPIAccessAndAudit(t *testing.T) {
	for _, role := range []auth.Role{auth.RoleViewer, auth.RoleOperator, auth.RoleAdmin} {
		t.Run(string(role), func(t *testing.T) {
			backend := memory.New()
			store := backend.Auth()
			createUser(t, store, "person", "password", role)
			token := createSession(t, store, "person")
			manager := &fakeClusterAPI{}
			e := infraapi.NewServer(infraapi.ServerConfig{AuthStore: store, AuthService: infraapi.NewAuthService(store, time.Hour), ClusterAPI: manager}).Echo()
			requireStatus(t, doRequest(e, http.MethodGet, "/api/v1/cluster-api", nil, ""), http.StatusUnauthorized)
			requireStatus(t, doRequest(e, http.MethodGet, "/api/v1/cluster-api", nil, token), http.StatusOK)
			result := doRequest(e, http.MethodPatch, "/api/v1/cluster-api/lab/clusters/demo", map[string]interface{}{"apiVersion": "cluster.x-k8s.io/v1beta2", "uid": "uid", "resourceVersion": "7", "paused": true}, token)
			if role == auth.RoleViewer {
				requireStatus(t, result, http.StatusForbidden)
				if manager.calls != 0 {
					t.Fatal("viewer reached management cluster")
				}
				return
			}
			requireStatus(t, result, http.StatusOK)
			if manager.change.Paused == nil || !*manager.change.Paused {
				t.Fatal("change lost")
			}
			events, err := store.ListAuditEvents(context.Background(), "", 10)
			if err != nil || len(events) != 1 || events[0].Action != "cluster-api.update" {
				t.Fatalf("%+v %v", events, err)
			}
			manager.err = clusterapi.ErrConflict
			requireStatus(t, doRequest(e, http.MethodPatch, "/api/v1/cluster-api/lab/clusters/demo", map[string]interface{}{"paused": false}, token), http.StatusConflict)
			events, _ = store.ListAuditEvents(context.Background(), "", 10)
			if len(events) != 2 {
				t.Fatal("failed attempt not audited")
			}
			manager.err = errors.New("secret server connection detail")
			result = doRequest(e, http.MethodGet, "/api/v1/cluster-api", nil, token)
			requireStatus(t, result, http.StatusBadGateway)
			if strings.Contains(result.Body.String(), "secret server connection detail") {
				t.Fatal("raw upstream details exposed")
			}
		})
	}
}
func TestClusterAPIOptInAndStrictBody(t *testing.T) {
	backend := memory.New()
	store := backend.Auth()
	createUser(t, store, "person", "password", auth.RoleAdmin)
	token := createSession(t, store, "person")
	e := infraapi.NewServer(infraapi.ServerConfig{AuthStore: store, AuthService: infraapi.NewAuthService(store, time.Hour)}).Echo()
	result := doRequest(e, http.MethodGet, "/api/v1/cluster-api", nil, token)
	requireStatus(t, result, http.StatusOK)
	if result.Body.String() == "" {
		t.Fatal("missing disabled response")
	}
	requireStatus(t, doRequest(e, http.MethodPatch, "/api/v1/cluster-api/lab/clusters/demo", map[string]interface{}{"paused": true}, token), http.StatusServiceUnavailable)
	manager := &fakeClusterAPI{}
	e = infraapi.NewServer(infraapi.ServerConfig{AuthStore: store, AuthService: infraapi.NewAuthService(store, time.Hour), ClusterAPI: manager}).Echo()
	requireStatus(t, doRequest(e, http.MethodPatch, "/api/v1/cluster-api/lab/clusters/demo", map[string]interface{}{"spec": map[string]interface{}{"arbitrary": "value"}}, token), http.StatusBadRequest)
	if manager.calls != 0 {
		t.Fatal("arbitrary body reached Kubernetes")
	}
}
