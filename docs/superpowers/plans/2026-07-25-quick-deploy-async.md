# Quick Deploy Async Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Quick Deploy return control to the user immediately, while guaranteeing that repeated clicks, page reloads, and server restarts never corrupt or strand a deploy.

**Architecture:** The VM create handler persists the record and returns `201` at once, running `Deploy()` on a detached-context goroutine so a disconnecting client cannot cancel it. A resume sweep inside the existing VM runtime sync loop restarts deploys that a process restart interrupted, using the already-persisted `ProvisioningStatus` fields to decide whether a deploy had reached domain definition. Because volume creation is not idempotent, the sweep tears down leftover host state before re-running.

**Tech Stack:** Go 1.25.4, Echo v4, libvirt (go-libvirt RPC), SQLite/PostgreSQL, React 19 + TypeScript + Vite 7.

**Design doc:** `docs/superpowers/specs/2026-07-25-quick-deploy-async-design.md`

## Global Constraints

- All code, comments, UI text, and string literals must be in English.
- Keep source files under ~300 lines where practical; look for a natural split before pushing a file past ~500. `internal/infra/api/vm.go` is already 435 lines — Task 2 moves code out of it, so do not add net new bulk there.
- Do not hide unrelated refactors inside this work.
- OS deploy behaviour must not become Ubuntu-specific. The curtin path and the non-curtin (preseed/PXE) path must both be covered by tests.
- Go tests run with `go test ./...`; web tests with `npm test` from `web/`.
- No Japanese in source. No Claude signatures in commit messages.
- Breaking changes are acceptable in this project; backward compatibility is not a goal.

---

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `internal/vm/deploy.go` | Deploy/Redeploy orchestration; gains an injectable executor factory | Modify |
| `internal/vm/teardown.go` | Host-state teardown shared by the API and the sync loop | Create |
| `internal/vm/resume.go` | Decide and perform resume of interrupted deploys | Create |
| `internal/vm/teardown_test.go` | Teardown behaviour incl. not-found tolerance | Create |
| `internal/vm/resume_test.go` | Resume decision matrix + cleanup-before-retry | Create |
| `internal/vm/deploy_test.go` | Executor-factory seam coverage | Modify |
| `internal/infra/api/vm.go` | Create handler: return 201 early, duplicate guard, background deploy | Modify |
| `internal/app/sync.go` | Invoke the resume sweep from the runtime sync loop | Modify |
| `web/src/components/views/virtual-machines/useVirtualMachineOperations.ts` | Optimistic count increment for VM Quick Deploy | Modify |
| `web/src/components/views/machines/useMachineOperations.ts` | Same for Machine Quick Deploy | Modify |

Task order matters: Task 1 creates the test seam that Tasks 3–5 depend on.

---

### Task 1: Make the Deployer's libvirt executor injectable

`Deployer` calls `libvirt.NewExecutor(cfg)` directly (`internal/vm/deploy.go:40` and `:148`), so `Deploy()` cannot be tested. `RuntimeSyncer` already solves this with an injected `ExecutorFactory` (`internal/vm/runtime_sync.go:25`), used by 13 existing tests. This task brings `Deployer` to the same pattern. Every later task depends on it.

**Files:**
- Modify: `internal/vm/deploy.go:15-21` (struct), `:40`, `:148`
- Test: `internal/vm/deploy_test.go`

**Interfaces:**
- Consumes: `libvirt.Executor`, `libvirt.LibvirtConfig`, existing `vm.ExecutorFactory` type (`internal/vm/runtime_sync.go:34`).
- Produces: `Deployer.ExecutorFactory ExecutorFactory` field and unexported method `func (d *Deployer) newExecutor(ctx context.Context, cfg libvirt.LibvirtConfig) (libvirt.Executor, error)`. Tasks 3 and 5 inject fakes through this field.

- [ ] **Step 1: Write the failing test**

Add to `internal/vm/deploy_test.go` (package `vm`, internal test package — note this file is `package vm`, unlike `runtime_sync_test.go`):

```go
func TestDeployerNewExecutorUsesInjectedFactory(t *testing.T) {
	called := false
	d := &Deployer{
		ExecutorFactory: func(context.Context, libvirt.LibvirtConfig) (libvirt.Executor, error) {
			called = true
			return nil, errors.New("injected")
		},
	}
	if _, err := d.newExecutor(context.Background(), libvirt.LibvirtConfig{}); err == nil {
		t.Fatal("expected injected factory error")
	}
	if !called {
		t.Fatal("expected injected factory to be used")
	}
}
```

Add `"github.com/sugaf1204/gomi/internal/libvirt"` to that file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vm/ -run TestDeployerNewExecutorUsesInjectedFactory -v`
Expected: compile error — `d.newExecutor undefined` and `unknown field ExecutorFactory`.

- [ ] **Step 3: Write minimal implementation**

In `internal/vm/deploy.go`, add the field to the struct:

```go
type Deployer struct {
	Hypervisors *hypervisor.Service
	OSImages    *osimage.Service
	VMs         *Service
	PXEBaseURL  string
	ListenAddr  string
	// ExecutorFactory builds the libvirt executor. Tests inject a fake;
	// production leaves it nil and gets libvirt.NewExecutor.
	ExecutorFactory ExecutorFactory
}

