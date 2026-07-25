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
| `internal/vm/teardown_test.go` | Teardown behaviour incl. not-found tolerance and the not-attempted marker | Create |
| `internal/vm/resume_test.go` | Resume decision matrix, epoch ownership, cleanup-before-retry | Create |
| `internal/vm/types.go` | `ProvisioningStatus.DeployEpoch` field | Modify |
| `internal/vm/store.go`, `internal/infra/sql/vm_store.go`, `internal/infra/memory/` | Insert-only create for atomic duplicate rejection | Modify |
| `internal/vm/deploy_test.go` | Executor-factory seam coverage | Modify |
| `internal/infra/api/vm.go` | Create handler: 201 early, atomic duplicate guard, background deploy with identity-checked cleanup | Modify |
| `internal/app/sync.go` | Run the resume sweep before `SyncAll`, off the loop's critical path | Modify |
| `web/src/components/views/virtual-machines/useVirtualMachineOperations.ts` | Optimistic count increment for VM Quick Deploy | Modify |
| `web/src/components/views/machines/useMachineOperations.ts` | Same for Machine Quick Deploy | Modify |

Task order matters: Task 1 creates the test seam that Tasks 4–7 depend on.

## Revision note (after review of PR #46)

The first draft of this plan had defects that would have shipped a broken recovery
path. They are corrected below; the reasoning is recorded so the fixes are not
re-simplified away:

1. **The sweep would have killed live deploys.** A healthy deploy between the `201` and
   `DefineDomain` has exactly the state the sweep selected on. Fixed by a deploy owner
   epoch (Task 3) — without it, every Quick Deploy risks a concurrent double deploy.
2. **`DecideResumeAction` duplicated `vmDeployInFlight`** (`internal/vm/runtime_sync.go:323`),
   which already encodes the same predicate and whose comment already anticipates a
   crashed server. Task 5 reuses it instead.
3. **The expired branch was unreachable.** `SyncAll` marks expired records `Missing`
   with `Active=false` before the sweep sees them. Task 7 runs the sweep first.
4. **The expired branch leaked volumes.** Marking `Error` without teardown leaves the
   orphan volume this design exists to prevent.
5. **The duplicate guard was a TOCTOU race.** `Get`-then-`Upsert` lets two concurrent
   requests both win. Task 2 adds an insert-only store operation.
6. **The create test would have passed vacuously.** `setupTestEnv` leaves `VMDeployer`
   nil, so nothing asynchronous ran. Task 4 wires a blocking fake.
7. **The teardown move dropped `ErrVMTeardownNotAttempted`**, breaking the record-only
   delete fallback for unreachable hypervisors (`internal/infra/api/vm.go:217`).

Two further gaps are recorded in the design doc and scoped as separate work below:
the post-definition crash window (Task 7b) and backing-image upload atomicity (Task 7c).

### Second round

The epoch fix above introduced its own failure modes, corrected in turn:

8. **The lease was taken but never released.** A goroutine killed by its own context
   deadline left the record stamped with the current epoch permanently — and `Deploy`'s
   `FailDeploy` runs on that same expired context, so even the `Error` write could fail.
   The sweep would skip a dead worker's record forever, reproducing the stuck-`Pending`
   bug. Ownership is now a lease released on every exit path via a fresh context
   (Task 2c, Task 3).
9. **Ownership did not survive dispatch.** A record classified by the sweep and then
   deleted/recreated before its worker ran would have had the *replacement's* host state
   torn down. Workers now re-read and re-check the completion token before any libvirt
   call (Task 5).
10. **The domain-defined marker was treated as proof.** `markDomainDefined` logs and
    continues when its store write fails (`internal/vm/deploy.go:241`) though
    `DefineDomain` already succeeded, so a nil marker can coexist with a live domain.
    Destructive paths now verify host state before tearing down (Task 5, Task 7b).
11. **Finalization was `Pending`-only.** A deploy whose pre-domain work outlived the
    deadline is marked `Missing`, then re-armed by `markDomainDefined` without a phase
    restore. Such records were ignored by recovery while `SyncAll` mapped them to
    `Provisioning` without ever starting them. Candidates are now selected by
    provisioning window and domain marker across `Pending`/`Missing`/`Provisioning`
    (Task 4).

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

// ErrTeardownNotAttempted marks teardown failures that happened before any host
// mutation (hypervisor connection setup), so a Missing VM may safely fall back
// to a record-only delete. DeleteVirtualMachine depends on this distinction.
var ErrTeardownNotAttempted = errors.New("vm runtime teardown not attempted")

