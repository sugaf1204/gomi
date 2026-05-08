package api_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	"github.com/sugaf1204/gomi/internal/auth"
	"github.com/sugaf1204/gomi/internal/bootenv"
	"github.com/sugaf1204/gomi/internal/cloudinit"
	"github.com/sugaf1204/gomi/internal/discovery"
	"github.com/sugaf1204/gomi/internal/hwinfo"
	"github.com/sugaf1204/gomi/internal/hypervisor"
	infraapi "github.com/sugaf1204/gomi/internal/infra/api"
	"github.com/sugaf1204/gomi/internal/infra/memory"
	"github.com/sugaf1204/gomi/internal/machine"
	"github.com/sugaf1204/gomi/internal/osimage"
	"github.com/sugaf1204/gomi/internal/power"
	"github.com/sugaf1204/gomi/internal/sshkey"
	"github.com/sugaf1204/gomi/internal/vm"
)

// testEnv bundles the echo instance, a single admin token, and the auth store.
type testEnv struct {
	echo      *echo.Echo
	token     string
	authStore auth.Store
	machines  *machine.Service
}

// setupTestEnv creates a fully wired Server with in-memory backend and
// an auth session for an admin user.
func setupTestEnv(t *testing.T) testEnv {
	return setupTestEnvWithPowerExecutor(t, nil)
}

func setupTestEnvWithPowerExecutor(t *testing.T, powerExecutor infraapi.PowerExecutor) testEnv {
	return setupTestEnvWithOptions(t, powerExecutor, true)
}

func setupFirstRunTestEnv(t *testing.T) testEnv {
	return setupTestEnvWithOptions(t, nil, false)
}

func setupTestEnvWithOptions(t *testing.T, powerExecutor infraapi.PowerExecutor, withAdmin bool) testEnv {
	t.Helper()

	backend := memory.New()

	machineSvc := machine.NewService(backend.Machines())
	hypervisorSvc := hypervisor.NewService(backend.Hypervisors(), backend.HypervisorTokens(), backend.AgentTokens())
	vmSvc := vm.NewService(backend.VMs())
	cloudInitSvc := cloudinit.NewService(backend.CloudInits())
	osimageSvc := osimage.NewService(backend.OSImages())
	sshkeySvc := sshkey.NewService(backend.SSHKeys())
	hwinfoSvc := hwinfo.NewService(backend.HWInfo())
	discoverySvc := discovery.NewService(backend.Machines())

	authStore := backend.Auth()
	authService := infraapi.NewAuthService(authStore, 1*time.Hour)

	adminToken := ""
	if withAdmin {
		createUser(t, authStore, "admin", "adminpass", auth.RoleAdmin)
		adminToken = createSession(t, authStore, "admin")
	}

	srv := infraapi.NewServer(infraapi.ServerConfig{
		Machines:        machineSvc,
		PowerExecutor:   powerExecutor,
		Subnets:         backend.Subnets(),
		AuthStore:       authStore,
		AuthService:     authService,
		Discovery:       discoverySvc,
		SSHKeys:         sshkeySvc,
		HWInfo:          hwinfoSvc,
		Hypervisors:     hypervisorSvc,
		AgentTokenStore: backend.AgentTokens(),
		VMs:             vmSvc,
		CloudInits:      cloudInitSvc,
		OSImages:        osimageSvc,
		FilesDir:        t.TempDir(),
		ImageStorageDir: t.TempDir(),
		HealthCheck:     nil,
		VMRuntimeDeleter: func(context.Context, vm.VirtualMachine) error {
			return nil
		},
		BootEnvs: bootenv.NewManager(bootenv.Config{
			DataDir:  t.TempDir(),
			FilesDir: t.TempDir(),
		}),
	})

	return testEnv{
		echo:      srv.Echo(),
		token:     adminToken,
		authStore: authStore,
		machines:  machineSvc,
	}
}

type recordingPowerExecutor struct {
	calls chan power.Action
	infos chan power.MachineInfo
}

func newRecordingPowerExecutor() *recordingPowerExecutor {
	return &recordingPowerExecutor{
		calls: make(chan power.Action, 4),
		infos: make(chan power.MachineInfo, 4),
	}
}

func (r *recordingPowerExecutor) Execute(_ context.Context, mi power.MachineInfo, action power.Action) error {
	r.calls <- action
	r.infos <- mi
	return nil
}

func (r *recordingPowerExecutor) CheckStatus(_ context.Context, _ power.MachineInfo) (power.PowerState, error) {
	return power.PowerStateStopped, nil
}

func (r *recordingPowerExecutor) ConfigureBootOrder(_ context.Context, _ power.MachineInfo, _ power.BootOrder) error {
	return nil
}

func waitPowerAction(t *testing.T, calls <-chan power.Action) power.Action {
	t.Helper()
	select {
	case action := <-calls:
		return action
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for power action")
		return ""
	}
}

func waitPowerInfo(t *testing.T, infos <-chan power.MachineInfo) power.MachineInfo {
	t.Helper()
	select {
	case info := <-infos:
		return info
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for power info")
		return power.MachineInfo{}
	}
}

func createUser(t *testing.T, store auth.Store, username, password string, role auth.Role) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt hash: %v", err)
	}
	if err := store.UpsertUser(context.Background(), auth.User{
		Username:     username,
		PasswordHash: string(hash),
		Role:         role,
		CreatedAt:    time.Now().UTC(),
	}); err != nil {
		t.Fatalf("upsert user %s: %v", username, err)
	}
}