func (d *Deployer) newExecutor(ctx context.Context, cfg libvirt.LibvirtConfig) (libvirt.Executor, error) {
	if d.ExecutorFactory != nil {
		return d.ExecutorFactory(ctx, cfg)
	}
	return libvirt.NewExecutor(cfg)
}
```

Replace the two call sites. At `internal/vm/deploy.go:40`:

```go
	exec, err := d.newExecutor(ctx, cfg)
```

And the same substitution at `internal/vm/deploy.go:148` inside `Redeploy`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vm/ -run TestDeployerNewExecutor -v`
Expected: PASS

Run: `go test ./...`
Expected: PASS (no behaviour change when `ExecutorFactory` is nil)

- [ ] **Step 5: Commit**

```bash
git add internal/vm/deploy.go internal/vm/deploy_test.go
git commit -m "Make VM deployer libvirt executor injectable for testing"
```

---

### Task 2: Move host teardown into internal/vm

`teardownVMRuntimeOnHypervisor` lives in `internal/infra/api/vm.go:276` but is needed by both the API deploy goroutine (Task 3) and the resume sweep (Task 5, in `internal/app`). Its dependencies — `BuildLibvirtConfig`, `IsIgnorableDestroyError`, `SkipHostStorageCleanup` — are all already in `internal/vm` (`internal/vm/domain_config.go:10,100,118`), so this removes an api→vm indirection. It also trims the 435-line `vm.go`.

**Files:**
- Create: `internal/vm/teardown.go`
- Create: `internal/vm/teardown_test.go`
- Modify: `internal/infra/api/vm.go` (remove the moved function; update its callers)

**Interfaces:**
- Consumes: `Deployer.newExecutor` from Task 1.
- Produces: `func (d *Deployer) TeardownHostState(ctx context.Context, hv hypervisor.Hypervisor, v VirtualMachine) error` — tolerates missing domains and volumes. Task 3 and Task 5 call it.

- [ ] **Step 1: Write the failing test**

Create `internal/vm/teardown_test.go` (package `vm`):

```go
package vm

import (
	"context"
	"testing"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/libvirt"
)

type recordingTeardownExecutor struct {
	libvirt.Executor
	destroyed  []string
	undefined  []string
	deletedVol []string
}

func (e *recordingTeardownExecutor) DestroyDomain(_ context.Context, name string) error {
	e.destroyed = append(e.destroyed, name)
	return nil
}

func (e *recordingTeardownExecutor) UndefineDomain(_ context.Context, name string) error {
	e.undefined = append(e.undefined, name)
	return nil
}

func (e *recordingTeardownExecutor) DeleteVolume(_ context.Context, name string) error {
	e.deletedVol = append(e.deletedVol, name)
	return nil
}

func (e *recordingTeardownExecutor) Close() error { return nil }

func TestTeardownHostStateRemovesDomainAndVolume(t *testing.T) {
	exec := &recordingTeardownExecutor{}
	d := &Deployer{
		ExecutorFactory: func(context.Context, libvirt.LibvirtConfig) (libvirt.Executor, error) {
			return exec, nil
		},
	}
	v := VirtualMachine{Name: "vm-teardown", Phase: PhasePending}
	if err := d.TeardownHostState(context.Background(), hypervisor.Hypervisor{Name: "hv"}, v); err != nil {
		t.Fatalf("TeardownHostState: %v", err)
	}
	if len(exec.destroyed) != 1 || exec.destroyed[0] != "vm-teardown" {
		t.Fatalf("expected domain destroy, got %v", exec.destroyed)
	}
	if len(exec.undefined) != 1 || exec.undefined[0] != "vm-teardown" {
		t.Fatalf("expected domain undefine, got %v", exec.undefined)
	}
	if len(exec.deletedVol) != 1 || exec.deletedVol[0] != "vm-teardown" {
		t.Fatalf("expected volume delete, got %v", exec.deletedVol)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vm/ -run TestTeardownHostState -v`
Expected: FAIL — `d.TeardownHostState undefined`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/vm/teardown.go`:

```go
package vm

import (
	"context"
	"fmt"
	"strings"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/libvirt"
)