// TeardownHostState removes the libvirt domain and volume a deploy may have
// created. Missing domains and volumes are not errors: the caller uses this to
// clean up partial state whose exact extent is unknown.
func (d *Deployer) TeardownHostState(ctx context.Context, hv hypervisor.Hypervisor, v VirtualMachine) error {
	cfg := BuildLibvirtConfig(hv)
	exec, err := d.newExecutor(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect to hypervisor %s for teardown: %w: %w", hv.Name, err, ErrTeardownNotAttempted)
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

**Preserve the not-attempted marker.** `DeleteVirtualMachine` checks `errors.Is(err, ErrVMTeardownNotAttempted)` at `internal/infra/api/vm.go:217` so a `Missing` VM on an unreachable hypervisor can be deleted record-only instead of returning `502`. Repoint that check at the new `vm.ErrTeardownNotAttempted` and delete the old sentinel at `internal/infra/api/vm.go:237`:

```go
		if v.Phase != vm.PhaseMissing || !errors.Is(err, vm.ErrTeardownNotAttempted) {
```

- [ ] **Step 5b: Prove the fallback still works**

Add to `internal/vm/teardown_test.go`:

```go
func TestTeardownHostStateMarksConnectFailureNotAttempted(t *testing.T) {
	d := &Deployer{
		ExecutorFactory: func(context.Context, libvirt.LibvirtConfig) (libvirt.Executor, error) {
			return nil, errors.New("dial tcp: connection refused")
		},
	}
	err := d.TeardownHostState(context.Background(), hypervisor.Hypervisor{Name: "hv"}, VirtualMachine{Name: "vm-unreachable"})
	if !errors.Is(err, ErrTeardownNotAttempted) {
		t.Fatalf("expected ErrTeardownNotAttempted, got %v", err)
	}
}
```

Also confirm the existing delete-path coverage still passes:

Run: `go test ./internal/infra/api/ -run TestDeleteVirtualMachine -v`
Expected: PASS. If no test covers the Missing + unreachable-hypervisor fallback, add one — this is the behaviour the marker exists for.

- [ ] **Step 6: Run the full suite**

Run: `go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/vm/teardown.go internal/vm/teardown_test.go internal/infra/api/vm.go
git commit -m "Move VM host teardown into internal/vm for reuse by resume sweep"
```

---

### Task 2b: Add an insert-only store operation

The duplicate guard must be atomic. `Upsert`'s `ON CONFLICT (name) DO UPDATE` (`internal/infra/sql/vm_store.go:118`) is precisely what allows a duplicate to overwrite, so the handler cannot fix this on its own.

**Files:**
- Modify: `internal/vm/store.go` (interface), `internal/infra/sql/vm_store.go`, the in-memory store under `internal/infra/memory/`, `internal/vm/service.go`

**Interfaces:**
- Produces: `Store.Insert(ctx context.Context, v VirtualMachine) error` returning `resource.ErrAlreadyExists` on a name collision, and `Service.CreateExclusive(ctx, v) (VirtualMachine, error)` wrapping it with the same validation/normalisation `Create` performs. Task 3 consumes `CreateExclusive`.

- [ ] **Step 1: Write the failing test**

In `internal/vm/service_test.go` (match the file's existing package clause):

```go
func TestCreateExclusiveRejectsDuplicateName(t *testing.T) {
	backend := memory.New()
	svc := vm.NewService(backend.VMs())
	ctx := context.Background()
	v := vm.VirtualMachine{
		Name:          "vm-once",
		HypervisorRef: "hv",
		Resources:     vm.ResourceSpec{CPUCores: 1, MemoryMB: 1024, DiskGB: 8},
		OSImageRef:    "ubuntu-test",
	}
	if _, err := svc.CreateExclusive(ctx, v); err != nil {
		t.Fatalf("first CreateExclusive: %v", err)
	}
	if _, err := svc.CreateExclusive(ctx, v); !errors.Is(err, resource.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}
```

Check whether `resource.ErrAlreadyExists` exists — run `grep -rn "ErrAlreadyExists" internal/resource/`. If it does not, add it beside `ErrNotFound` following that file's style.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vm/ -run TestCreateExclusive -v`
Expected: FAIL — `CreateExclusive` undefined.

- [ ] **Step 3: Implement across the interface and both stores**

Add `Insert` to the `Store` interface in `internal/vm/store.go`. In `internal/infra/sql/vm_store.go`, mirror `Upsert` but without the conflict clause, translating the driver's unique-violation into `resource.ErrAlreadyExists`:

```go
func (s *VMStore) Insert(ctx context.Context, v vm.VirtualMachine) error {
	specJSON, statusJSON, err := marshalVMColumns(v)
	if err != nil {
		return err
	}
	_, err = s.b.exec(ctx, `
		INSERT INTO virtual_machines (name, hypervisor_ref, spec, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		v.Name, v.HypervisorRef, specJSON, statusJSON, v.CreatedAt, v.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return resource.ErrAlreadyExists
		}
		return err
	}
	s.notify()
	return nil
}
```

`isUniqueViolation` must cover both drivers this project supports (SQLite and PostgreSQL). Check whether a helper already exists — `grep -rn "unique\|UNIQUE\|23505" internal/infra/sql/` — and reuse it; only write a new one if none is there.

In `internal/vm/service.go`, add `CreateExclusive` alongside `Create`, sharing the same preparation. Extract the common setup rather than copying it, so the two cannot drift.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/vm/ ./internal/infra/sql/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/vm/store.go internal/vm/service.go internal/vm/service_test.go internal/infra/sql/vm_store.go internal/infra/memory/ internal/resource/
git commit -m "Add insert-only VM store operation for atomic duplicate rejection"
```

---

### Task 2c: Add the deploy owner epoch

Without this, the sweep cannot distinguish a crashed deploy from a healthy one that has not yet defined its domain — they are byte-identical in the database — and would tear down live deploys every 5 seconds.

**Files:**
- Modify: `internal/vm/types.go:96-110` (`ProvisioningStatus`), `internal/app/app.go`, `internal/infra/api/server.go`, `internal/infra/api/vm.go`

**Interfaces:**
- Produces: `ProvisioningStatus.DeployEpoch string`, a per-process epoch generated at startup and exposed as `Runtime.deployEpoch` and `Server.deployEpoch`; and `Service.ReleaseDeployEpoch(ctx context.Context, name, completionToken string) error`, which clears the stamp only when the stored token still matches. Tasks 3, 5 and 6 consume both.

**The lease must be released, or this makes things worse.** A stamp that is never
cleared means a goroutine killed by its own context deadline leaves the record marked
"owned" permanently — and `Deploy`'s `FailDeploy` runs on that same expired context, so
even the `Error` write may not land. The sweep would then skip a dead worker's record
forever, reproducing the stuck-`Pending` bug in a new form.

- [ ] **Step 1: Add the field**

In `internal/vm/types.go`, inside `ProvisioningStatus`:

```go
	// DeployEpoch identifies the server process that started this deploy. The
	// resume sweep uses it to tell an orphaned deploy from a live one: a
	// record carrying the running process's epoch is owned by a goroutine that
	// is either still working or died with the process. Records from an
	// earlier epoch have no owner and are safe to recover.
	DeployEpoch string `json:"deployEpoch,omitempty"`
```

It is stored inside the existing `status` JSON column, so no migration is required — confirm by checking `marshalVMColumns` in `internal/infra/sql/vm_store.go`.

- [ ] **Step 2: Generate it once per process**

In `internal/app/app.go`, generate an epoch when the runtime is built and store it on `Runtime`. Reuse the existing token generator (`httputil.GenerateProvisioningToken`, used at `internal/infra/api/vm.go:58`) rather than adding a new random source. Pass it into `ServerConfig` so the create handler can stamp it.

- [ ] **Step 3: Stamp it on create**

In `internal/infra/api/vm.go`, extend the `v.Provisioning` initialisation at line 63:

```go
	v.Provisioning = vm.ProvisioningStatus{
		Active:          true,
		StartedAt:       httputil.TimePtr(now),
		DeadlineAt:      httputil.TimePtr(now.Add(s.provisionTimeout)),
		CompletionToken: token,
		DeployEpoch:     s.deployEpoch,
	}
```

- [ ] **Step 3b: Add the release operation**

In `internal/vm/service.go`, beside `FailDeploy` (which already shows the token-guard pattern at `internal/vm/service.go:193-200`):

```go
// ReleaseDeployEpoch clears the ownership stamp when the deploy that took it
// finishes or dies. The token guard stops a stale worker from releasing a lease
// a newer deploy has since taken.
func (s *Service) ReleaseDeployEpoch(ctx context.Context, name, completionToken string) error {
	v, err := s.store.Get(ctx, name)
	if err != nil {
		if errors.Is(err, resource.ErrNotFound) {
			return nil
		}
		return err
	}
	if v.Provisioning.CompletionToken != completionToken || v.Provisioning.DeployEpoch == "" {
		return nil
	}
	v.Provisioning.DeployEpoch = ""
	v.UpdatedAt = time.Now().UTC()
	_, err = writeExisting(ctx, s.store, v)
	return err
}
```

Add a test asserting that a release with a mismatched token is a no-op, and that a matching release clears the stamp so `DecideResumeAction` stops returning `ResumeNone`.

- [ ] **Step 4: Build and test**

Run: `go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/vm/types.go internal/vm/service.go internal/vm/service_test.go internal/app/app.go internal/infra/api/server.go internal/infra/api/vm.go
git commit -m "Stamp and release a per-process deploy ownership lease"
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

**Two prerequisites, or these tests are worthless:**

1. `setupTestEnv` does not seed any OS image, so `applyInstallConfigByOSImage` (`internal/infra/api/vm.go:55`) rejects the request with `400` before anything else runs. Seed a vm-capable qcow2 image named `ubuntu-test` via `env.osimages` in `seedVMHypervisor`, matching the fields `applyInstallConfigByOSImage` requires — read that function to see which they are.

2. `setupTestEnv` never sets `ServerConfig.VMDeployer` (`internal/infra/api/handler_test.go:65-101` wires `OSImages` but not the deployer), so `s.vmDeployer` is nil and **no goroutine ever starts**. `TestCreateVirtualMachineReturnsBeforeDeployCompletes` would then pass without proving anything.

Add a variant that injects a deployer which blocks until released, so the test proves the response arrives *while the deploy is in flight*:

```go
func TestCreateVirtualMachineRespondsWhileDeployInFlight(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	env := setupTestEnvWithVMDeployer(t, blockingDeployer(started, release))
	seedVMHypervisor(t, env, "hv-block")
	t.Cleanup(func() { close(release) })

	rec := doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmCreateBody("vm-block", "hv-block"), env.token)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 before the deploy finishes, got %d: %s", rec.Code, rec.Body.String())
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("deploy goroutine did not start")
	}
}
```

`setupTestEnvWithVMDeployer` does not exist yet — add it beside the existing `setupTestEnvWithVMRuntimeDeleter` (`internal/infra/api/handler_test.go:55`), following that helper's shape. The blocking deployer must satisfy whatever type `ServerConfig.VMDeployer` takes; since it is currently the concrete `*vm.Deployer` (`internal/infra/api/server.go:107`), either introduce a narrow interface for it or inject a `*vm.Deployer` whose `ExecutorFactory` (Task 1) returns a fake that blocks in `CreateVolume`. Prefer the latter — it needs no production type change.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/infra/api/ -run TestCreateVirtualMachine -v`
Expected: FAIL — the duplicate create returns 201, and the phase is not `Pending`.

