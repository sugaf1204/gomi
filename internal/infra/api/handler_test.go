package api_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/labstack/echo/v4"
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
	"golang.org/x/crypto/bcrypt"
	"io"
	"net/http/httptest"
	"testing"
	"time"
)

type testEnv struct {
	echo      *echo.Echo
	token     string
	authStore auth.Store
	machines  *machine.Service
	osimages  *osimage.Service
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
		osimages:  osimageSvc,
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

func listValues(t *testing.T, body map[string]any) []any {
	t.Helper()
	for _, key := range []string{
		"machines",
		"virtualMachines",
		"hypervisors",
		"subnets",
		"sshKeys",
		"cloudInitTemplates",
		"osImages",
		"bootEnvironments",
		"dhcpLeases",
		"auditEvents",
		"dnsRecords",
		"items",
	} {
		if items, ok := body[key].([]any); ok {
			return items
		}
	}
	t.Fatalf("expected list response array, got %v", body)
	return nil
}

func signTestPowerEvent(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// ---------------------------------------------------------------------------
// Hypervisor CRUD Tests
// ---------------------------------------------------------------------------