func createSession(t *testing.T, store auth.Store, username string) string {
	t.Helper()
	token := "test-token-" + username
	now := time.Now().UTC()
	if err := store.CreateSession(context.Background(), auth.Session{
		Token:     token,
		Username:  username,
		CreatedAt: now,
		ExpiresAt: now.Add(1 * time.Hour),
	}); err != nil {
		t.Fatalf("create session for %s: %v", username, err)
	}
	return token
}

// doRequest performs a JSON HTTP request against the echo router.
func doRequest(e *echo.Echo, method, path string, body any, token string) *httptest.ResponseRecorder {
	var bodyReader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// parseBody parses the JSON response body into a map.
func parseBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("parse JSON body: %v (body: %s)", err, rec.Body.String())
	}
	return m
}

// requireStatus checks that the response has the expected status code.
func requireStatus(t *testing.T, rec *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if rec.Code != expected {
		t.Fatalf("expected status %d, got %d; body: %s", expected, rec.Code, rec.Body.String())
	}
}

func signTestPowerEvent(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// ---------------------------------------------------------------------------
// Hypervisor CRUD Tests
// ---------------------------------------------------------------------------

func TestHypervisorCRUD(t *testing.T) {
	env := setupTestEnv(t)

	// POST /api/v1/hypervisors - Create
	hvBody := map[string]any{
		"name": "hv-01",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
			"port": 16509,
		},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", hvBody, env.token)
	requireStatus(t, rec, http.StatusCreated)
	created := parseBody(t, rec)
	if created["name"] != "hv-01" {
		t.Fatalf("expected name hv-01, got %v", created["name"])
	}

	// GET /api/v1/hypervisors - List
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 hypervisor, got %v", body["items"])
	}

	// GET /api/v1/hypervisors/hv-01 - Get
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors/hv-01", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	got := parseBody(t, rec)
	if got["name"] != "hv-01" {
		t.Fatalf("expected name hv-01, got %v", got["name"])
	}

	// DELETE /api/v1/hypervisors/hv-01 - Delete
	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/hypervisors/hv-01", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)

	// GET after delete - should 404
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors/hv-01", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// Hypervisor Registration Flow Tests
// ---------------------------------------------------------------------------

func TestHypervisorRegistration(t *testing.T) {
	env := setupTestEnv(t)

	// Create registration token.
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/registration-tokens", nil, env.token)
	requireStatus(t, rec, http.StatusCreated)
	tokenBody := parseBody(t, rec)
	regToken, ok := tokenBody["token"].(string)
	if !ok || regToken == "" {
		t.Fatalf("expected non-empty token, got %v", tokenBody)
	}

	// Register hypervisor with valid token (unauthenticated endpoint).
	regReq := map[string]any{
		"token":    regToken,
		"hostname": "hv-registered-01",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
			"port": 16509,
		},
		"capacity": map[string]any{
			"cpuCores": 8,
			"memoryMB": 16384,
		},
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/register", regReq, "")
	requireStatus(t, rec, http.StatusCreated)
	registered := parseBody(t, rec)
	regHV, _ := registered["hypervisor"].(map[string]any)
	if regHV["name"] != "hv-registered-01" {
		t.Fatalf("expected name hv-registered-01, got %v", regHV["name"])
	}

	// Register with same token again - should fail (token already used).
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/register", regReq, "")
	requireStatus(t, rec, http.StatusBadRequest)
	errBody := parseBody(t, rec)
	if _, hasErr := errBody["error"]; !hasErr {
		t.Fatalf("expected error in response body for used token")
	}

	// Register with invalid token - should fail.
	regReq["token"] = "definitely-not-valid"
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors/register", regReq, "")
	requireStatus(t, rec, http.StatusBadRequest)
}

// ---------------------------------------------------------------------------
// VirtualMachine CRUD Tests
// ---------------------------------------------------------------------------

func TestVirtualMachineCRUD(t *testing.T) {
	env := setupTestEnv(t)

	// Create a hypervisor first (prerequisite).
	hvBody := map[string]any{
		"name": "hv-for-vm",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
		},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", hvBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// Create an OS image prerequisite for PXE install type resolution.
	imgBody := map[string]any{
		"name":      "ubuntu-22.04",
		"osFamily":  "ubuntu",
		"osVersion": "22.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/os-images", imgBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// POST /api/v1/virtual-machines - Create VM
	vmBody := map[string]any{
		"name":          "vm-01",
		"hypervisorRef": "hv-for-vm",
		"resources": map[string]any{
			"cpuCores": 2,
			"memoryMB": 4096,
			"diskGB":   40,
		},
		"osImageRef": "ubuntu-22.04",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmBody, env.token)
	requireStatus(t, rec, http.StatusCreated)
	created := parseBody(t, rec)
	if created["name"] != "vm-01" {
		t.Fatalf("expected name vm-01, got %v", created["name"])
	}
	installCfg, _ := created["installConfig"].(map[string]any)
	if installCfg["type"] != "curtin" {
		t.Fatalf("expected installConfig.type=curtin for ubuntu image, got %v", installCfg["type"])
	}
	provisioning, _ := created["provisioning"].(map[string]any)
	if active, _ := provisioning["active"].(bool); !active {
		t.Fatalf("expected provisioning.active=true, got %v", provisioning["active"])
	}
	if token, _ := provisioning["completionToken"].(string); strings.TrimSpace(token) == "" {
		t.Fatalf("expected provisioning.completionToken to be set, got %v", provisioning["completionToken"])
	}

	// POST - Create VM referencing non-existent hypervisor
	badVMBody := map[string]any{
		"name":          "vm-bad-ref",
		"hypervisorRef": "nonexistent-hv",
		"resources": map[string]any{
			"cpuCores": 1,
			"memoryMB": 1024,
			"diskGB":   10,
		},
		"osImageRef": "ubuntu-22.04",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", badVMBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)

	// GET /api/v1/virtual-machines - List
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 vm, got %v", body["items"])
	}

	// GET /api/v1/virtual-machines/vm-01 - Get
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines/vm-01", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	got := parseBody(t, rec)
	if got["name"] != "vm-01" {
		t.Fatalf("expected name vm-01, got %v", got["name"])
	}

	// POST /api/v1/virtual-machines/vm-01/actions/power-on - Power action
	// This will fail at the libvirt level (no real hypervisor), but should not 403
	// and should not panic. Expect an internal server error since there's no SSH key.
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines/vm-01/actions/power-on", nil, env.token)
	// Accept 500 (libvirt/SSH failure) but NOT 403/401
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
		t.Fatalf("authenticated user should be allowed for power-on, got status %d", rec.Code)
	}

	// POST /api/v1/virtual-machines/vm-01/actions/redeploy
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines/vm-01/actions/redeploy", map[string]any{"confirm": "vm-01"}, env.token)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized || rec.Code == http.StatusNotFound {
		t.Fatalf("redeploy route should be reachable, got status %d", rec.Code)
	}

	// POST /api/v1/virtual-machines/vm-01/actions/reinstall - legacy route should remain compatible
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines/vm-01/actions/reinstall", map[string]any{"confirm": "vm-01"}, env.token)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized || rec.Code == http.StatusNotFound {
		t.Fatalf("reinstall route should be reachable, got status %d", rec.Code)
	}

	// DELETE /api/v1/virtual-machines/vm-01 - Delete
	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/virtual-machines/vm-01", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)

	// Verify deleted
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines/vm-01", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// CloudInitTemplate CRUD Tests
// ---------------------------------------------------------------------------