- [ ] **Step 3: Add the atomic duplicate guard**

A `Get`-then-`Upsert` check in the handler is a TOCTOU race: two concurrent requests can both find the name absent and both upsert, so one silently overwrites the other and both return `201`. The rejection must happen atomically in the store, where the `name` primary key can enforce it.

This depends on Task 2b (insert-only store operation). In `internal/infra/api/vm.go`, replace `created, err := s.vms.Create(ctx, v)` (line 91) with the insert-only call and map the conflict:

```go
	created, err := s.vms.CreateExclusive(ctx, v)
	if errors.Is(err, resource.ErrAlreadyExists) {
		return c.JSON(gohttp.StatusConflict, jsonError("virtual machine already exists: "+v.Name))
	}
	if err != nil {
		return c.JSON(gohttp.StatusBadRequest, jsonErrorErr(err))
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

	token := created.Provisioning.CompletionToken

	// Release the ownership lease on every exit path, using a context that is
	// NOT the (possibly already expired) deploy context. Without this, a
	// goroutine killed by its own deadline leaves the record stamped with the
	// current epoch forever, and the sweep treats a dead worker as live —
	// exactly the stuck-Pending failure this design removes.
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer releaseCancel()
		if err := s.vms.ReleaseDeployEpoch(releaseCtx, created.Name, token); err != nil {
			log.Printf("create vm %s: release deploy lease: %v", created.Name, err)
		}
	}()

	deployErr := s.vmDeployer.Deploy(ctx, &created, pxehttp.RenderNoCloudLineConfig)

	// Compare deploy identity, not mere existence. With rapid consecutive
	// deploys a VM can be deleted and recreated under the same name while this
	// goroutine runs; an existence-only check would find the new record, skip
	// cleanup, and orphan this deploy's domain and volume on deployHV.
	current, getErr := s.vms.Get(ctx, created.Name)
	stale := errors.Is(getErr, resource.ErrNotFound) ||
		(getErr == nil && current.Provisioning.CompletionToken != token)
	if stale {
		if cleanupErr := s.vmDeployer.TeardownHostState(ctx, deployHV, created); cleanupErr != nil {
			log.Printf("create vm %s: cleanup after superseded deploy: %v", created.Name, cleanupErr)
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

	const currentEpoch = "epoch-current"
	const priorEpoch = "epoch-previous"

	tests := []struct {
		name string
		vm   VirtualMachine
		want ResumeAction
	}{
		{
			name: "interrupted deploy from a previous process resumes",
			vm:   VirtualMachine{Phase: PhasePending, Provisioning: ProvisioningStatus{Active: true, DeadlineAt: &future, DeployEpoch: priorEpoch}},
			want: ResumeRedeploy,
		},
		{
			name: "live deploy owned by this process is left alone",
			vm:   VirtualMachine{Phase: PhasePending, Provisioning: ProvisioningStatus{Active: true, DeadlineAt: &future, DeployEpoch: currentEpoch}},
			want: ResumeNone,
		},
		{
			name: "interrupted deploy past deadline fails",
			vm:   VirtualMachine{Phase: PhasePending, Provisioning: ProvisioningStatus{Active: true, DeadlineAt: &past, DeployEpoch: priorEpoch}},
			want: ResumeFail,
		},
		{
			name: "domain already defined is finalized, not redeployed",
			vm:   VirtualMachine{Phase: PhasePending, Provisioning: ProvisioningStatus{Active: true, DeadlineAt: &future, DomainObservedAt: &observed, DeployEpoch: priorEpoch}},
			want: ResumeFinalize,
		},
		{
			name: "re-armed Missing record with a defined domain is finalized",
			vm:   VirtualMachine{Phase: PhaseMissing, Provisioning: ProvisioningStatus{Active: true, DeadlineAt: &future, DomainObservedAt: &observed, DeployEpoch: priorEpoch}},
			want: ResumeFinalize,
		},
		{
			name: "provisioning record with a defined domain is finalized",
			vm:   VirtualMachine{Phase: PhaseProvisioning, Provisioning: ProvisioningStatus{Active: true, DeadlineAt: &future, DomainObservedAt: &observed, DeployEpoch: priorEpoch}},
			want: ResumeFinalize,
		},
		{
			name: "released lease makes a timed-out worker's record recoverable",
			vm:   VirtualMachine{Phase: PhasePending, Provisioning: ProvisioningStatus{Active: true, DeadlineAt: &future, DeployEpoch: ""}},
			want: ResumeRedeploy,
		},
		{
			name: "inactive provisioning window is ignored",
			vm:   VirtualMachine{Phase: PhasePending, Provisioning: ProvisioningStatus{Active: false, DeadlineAt: &future, DeployEpoch: priorEpoch}},
			want: ResumeNone,
		},
		{
			name: "running vm is ignored",
			vm:   VirtualMachine{Phase: PhaseRunning, Provisioning: ProvisioningStatus{Active: true, DeadlineAt: &future, DeployEpoch: priorEpoch}},
			want: ResumeNone,
		},
		{
			name: "completed window is ignored",
			vm:   VirtualMachine{Phase: PhasePending, Provisioning: ProvisioningStatus{Active: true, DeadlineAt: &future, CompletedAt: &observed, DeployEpoch: priorEpoch}},
			want: ResumeNone,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := DecideResumeAction(tc.vm, currentEpoch, now); got != tc.want {
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
	// ResumeNone means the record needs no resume handling: it is not
	// mid-deploy, it is owned by a live goroutine in this process, or its
	// libvirt domain already exists and the runtime sync loop owns it.
	ResumeNone ResumeAction = iota
	// ResumeRedeploy means the deploy never reached domain definition, so the
	// host state must be cleaned up and the deploy re-run.
	ResumeRedeploy
	// ResumeFail means the provisioning window expired while the deploy was
	// interrupted. The record becomes Error, but its partial host state must
	// still be cleaned up first.
	ResumeFail
)

// DecideResumeAction classifies a VM record as recovery work for this process.
//
// currentEpoch identifies the running server process. A record stamped with it
// belongs to a deploy goroutine in *this* process: either still running, or
// gone with the process — and if the process is alive, so is the goroutine.
// Without this check the sweep cannot tell a crashed deploy from a healthy one
// that simply has not defined its domain yet, because those states are
// identical in the database. Resuming a healthy deploy would delete the volume
// a live goroutine is using and start a second concurrent Deploy.
//
// The shared "is this deploy still preparing its domain" predicate lives in
// vmDeployInFlight; this function adds only ownership and expiry on top.
func DecideResumeAction(v VirtualMachine, currentEpoch string, now time.Time) ResumeAction {
	// Phase alone does not select candidates. When pre-domain work outlives the
	// deadline the sync loop marks the record Missing, and markDomainDefined
	// then re-arms the window without restoring the phase
	// (internal/vm/deploy.go:237), so a crashed deploy can be Missing or
	// Provisioning rather than Pending.
	switch v.Phase {
	case PhasePending, PhaseMissing, PhaseProvisioning:
	default:
		return ResumeNone
	}
	if !v.Provisioning.Active || v.Provisioning.CompletedAt != nil {
		return ResumeNone
	}
	// A live goroutine in this process owns it. The lease is released on every
	// exit path, so a stamp that is still present means the owner is alive.
	if v.Provisioning.DeployEpoch == currentEpoch {
		return ResumeNone
	}
	if v.Provisioning.DomainObservedAt != nil {
		return ResumeFinalize
	}
	if v.Provisioning.DeadlineAt != nil && now.After(*v.Provisioning.DeadlineAt) {
		return ResumeFail
	}
	return ResumeRedeploy
}
```

