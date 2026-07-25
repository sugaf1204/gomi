package api_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

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
	"github.com/sugaf1204/gomi/internal/resource"
	"github.com/sugaf1204/gomi/internal/sshkey"
	"github.com/sugaf1204/gomi/internal/vm"
)

// failingHypervisorStore simulates a transient database blip on the create
// handler's post-insert hypervisor recheck (vm.go:103). The handler resolves the
// hypervisor several times before that point — reference validation and bridge
// resolution — so the trigger is "the VM row already exists" rather than a call
// count, which would break if those earlier lookups are reordered.
type failingHypervisorStore struct {
	hypervisor.Store
	failWhenExists func() bool
}

func (s *failingHypervisorStore) Get(ctx context.Context, name string) (hypervisor.Hypervisor, error) {
	if s.failWhenExists != nil && s.failWhenExists() {
		return hypervisor.Hypervisor{}, errors.New("transient store failure")
	}
	return s.Store.Get(ctx, name)
}

type vmCreateEnv struct {
	echo        *echo.Echo
	token       string
	hypervisors *hypervisor.Service
	osimages    *osimage.Service
	vms         *vm.Service
	hvStore     *failingHypervisorStore
}

// Builds a server whose hypervisor Get can be made to fail on demand, which the
// shared harness cannot do. Everything else mirrors setupTestEnvFull.
func setupTestEnvWithFailingHypervisorGet(t *testing.T) vmCreateEnv {
	t.Helper()

	backend := memory.New()
	hvStore := &failingHypervisorStore{Store: backend.Hypervisors()}

	hypervisorSvc := hypervisor.NewService(hvStore, backend.HypervisorTokens(), backend.AgentTokens())
	vmSvc := vm.NewService(backend.VMs())
	osimageSvc := osimage.NewService(backend.OSImages())

	authStore := backend.Auth()
	authService := infraapi.NewAuthService(authStore, time.Hour)
	createUser(t, authStore, "admin", "adminpass", auth.RoleAdmin)

	srv := infraapi.NewServer(infraapi.ServerConfig{
		Machines:        machine.NewService(backend.Machines()),
		Subnets:         backend.Subnets(),
		AuthStore:       authStore,
		AuthService:     authService,
		Discovery:       discovery.NewService(backend.Machines()),
		SSHKeys:         sshkey.NewService(backend.SSHKeys()),
		HWInfo:          hwinfo.NewService(backend.HWInfo()),
		Hypervisors:     hypervisorSvc,
		AgentTokenStore: backend.AgentTokens(),
		VMs:             vmSvc,
		CloudInits:      cloudinit.NewService(backend.CloudInits()),
		OSImages:        osimageSvc,
		FilesDir:        t.TempDir(),
		ImageStorageDir: t.TempDir(),
		VMRuntimeDeleter: func(context.Context, vm.VirtualMachine) error {
			return nil
		},
		BootEnvs: bootenv.NewManager(bootenv.Config{
			DataDir:  t.TempDir(),
			FilesDir: t.TempDir(),
		}),
	})

	return vmCreateEnv{
		echo:        srv.Echo(),
		token:       createSession(t, authStore, "admin"),
		hypervisors: hypervisorSvc,
		osimages:    osimageSvc,
		vms:         vmSvc,
		hvStore:     hvStore,
	}
}

func seedVMCreatePrereqs(t *testing.T, env vmCreateEnv, hvName string) {
	t.Helper()
	ctx := context.Background()
	if _, err := env.hypervisors.Create(ctx, hypervisor.Hypervisor{
		Name:       hvName,
		Connection: hypervisor.ConnectionSpec{Type: hypervisor.ConnectionTCP, Host: "192.0.2.10", Port: 16509},
		Phase:      hypervisor.PhaseRegistered,
	}); err != nil {
		t.Fatalf("create hypervisor: %v", err)
	}
	if _, err := env.osimages.Create(ctx, osimage.OSImage{
		Name:      "ubuntu-test",
		Format:    osimage.FormatQCOW2,
		Variant:   osimage.VariantCloud,
		OSFamily:  "ubuntu",
		OSVersion: "24.04",
		Arch:      "amd64",
		Source:    osimage.SourceURL,
		URL:       "https://example.invalid/ubuntu.qcow2",
	}); err != nil {
		t.Fatalf("create os image: %v", err)
	}
}

func vmCreateRequestBody(name, hvName string) map[string]any {
	return map[string]any{
		"name":               name,
		"hypervisorRef":      hvName,
		"resources":          map[string]any{"cpuCores": 1, "memoryMB": 1024, "diskGB": 8},
		"osImageRef":         "ubuntu-test",
		"powerControlMethod": "libvirt",
	}
}

// The record is written with an exclusive insert, so a row left behind by a
// failed pre-deploy recheck would make every retry return 409 while the VM sits
// Pending with no deploy. The former upsert let a retry overwrite it.
func TestCreateVirtualMachineRollsBackOnTransientRecheckError(t *testing.T) {
	env := setupTestEnvWithFailingHypervisorGet(t)
	seedVMCreatePrereqs(t, env, "hv-rollback")

	// Fail only once the row exists, i.e. on the post-insert recheck.
	env.hvStore.failWhenExists = func() bool {
		_, err := env.vms.Get(context.Background(), "vm-rollback")
		return err == nil
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmCreateRequestBody("vm-rollback", "hv-rollback"), env.token)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 from the transient recheck failure, got %d: %s", rec.Code, rec.Body.String())
	}

	if _, err := env.vms.Get(context.Background(), "vm-rollback"); !errors.Is(err, resource.ErrNotFound) {
		t.Fatalf("expected the inserted record to be rolled back, got err=%v", err)
	}

	// With the row gone, the same name must be creatable again rather than
	// permanently rejected with 409.
	env.hvStore.failWhenExists = nil
	rec = doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmCreateRequestBody("vm-rollback", "hv-rollback"), env.token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("retry after rollback: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if _, err := env.vms.Get(context.Background(), "vm-rollback"); err != nil {
		t.Fatalf("expected the retry to store the VM: %v", err)
	}
}

func TestCreateVirtualMachineRejectsDuplicateName(t *testing.T) {
	env := setupTestEnvWithFailingHypervisorGet(t)
	seedVMCreatePrereqs(t, env, "hv-dup")
	body := vmCreateRequestBody("vm-dup", "hv-dup")

	if rec := doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", body, env.token); rec.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	rec := doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", body, env.token)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second create: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	items, err := env.vms.List(context.Background())
	if err != nil {
		t.Fatalf("list vms: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected exactly one VM record, got %d", len(items))
	}
	if items[0].Phase == vm.PhaseError {
		t.Fatalf("the rejected duplicate must not have altered the stored record: %+v", items[0])
	}
}