func TestCloudInitTemplateCRUD(t *testing.T) {
	env := setupTestEnv(t)

	// POST /api/v1/cloud-init-templates - Create
	ciBody := map[string]any{
		"name":        "ci-basic",
		"userData":    "#cloud-config\npackages:\n  - vim\n",
		"description": "basic cloud-init template",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", ciBody, env.token)
	requireStatus(t, rec, http.StatusCreated)
	created := parseBody(t, rec)
	if created["name"] != "ci-basic" {
		t.Fatalf("expected name ci-basic, got %v", created["name"])
	}

	// GET /api/v1/cloud-init-templates - List
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/cloud-init-templates", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 cloud-init template, got %v", body["items"])
	}

	// GET /api/v1/cloud-init-templates/ci-basic - Get
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/cloud-init-templates/ci-basic", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	got := parseBody(t, rec)
	if got["userData"] != "#cloud-config\npackages:\n  - vim\n" {
		t.Fatalf("unexpected userData: %v", got["userData"])
	}

	// PUT /api/v1/cloud-init-templates/ci-basic - Update
	updateBody := map[string]any{
		"userData":    "#cloud-config\npackages:\n  - vim\n  - curl\n",
		"description": "updated cloud-init template",
	}
	rec = doRequest(env.echo, http.MethodPut, "/api/v1/cloud-init-templates/ci-basic", updateBody, env.token)
	requireStatus(t, rec, http.StatusOK)
	updated := parseBody(t, rec)
	if updated["userData"] != "#cloud-config\npackages:\n  - vim\n  - curl\n" {
		t.Fatalf("unexpected updated userData: %v", updated["userData"])
	}

	// DELETE /api/v1/cloud-init-templates/ci-basic - Delete
	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/cloud-init-templates/ci-basic", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)

	// Verify deleted
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/cloud-init-templates/ci-basic", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// OSImage CRUD Tests
// ---------------------------------------------------------------------------

func TestOSImageCRUD(t *testing.T) {
	env := setupTestEnv(t)

	// POST /api/v1/os-images - Create
	imgBody := map[string]any{
		"name":      "ubuntu-22.04",
		"osFamily":  "ubuntu",
		"osVersion": "22.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", imgBody, env.token)
	requireStatus(t, rec, http.StatusCreated)
	created := parseBody(t, rec)
	if created["name"] != "ubuntu-22.04" {
		t.Fatalf("expected name ubuntu-22.04, got %v", created["name"])
	}

	// GET /api/v1/os-images - List
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/os-images", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 os-image, got %v", body["items"])
	}

	// GET /api/v1/os-images/ubuntu-22.04 - Get
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/os-images/ubuntu-22.04", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	got := parseBody(t, rec)
	if got["osFamily"] != "ubuntu" {
		t.Fatalf("expected osFamily ubuntu, got %v", got["osFamily"])
	}

	// DELETE /api/v1/os-images/ubuntu-22.04 - Delete
	rec = doRequest(env.echo, http.MethodDelete, "/api/v1/os-images/ubuntu-22.04", nil, env.token)
	requireStatus(t, rec, http.StatusNoContent)

	// Verify deleted
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/os-images/ubuntu-22.04", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestCreateURLOSImageDownloadsToServerStorage(t *testing.T) {
	env := setupTestEnv(t)
	content := []byte("fake image bytes")
	sum := sha256.Sum256(content)
	checksum := hex.EncodeToString(sum[:])
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/debian.raw" {
			http.NotFound(w, r)
			return
		}
		w.Write(content)
	}))
	defer srv.Close()

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", map[string]any{
		"name":      "debian-url",
		"osFamily":  "debian",
		"osVersion": "13",
		"arch":      "amd64",
		"format":    "raw",
		"source":    "url",
		"url":       srv.URL + "/images/debian.raw",
		"checksum":  "sha256:" + checksum,
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)
	created := parseBody(t, rec)
	if ready, _ := created["ready"].(bool); !ready {
		t.Fatalf("expected ready URL image, got %v", created)
	}
	localPath, _ := created["localPath"].(string)
	if strings.TrimSpace(localPath) == "" {
		t.Fatalf("expected localPath, got %v", created)
	}
	data, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read downloaded image: %v", err)
	}
	if string(data) != string(content) {
		t.Fatalf("unexpected downloaded content: %q", data)
	}
}