`ResumeFinalize` is defined in Task 7b. Add the constant here so the decision function is complete in one place, and let Task 7b supply its execution path.

**The marker is a hint, not proof.** `markDomainDefined` logs and continues when its store write fails (`internal/vm/deploy.go:241`) even though `DefineDomain` already succeeded, so a nil `DomainObservedAt` does not guarantee the absence of a domain. `ResumeFail` and `ResumeRedeploy` are therefore both host-verified at execution time: the worker checks whether the domain exists before any destructive teardown and finalizes instead if it does. Only the executor knows this; the pure decision function cannot.

Note `vmDeployInFlight` (`internal/vm/runtime_sync.go:323`) already encodes active + not-timed-out + `DomainObservedAt == nil`. Do not restate that logic — if you find yourself copying its body, call it instead. The two functions must stay in agreement, since the sync loop uses it to decide whether to leave a mid-deploy VM alone.

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
	createdVolume []string
	definedDomain []string
}

func (e *resumeExecutor) DestroyDomain(context.Context, string) error  { return nil }
func (e *resumeExecutor) UndefineDomain(context.Context, string) error { return nil }
func (e *resumeExecutor) Close() error                                 { return nil }

// The deploy path is exercised for real, so every method Deploy calls must be
// implemented. Leaving them to the nil embedded interface panics as soon as the
// retry reaches CreateVolume, which would silently gut this test.
func (e *resumeExecutor) CreateVolume(_ context.Context, name string, _ int, _ string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.createdVolume = append(e.createdVolume, name)
	return nil
}

