package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/sugaf1204/gomi/internal/auth"
	infraapi "github.com/sugaf1204/gomi/internal/infra/api"
	"github.com/sugaf1204/gomi/internal/infra/dns"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/subnet"
)

func TestDNSRecordsRequireEmbeddedMode(t *testing.T) {
	env := setupTestEnv(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dns-records", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	rec := httptest.NewRecorder()
	env.echo.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDNSRecordAPIManagesEmbeddedRecords(t *testing.T) {
	env := setupDNSTestEnv(t)

	body := bytes.NewBufferString(`{"name":"app.lab.local","type":"A","ttl":60,"values":["10.0.0.50"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/dns-records", body)
	req.Header.Set("Authorization", "Bearer "+env.token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.echo.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/dns-records", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	rec = httptest.NewRecorder()
	env.echo.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var list struct {
		Items []dns.DynamicRecord `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 1 || list.Items[0].Name != "app.lab.local." || list.Items[0].Values[0] != "10.0.0.50" {
		t.Fatalf("unexpected list response: %#v", list.Items)
	}

	body = bytes.NewBufferString(`{"ttl":120,"values":["10.0.0.51"]}`)
	req = httptest.NewRequest(http.MethodPut, "/api/v1/dns-records/app.lab.local./A", body)
	req.Header.Set("Authorization", "Bearer "+env.token)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	env.echo.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/v1/dns-records/app.lab.local./A", nil)
	req.Header.Set("Authorization", "Bearer "+env.token)
	rec = httptest.NewRecorder()
	env.echo.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDNSRecordAPIRejectsInvalidRecord(t *testing.T) {
	env := setupDNSTestEnv(t)

	body := bytes.NewBufferString(`{"name":"app.lab.local","type":"A","values":["not-an-ip"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/dns-records", body)
	req.Header.Set("Authorization", "Bearer "+env.token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.echo.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func setupDNSTestEnv(t *testing.T) testEnv {
	t.Helper()

	backend := memory.New()
	authStore := backend.Auth()
	createUser(t, authStore, "admin", "adminpass", auth.RoleAdmin)
	adminToken := createSession(t, authStore, "admin")

	ctx := context.Background()
	now := time.Now().UTC()
	if err := backend.Subnets().Upsert(ctx, subnet.Subnet{
		Name:      "lab",
		Spec:      subnet.SubnetSpec{CIDR: "10.0.0.0/24", DomainName: "lab.local"},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	dnsServer := dns.NewEmbeddedServer(dns.EmbeddedConfig{
		Addr:               ":0",
		TTL:                300 * time.Second,
		DynamicRecordsPath: filepath.Join(t.TempDir(), "dns-records.json"),
		Machines:           backend.Machines(),
		VMs:                backend.VMs(),
		Subnets:            backend.Subnets(),
	})
	if err := dnsServer.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	srv := infraapi.NewServer(infraapi.ServerConfig{
		AuthStore:   authStore,
		AuthService: infraapi.NewAuthService(authStore, time.Hour),
		DNSRecords:  dnsServer,
	})
	return testEnv{echo: srv.Echo(), token: adminToken, authStore: authStore}
}