// ---------------------------------------------------------------------------
// Auth Tests: Unauthenticated and Invalid Token (still enforced)
// ---------------------------------------------------------------------------

func TestAuth_Unauthenticated(t *testing.T) {
	env := setupTestEnv(t)

	// Unauthenticated request to GET /api/v1/hypervisors - should 401
	rec := doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors", nil, "")
	requireStatus(t, rec, http.StatusUnauthorized)
}

func TestAuth_InvalidToken(t *testing.T) {
	env := setupTestEnv(t)

	// Request with invalid token
	rec := doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors", nil, "invalid-token-abc")
	requireStatus(t, rec, http.StatusUnauthorized)
}

// ---------------------------------------------------------------------------
// Error Case Tests
// ---------------------------------------------------------------------------

func TestError_GetNonexistentVM(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines/nonexistent", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestError_GetNonexistentHypervisor(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors/nonexistent", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestError_GetNonexistentCloudInit(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/cloud-init-templates/nonexistent", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestError_GetNonexistentOSImage(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/os-images/nonexistent", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestError_CreateHypervisorInvalidBody(t *testing.T) {
	env := setupTestEnv(t)

	// Send completely invalid JSON
	req := httptest.NewRequest(http.MethodPost, "/api/v1/hypervisors", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.token)
	rec := httptest.NewRecorder()
	env.echo.ServeHTTP(rec, req)
	requireStatus(t, rec, http.StatusBadRequest)
}

func TestError_CreateHypervisorMissingName(t *testing.T) {
	env := setupTestEnv(t)

	// Missing name (only has connection)
	hvBody := map[string]any{
		"connection": map[string]any{
			"host": "127.0.0.1",
		},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", hvBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)
}

func TestError_CreateHypervisorMissingHost(t *testing.T) {
	env := setupTestEnv(t)

	// Missing connection host
	hvBody := map[string]any{
		"name":       "hv-no-host",
		"connection": map[string]any{},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", hvBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)
}

func TestError_CreateVMAutoPlacementNoHypervisors(t *testing.T) {
	env := setupTestEnv(t)

	// No hypervisorRef and no hypervisors exist -> auto-placement fails.
	vmBody := map[string]any{
		"name": "vm-no-ref",
		"resources": map[string]any{
			"cpuCores": 1,
			"memoryMB": 1024,
			"diskGB":   10,
		},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)
	body := parseBody(t, rec)
	errMsg, _ := body["error"].(string)
	if errMsg == "" {
		t.Fatal("expected error message for auto-placement with no hypervisors")
	}
}

func TestError_CreateCloudInitMissingUserData(t *testing.T) {
	env := setupTestEnv(t)

	ciBody := map[string]any{
		"name":        "ci-no-data",
		"description": "no userData",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", ciBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)
}

func TestError_CreateOSImageMissingFields(t *testing.T) {
	env := setupTestEnv(t)

	// Missing osFamily and osVersion
	imgBody := map[string]any{
		"name": "img-bad",
		"arch": "amd64",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", imgBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)
}

func TestError_DeleteNonexistentHypervisor(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/hypervisors/nonexistent", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestError_DeleteNonexistentVM(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/virtual-machines/nonexistent", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestError_DeleteNonexistentCloudInit(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/cloud-init-templates/nonexistent", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestError_DeleteNonexistentOSImage(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodDelete, "/api/v1/os-images/nonexistent", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

// ---------------------------------------------------------------------------
// Auth Login/Logout Tests
// ---------------------------------------------------------------------------

func TestAuthLoginLogout(t *testing.T) {
	env := setupTestEnv(t)

	// Login with valid credentials
	loginBody := map[string]any{
		"username": "admin",
		"password": "adminpass",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/auth/login", loginBody, "")
	requireStatus(t, rec, http.StatusOK)
	loginResp := parseBody(t, rec)
	loginToken, ok := loginResp["token"].(string)
	if !ok || loginToken == "" {
		t.Fatalf("expected non-empty token from login, got %v", loginResp)
	}

	// Use the token to access a protected endpoint
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/me", nil, loginToken)
	requireStatus(t, rec, http.StatusOK)
	meResp := parseBody(t, rec)
	if meResp["username"] != "admin" {
		t.Fatalf("expected username admin from /me, got %v", meResp["username"])
	}

	// Logout
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/auth/logout", nil, loginToken)
	requireStatus(t, rec, http.StatusNoContent)

	// Token should now be invalid
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/me", nil, loginToken)
	requireStatus(t, rec, http.StatusUnauthorized)
}

func TestAuthLoginInvalidCredentials(t *testing.T) {
	env := setupTestEnv(t)

	loginBody := map[string]any{
		"username": "admin",
		"password": "wrongpassword",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/auth/login", loginBody, "")
	requireStatus(t, rec, http.StatusUnauthorized)
}

func TestAuthLoginNonexistentUser(t *testing.T) {
	env := setupTestEnv(t)

	loginBody := map[string]any{
		"username": "nobody",
		"password": "nopass",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/auth/login", loginBody, "")
	requireStatus(t, rec, http.StatusUnauthorized)
}

func TestSetupStatusRequiresFirstAdminWhenNoUsersExist(t *testing.T) {
	env := setupFirstRunTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/setup/status", nil, "")
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	if body["required"] != true {
		t.Fatalf("expected setup required, got %v", body["required"])
	}
}

func TestSetupAdminCreatesFirstAdmin(t *testing.T) {
	env := setupFirstRunTestEnv(t)

	setupBody := map[string]any{
		"username": "owner",
		"password": "secret123",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/setup/admin", setupBody, "")
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/setup/status", nil, "")
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	if body["required"] != false {
		t.Fatalf("expected setup not required after admin creation, got %v", body["required"])
	}

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/auth/login", setupBody, "")
	requireStatus(t, rec, http.StatusOK)
}

func TestSetupAdminRejectedAfterUserExists(t *testing.T) {
	env := setupTestEnv(t)

	setupBody := map[string]any{
		"username": "owner",
		"password": "secret123",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/setup/admin", setupBody, "")
	requireStatus(t, rec, http.StatusConflict)
}

// ---------------------------------------------------------------------------
// Health Check Tests
// ---------------------------------------------------------------------------

func TestHealthCheck(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/healthz", nil, "")
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	if body["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", body["status"])
	}
}

// ---------------------------------------------------------------------------
// PowerOnVM for nonexistent VM returns 404
// ---------------------------------------------------------------------------

func TestPowerOnNonexistentVM(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines/nonexistent/actions/power-on", nil, env.token)
	requireStatus(t, rec, http.StatusNotFound)
}

func TestReinstallPXEVM_UpdatesInstallConfigAndCloudInitRef(t *testing.T) {
	env := setupTestEnv(t)

	hvBody := map[string]any{
		"name": "hv-redeploy",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
		},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", hvBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	imgBody := map[string]any{
		"name":      "debian-13-amd64",
		"osFamily":  "debian",
		"osVersion": "13",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/os-images", imgBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	vmBody := map[string]any{
		"name":          "vm-redeploy",
		"hypervisorRef": "hv-redeploy",
		"resources": map[string]any{
			"cpuCores": 2,
			"memoryMB": 4096,
			"diskGB":   40,
		},
		"osImageRef": "debian-13-amd64",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmBody, env.token)
	requireStatus(t, rec, http.StatusCreated)
	createdVM := parseBody(t, rec)
	createdInstallCfg, _ := createdVM["installConfig"].(map[string]any)
	if createdInstallCfg["type"] != "curtin" {
		t.Fatalf("expected Debian 13 VM installConfig.type=curtin, got %v", createdInstallCfg["type"])
	}

	ciBody := map[string]any{
		"name":     "ci-reinstall",
		"userData": "#cloud-config\nhostname: reinstall\n",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", ciBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/subnets", map[string]any{
		"name": "vm-redeploy-net",
		"spec": map[string]any{
			"cidr":         "192.168.30.0/24",
			"dnsServers":   []string{"8.8.8.8"},
			"pxeInterface": "br-lab",
		},
	}, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines/vm-redeploy/actions/redeploy", map[string]any{"confirm": "vm-redeploy"}, env.token)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized || rec.Code == http.StatusNotFound {
		t.Fatalf("redeploy route should exist and be writable, got status %d", rec.Code)
	}

	reinstallBody := map[string]any{
		"confirm":      "vm-redeploy",
		"cloudInitRef": "ci-reinstall",
		"installConfig": map[string]any{
			"type":   "preseed",
			"inline": "d-i passwd/username string redeploy-user",
		},
		"subnetRef":    "vm-redeploy-net",
		"ipAssignment": "static",
		"ip":           "192.168.30.77",
		"network": []map[string]any{
			{
				"name":      "default",
				"bridge":    "br-lab",
				"network":   "vm-redeploy-net",
				"ipAddress": "192.168.30.77",
			},
		},
		"advancedOptions": map[string]any{
			"cpuMode":    "host-passthrough",
			"diskDriver": "scsi",
			"ioThreads":  2,
		},
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines/vm-redeploy/actions/reinstall", reinstallBody, env.token)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized || rec.Code == http.StatusNotFound {
		t.Fatalf("legacy reinstall route should exist and be writable, got status %d", rec.Code)
	}

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines/vm-redeploy", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	installCfg, _ := body["installConfig"].(map[string]any)
	if installCfg["type"] != "curtin" {
		t.Fatalf("expected installConfig.type=curtin, got %v", installCfg["type"])
	}
	inline, _ := installCfg["inline"].(string)
	if !strings.Contains(inline, "redeploy-user") {
		t.Fatalf("expected inline install config to be preserved, got %q", inline)
	}
	cloudInitRefs, _ := body["cloudInitRefs"].([]any)
	if len(cloudInitRefs) == 0 || cloudInitRefs[0] != "ci-reinstall" {
		t.Fatalf("expected cloudInitRefs first item to be ci-reinstall, got %v", cloudInitRefs)
	}
	if body["lastDeployedCloudInitRef"] != "ci-reinstall" {
		t.Fatalf("expected lastDeployedCloudInitRef=ci-reinstall, got %v", body["lastDeployedCloudInitRef"])
	}
	if body["subnetRef"] != "vm-redeploy-net" {
		t.Fatalf("expected subnetRef=vm-redeploy-net, got %v", body["subnetRef"])
	}
	if body["ipAssignment"] != "static" {
		t.Fatalf("expected ipAssignment=static, got %v", body["ipAssignment"])
	}
	advancedOptions, _ := body["advancedOptions"].(map[string]any)
	if advancedOptions["cpuMode"] != "host-passthrough" {
		t.Fatalf("expected advancedOptions.cpuMode=host-passthrough, got %v", advancedOptions["cpuMode"])
	}
	if advancedOptions["diskDriver"] != "scsi" {
		t.Fatalf("expected advancedOptions.diskDriver=scsi, got %v", advancedOptions["diskDriver"])
	}
	if advancedOptions["ioThreads"] != float64(2) {
		t.Fatalf("expected advancedOptions.ioThreads=2, got %v", advancedOptions["ioThreads"])
	}
	network, _ := body["network"].([]any)
	if len(network) == 0 {
		t.Fatalf("expected redeploy to keep network config, got %v", body["network"])
	}
	nic, _ := network[0].(map[string]any)
	mac, _ := nic["mac"].(string)
	if !strings.HasPrefix(mac, "52:54:00:") {
		t.Fatalf("expected redeploy to persist a generated KVM MAC, got %q", mac)
	}

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/audit-events?machine=vm-redeploy", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	auditBody := parseBody(t, rec)
	items, _ := auditBody["items"].([]any)
	found := false
	for _, item := range items {
		event, _ := item.(map[string]any)
		if event["action"] == "redeploy-vm" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected redeploy-vm audit event, got %v", items)
	}
}

func TestPXENocloudUserData_FallsBackToCloudInitTemplate(t *testing.T) {
	env := setupTestEnv(t)

	hvBody := map[string]any{
		"name": "hv-pxe-fallback",
		"connection": map[string]any{
			"type": "tcp",
			"host": "127.0.0.1",
		},
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", hvBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	imgBody := map[string]any{
		"name":      "ubuntu-pxe-fallback",
		"osFamily":  "ubuntu",
		"osVersion": "24.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/os-images", imgBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	ciBody := map[string]any{
		"name":     "ci-pxe-fallback",
		"userData": "#cloud-config\nusers:\n  - name: fallback-user\n",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", ciBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	ciBody = map[string]any{
		"name":     "ci-pxe-priority",
		"userData": "#cloud-config\nusers:\n  - name: priority-user\n",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", ciBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	vmBody := map[string]any{
		"name":          "vm-pxe-fallback",
		"hypervisorRef": "hv-pxe-fallback",
		"resources": map[string]any{
			"cpuCores": 2,
			"memoryMB": 4096,
			"diskGB":   40,
		},
		"osImageRef":    "ubuntu-pxe-fallback",
		"cloudInitRefs": []string{"ci-pxe-fallback"},
		"network": []map[string]any{
			{
				"name": "eth0",
				"mac":  "52:54:00:de:ad:be",
			},
		},
		// installConfig.type is inferred from osImageRef — no explicit type needed.
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	rec = doRequest(env.echo, http.MethodGet, "/pxe/nocloud/525400deadbe/user-data", nil, "")
	requireStatus(t, rec, http.StatusOK)
	if !strings.Contains(rec.Body.String(), "fallback-user") {
		t.Fatalf("expected cloud-init template fallback user-data, got: %s", rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// List returns empty array when no resources exist
// ---------------------------------------------------------------------------

func TestListEmpty(t *testing.T) {
	env := setupTestEnv(t)

	endpoints := []string{
		"/api/v1/hypervisors",
		"/api/v1/virtual-machines",
		"/api/v1/cloud-init-templates",
		"/api/v1/os-images",
	}

	for _, ep := range endpoints {
		rec := doRequest(env.echo, http.MethodGet, ep, nil, env.token)
		requireStatus(t, rec, http.StatusOK)
		body := parseBody(t, rec)
		items, ok := body["items"].([]any)
		if !ok {
			t.Fatalf("[%s] expected items array, got %v", ep, body["items"])
		}
		if len(items) != 0 {
			t.Fatalf("[%s] expected 0 items, got %d", ep, len(items))
		}
	}
}

// ---------------------------------------------------------------------------
// Public endpoints: register script does not require auth
// ---------------------------------------------------------------------------

func TestSetupAndRegisterScriptPublic(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors/setup-and-register.sh", nil, "")
	requireStatus(t, rec, http.StatusOK)
	if rec.Body.Len() == 0 {
		t.Fatal("expected non-empty script body")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "qemu-system") {
		t.Fatalf("expected setup script to install qemu-system, got:\n%s", body)
	}
	if !strings.Contains(body, "zstd") {
		t.Fatalf("expected setup script to install zstd for artifact image sync, got:\n%s", body)
	}
	if strings.Contains(body, "qemu-kvm") {
		t.Fatalf("setup script must not request obsolete qemu-kvm package, got:\n%s", body)
	}
	if !strings.Contains(body, `HOSTNAME="${GOMI_HOSTNAME:-$(hostname -f)}"`) {
		t.Fatalf("expected setup script to support GOMI_HOSTNAME override, got:\n%s", body)
	}
	if !strings.Contains(body, `auth_tcp = "none"`) {
		t.Fatalf("expected setup script to disable libvirt TCP auth for lab hypervisors, got:\n%s", body)
	}
	if !strings.Contains(body, `/files/gomi-hypervisor.service`) {
		t.Fatalf("expected setup script to install packaged hypervisor unit file, got:\n%s", body)
	}
	if strings.Contains(body, "cat > /etc/systemd/system/gomi-hypervisor.service") {
		t.Fatalf("setup script must not inline the hypervisor unit file, got:\n%s", body)
	}
}

func TestPXEInstallCompletePublic(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/pxe/install-complete", nil, "")
	requireStatus(t, rec, http.StatusBadRequest)
	body := parseBody(t, rec)
	if body["error"] != "token is required" {
		t.Fatalf("expected token required error, got %v", body["error"])
	}
}

// ---------------------------------------------------------------------------
// Create user
// ---------------------------------------------------------------------------

func TestCreateUser(t *testing.T) {
	env := setupTestEnv(t)

	userBody := map[string]any{
		"username": "newuser",
		"password": "secret123",
		"role":     "viewer",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/users", userBody, env.token)
	requireStatus(t, rec, http.StatusCreated)

	// New user can login
	loginBody := map[string]any{
		"username": "newuser",
		"password": "secret123",
	}
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/auth/login", loginBody, "")
	requireStatus(t, rec, http.StatusOK)
	loginResp := parseBody(t, rec)
	token, _ := loginResp["token"].(string)
	if token == "" {
		t.Fatalf("expected login token, got %v", loginResp)
	}

	rec = doRequest(env.echo, http.MethodGet, "/api/v1/me", nil, token)
	requireStatus(t, rec, http.StatusOK)
	meResp := parseBody(t, rec)
	if meResp["role"] != "viewer" {
		t.Fatalf("expected role viewer, got %v", meResp["role"])
	}
}

func TestCreateUserSupportsOperatorAndAdminRoles(t *testing.T) {
	env := setupTestEnv(t)

	for _, role := range []string{"operator", "admin"} {
		userBody := map[string]any{
			"username": "new-" + role,
			"password": "secret123",
			"role":     role,
		}
		rec := doRequest(env.echo, http.MethodPost, "/api/v1/users", userBody, env.token)
		requireStatus(t, rec, http.StatusCreated)

		user, err := env.authStore.GetUser(context.Background(), "new-"+role)
		if err != nil {
			t.Fatalf("get user for role %s: %v", role, err)
		}
		if string(user.Role) != role {
			t.Fatalf("expected role %s, got %s", role, user.Role)
		}
	}
}

func TestCreateUserRejectsInvalidRole(t *testing.T) {
	env := setupTestEnv(t)

	userBody := map[string]any{
		"username": "bad-role",
		"password": "secret123",
		"role":     "owner",
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/users", userBody, env.token)
	requireStatus(t, rec, http.StatusBadRequest)
}

// ---------------------------------------------------------------------------
// RBAC Tests: viewer can read, but cannot create/delete sensitive resources
// ---------------------------------------------------------------------------

func TestRBAC_ViewerCanRead(t *testing.T) {
	env := setupTestEnv(t)

	// Create viewer user and session.
	createUser(t, env.authStore, "viewer-user", "viewerpass", auth.RoleViewer)
	viewerToken := createSession(t, env.authStore, "viewer-user")

	// Viewer can GET hypervisors (read-only).
	rec := doRequest(env.echo, http.MethodGet, "/api/v1/hypervisors", nil, viewerToken)
	requireStatus(t, rec, http.StatusOK)

	// Viewer can GET virtual-machines (read-only).
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/virtual-machines", nil, viewerToken)
	requireStatus(t, rec, http.StatusOK)

	// Viewer can GET ssh-keys (read-only, sanitized).
	rec = doRequest(env.echo, http.MethodGet, "/api/v1/ssh-keys", nil, viewerToken)
	requireStatus(t, rec, http.StatusOK)

}

func TestRBAC_ViewerCannotWrite(t *testing.T) {
	env := setupTestEnv(t)

	// Create viewer user and session.
	createUser(t, env.authStore, "viewer-user", "viewerpass", auth.RoleViewer)
	viewerToken := createSession(t, env.authStore, "viewer-user")

	// Viewer cannot create SSH key (admin only -- handles secrets).
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/ssh-keys", map[string]any{
		"name": "test-key", "publicKey": "ssh-ed25519 AAAA...",
	}, viewerToken)
	requireStatus(t, rec, http.StatusForbidden)

	// Viewer cannot create hypervisor (admin only).
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", map[string]any{
		"name": "hv-test", "connection": map[string]any{"type": "tcp", "host": "127.0.0.1", "port": 16509},
	}, viewerToken)
	requireStatus(t, rec, http.StatusForbidden)

	// Viewer cannot create virtual machine (operator+ only).
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", map[string]any{
		"name": "vm-test", "hypervisorRef": "hv-1", "resources": map[string]any{"cpuCores": 1, "memoryMB": 1024, "diskGB": 10},
	}, viewerToken)
	requireStatus(t, rec, http.StatusForbidden)

	// Viewer cannot create user (admin only).
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "hacker", "password": "test", "role": "admin",
	}, viewerToken)
	requireStatus(t, rec, http.StatusForbidden)
}

func TestRBAC_OperatorCanWriteButNotAdmin(t *testing.T) {
	env := setupTestEnv(t)

	// Create operator user and session.
	createUser(t, env.authStore, "operator-user", "operatorpass", auth.RoleOperator)
	operatorToken := createSession(t, env.authStore, "operator-user")

	// Operator can create cloud-init templates (operator+).
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/cloud-init-templates", map[string]any{
		"name": "ci-op-test", "userData": "#cloud-config\npackages:\n  - curl",
	}, operatorToken)
	requireStatus(t, rec, http.StatusCreated)

	// Operator cannot create SSH keys (admin only).
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/ssh-keys", map[string]any{
		"name": "op-key", "publicKey": "ssh-ed25519 AAAA...",
	}, operatorToken)
	requireStatus(t, rec, http.StatusForbidden)

	// Operator cannot create users (admin only).
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/users", map[string]any{
		"username": "escalate", "password": "test", "role": "admin",
	}, operatorToken)
	requireStatus(t, rec, http.StatusForbidden)

	// Operator cannot create hypervisors (admin only).
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/hypervisors", map[string]any{
		"name": "hv-op", "connection": map[string]any{"type": "tcp", "host": "127.0.0.1", "port": 16509},
	}, operatorToken)
	requireStatus(t, rec, http.StatusForbidden)
}

// ---------------------------------------------------------------------------
// Machine OS Preset auto-resolution from OS Image
// ---------------------------------------------------------------------------

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
	if osPreset["imageRef"] != "debian-13-amd64" {
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

func TestMachineAPIResponsesRedactSensitiveFields(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/os-images", map[string]any{
		"name":      "ubuntu-redact",
		"osFamily":  "ubuntu",
		"osVersion": "24.04",
		"arch":      "amd64",
		"format":    "qcow2",
		"source":    "upload",
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

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines/machine-powered-redeploy/actions/redeploy", map[string]any{
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

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/machines/machine-dhcp-redeploy/actions/redeploy", map[string]any{
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

func TestOSCatalogListsSupportedImages(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/os-catalog", nil, env.token)
	requireStatus(t, rec, http.StatusOK)

	body := parseBody(t, rec)
	items, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("expected items array, got %T", body["items"])
	}
	seen := map[string]string{}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("expected catalog item object, got %T", raw)
		}
		entry, ok := item["entry"].(map[string]any)
		if !ok {
			t.Fatalf("expected entry object, got %T", item["entry"])
		}
		name, _ := entry["name"].(string)
		bootEnv, _ := entry["bootEnvironment"].(string)
		expectedFormat := "raw"
		expectedCompression := "zstd"
		expectedSuffix := ".raw.zst"
		if name == "ubuntu-22.04-amd64-baremetal" {
			expectedFormat = "squashfs"
			expectedCompression = ""
			expectedSuffix = ".rootfs.squashfs"
		}
		if entry["format"] != expectedFormat {
			t.Fatalf("expected %s catalog format %s, got %v", name, expectedFormat, entry["format"])
		}
		if entry["sourceFormat"] != expectedFormat {
			t.Fatalf("expected %s catalog sourceFormat %s, got %v", name, expectedFormat, entry["sourceFormat"])
		}
		gotCompression, hasCompression := entry["sourceCompression"]
		if expectedCompression == "" && !hasCompression {
			gotCompression = ""
		}
		if gotCompression != expectedCompression {
			t.Fatalf("expected %s catalog sourceCompression %q, got %v", name, expectedCompression, entry["sourceCompression"])
		}
		url, _ := entry["url"].(string)
		if strings.Contains(url, ".qcow2") || strings.Contains(url, "cloud-images.ubuntu.com") || !strings.HasSuffix(url, expectedSuffix) {
			t.Fatalf("expected %s catalog URL to reference prebuilt %s artifact, got %q", name, expectedSuffix, url)
		}
		seen[name] = bootEnv
	}

	if seen["ubuntu-22.04-amd64-baremetal"] != "ubuntu-minimal-cloud-amd64" {
		t.Fatalf("expected ubuntu-22.04-amd64-baremetal to use ubuntu-minimal-cloud-amd64 boot environment, got %q", seen["ubuntu-22.04-amd64-baremetal"])
	}
}

func TestOSCatalogInstallExternalURLOnlyImage(t *testing.T) {
	imageSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("raw-image-content"))
	}))
	defer imageSrv.Close()

	catalogPath := filepath.Join(t.TempDir(), "catalog.yaml")
	if err := os.WriteFile(catalogPath, []byte(fmt.Sprintf(`
entries:
  - name: external-url-only
    osFamily: custom
    osVersion: "1"
    arch: amd64
    variant: baremetal
    format: raw
    sourceFormat: raw
    url: %s/root.raw
    bootEnvironment: ubuntu-minimal-cloud-amd64
`, imageSrv.URL)), 0o644); err != nil {
		t.Fatalf("write catalog: %v", err)
	}
	t.Setenv("GOMI_OS_CATALOG_FILE", catalogPath)
	t.Setenv("GOMI_OS_CATALOG_REPLACE", "true")
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/os-catalog", nil, env.token)
	requireStatus(t, rec, http.StatusOK)
	body := parseBody(t, rec)
	items := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one external catalog item, got %d", len(items))
	}

	rec = doRequest(env.echo, http.MethodPost, "/api/v1/os-catalog/external-url-only/install", nil, env.token)
	requireStatus(t, rec, http.StatusAccepted)

	for i := 0; i < 20; i++ {
		rec = doRequest(env.echo, http.MethodGet, "/api/v1/os-images/external-url-only", nil, env.token)
		if rec.Code == http.StatusOK {
			body = parseBody(t, rec)
			if body["ready"] == true {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected external URL-only image to become ready")
}

func TestBootEnvironmentsListStartsMissing(t *testing.T) {
	env := setupTestEnv(t)

	rec := doRequest(env.echo, http.MethodGet, "/api/v1/boot-environments", nil, env.token)
	requireStatus(t, rec, http.StatusOK)

	body := parseBody(t, rec)
	items, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("expected items array, got %T", body["items"])
	}
	want := map[string]bool{
		"ubuntu-minimal-cloud-amd64": false,
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("expected boot environment object, got %T", raw)
		}
		if _, ok := want[item["name"].(string)]; ok {
			if item["phase"] != "missing" {
				t.Fatalf("expected %s to start missing, got %v", item["name"], item["phase"])
			}
			want[item["name"].(string)] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("expected %s boot environment", name)
		}
	}
}