// TeardownHostState removes the libvirt domain and volume a deploy may have
// created. Missing domains and volumes are not errors: the caller uses this to
// clean up partial state whose exact extent is unknown.
func (d *Deployer) TeardownHostState(ctx context.Context, hv hypervisor.Hypervisor, v VirtualMachine) error {
	cfg := BuildLibvirtConfig(hv)
	exec, err := d.newExecutor(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect to hypervisor %s for teardown: %w", hv.Name, err)
	}
	defer exec.Close()

	domainName := strings.TrimSpace(v.LibvirtDomain)
	if domainName == "" {
		domainName = v.Name
	}
	destroyErr := exec.DestroyDomain(ctx, domainName)
	if destroyErr != nil && !IsIgnorableDestroyError(destroyErr) && !libvirt.IsDomainNotFoundError(destroyErr) {
		return fmt.Errorf("stop domain %s: %w", domainName, destroyErr)
	}
	undefineErr := exec.UndefineDomain(ctx, domainName)
	if undefineErr != nil && !IsIgnorableDestroyError(undefineErr) && !libvirt.IsDomainNotFoundError(undefineErr) {
		return fmt.Errorf("undefine domain %s: %w", domainName, undefineErr)
	}
	if SkipHostStorageCleanup(v.Phase, destroyErr, undefineErr) {
		return nil
	}
	if err := exec.DeleteVolume(ctx, v.Name); err != nil && !libvirt.IsVolumeNotFoundError(err) {
		return fmt.Errorf("delete volume %s: %w", v.Name, err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/vm/ -run TestTeardownHostState -v`
Expected: PASS

- [ ] **Step 5: Repoint the API callers and delete the old function**

In `internal/infra/api/vm.go`, delete `teardownVMRuntimeOnHypervisor` (lines 276-303) and change `teardownVMRuntimeForCleanup` (line 265) to delegate:

```go
func (s *Server) teardownVMRuntimeForCleanup(ctx context.Context, hv hypervisor.Hypervisor, v vm.VirtualMachine) error {
	if s.vmDeployer == nil {
		return nil
	}
	return s.vmDeployer.TeardownHostState(ctx, hv, v)
}
```

Then find every other caller and update it:

Run: `grep -rn "teardownVMRuntimeOnHypervisor" internal/`
Expected: only matches you are about to fix. Update each to call `s.vmDeployer.TeardownHostState`. Remove now-unused imports from `internal/infra/api/vm.go` (`strings` and `libvirt` may become unused — the compiler will tell you).

- [ ] **Step 6: Run the full suite**

Run: `go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/vm/teardown.go internal/vm/teardown_test.go internal/infra/api/vm.go
git commit -m "Move VM host teardown into internal/vm for reuse by resume sweep"
```

---

### Task 3: Return 201 before deploying, and reject duplicate names

Two changes to the create handler, committed together because the duplicate guard is what makes the early return safe.

**Files:**
- Modify: `internal/infra/api/vm.go:48-132`
- Test: `internal/infra/api/handler_hypervisor_vm_test.go`

**Interfaces:**
- Consumes: `Deployer.TeardownHostState` (Task 2).
- Produces: `POST /virtualmachines` returns `201` with `Phase=Pending` before the deploy runs, and `409` when the name exists.

- [ ] **Step 1: Write the failing test**

Create `internal/infra/api/handler_vm_create_test.go`. It uses the package's existing helpers: `setupTestEnv` (`internal/infra/api/handler_test.go:43`), `doRequest(e, method, path, body any, token)` (`:221`) and `parseBody` (`:238`). Note the route is `/api/v1/virtual-machines` — hyphenated (`internal/infra/api/server.go:269`).

```go
package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/sugaf1204/gomi/internal/hypervisor"
	"github.com/sugaf1204/gomi/internal/vm"
)

func seedVMHypervisor(t *testing.T, env testEnv, name string) {
	t.Helper()
	if _, err := env.hypervisors.Create(context.Background(), hypervisor.Hypervisor{
		Name:       name,
		Connection: hypervisor.ConnectionSpec{Type: hypervisor.ConnectionTCP, Host: "192.0.2.10", Port: 16509},
		Phase:      hypervisor.PhaseRegistered,
	}); err != nil {
		t.Fatalf("create hypervisor %s: %v", name, err)
	}
}

func vmCreateBody(name, hypervisorRef string) map[string]any {
	return map[string]any{
		"name":               name,
		"hypervisorRef":      hypervisorRef,
		"resources":          map[string]any{"cpuCores": 1, "memoryMB": 1024, "diskGB": 8},
		"osImageRef":         "ubuntu-test",
		"powerControlMethod": "libvirt",
	}
}

func TestCreateVirtualMachineRejectsDuplicateName(t *testing.T) {
	env := setupTestEnv(t)
	seedVMHypervisor(t, env, "hv-dup")
	body := vmCreateBody("vm-dup", "hv-dup")

	rec1 := doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", body, env.token)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d: %s", rec1.Code, rec1.Body.String())
	}

	rec2 := doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", body, env.token)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("second create: expected 409, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

func TestCreateVirtualMachineReturnsBeforeDeployCompletes(t *testing.T) {
	env := setupTestEnv(t)
	seedVMHypervisor(t, env, "hv-async")

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmCreateBody("vm-async", "hv-async"), env.token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	got := parseBody(t, rec)
	if got["phase"] != string(vm.PhasePending) {
		t.Fatalf("expected Pending phase in the immediate response, got %v", got["phase"])
	}
}
```

Check the package clause of the existing `*_test.go` files in that directory and match it (they import the server as `infraapi`, which indicates an external `api_test` package). If `setupTestEnv` returns a `testEnv` whose fields differ from `echo`/`token`/`hypervisors`, adapt to the real field names rather than adding helpers.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/infra/api/ -run TestCreateVirtualMachine -v`
Expected: FAIL — the duplicate create returns 201, and the phase is not `Pending`.

- [ ] **Step 3: Add the duplicate guard**

In `internal/infra/api/vm.go`, immediately before `created, err := s.vms.Create(ctx, v)` (line 91):

```go
	if _, err := s.vms.Get(ctx, v.Name); err == nil {
		return c.JSON(gohttp.StatusConflict, jsonError("virtual machine already exists: "+v.Name))
	} else if !errors.Is(err, resource.ErrNotFound) {
		return c.JSON(gohttp.StatusInternalServerError, jsonErrorErr(err))
	}
```

- [ ] **Step 4: Move the deploy to a background goroutine**

Replace the whole `if s.vmDeployer != nil { ... }` block (lines 109-130) with:

```go
	if s.vmDeployer != nil {
		httputil.CreateAudit(c, s.authStore, created.Name, "create-vm", "accepted", "virtual machine deploy started", nil)
		go s.runVMDeploy(deployHV, created)
	} else {
		httputil.CreateAudit(c, s.authStore, created.Name, "create-vm", "success", "virtual machine created", nil)
	}
	return c.JSON(gohttp.StatusCreated, virtualMachineResponse(created))
}

// runVMDeploy performs the libvirt deploy outside the request lifecycle so a
// disconnecting client cannot cancel it. The concurrent-hypervisor-delete
// recheck moves here with it: the handler has already responded, so a record
// swept mid-deploy must be cleaned up here rather than reported to the caller.
func (s *Server) runVMDeploy(deployHV hypervisor.Hypervisor, created vm.VirtualMachine) {
	ctx, cancel := context.WithTimeout(context.Background(), s.provisionTimeout)
	defer cancel()

	deployErr := s.vmDeployer.Deploy(ctx, &created, pxehttp.RenderNoCloudLineConfig)

	if _, getErr := s.vms.Get(ctx, created.Name); errors.Is(getErr, resource.ErrNotFound) {
		if cleanupErr := s.vmDeployer.TeardownHostState(ctx, deployHV, created); cleanupErr != nil {
			log.Printf("create vm %s: cleanup after concurrent hypervisor delete: %v", created.Name, cleanupErr)
		}
		return
	}
	if deployErr != nil {
		log.Printf("create vm %s: deploy failed: %v", created.Name, deployErr)
	}
}
```

`Deploy()` already persists the outcome on every path — `FailDeploy` on error (`internal/vm/deploy.go:254`) and `UpdateDeployStatus` on success (`internal/vm/deploy.go:119`) — so the goroutine needs no extra status writes. Add `"context"` and `"github.com/sugaf1204/gomi/internal/hypervisor"` to the imports if not already present.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/infra/api/ -run TestCreateVirtualMachine -v`
Expected: PASS

- [ ] **Step 6: Fix tests that assumed a synchronous deploy**

Run: `go test ./...`

Some existing tests assert post-deploy state right after the POST. Those now race. For each failure, poll for the expected phase instead of reading once:

```go
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := env.vms.Get(context.Background(), "vm-name")
		if err == nil && got.Phase == vm.PhaseProvisioning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("vm did not reach Provisioning: %+v (err=%v)", got, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
```

Do not weaken an assertion to make it pass — if a test checked that a deploy happened, it must still check that, just asynchronously.

- [ ] **Step 7: Run the full suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/infra/api/vm.go internal/infra/api/
git commit -m "Return 201 before VM deploy and reject duplicate VM names"
```

---

### Task 4: Decide which interrupted deploys to resume

Pure decision logic, separated from execution so the matrix is testable without libvirt.

**Files:**
- Create: `internal/vm/resume.go`
- Create: `internal/vm/resume_test.go`

**Interfaces:**
- Produces:
  - `type ResumeAction int` with constants `ResumeNone`, `ResumeRedeploy`, `ResumeFail`.
  - `func DecideResumeAction(v VirtualMachine, now time.Time) ResumeAction`

  Task 5 consumes both.

- [ ] **Step 1: Write the failing test**

Create `internal/vm/resume_test.go` (package `vm`):

```go
package vm

import (
	"testing"
	"time"
)

func TestDecideResumeAction(t *testing.T) {
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	future := now.Add(10 * time.Minute)
	past := now.Add(-10 * time.Minute)
	observed := now.Add(-time.Minute)

	tests := []struct {
		name string
		vm   VirtualMachine
		want ResumeAction
	}{
		{
			name: "pending without domain resumes",
			vm:   VirtualMachine{Phase: PhasePending, Provisioning: ProvisioningStatus{Active: true, DeadlineAt: &future}},
			want: ResumeRedeploy,
		},
		{
			name: "pending past deadline fails",
			vm:   VirtualMachine{Phase: PhasePending, Provisioning: ProvisioningStatus{Active: true, DeadlineAt: &past}},
			want: ResumeFail,
		},
		{
			name: "domain already defined is left to the sync loop",
			vm:   VirtualMachine{Phase: PhasePending, Provisioning: ProvisioningStatus{Active: true, DeadlineAt: &future, DomainObservedAt: &observed}},
			want: ResumeNone,
		},
		{
			name: "inactive provisioning window is ignored",
			vm:   VirtualMachine{Phase: PhasePending, Provisioning: ProvisioningStatus{Active: false, DeadlineAt: &future}},
			want: ResumeNone,
		},
		{
			name: "running vm is ignored",
			vm:   VirtualMachine{Phase: PhaseRunning, Provisioning: ProvisioningStatus{Active: true, DeadlineAt: &future}},
			want: ResumeNone,
		},
		{
			name: "completed window is ignored",
			vm:   VirtualMachine{Phase: PhasePending, Provisioning: ProvisioningStatus{Active: true, DeadlineAt: &future, CompletedAt: &observed}},
			want: ResumeNone,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecideResumeAction(tc.vm, now); got != tc.want {
				t.Fatalf("DecideResumeAction = %v, want %v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vm/ -run TestDecideResumeAction -v`
Expected: FAIL — `undefined: ResumeAction`, `undefined: DecideResumeAction`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/vm/resume.go`:

```go
package vm

import "time"

// ResumeAction is what a restart-interrupted deploy needs.
type ResumeAction int

const (
	// ResumeNone means the record needs no resume handling: either it is not
	// mid-deploy, or its libvirt domain already exists and the runtime sync
	// loop owns it from here.
	ResumeNone ResumeAction = iota
	// ResumeRedeploy means the deploy never reached domain definition, so the
	// host state must be cleaned up and the deploy re-run.
	ResumeRedeploy
	// ResumeFail means the provisioning window expired while the deploy was
	// interrupted; retrying would outlive its own deadline.
	ResumeFail
)

// DecideResumeAction classifies a VM record after a process restart.
//
// Only Pending records are candidates: once a deploy defines its domain the
// phase advances and the runtime sync loop takes over. DomainObservedAt is the
// marker for that handover — while it is nil the domain does not exist, so a
// re-run is safe; once set, re-running would fight the sync loop.
func DecideResumeAction(v VirtualMachine, now time.Time) ResumeAction {
	if v.Phase != PhasePending {
		return ResumeNone
	}
	if !v.Provisioning.Active || v.Provisioning.CompletedAt != nil {
		return ResumeNone
	}
	if v.Provisioning.DomainObservedAt != nil {
		return ResumeNone
	}
	if v.Provisioning.DeadlineAt != nil && now.After(*v.Provisioning.DeadlineAt) {
		return ResumeFail
	}
	return ResumeRedeploy
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/vm/ -run TestDecideResumeAction -v`
Expected: PASS (all 6 subtests)

- [ ] **Step 5: Commit**

```bash
git add internal/vm/resume.go internal/vm/resume_test.go
git commit -m "Add resume decision logic for restart-interrupted VM deploys"
```

---

### Task 5: Execute the resume sweep

Cleanup-then-retry, because `CreateVolume`/`CreateOverlayVolume` fail on an existing volume (`internal/libvirt/storage.go:32,62`) while `DefineDomain` is idempotent (`internal/libvirt/domain.go:16`). Without the teardown, every restart-interrupted VM would fail with "already exists".

**Files:**
- Modify: `internal/vm/resume.go`
- Modify: `internal/vm/resume_test.go`

**Interfaces:**
- Consumes: `DecideResumeAction` (Task 4), `TeardownHostState` (Task 2), `Deployer.Deploy`, `Service.FailDeploy` (`internal/vm/service.go:193`).
- Produces: `func (d *Deployer) ResumeInterruptedDeploys(ctx context.Context, pxeNoCloudFn func(base string, installType InstallConfigType, mac string) string) error`. Task 6 calls it.

- [ ] **Step 1: Write the failing test**

Append to `internal/vm/resume_test.go`. Add imports `"context"`, `"sync"`, `"github.com/sugaf1204/gomi/internal/hypervisor"`, `"github.com/sugaf1204/gomi/internal/libvirt"`, `"github.com/sugaf1204/gomi/internal/infra/memory"`.

```go
type resumeExecutor struct {
	libvirt.Executor
	mu            sync.Mutex
	deletedVolume []string
	definedDomain []string
}

func (e *resumeExecutor) DestroyDomain(context.Context, string) error  { return nil }
func (e *resumeExecutor) UndefineDomain(context.Context, string) error { return nil }
func (e *resumeExecutor) Close() error                                 { return nil }

func (e *resumeExecutor) DeleteVolume(_ context.Context, name string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.deletedVolume = append(e.deletedVolume, name)
	return nil
}

func (e *resumeExecutor) DefineDomain(_ context.Context, cfg libvirt.DomainConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.definedDomain = append(e.definedDomain, cfg.Name)
	return nil
}

func TestResumeInterruptedDeploysCleansUpBeforeRetry(t *testing.T) {
	backend := memory.New()
	hypervisors := hypervisor.NewService(backend.Hypervisors(), backend.HypervisorTokens(), backend.AgentTokens())
	vms := NewService(backend.VMs())
	ctx := context.Background()

	if _, err := hypervisors.Create(ctx, hypervisor.Hypervisor{
		Name:       "hv-resume",
		Connection: hypervisor.ConnectionSpec{Type: hypervisor.ConnectionTCP, Host: "192.0.2.10", Port: 16509},
		Phase:      hypervisor.PhaseRegistered,
	}); err != nil {
		t.Fatalf("create hypervisor: %v", err)
	}

	deadline := time.Now().UTC().Add(10 * time.Minute)
	if _, err := vms.Create(ctx, VirtualMachine{
		Name:          "vm-resume",
		HypervisorRef: "hv-resume",
		Resources:     ResourceSpec{CPUCores: 1, MemoryMB: 1024, DiskGB: 8},
		OSImageRef:    "ubuntu-test",
		Phase:         PhasePending,
		Provisioning:  ProvisioningStatus{Active: true, DeadlineAt: &deadline, CompletionToken: "tok-resume"},
	}); err != nil {
		t.Fatalf("create vm: %v", err)
	}

	exec := &resumeExecutor{}
	d := &Deployer{
		Hypervisors: hypervisors,
		VMs:         vms,
		ExecutorFactory: func(context.Context, libvirt.LibvirtConfig) (libvirt.Executor, error) {
			return exec, nil
		},
	}

	if err := d.ResumeInterruptedDeploys(ctx, func(base string, _ InstallConfigType, _ string) string { return base }); err != nil {
		t.Fatalf("ResumeInterruptedDeploys: %v", err)
	}

	exec.mu.Lock()
	defer exec.mu.Unlock()
	if len(exec.deletedVolume) == 0 {
		t.Fatal("expected leftover volume to be deleted before retrying the deploy")
	}
}

func TestResumeInterruptedDeploysFailsExpiredWindow(t *testing.T) {
	backend := memory.New()
	hypervisors := hypervisor.NewService(backend.Hypervisors(), backend.HypervisorTokens(), backend.AgentTokens())
	vms := NewService(backend.VMs())
	ctx := context.Background()

	if _, err := hypervisors.Create(ctx, hypervisor.Hypervisor{
		Name:       "hv-expired",
		Connection: hypervisor.ConnectionSpec{Type: hypervisor.ConnectionTCP, Host: "192.0.2.10", Port: 16509},
		Phase:      hypervisor.PhaseRegistered,
	}); err != nil {
		t.Fatalf("create hypervisor: %v", err)
	}

	expired := time.Now().UTC().Add(-time.Minute)
	if _, err := vms.Create(ctx, VirtualMachine{
		Name:          "vm-expired",
		HypervisorRef: "hv-expired",
		Resources:     ResourceSpec{CPUCores: 1, MemoryMB: 1024, DiskGB: 8},
		OSImageRef:    "ubuntu-test",
		Phase:         PhasePending,
		Provisioning:  ProvisioningStatus{Active: true, DeadlineAt: &expired, CompletionToken: "tok-expired"},
	}); err != nil {
		t.Fatalf("create vm: %v", err)
	}

	d := &Deployer{
		Hypervisors: hypervisors,
		VMs:         vms,
		ExecutorFactory: func(context.Context, libvirt.LibvirtConfig) (libvirt.Executor, error) {
			return &resumeExecutor{}, nil
		},
	}
	if err := d.ResumeInterruptedDeploys(ctx, func(base string, _ InstallConfigType, _ string) string { return base }); err != nil {
		t.Fatalf("ResumeInterruptedDeploys: %v", err)
	}

	got, err := vms.Get(ctx, "vm-expired")
	if err != nil {
		t.Fatalf("get vm: %v", err)
	}
	if got.Phase != PhaseError {
		t.Fatalf("expected expired deploy to be marked Error, got %s", got.Phase)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vm/ -run TestResumeInterruptedDeploys -v`
Expected: FAIL — `d.ResumeInterruptedDeploys undefined`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/vm/resume.go` (add imports `"context"`, `"log"`):

```go
// ResumeInterruptedDeploys restarts deploys that a process restart cut short.
// It runs on every runtime sync tick, so it must be cheap when there is
// nothing to do: the store scan short-circuits on phase before any host call.
func (d *Deployer) ResumeInterruptedDeploys(ctx context.Context, pxeNoCloudFn func(base string, installType InstallConfigType, mac string) string) error {
	items, err := d.VMs.List(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, v := range items {
		switch DecideResumeAction(v, now) {
		case ResumeFail:
			if _, err := d.VMs.FailDeploy(ctx, v.Name, "resume", "deploy interrupted by restart", v.Provisioning.CompletionToken); err != nil {
				log.Printf("resume vm %s: mark failed: %v", v.Name, err)
			}
		case ResumeRedeploy:
			d.resumeOne(ctx, v, pxeNoCloudFn)
		case ResumeNone:
		}
	}
	return nil
}

// resumeOne clears any partial host state before re-running the deploy.
// Volume creation is not idempotent, so a retry without this teardown fails
// with "already exists" whenever the restart landed after volume creation.
func (d *Deployer) resumeOne(ctx context.Context, v VirtualMachine, pxeNoCloudFn func(base string, installType InstallConfigType, mac string) string) {
	hv, err := d.Hypervisors.Get(ctx, v.HypervisorRef)
	if err != nil {
		log.Printf("resume vm %s: resolve hypervisor %s: %v", v.Name, v.HypervisorRef, err)
		return
	}
	if err := d.TeardownHostState(ctx, hv, v); err != nil {
		log.Printf("resume vm %s: teardown partial state: %v", v.Name, err)
		return
	}
	resumed := v
	if err := d.Deploy(ctx, &resumed, pxeNoCloudFn); err != nil {
		log.Printf("resume vm %s: deploy: %v", v.Name, err)
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vm/ -run TestResumeInterruptedDeploys -v`
Expected: PASS (both tests)

Run: `go test ./internal/vm/`
Expected: PASS

- [ ] **Step 5: Add non-curtin coverage**

The two tests above use the default (preseed/PXE) install path. Add one more that sets `InstallCfg` to the curtin type so both OS deploy paths are exercised, per the project OS-deployment policy. Copy `TestResumeInterruptedDeploysCleansUpBeforeRetry`, rename it to `TestResumeInterruptedDeploysCurtinPath`, and set on the created VM:

```go
		InstallCfg: &InstallConfig{Type: InstallConfigCurtin},
```

Verify the field name and constructor against `internal/vm/types.go` before writing it; if `InstallConfig` has required companion fields, populate them.

Run: `go test ./internal/vm/ -run TestResumeInterruptedDeploys -v`
Expected: PASS (all three)

- [ ] **Step 6: Commit**

```bash
git add internal/vm/resume.go internal/vm/resume_test.go
git commit -m "Resume restart-interrupted VM deploys after clearing partial host state"
```

---

### Task 6: Run the resume sweep from the runtime sync loop

`runVMRuntimeSyncLoop` already runs once at startup (`internal/app/sync.go:37`) and every 5s after, which gives recovery within ~5 seconds of a restart.

**Files:**
- Modify: `internal/app/sync.go:33-57`
- Modify: `internal/app/app.go:207-209` (only if the deployer is not already reachable from the loop)

**Interfaces:**
- Consumes: `Deployer.ResumeInterruptedDeploys` (Task 5), `pxehttp.RenderNoCloudLineConfig`.

- [ ] **Step 1: Wire the sweep into the sync tick**

In `internal/app/sync.go`, extend `syncVMRuntimeStates`:

```go
func (r *Runtime) syncVMRuntimeStates(ctx context.Context, syncer *vm.RuntimeSyncer) {
	leaseIPByMAC := r.vmLeaseIPsByMAC(ctx)
	if err := syncer.SyncAll(ctx, leaseIPByMAC); err != nil {
		log.Printf("vm-sync: runtime sync failed: %v", err)
	}
	if r.vmDeployer != nil {
		if err := r.vmDeployer.ResumeInterruptedDeploys(ctx, pxehttp.RenderNoCloudLineConfig); err != nil {
			log.Printf("vm-sync: resume interrupted deploys failed: %v", err)
		}
	}
}
```

Check whether `Runtime` already stores the deployer. Run:

`grep -n "vmDeployer" internal/app/app.go`

`internal/app/app.go:156` builds it as a local `vmDeployer`. If it is not retained on `Runtime`, add a `vmDeployer *vm.Deployer` field to the `Runtime` struct and assign it there (`r.vmDeployer = vmDeployer`) before it is passed to the server config at line 201. Add the `pxehttp` import to `sync.go`.

- [ ] **Step 2: Build and run the suite**

Run: `go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 3: Verify the sweep is idempotent when idle**

Run: `go test ./internal/app/ -v`
Expected: PASS. The sweep runs every 5s; with no `Pending` records it must make no host calls. `DecideResumeAction` returns `ResumeNone` before any hypervisor lookup, so a store read is the only cost.

- [ ] **Step 4: Commit**

```bash
git add internal/app/sync.go internal/app/app.go
git commit -m "Run interrupted-deploy resume sweep from VM runtime sync loop"
```

---

### Task 7: Increment the Quick Deploy count on click

With the server now returning immediately, the remaining collision risk is the UI reusing a name between the click and the response. `count` is already persisted to `localStorage` on change (`web/src/components/views/VirtualMachinesView.tsx:181`), so moving the increment earlier also survives a reload.

**Files:**
- Modify: `web/src/components/views/virtual-machines/useVirtualMachineOperations.ts:130-166`
- Modify: `web/src/components/views/machines/useMachineOperations.ts` (equivalent handler)

**Interfaces:**
- Produces: no signature change; `handleQuickDeploy` reserves its name before awaiting.

- [ ] **Step 1: Move the increment ahead of the request**

In `handleQuickDeploy` (`web/src/components/views/virtual-machines/useVirtualMachineOperations.ts:130`), the increment currently sits at line 158, after `await api.createVirtualMachine`. Move it to just after the readiness check, before the network call:

```ts
    const vmName = quickDeployVMName(preset)
    // Reserve the next name before awaiting so a rapid second click cannot
    // reuse this one. The server rejects duplicates with 409 as a backstop.
    args.setQuickDeployPreset((current) => ({ ...current, count: String(Math.max(1, Number(current.count) || 1) + 1) }))
```

Delete the old increment at line 158.

- [ ] **Step 2: Verify the button re-enables promptly**

`quickDeploying` now spans only the request dispatch, not the deploy. Leave `setQuickDeploying(true)`/`finally { setQuickDeploying(false) }` in place — the server returns as soon as the record is written, so the disabled window is now brief rather than deploy-length.

- [ ] **Step 3: Apply the same change to the Machines side**

Per the project UI policy, both surfaces must behave the same. `handleQuickDeploy` in `web/src/components/views/machines/useMachineOperations.ts:158` mirrors the VM one: the increment sits at line 169, after `await api.createMachine`. Move it up to just after `const machineName = quickDeployMachineName(preset)` (line 164):

```ts
    const machineName = quickDeployMachineName(preset)
    // Reserve the next name before awaiting so a rapid second click cannot
    // reuse this one.
    args.setQuickDeployPreset((current) => ({ ...current, count: String(Math.max(1, Number(current.count) || 1) + 1) }))
```

Delete the old increment at line 169.

Note: the server-side duplicate guard from Task 3 covers `POST /virtual-machines` only. Whether `POST /machines` needs the same 409 guard depends on how the machine store handles a duplicate name — check `internal/machine/service.go` for an existing existence check. If it already rejects duplicates, no server change is needed here; if it upserts like the VM store did, add the equivalent guard and note it in the PR.

- [ ] **Step 4: Typecheck and test**

Run: `cd web && npm run build`
Expected: no TypeScript errors

Run: `cd web && npm test`
Expected: PASS. If no test covers quick deploy naming, add one asserting that two consecutive `handleQuickDeploy` calls request two different names.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/views/virtual-machines/useVirtualMachineOperations.ts web/src/components/views/machines/useMachineOperations.ts
git commit -m "Reserve next Quick Deploy name on click to allow rapid consecutive deploys"
```

---

### Task 8: Full verification

**Files:** none modified unless a defect is found.

- [ ] **Step 1: Run everything**

```bash
go build ./... && go test ./... && cd web && npm run build && npm test
```

Expected: all PASS.

- [ ] **Step 2: Confirm each design-doc verification item**

Walk the "Verification" section of `docs/superpowers/specs/2026-07-25-quick-deploy-async-design.md` and confirm a test covers each. Currently expected coverage:

| Item | Covered by |
|---|---|
| Idempotency (both install paths) | Task 5 Steps 1, 5 |
| Duplicate names | Task 3 Step 1 |
| Restart resume + deadline expiry | Task 5 Step 1 |
| Resume decision matrix | Task 4 Step 1 |
| Existing sync tests still pass | Task 3 Step 6 |
| Cascade delete teardown | Task 2 Step 1 + Task 3 Step 4 |
| Rapid clicks | Task 7 Step 4 |

If any row has no test, write one now rather than marking this task done.

- [ ] **Step 3: Manual check against a real deploy**

This is the part automated tests cannot prove. On the GOMI server, click Quick Deploy several times in quick succession and confirm: distinct VM names, buttons usable throughout, all VMs reaching a terminal phase. Then start a deploy, restart the GOMI service mid-deploy, and confirm the VM resumes and reaches a terminal phase rather than sitting at `Pending`.

Per the project's production-debugging policy, record in the PR notes what was verified live and what remains unverified.

- [ ] **Step 4: Commit any fixes**

```bash
git add -A
git commit -m "Fix issues found during Quick Deploy async verification"
```

---

## Notes for the implementer

- **Task 1 is load-bearing.** Skipping it makes Tasks 3–5 untestable, since `Deploy()` would still construct a real libvirt connection.
- **The volume/domain idempotency asymmetry is the crux.** `DefineDomain` overwrites happily; `CreateVolume` and `CreateOverlayVolume` fail on an existing volume. That asymmetry is the entire reason `resumeOne` tears down before retrying. If you find yourself removing that teardown, the resume path will fail on every VM whose restart landed after volume creation.
- **`DomainObservedAt` is the handover marker**, not a timestamp for display. Nil means the domain does not exist yet and a re-run is safe; non-nil means the runtime sync loop owns the record. The existing comment at `internal/vm/types.go:104-108` explains the original intent.
- **Do not add client-side deploy-result notifications.** Deploy outcomes are already persisted server-side and rendered from the VM list; a parallel client-side channel would be lost on reload, defeating the point of this work.