func (e *resumeExecutor) CreateOverlayVolume(_ context.Context, name string, _ int, _ string, _ string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.createdVolume = append(e.createdVolume, name)
	return nil
}

func (e *resumeExecutor) VolumeExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (e *resumeExecutor) StartDomain(context.Context, string) error              { return nil }
func (e *resumeExecutor) SetDomainBootDevice(context.Context, string, string) error { return nil }

func (e *resumeExecutor) DomainInterfaces(context.Context, string) ([]libvirt.DomainInterface, error) {
	return nil, nil
}

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

	if err := d.ResumeInterruptedDeploys(ctx, "epoch-current", func(base string, _ InstallConfigType, _ string) string { return base }); err != nil {
		t.Fatalf("ResumeInterruptedDeploys: %v", err)
	}

	exec.mu.Lock()
	defer exec.mu.Unlock()
	if len(exec.deletedVolume) == 0 {
		t.Fatal("expected leftover volume to be deleted before retrying the deploy")
	}
	// The retry must actually run, not just clean up.
	if len(exec.createdVolume) == 0 {
		t.Fatal("expected the deploy to be re-run after cleanup")
	}
	if len(exec.definedDomain) == 0 {
		t.Fatal("expected the resumed deploy to define the domain")
	}
}

func TestResumeInterruptedDeploysLeavesLiveDeployAlone(t *testing.T) {
	backend := memory.New()
	hypervisors := hypervisor.NewService(backend.Hypervisors(), backend.HypervisorTokens(), backend.AgentTokens())
	vms := NewService(backend.VMs())
	ctx := context.Background()

	if _, err := hypervisors.Create(ctx, hypervisor.Hypervisor{
		Name:       "hv-live",
		Connection: hypervisor.ConnectionSpec{Type: hypervisor.ConnectionTCP, Host: "192.0.2.10", Port: 16509},
		Phase:      hypervisor.PhaseRegistered,
	}); err != nil {
		t.Fatalf("create hypervisor: %v", err)
	}

	// Exactly the state a healthy deploy occupies between the 201 response and
	// DefineDomain: Pending, active window, no domain yet — but stamped with
	// this process's epoch.
	deadline := time.Now().UTC().Add(10 * time.Minute)
	if _, err := vms.Create(ctx, VirtualMachine{
		Name:          "vm-live",
		HypervisorRef: "hv-live",
		Resources:     ResourceSpec{CPUCores: 1, MemoryMB: 1024, DiskGB: 8},
		OSImageRef:    "ubuntu-test",
		Phase:         PhasePending,
		Provisioning:  ProvisioningStatus{Active: true, DeadlineAt: &deadline, CompletionToken: "tok-live", DeployEpoch: "epoch-current"},
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
	if err := d.ResumeInterruptedDeploys(ctx, "epoch-current", func(base string, _ InstallConfigType, _ string) string { return base }); err != nil {
		t.Fatalf("ResumeInterruptedDeploys: %v", err)
	}

	exec.mu.Lock()
	defer exec.mu.Unlock()
	if len(exec.deletedVolume) != 0 || len(exec.definedDomain) != 0 {
		t.Fatalf("sweep touched a live deploy: deleted=%v defined=%v", exec.deletedVolume, exec.definedDomain)
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

	exec := &resumeExecutor{}
	d := &Deployer{
		Hypervisors: hypervisors,
		VMs:         vms,
		ExecutorFactory: func(context.Context, libvirt.LibvirtConfig) (libvirt.Executor, error) {
			return exec, nil
		},
	}
	if err := d.ResumeInterruptedDeploys(ctx, "epoch-current", func(base string, _ InstallConfigType, _ string) string { return base }); err != nil {
		t.Fatalf("ResumeInterruptedDeploys: %v", err)
	}

	got, err := vms.Get(ctx, "vm-expired")
	if err != nil {
		t.Fatalf("get vm: %v", err)
	}
	if got.Phase != PhaseError {
		t.Fatalf("expected expired deploy to be marked Error, got %s", got.Phase)
	}
	// An expired deploy may have created a volume before the crash; leaving it
	// behind would make a later redeploy under the same name fail.
	exec.mu.Lock()
	defer exec.mu.Unlock()
	if len(exec.deletedVolume) == 0 {
		t.Fatal("expected partial host state to be cleaned up before failing the record")
	}
	if len(exec.definedDomain) != 0 {
		t.Fatal("expired deploy must not be re-run")
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
func (d *Deployer) ResumeInterruptedDeploys(ctx context.Context, currentEpoch string, pxeNoCloudFn func(base string, installType InstallConfigType, mac string) string) error {
	items, err := d.VMs.List(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, v := range items {
		switch DecideResumeAction(v, currentEpoch, now) {
		case ResumeFail:
			d.failInterrupted(ctx, v)
		case ResumeRedeploy:
			d.resumeOne(ctx, v, pxeNoCloudFn)
		case ResumeNone:
		}
	}
	return nil
}

// failInterrupted marks an expired interrupted deploy as failed. Host cleanup
// runs first: a crash after volume creation leaves an orphan that would make a
// later redeploy under the same name fail with "already exists". The record is
// marked Error regardless, so a cleanup failure does not leave it Pending
// forever; the leftover volume is reported for operator follow-up.
func (d *Deployer) failInterrupted(ctx context.Context, v VirtualMachine) {
	if hv, err := d.Hypervisors.Get(ctx, v.HypervisorRef); err != nil {
		log.Printf("resume vm %s: resolve hypervisor for cleanup: %v", v.Name, err)
	} else if err := d.TeardownHostState(ctx, hv, v); err != nil {
		log.Printf("resume vm %s: cleanup after expired deploy failed, host state may remain: %v", v.Name, err)
	}
	if _, err := d.VMs.FailDeploy(ctx, v.Name, "resume", "deploy interrupted by restart", v.Provisioning.CompletionToken); err != nil {
		log.Printf("resume vm %s: mark failed: %v", v.Name, err)
	}
}

// resumeOne clears any partial host state before re-running the deploy.
// Volume creation is not idempotent, so a retry without this teardown fails
// with "already exists" whenever the restart landed after volume creation.
func (d *Deployer) resumeOne(ctx context.Context, v VirtualMachine, pxeNoCloudFn func(base string, installType InstallConfigType, mac string) string) {
	// The record was classified on a snapshot and may have been dispatched to a
	// worker some time ago. Re-read it: if it was deleted and recreated under
	// the same name meanwhile, this worker would otherwise tear down the
	// replacement's host state. Token-gated status writes protect rows, not
	// host mutations, so this check must happen before any libvirt call.
	current, err := d.VMs.Get(ctx, v.Name)
	if err != nil || current.Provisioning.CompletionToken != v.Provisioning.CompletionToken {
		log.Printf("resume vm %s: superseded before recovery, skipping", v.Name)
		return
	}

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

Apply the same re-read guard at the top of `failInterrupted`, for the same reason: it also tears down host state.

**Host verification before destructive work.** Both `resumeOne` and `failInterrupted` must check whether the domain already exists before tearing anything down, because a nil `DomainObservedAt` is not proof that `DefineDomain` never ran — `markDomainDefined` continues after a failed marker write (`internal/vm/deploy.go:241`). Query the domain via the executor; if it exists, hand off to the finalize path (Task 7b) instead of destroying a defined or running domain.

Add a test for this: a record with a nil marker whose fake executor reports an existing domain must not be torn down.

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
	// The resume classification must run BEFORE SyncAll. For an expired
	// interrupted deploy with no domain, vmDeployInFlight returns false and
	// markVMMissing rewrites the record to Missing with Active=false —
	// destroying the very state DecideResumeAction needs to recognise it. A
	// sweep running afterwards would return ResumeNone and the deploy would
	// never be failed or cleaned up.
	if r.vmDeployer != nil {
		if err := r.vmDeployer.ResumeInterruptedDeploys(ctx, r.deployEpoch, pxehttp.RenderNoCloudLineConfig); err != nil {
			log.Printf("vm-sync: resume interrupted deploys failed: %v", err)
		}
	}
	leaseIPByMAC := r.vmLeaseIPsByMAC(ctx)
	if err := syncer.SyncAll(ctx, leaseIPByMAC); err != nil {
		log.Printf("vm-sync: runtime sync failed: %v", err)
	}
}
```

**Do not let recovery block the tick.** As written above, `ResumeInterruptedDeploys` performs teardown and a full `Deploy` synchronously on the only VM runtime-sync loop. One stuck hypervisor or slow backing-image transfer would stall every subsequent tick and freeze status updates for all other VMs.

Make the scan cheap and the work asynchronous: `ResumeInterruptedDeploys` classifies records and hands `ResumeRedeploy`/`ResumeFail` items to a bounded worker pool, returning immediately. Guard against a record being picked up twice by consecutive ticks — an in-flight set keyed by VM name, checked before dispatch, is sufficient. The classification itself only reads the store, so it stays fast.

Because the resumed deploy now runs off the tick, it must stamp the current epoch on the record when it starts, exactly as `CreateVirtualMachine` does; otherwise the next tick classifies it as interrupted again and dispatches a duplicate.

Check whether `Runtime` already stores the deployer. Run:

`grep -n "vmDeployer" internal/app/app.go`

`internal/app/app.go:156` builds it as a local `vmDeployer`. If it is not retained on `Runtime`, add a `vmDeployer *vm.Deployer` field to the `Runtime` struct and assign it there (`r.vmDeployer = vmDeployer`) before it is passed to the server config at line 201. Add the `pxehttp` import to `sync.go`. `r.deployEpoch` comes from Task 3.

- [ ] **Step 1b: Test the ordering through the real tick**

Calling `ResumeInterruptedDeploys` directly cannot catch the ordering bug — only the combined tick can. Add a test in `internal/app` that seeds an expired interrupted deploy (Pending, active window, past deadline, prior epoch, no domain), runs `syncVMRuntimeStates` once, and asserts the record ends `Error` rather than `Missing`:

```go
	if got.Phase != vm.PhaseError {
		t.Fatalf("expected expired interrupted deploy to be failed by the sweep, got %s", got.Phase)
	}
```

If `internal/app` has no existing harness for building a `Runtime` with in-memory stores, follow whatever the package's current tests do; if none exist, place this test in `internal/vm` instead, calling the sweep and `SyncAll` in the same order the tick uses.

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

### Task 7b: Recover the post-definition crash window

`markDomainDefined` persists at `internal/vm/deploy.go:91`, but `StartDomain` (`:102`) and the network-to-`hd` boot-device switch (`:108`) run after it. A crash in either gap leaves `DomainObservedAt` set, so the resume rules skip the record — yet the runtime sync loop only observes domain state; it never starts an inactive guest or completes the boot-device switch. The VM sits until its window times out, or reboots back into PXE.

**Files:**
- Modify: `internal/vm/resume.go`, `internal/vm/resume_test.go`

- [ ] **Step 1: Implement the finalize execution path**

`DecideResumeAction` already returns `ResumeFinalize` (Task 4) for a prior-epoch record with an active window and `DomainObservedAt` set, in any of the `Pending`/`Missing`/`Provisioning` phases. This task supplies the executor.

Recovery does not redeploy: it inspects the domain and, if it is not running, calls `StartDomain` and reasserts the boot device to `hd`. Both operations are idempotent — starting a running domain and setting an already-`hd` boot device are no-ops — so the path is safe to repeat. Restore the phase to `Provisioning` on success, since a re-armed record may still read `Missing`.

Do not fold this into `ResumeRedeploy`: tearing down a defined domain to rebuild it would discard an install that may already be under way.

- [ ] **Step 2: Test both crash windows and the re-armed Missing case**

Using the `resumeExecutor` fake:

- domain exists but is shut off → expect `StartDomain`
- domain is running with boot device still `network` → expect `SetDomainBootDevice` to `hd`
- record is `Missing` with an active re-armed window and a defined domain → expect finalize, not `ResumeNone`. This is the timeout-before-define path: `markDomainDefined` re-arms the window (`internal/vm/deploy.go:237`) without restoring the phase, and `SyncAll` maps the shut-off domain to `Provisioning` (`internal/vm/runtime_sync.go:401-405`) but never starts it.
- nil `DomainObservedAt` but the domain exists on the host → expect finalize, **not** teardown. This is the lost-checkpoint case from `internal/vm/deploy.go:241`.

Assert no volume is deleted in any of these.

- [ ] **Step 3: Commit**

```bash
git add internal/vm/resume.go internal/vm/resume_test.go
git commit -m "Complete post-definition deploy steps after an interrupted deploy"
```

---

### Task 7c: Make backing image publication atomic

`prepareCloudImageBacking` treats `VolumeExists` as proof a previous upload completed (`internal/vm/cloud_image.go:47-52`). A crash during `StorageVolUpload` leaves the shared hashed backing volume allocated but partially written. `TeardownHostState` will not remove it — it is keyed by image hash, not VM name — so a resumed deploy builds an overlay on corrupt data and the guest boots garbage.

This is not resume-specific: any later deploy reusing that image hits the same corrupt backing.

**Files:**
- Modify: `internal/vm/cloud_image.go`, `internal/vm/cloud_image_download_test.go`

- [ ] **Step 1: Make publication atomic**

Upload to a temporary volume name and rename on success, so a partially written volume never occupies the final name. If the storage backend cannot rename, persist a completion marker that `prepareCloudImageBacking` verifies before treating an existing volume as reusable. Read `internal/libvirt/storage.go` to see which primitives are available before choosing.

- [ ] **Step 2: Test the interrupted upload**

Using the existing `fakeCloudImageStorage` (`internal/vm/deploy_test.go:15`), simulate an upload that fails partway and assert the next `prepareCloudImageBacking` re-uploads rather than reusing the partial volume.

- [ ] **Step 3: Commit**

```bash
git add internal/vm/cloud_image.go internal/vm/cloud_image_download_test.go
git commit -m "Publish cloud image backing volumes atomically"
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
| **Live deploy untouched by the sweep** | Task 5 Step 1 (`TestResumeInterruptedDeploysLeavesLiveDeployAlone`) |
| Idempotency (both install paths) | Task 5 Steps 1, 5 |
| Duplicate names (concurrent) | Task 2b Step 1 + Task 3 Step 1 |
| Restart resume + deadline expiry with cleanup | Task 5 Step 1 |
| Resume decision matrix incl. epoch ownership | Task 4 Step 1 |
| Sweep-before-SyncAll ordering via the real tick | Task 6 Step 1b |
| Async response proven with a blocking deployer | Task 3 Step 1 |
| Teardown not-attempted marker preserved | Task 2 Step 5b |
| Stale-deploy identity check | Task 3 Step 4 |
| Post-definition crash windows | Task 7b Step 2 |
| Lease released on goroutine timeout | Task 2c Step 3b + Task 4 Step 1 |
| Dispatch race (recreated during recovery) | Task 5 Step 3 (`resumeOne` re-read) |
| Lost checkpoint: defined domain, nil marker | Task 5 Step 3 + Task 7b Step 2 |
| Re-armed Missing finalized | Task 4 Step 1 + Task 7b Step 2 |
| Backing image integrity | Task 7c Step 2 |
| Existing sync tests still pass | Task 3 Step 6 |
| Cascade delete teardown | Task 2 Step 1 + Task 3 Step 4 |
| Rapid clicks | Task 7 Step 4 |
| Sync loop not blocked by recovery | Task 6 Step 1 (worker pool) |

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
