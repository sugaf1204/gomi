# Quick Deploy Async Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **STOP — this plan is not ready to execute.** Nine review rounds have produced 73
> findings and the count is not converging. Round nine established a general rule the
> earlier rounds were discovering piecemeal: **detaching the deploy makes every code
> path that touches a VM name, and every write that touches its record, part of this
> change.** The per-name guard grew from create→create (round 7) to redeploy (round 8)
> to delete (round 9); the conditional-write requirement grew from
> `UpdateDeployStatus`/`FailDeploy` to `markDomainDefined`. There is no reason to think
> that enumeration is complete.
>
> The approved subset also turned out to depend on finding 20 (11 of 12 `rpcExecutor`
> methods ignore their context), which round five had classified as out of scope: a
> bounded deploy pool cannot be added safely until RPCs are cancellable.
>
> **Decision required before implementation** — see "Scope options" below. Do not begin
> Task 1 until one is chosen.

> **SCOPE — read before executing anything.** Only **Tasks 1, 2, 2b, 3 and 7** are approved for implementation. **Tasks 2c, 3b, 4, 5, 6, 7b and 7c are DEFERRED and must not be built**: five review rounds found 32 defects in that recovery design, three of which cannot be fixed inside this feature (see the fifth-round log below). They are retained as a record of the constraints, not as work items. An agent executing this plan task-by-task must skip them, and Task 8 verifies only the approved subset.

**Goal:** Make Quick Deploy return control to the user immediately, so consecutive deploys are possible, without letting repeated clicks or a page reload corrupt a deploy.

**Architecture:** The VM create handler persists the record and returns `201` at once, running `Deploy()` on a detached-context goroutine so a disconnecting client cannot cancel it. Duplicate names are rejected atomically at the store. The UI reserves the next name on click rather than after the response.

**Out of scope:** automatic recovery of deploys interrupted by a *server restart*. Such a record remains `Pending` until its provisioning window expires, after which the existing runtime sync loop marks it `Missing` — the same behaviour as today. Recovery is the operator's existing Redeploy action. See the design doc's status section for why the automatic path was abandoned.

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

### Ninth round — recorded, not patched

Eight findings, all verified against the code. They are recorded rather than fixed
because they demonstrate a pattern rather than eight independent bugs.

**The guard keeps growing.** Round 7 serialised create-vs-create; round 8 added
redeploy; round 9 adds delete:

29. `DeleteVirtualMachine` is not in the per-name guard. A user can delete the freshly
    returned `Pending` VM — the handler sees no domain yet, removes the record, returns
    `204` — while the create worker goes on to build a volume and domain. The worker's
    stale cleanup only runs after `Deploy` returns, and most libvirt calls ignore
    cancellation, so artifacts can outlive a successful delete indefinitely.

**The conditional-write requirement keeps growing.** Round 8 covered
`UpdateDeployStatus`/`FailDeploy`; round 9 finds another:

30. `markDomainDefined` (`internal/vm/deploy.go:210`) is the same
    Get → token-compare → name-only `writeExisting` sequence, so a stale worker can
    overwrite a replacement's row, token included.

**Remaining findings:**

31. The post-deadline success repair passes `created.Phase`, which is still `Pending`
    because `Deploy`'s expired-context write never assigned back — so it would persist
    `Pending`/`create` rather than the real outcome.
32. Registering `onDeployWorkerDone` after the recovery defer makes it fire *first*
    during panic unwinding (LIFO), so a test can tear down while `FailDeploy` and the
    audit write are still running.
33. Moving Machine token creation after the insert leaves the token fields only in
    memory; the row is never updated, so PXE issues a second token and orphans the first.
34. No approved test exercises the non-curtin `CreateVolume`/PXE branch —
    `inferInstallConfigType` returns curtin for every non-empty family, so both the
    Ubuntu and Debian fixtures take the same path.
35. Task 7's commit stages only the two TypeScript files, omitting every Machine-side
    server and test file Step 3b requires.
36. The manual restart check demands a `Missing` outcome, but a crash after
    `DomainObservedAt` is persisted yields `Error` via `IsProvisioningTimedOut` — so
    correct late-stage behaviour would read as a regression.

### Tenth round — the guard grows a fourth time

37. **Power actions are not in the guard.** `runPrimaryAction` gates `console` and
    `migrate` on phase but not power
    (`web/src/components/views/virtual-machines/useVirtualMachineOperations.ts:350-351`),
    and the server's power handlers have no phase rejection either. Immediately after
    the new `201`, a user can power off a `Pending` VM — marking it `Missing` while the
    create worker goes on to define and start the domain — or race a second
    `StartDomain` via power on.

38. **The per-name guard has an ordering flaw.** Round eight moved acquisition inside
    the worker so the handler would not block. But dispatch order then does not
    determine acquisition order: if the original worker has been dispatched and not yet
    scheduled, a delete/recreate can dispatch a replacement whose goroutine takes the
    guard first. The stale worker then acquires it afterwards, mutates same-named host
    artifacts, and tears down the replacement during its identity cleanup. A FIFO slot
    must be reserved synchronously in the handler (without waiting), with the goroutine
    awaiting its predecessor.

    Note this is the round-eight fix producing a round-ten defect, which is the pattern
    itself rather than an isolated bug.

**Guard growth by round:** create↔create (7) → redeploy (8) → delete (9) → power (10).
Four rounds, four paths, no sign of the enumeration closing. Every handler that touches
a VM name is a candidate, and each new one also has to interact correctly with the
guard's own semantics (finding 38).

## Scope options

Findings 29-30 are not two bugs; they are two more instances of one rule: **detaching
the deploy pulls in every path that touches a VM name and every write that touches its
record.** Each round has found the next instance. Choose an option before implementing.

**Option C — drop the async change (smallest, recommended first step).**
Keep only the optimistic UI counter (Task 7) and the atomic duplicate guard
(Tasks 2b, 3's `409`). This fixes the reported rapid-click corruption. Button grey-out
time is unchanged. None of findings 20-36 apply, because nothing is detached.

**Option B — fix the foundations first.**
Make the libvirt RPC boundary cancellable (finding 20) and give domains typed
generation identity (finding 22) as their own changes, then revisit async deploy. Most
of the workarounds in this plan — the per-name guard, host-verified cleanup, the pool
caveat — exist only because those two are missing.

**Option A — implement the current plan.**
Viable only with the bounded pool caveat accepted (unbounded fan-out, recorded as a
known limitation) and findings 29-36 fixed first. Given the trend, expect further
instances of the same rule during implementation.

---

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `internal/vm/deploy.go` | Deploy/Redeploy orchestration; gains an injectable executor factory | Modify |
| `internal/vm/teardown.go` | Host-state teardown shared by the API and the sync loop | Create |
| ~~`internal/vm/resume.go`~~ | *(deferred — recovery scope, not built)* | — |
| `internal/vm/teardown_test.go` | Teardown behaviour incl. not-found tolerance and the not-attempted marker | Create |
| ~~`internal/vm/resume_test.go`~~ | *(deferred — recovery scope, not built)* | — |
| ~~`internal/vm/types.go`~~ | *(deferred — `DeployEpoch`/`HostSetupDoneAt` belong to the recovery scope)* | — |
| `internal/vm/store.go`, `internal/infra/sql/vm_store.go`, `internal/infra/memory/` | Insert-only create for atomic duplicate rejection | Modify |
| `internal/vm/deploy_test.go` | Executor-factory seam coverage | Modify |
| `internal/infra/api/vm.go` | Create handler: 201 early, atomic duplicate guard | Modify |
| `internal/infra/api/vm_deploy_async.go` | Background deploy worker: panic recovery, identity-checked cleanup, timeout persistence, audit | Create |
| ~~`internal/infra/api/vm_reinstall.go`~~ | *(deferred — no lease without the sweep)* | — |
| ~~`internal/app/sync.go`~~ | *(deferred — no resume sweep is added)* | — |
| `web/src/components/views/virtual-machines/useVirtualMachineOperations.ts` | Optimistic count increment for VM Quick Deploy | Modify |
| `web/src/components/views/machines/useMachineOperations.ts` | Same for Machine Quick Deploy | Modify |
| `internal/machine/`, `internal/infra/sql/machine_store.go` | Insert-only create + 409 for Machines (mirrors the VM guard) | Modify |

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

### Third round

Fixes 8 and 11 combined into a defect that broke the normal path:

12. **Every healthy VM would have been re-finalized every 5 seconds.** A successful
    deploy ends in `Provisioning` with an active window and `DomainObservedAt` set
    (`internal/vm/deploy.go:119`), and fix 8 then clears its epoch — making it
    indistinguishable from a crash just before `StartDomain`, which fix 11 had just
    made a finalize candidate. Absence of an epoch cannot mean "orphaned". Added a
    positive `HostSetupDoneAt` marker written when server-side work completes; records
    carrying it are never recovery candidates (Task 2c, Task 4).
13. **Resumed workers never released their lease.** Fix 8 covered only the create
    goroutine. A resumed worker stamps the epoch but had no release on any exit path,
    stranding the record permanently (Task 6).
14. **Lease release was a read-modify-write race.** `writeExisting` only checks that a
    record exists (`internal/vm/store.go:32`), so a delete-and-recreate between the
    read and the write let a stale worker overwrite the replacement. Release is now a
    single conditional update matching name and token (Task 2c).
15. **Stale cleanup used the expired deploy context.** When `Deploy` returns at the
    provisioning timeout, `ctx` is already cancelled: the identity read fails with
    "context deadline exceeded" — matching neither stale condition — and teardown
    cannot connect. Both now use a fresh bounded context (Task 3).
16. **Finalize left an expired deadline.** `SyncAll` runs immediately after the sweep
    and fails any record whose active window has expired
    (`internal/vm/runtime_sync.go:291-294`), so a VM recovered from a slow define was
    failed microseconds later. Finalize now renews the deadline and restores the
    marker in the same write (Task 7b).

### Fourth round

17. **The redeploy path was never leased.** The reinstall handler arms an active
    provisioning window (`internal/infra/api/vm_reinstall.go:98-104`) and then runs
    `Redeploy` synchronously, with no epoch on the record. A sweep tick landing
    mid-redeploy would have classified it as orphaned and torn down host state under a
    live operation — the same defect the epoch was introduced to prevent, on a path
    this design does not otherwise touch. The lesson generalises: the sweep's candidate
    set is defined by the provisioning window, so **every** path that arms one must take
    the lease (Task 3b).
18. **Recovery jobs had no timeout.** A bounded worker pool bounds nothing if jobs never
    finish; the application-lifetime context let one hung hypervisor hold a slot
    indefinitely. Each job now runs under its own bounded context (Task 6).
19. **"Non-Ubuntu coverage" was only an install-type change.** The curtin test kept
    `OSImageRef: "ubuntu-test"`, so it varied the install path, not the OS family, and
    could not catch Ubuntu-specific assumptions. A real Debian/Red Hat-family fixture is
    now required (Task 5).

### Fifth round — open, not fixed

Round five produced nine findings, more than round four. They are recorded here rather
than patched, because three of them cannot be resolved inside this feature's boundary
and change the viability of the restart-resilience scope. All were verified against the
code.

**Outside this feature's boundary:**

20. **Per-job timeouts do not interrupt libvirt RPCs.** *(Also a prerequisite for the
    approved scope: a global deploy pool cannot be added safely until RPCs are
    cancellable, or capacity is partitioned per hypervisor — see Task 3 Step 4a.)* 11 of 12 `rpcExecutor` methods
    discard their context (`internal/libvirt/domain.go:10-86`,
    `internal/libvirt/storage.go:11-124`); only `CreateVolumeFromReader` accepts one. The
    bounded context added in round four cannot free a slot held by a hung `StartDomain`
    or `CreateVolume`. Requires cancellable RPCs across the libvirt layer.
21. **Deploy status writes are not conditional.** `UpdateDeployStatus` and `FailDeploy`
    are Get-check-`writeExisting` (`internal/vm/service.go:193-215`), so a
    delete-and-recreate between the read and the write lets a stale worker overwrite the
    replacement. Touches every existing deploy path. **Now in scope** — the async worker
    makes this race reachable, so it is Task 3 Step 3d rather than deferred work.
22. **libvirt domains carry no generation identity.** `BuildDomainConfig` stores no
    completion token, so host verification can finalize a same-named predecessor's domain
    as if it belonged to the current deploy.

**Inside the boundary, but unfixed pending a scope decision:**

23. `ResumeFinalize` is never dispatched — Task 5's switch and Task 6's worker pool
    handle only `ResumeRedeploy` and `ResumeFail`, so Task 7b's finalizer is dead code.
24. `resumeOne` has no post-deploy identity recheck, unlike `runVMDeploy`.
25. A failed `ReleaseDeployEpoch` only logs, leaving the record stamped and stranded
    until the next restart; no retry, no in-memory ownership fallback.
26. `failInterrupted` marks the record terminal even when teardown failed, so the orphan
    volume is never retried.
27. Async dispatch reopened the ordering fix: the scan returns before the worker runs, so
    `SyncAll` can still rewrite an expired record to `Missing`/inactive before the worker
    acts on it. The claim must be persisted at dispatch, not at execution.
28. Temporary backing volumes need a stale-temp lifecycle — a deterministic temp name
    collides on retry, a unique one leaks an image-sized volume per crash.

**Assessment.** Findings 20–22 are legitimate and none is about Quick Deploy; they are
the cost of correct deploy recovery against arbitrary process death. That cost exceeds
the feature that prompted it. Tasks 2c, 3b, 4, 5, 6, 7b and 7c — the restart-resilience
half of this plan — should not be implemented as written.

Tasks 1, 2, 2b, 3 and 7 (executor seam, teardown move, atomic create, async response
with identity-checked cleanup, optimistic UI count) are self-contained, were not
implicated in findings 20–28, and deliver the original request: buttons that return
immediately, rapid clicks that cannot collide, and deploys that survive a page reload.

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

- [ ] **Step 3b: Return the hypervisor Deploy actually used**

`Deploy` resolves the hypervisor itself at `internal/vm/deploy.go:24` instead of taking the caller's handle. Task 3's cleanup path needs to know which host the domain and volume really landed on, because the record can be replaced under the same name with a different connection between the handler's lookup and the deploy.

Change the signature to return it:

```go
func (d *Deployer) Deploy(ctx context.Context, created *VirtualMachine, pxeNoCloudFn func(base string, installType InstallConfigType, mac string) string) (hypervisor.Hypervisor, error) {
```

Return `hv` on every path after line 24 resolves it, and the zero value on the early failure before that. Update all callers — run `grep -rn "vmDeployer.Deploy(\|\.Deploy(ctx" internal/ | grep -v _test` to find them — plus any tests that call it.

- [ ] **Step 3c: Clean up host state when Deploy panics**

A panic inside `Deploy` unwinds before it can return anything, so the caller's `usedHV` still holds the pre-deploy handle and cannot be trusted for cleanup. `Deploy` is the only place that knows which host it selected, so it must handle this itself:

```go
	defer func() {
		if r := recover(); r != nil {
			// hv is resolved above and is the host artifacts were created on.
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := d.TeardownHostState(cleanupCtx, hv, *created); err != nil {
				log.Printf("deploy vm %s: cleanup after panic: %v", created.Name, err)
			}
			panic(r) // let the caller's recover record the failure and audit it
		}
	}()
```

**Arm it only after the first successful host mutation.** Placing it immediately after `hv` is resolved would make a panic in `resolvePXEBaseURL`, `BuildLibvirtConfig` or executor construction — all before this deploy touches the host — destroy a same-named domain and volume that a previous partial deploy left behind. The normal create path fails without deleting that state, and recovery cleanup must not be more destructive than the path it stands in for.

A single `mutated` boolean is **not** sufficient. Consider a host that already has a same-named domain but no volume: this deploy creates the volume, sets the flag, then panics defining the domain — and `TeardownHostState` destroys both the new volume *and* the pre-existing domain.

Track ownership per artifact: separate `createdVolume` / `definedDomain` flags set after each successful mutation, and tear down only what this invocation created. Rejecting the mixed pre-existing state before mutating is an acceptable alternative.

Test a domain-only pre-existing state with a panic during domain definition, asserting the pre-existing domain survives. Re-panicking keeps the caller's existing responsibilities — `FailDeploy`, the audit event, and not killing the process — intact.

Test with an executor whose `CreateOverlayVolume` panics after `CreateVolume` succeeded, asserting the volume is deleted and the panic still reaches the caller.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/vm/ -run TestDeployerNewExecutor -v`
Expected: PASS

Run: `go build ./... && go test ./...`
Expected: PASS (no behaviour change when `ExecutorFactory` is nil; the signature change is mechanical)

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
	// The injected hook keeps precedence, exactly as the current
	// implementation and deleteVirtualMachineRuntime do. The cascade and
	// reinstall tests use it to observe cleanup without opening a real libvirt
	// connection; delegating straight to the deployer would make those tests
	// attempt real network calls and silently drop their assertions.
	if s.vmRuntimeDeleter != nil {
		return s.vmRuntimeDeleter(ctx, v)
	}
	return s.vmTeardowner().TeardownHostState(ctx, hv, v)
}

// vmTeardowner returns a Deployer usable for host teardown. VMDeployer is
// optional (ServerConfig leaves it nil in tests and in deployless
// configurations), but VM deletion must still tear down host state, so fall
// back to a bare Deployer that builds a production executor. Routing deletion
// through s.vmDeployer directly would panic on a nil receiver inside
// newExecutor and turn every delete into a 500.
func (s *Server) vmTeardowner() *vm.Deployer {
	if s.vmDeployer != nil {
		return s.vmDeployer
	}
	return &vm.Deployer{}
}
```

`TeardownHostState` only needs `newExecutor`, which falls back to `libvirt.NewExecutor` when `ExecutorFactory` is nil (Task 1), so a zero-value `Deployer` is sufficient here. Do not extend this fallback to `runVMDeploy`: a deploy genuinely requires the configured deployer, and the handler already guards on `s.vmDeployer != nil` before starting one.

Add a regression test that deletes a VM in the default test environment (where `VMDeployer` is nil) and asserts a `204`, not a `500`.

Then find every other caller and update it:

Run: `grep -rn "teardownVMRuntimeOnHypervisor" internal/`
Expected: only matches you are about to fix. Update each to call **`s.vmTeardowner().TeardownHostState`**, never `s.vmDeployer.TeardownHostState` directly — `deleteVirtualMachineRuntime` (`internal/infra/api/vm.go:239`) is one of these callers, and `VMDeployer` is nil in the default test environment and in deployless configurations, so the direct call would panic and break ordinary VM deletion.

Test with **both** optional hooks nil. A test environment that installs a no-op `VMRuntimeDeleter` bypasses the faulty call entirely and would pass vacuously, so the regression test must leave `VMRuntimeDeleter` unset as well as `VMDeployer`. Remove now-unused imports from `internal/infra/api/vm.go` (`strings` and `libvirt` may become unused — the compiler will tell you).

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

Test it against **both** driver error forms. The in-memory store test above does not exercise this translation at all, so a helper handling only one driver would pass every planned test while duplicate creates on the other backend returned a generic `400` instead of `409`. Add focused tests in `internal/infra/sql/` that perform a genuinely conflicting insert on SQLite, and cover the pgx path either with an integration test or by asserting the helper classifies a real `23505` error value. Do not assert on error strings.

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

> **DEFERRED — do not implement.** Part of the abandoned restart-recovery scope; retained only as a record of the constraints found in review.

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
	// is either still working or died with the process. The stamp is cleared
	// on every exit path, so its absence alone does not mean orphaned — see
	// HostSetupDoneAt.
	DeployEpoch string `json:"deployEpoch,omitempty"`

	// HostSetupDoneAt records that the server finished everything it owes this
	// deploy: the domain is defined, started, and booting from disk. What
	// remains is the guest's own install, governed by the provisioning window.
	// Recovery needs this positive marker because a finished deploy and a
	// deploy that crashed just before StartDomain are otherwise identical —
	// both sit in Provisioning with an active window, DomainObservedAt set and
	// no epoch.
	HostSetupDoneAt *time.Time `json:"hostSetupDoneAt,omitempty"`
```

Stamp it in `Deploy` where the successful path already persists status (`internal/vm/deploy.go:119`), in the same `UpdateDeployStatus` write, so a crash cannot land between starting the domain and recording that fact. Set it for the `PhaseStopped` branch (`internal/vm/deploy.go:123`) too — that deploy also has no remaining server-side work.

Redeploy (`internal/vm/deploy.go:130`) arms a fresh provisioning window, so it must clear `HostSetupDoneAt` when it does; otherwise a redeploy interrupted before `StartDomain` would be treated as already finished. Clearing alone is not sufficient — see Task 3b, which is mandatory, not optional.

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

This must be a **single conditional update**, not read-modify-write. A read, token check, then write lets a delete-and-recreate slip in between, and the stale worker's snapshot overwrites the replacement's row. `writeExisting` does not save you: it only checks that *a* record exists (`internal/vm/store.go:32`) and falls back to a plain `Upsert` on stores without `ExistingUpdater` (`:36`).

Add a store method that clears the epoch where the name and token both match, in one statement. In `internal/infra/sql/vm_store.go`:

```go
// ReleaseDeployEpoch clears the ownership stamp in a single conditional write.
// Matching on the completion token inside the statement keeps a stale worker
// from clobbering a record a newer deploy has since taken over.
func (s *VMStore) ReleaseDeployEpoch(ctx context.Context, name, completionToken string) error {
	res, err := s.b.exec(ctx, `
		UPDATE virtual_machines
		SET status = json_set(json_remove(status, '$.provisioning.deployEpoch'), '$.x', '$.x'),
		    updated_at = ?
		WHERE name = ?
		  AND json_extract(status, '$.provisioning.completionToken') = ?`,
		time.Now().UTC(), name, completionToken)
	if err != nil {
		return err
	}
	_, _ = res.RowsAffected()
	s.notify()
	return nil
}
```

The JSON manipulation above is illustrative — SQLite and PostgreSQL differ here, and this project supports both. Check how `marshalVMColumns` shapes the `status` column and how other conditional updates in this file handle the two drivers, then write the equivalent for each. If per-driver JSON surgery proves unwieldy, an acceptable alternative is a compare-and-swap on the whole `status` column: read it, compute the new value, and `UPDATE ... WHERE name = ? AND status = <old>`, retrying once on zero rows affected. What is not acceptable is a read-then-unconditional-write.

Mirror the same semantics in the in-memory store under its existing lock.

Add tests: a release with a mismatched token is a no-op; a matching release clears the stamp so `DecideResumeAction` stops returning `ResumeNone`; and a release racing a same-name recreation leaves the new record intact.

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

// Sequential requests cannot prove atomicity: a non-atomic check-then-insert
// passes this while still letting two simultaneous requests both succeed.
//
// A barrier around doRequest is NOT enough either — the scheduler can run one
// request all the way through the store before the other starts, so a
// check-then-upsert would still pass. The requests must be held at the store
// boundary itself: wrap the VM store with a test double whose Insert blocks
// until both callers have arrived, then releases them together.
func TestCreateVirtualMachineRejectsDuplicateNameConcurrently(t *testing.T) {
	env := setupTestEnv(t)
	seedVMHypervisor(t, env, "hv-dup")
	body := vmCreateBody("vm-dup", "hv-dup")

	const racers = 2
	start := make(chan struct{})
	codes := make(chan int, racers)
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			codes <- doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", body, env.token).Code
		}()
	}
	close(start)
	wg.Wait()
	close(codes)

	created, conflict := 0, 0
	for code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("unexpected status %d", code)
		}
	}
	if created != 1 || conflict != racers-1 {
		t.Fatalf("expected exactly one 201 and %d 409, got %d created / %d conflict", racers-1, created, conflict)
	}

	// Exactly one record, and the loser must not have overwritten it.
	items, err := env.vms.List(context.Background())
	if err != nil {
		t.Fatalf("list vms: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected exactly one VM record, got %d", len(items))
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

The request must be issued from a goroutine. If the handler is still synchronous — precisely the state this test exists to reject — `doRequest` never returns while the fake blocks, so a straight-line call would hang the package until the global `go test` timeout instead of failing fast.

```go
func TestCreateVirtualMachineRespondsWhileDeployInFlight(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	finished := make(chan struct{})
	env := setupTestEnvWithVMDeployer(t, blockingDeployer(started, release, finished))
	seedVMHypervisor(t, env, "hv-block")

	var once sync.Once
	releaseDeploy := func() { once.Do(func() { close(release) }) }
	defer releaseDeploy()

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- doRequest(env.echo, http.MethodPost, "/api/v1/virtual-machines", vmCreateBody("vm-block", "hv-block"), env.token)
	}()

	// The response must arrive while the deploy is still blocked.
	select {
	case rec := <-done:
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 before the deploy finishes, got %d: %s", rec.Code, rec.Body.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not respond while the deploy was in flight (still synchronous?)")
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("deploy goroutine did not start")
	}

	// Let the worker finish AND wait for it. Closing the channel only unblocks
	// it; returning here would let its status writes, cleanup and audit run
	// during a later test in this package.
	releaseDeploy()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("deploy worker did not finish")
	}
}
```

The signal must come from the **worker**, not the executor. Closing a channel in the fake's last executor call still fires before `runVMDeploy` does its identity read, status handling, cleanup and completion audit — so the test could return while those still run against shared state.

Give `Server` an optional `onDeployWorkerDone func(name string)` test hook, invoked from `runVMDeploy`'s outermost `defer` (after the panic-recovery defer, so it fires on every path), and have the test wait on it. Waiting for the terminal audit event or phase under a bounded timeout is an acceptable alternative; a `time.Sleep` is not.

**Block a method the curtin path actually calls.** `applyInstallConfigByOSImage` derives the install type from the image's OS family via `inferInstallConfigType` (`internal/infra/api/vm_reinstall.go:335`), and a vm-capable qcow2 fixture resolves to curtin — so `Deploy` calls `CreateOverlayVolume`, never `CreateVolume`. A fake blocking in `CreateVolume` would never be reached, and the test would fail waiting on `started` for the wrong reason. Block in `CreateOverlayVolume`, and give the fake whatever backing-image behaviour `prepareCloudImageBacking` needs to get that far (`VolumeExists` returning true is the cheapest path — it short-circuits the download at `internal/vm/cloud_image.go:47-52`).

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

**Put the worker in a new file, `internal/infra/api/vm_deploy_async.go`.** `vm.go` is already 435 lines against the project's ~300-line guideline, and this worker adds roughly 90 more with several independent reasons to change (panic recovery, identity validation, cleanup, timeout persistence, auditing). Moving the teardown helper out in Task 2 only reclaims ~30 lines. Keep `CreateVirtualMachine` in `vm.go` and put `runVMDeploy` plus its helpers in the new file.

In `vm.go`, replace the whole `if s.vmDeployer != nil { ... }` block (lines 109-130) with the dispatch below; the `runVMDeploy` definition that follows belongs in `vm_deploy_async.go`:

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
func (s *Server) runVMDeploy(actor httputil.Actor, deployHV hypervisor.Hypervisor, created vm.VirtualMachine) {
	// Seeded with the handler's handle and overwritten with whatever Deploy
	// actually resolved. Every cleanup path below uses this, never deployHV.
	usedHV := deployHV

	// Echo's middleware.Recover() (internal/infra/api/server.go:121) only wraps
	// the request goroutine. Once the deploy runs here, a panic from the deploy
	// orchestration, the executor, or the libvirt boundary would take down the
	// whole server — a regression from the synchronous handler, which was
	// covered. Recover, and record the failure rather than dying silently.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("create vm %s: deploy panicked: %v", created.Name, r)
			failCtx, failCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer failCancel()
			detail := fmt.Sprintf("deploy panicked: %v", r)
			if _, err := s.vms.FailDeploy(failCtx, created.Name, "deploy", detail, created.Provisioning.CompletionToken); err != nil {
				log.Printf("create vm %s: record panic failure: %v", created.Name, err)
			}
			// Host cleanup for a panic is NOT done here — see Task 1 Step 3c.
			// A panic unwinds before `resolved` is assigned, so usedHV would
			// still hold the handler's pre-deploy handle; if the hypervisor
			// record was replaced in between, cleaning through it would target
			// a host the artifacts were never created on. Deploy owns that
			// cleanup, because only Deploy knows which host it selected.
			// Unwinding skips the normal completion audit below, so write the
			// terminal event here — otherwise the Activity UI keeps showing
			// "accepted" for a VM that is actually in Error.
			httputil.CreateAuditFor(failCtx, s.authStore, actor, created.Name, "create-vm", "failure", detail, nil)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), s.provisionTimeout)
	defer cancel()

	token := created.Provisioning.CompletionToken

	// Deploy returns the hypervisor it resolved and actually used. It re-reads
	// the reference itself (internal/vm/deploy.go:24) rather than taking the
	// caller's handle, so if the hypervisor record is replaced under the same
	// name with a different connection between the handler's lookup and the
	// deploy, deployHV points at a host the domain and volume were never
	// created on. Cleaning up through it would leave live artifacts behind.
	//
	// Declared before the call so the panic defer above can read it.
	resolved, deployErr := s.vmDeployer.Deploy(ctx, &created, pxehttp.RenderNoCloudLineConfig)
	if resolved.Name != "" {
		usedHV = resolved
	}

	// Compare deploy identity, not mere existence. With rapid consecutive
	// deploys a VM can be deleted and recreated under the same name while this
	// goroutine runs; an existence-only check would find the new record, skip
	// cleanup, and orphan this deploy's domain and volume on deployHV.
	//
	// Use a fresh context: when Deploy returned because s.provisionTimeout
	// expired, ctx is already cancelled, the read would fail with "context
	// deadline exceeded" (matching neither stale condition, so a deleted VM
	// goes unnoticed), and TeardownHostState could not even connect.
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer checkCancel()

	current, getErr := s.vms.Get(checkCtx, created.Name)
	stale := errors.Is(getErr, resource.ErrNotFound) ||
		(getErr == nil && current.Provisioning.CompletionToken != token)
	if stale {
		// Tear down through the hypervisor Deploy actually used (usedHV), not
		// the one resolved before it ran. See the note below on why they can
		// differ.
		//
		// DANGER: TeardownHostState destroys the domain and volume by NAME. If
		// the replacement worker has already created its own artifacts under
		// the same name on the same hypervisor, this deletes the replacement's,
		// not ours. Name is not identity. See "Serialising same-name deploys"
		// below — that guard is what makes this call safe.
		if cleanupErr := s.vmDeployer.TeardownHostState(checkCtx, usedHV, created); cleanupErr != nil {
			log.Printf("create vm %s: cleanup after superseded deploy: %v", created.Name, cleanupErr)
		}
		// This early return skips the completion audit below, so the original
		// "accepted" event would stay the last word for a deploy that was
		// superseded. Record the outcome here instead — on its OWN context:
		// TeardownHostState above may have consumed all of checkCtx, or
		// returned after it expired since libvirt RPCs ignore cancellation,
		// and the audit write would then be lost.
		auditCtx, auditCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer auditCancel()
		httputil.CreateAuditFor(auditCtx, s.authStore, actor, created.Name, "create-vm", "failure", "deploy superseded by a newer create or delete", nil)
		return
	}
	// Deploy persists its own outcome on every path EXCEPT the one where its
	// context expired: FailDeploy then runs on the cancelled ctx and the write
	// is lost, leaving a Pending record with a possibly-allocated volume. With
	// the recovery sweep deferred out of scope there is no later pass to repair
	// it, so persist the failure here on the fresh context.
	// The deadline check must be independent of deployErr. An uncancellable
	// libvirt call (StartDomain, say) can SUCCEED after the deadline passes:
	// Deploy then writes its terminal status through the expired context,
	// ignores the write error and returns nil. Without this, the worker would
	// audit a success while the record stayed Pending and was later timed out
	// by runtime sync.
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && deployErr == nil {
		if _, err := s.vms.UpdateDeployStatus(checkCtx, created.Name, created.Phase, "create", created.Provisioning); err != nil {
			log.Printf("create vm %s: persist post-deadline success: %v", created.Name, err)
		}
	}

	if deployErr != nil {
		log.Printf("create vm %s: deploy failed: %v", created.Name, deployErr)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			if _, err := s.vms.FailDeploy(checkCtx, created.Name, "deploy", deployErr.Error(), token); err != nil {
				log.Printf("create vm %s: persist timeout failure: %v", created.Name, err)
			}
			if cleanupErr := s.vmDeployer.TeardownHostState(checkCtx, usedHV, created); cleanupErr != nil {
				log.Printf("create vm %s: cleanup after timeout: %v", created.Name, cleanupErr)
			}
		}
	}

	// The handler could only record "accepted"; the Activity UI derives its
	// success/failure totals from persisted audit results, so without this the
	// outcome of every asynchronous create would stay "accepted" forever. The
	// actor is captured in the handler, since the request context is gone.
	outcome, detail := "success", "virtual machine created"
	if deployErr != nil {
		outcome, detail = "partial", "vm created but deploy failed: "+deployErr.Error()
	}
	// Own context, for the same reason as the superseded branch: the timeout
	// cleanup above can exhaust or outlive checkCtx, since libvirt RPCs ignore
	// cancellation, and the terminal event would then be silently dropped.
	doneCtx, doneCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer doneCancel()
	httputil.CreateAuditFor(doneCtx, s.authStore, actor, created.Name, "create-vm", outcome, detail, nil)
}
```

- [ ] **Step 3d: Make deploy status writes atomic (required before detaching workers)**

`UpdateDeployStatus` and `FailDeploy` check the completion token after a `Get` and then write through `writeExisting`, which resolves to `UpdateExisting` — and that statement matches on `WHERE name = ?` alone (`internal/infra/sql/vm_store.go`). The token check and the write are therefore not atomic.

The per-name guard from Step 4b does **not** cover this: it serialises *host* operations, while this is a database race. If a worker reads its row, the VM is then deleted and recreated, and the worker's write lands afterwards, the stale snapshot overwrites the replacement's row — *including its completion token*. The replacement worker then finds a token mismatch and can no longer persist its own outcome, leaving the record permanently wrong.

Add a store-level conditional update matching **both** name and completion token, and route `UpdateDeployStatus` and `FailDeploy` through it. This is the same treatment specified for the lease release in deferred Task 2c; here it is required, because the async worker makes the race reachable.

Test the ordering explicitly: read a row, delete and recreate the VM, then let the stale write land, and assert the replacement's token and phase are untouched.

- [ ] **Step 4a: Bound background deploy concurrency (required)**

The synchronous handler uses request duration as backpressure: the Create VM dialog loops up to 50 VMs (`web/src/components/views/virtual-machines/useVirtualMachineOperations.ts:101`) but each request waits, so exactly one deploy runs at a time. Returning `201` immediately removes that limit — a single batch create would launch 50 concurrent image, storage and libvirt jobs. Combined with finding 20 (11 of 12 `rpcExecutor` methods ignore their context), hung operations accumulate goroutines and connections with nothing to reclaim them.

**A single global pool is not safe on its own.** Because 11 of 12 `rpcExecutor` methods ignore their context (finding 20), `poolSize` hung operations exhaust the pool permanently: every later create still returns `201`, but no deploy can start — including on healthy hypervisors. That converts today's per-request stall into a system-wide outage, which is strictly worse than the unbounded fan-out it was meant to fix.

Two orderings are acceptable:

1. **Make the RPC boundary cancellable first** (finding 20), then add the global pool. This is the honest fix and is why finding 20 is a prerequisite, not an unrelated concern.
2. **Partition capacity per hypervisor** so a stuck host cannot consume every slot. Each hypervisor gets its own bounded pool; a hang degrades that host only.

Do not implement a single global pool without one of these. If neither is in scope for this change, leave the fan-out unbounded and record it as a known limitation rather than introducing a global chokepoint — an unbounded fan-out degrades under load, a poisoned global pool stops all deploys outright.

Enqueue must never block the handler; the `201` still returns immediately.

Tests: issue N creates exceeding the per-hypervisor limit and assert the cap holds while all N return `201` promptly; and — the case the batch test misses — hang `poolSize` deploys on hypervisor A and assert a create on healthy hypervisor B still starts.

- [ ] **Step 4b: Serialise same-name deploys (required for Step 4 to be safe)**

Every teardown in Step 4 destroys the domain and volume **by name**. Libvirt artifacts carry no generation identity — `BuildDomainConfig` stores no completion token — so a stale worker cleaning up "its" VM cannot distinguish its own artifacts from a replacement's created under the same name on the same hypervisor. The token check identifies *the record* as superseded; it says nothing about *the artifacts*.

This is reachable in the approved path: delete a VM, immediately Quick Deploy the same name (which the UI's optimistic counter makes easy after a delete), and the old worker's cleanup can destroy the new deploy's volume.

Add an in-process guard so only one host-mutating operation per VM name runs at a time: a `map[string]chan struct{}` (or `singleflight`-style set) on `Server`.

**Acquire it inside the worker, never in the handler.** Taking the lock before `go s.runVMDeploy(...)` would make a same-name `POST` block until the previous deploy finished — destroying the immediate `201` this whole feature exists to deliver, potentially forever given finding 20. Dispatch the goroutine immediately; it waits for the name inside, and releases in its outermost `defer`.

**Redeploy must share the same primitive.** `ReinstallVirtualMachine` has no phase guard, and after this change a `Pending` VM is immediately selectable with Redeploy enabled — so a user can trigger a synchronous destroy/undefine/recreate of the same named artifacts while the create worker is still deploying them. Either have the reinstall handler acquire the same per-name guard, or return `409` while a create worker owns the name. Prefer the `409`: reinstall runs in the request path, and making it wait would hang the request for the same reason as above.

This is sufficient because all these workers are in-process and this scope has no cross-process recovery. It would **not** be sufficient once recovery returns — that is why finding 22 (typed generation identity on the libvirt domain) is recorded as a prerequisite for the deferred scope.

Tests:
- start a blocked deploy for `vm-x`, delete the record, issue a second create for `vm-x`, release the first, and assert the second VM's volume and domain still exist;
- assert the second create's `201` arrives **while** the first worker is still blocked, proving the guard is not on the request path;
- create `vm-y`, then call Redeploy while its worker is blocked, and assert the reinstall is rejected (or serialised) rather than destroying the in-flight artifacts.

`httputil.CreateAudit` takes an `echo.Context` and cannot be used here. Read `internal/infra/httputil` for how the actor is extracted and add a background-safe variant that takes the already-resolved actor plus a plain context; capture the actor in the handler before it returns and pass it into `runVMDeploy`. Keep the handler's `accepted` event — it records that the request was received — and let this one record the result.

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

- [ ] **Step 6b: Cover a non-Ubuntu image through the async path**

The async worker now wraps *every* VM deployment — timeout handling, failure persistence, cleanup and auditing — but the tests above seed only `ubuntu-test`, so the retained path could ship without ever exercising Debian- or Red Hat-family catalog metadata. `applyInstallConfigByOSImage` selects the install type from `img.OSFamily` via `inferInstallConfigType`, so family is a live input to this path, not an incidental label.

Add a create test seeding the existing Debian fixture (`debian-13-amd64-cloud`, see `internal/vm/cloud_image_download_test.go:34` and the Debian catalog metadata in `internal/vm/domain_config_test.go:34-64`) and assert the same `201`-before-deploy behaviour. If a family is intentionally unsupported on this path, assert the explicit early error instead — per the project OS-deployment policy, silent Ubuntu assumptions are what must not ship.

- [ ] **Step 7: Run the full suite**

Run: `go test ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/infra/api/vm.go internal/infra/api/
git commit -m "Return 201 before VM deploy and reject duplicate VM names"
```

---

### Task 3b: Lease the redeploy path

> **DEFERRED — do not implement.** Part of the abandoned restart-recovery scope; retained only as a record of the constraints found in review.

**This task is not optional and must land before Task 6 enables the sweep.** The reinstall handler persists an active `Provisioning` window (`internal/infra/api/vm_reinstall.go:98-104`) and then runs `Redeploy` *synchronously*, which takes as long as recreating the volume and redefining the domain. With no epoch on that record, the sweep sees an active window, no owner, no `HostSetupDoneAt` and no domain yet — a textbook `ResumeRedeploy` candidate — and tears down host state underneath a live redeploy. That is the same "kill a live deploy" defect the epoch exists to prevent, reappearing on a path this design does not otherwise change.

The sweep's candidate set is defined by the provisioning window, so every path that arms one must take the lease.

**Files:**
- Modify: `internal/infra/api/vm_reinstall.go:98-104` and its exit paths

**Interfaces:**
- Consumes: `Server.deployEpoch` (Task 2c), `Service.ReleaseDeployEpoch` (Task 2c).

- [ ] **Step 1: Write the failing test**

In `internal/infra/api/` (or `internal/vm/` if the handler is awkward to drive), assert that a record mid-redeploy is not classified as recoverable:

```go
func TestRedeployArmsWindowWithCurrentEpoch(t *testing.T) {
	// After the reinstall handler arms the window but before Redeploy returns,
	// the stored record must carry the current epoch so the sweep skips it.
	// Drive the handler with a deployer whose ExecutorFactory blocks in
	// CreateVolume, then read the record and assert:
	//   got.Provisioning.DeployEpoch == env.deployEpoch
	//   vm.DecideResumeAction(got, env.deployEpoch, time.Now()) == vm.ResumeNone
}
```

Fill in the body using the blocking-deployer helper from Task 3. The two assertions are the point: the epoch is present, and the decision function therefore leaves it alone.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/infra/api/ -run TestRedeployArmsWindow -v`
Expected: FAIL — `DeployEpoch` is empty, and `DecideResumeAction` returns `ResumeRedeploy`.

- [ ] **Step 3: Stamp and release around the redeploy**

In `internal/infra/api/vm_reinstall.go`, extend the window initialisation at line 98:

```go
	current.Provisioning = vm.ProvisioningStatus{
		Active:          true,
		StartedAt:       httputil.TimePtr(now),
		DeadlineAt:      httputil.TimePtr(now.Add(s.provisionTimeout)),
		CompletionToken: token,
		DeployEpoch:     s.deployEpoch,
	}
```

`HostSetupDoneAt` is left nil by this fresh struct, which is correct: the previous deploy's completion no longer applies. Confirm no other code path copies it forward.

Then release the lease when the handler finishes, on every exit path after the window is persisted — success, redeploy failure, and the concurrent-delete branch. A `defer` placed immediately after the `UpdateExisting` write, using a fresh context as in Task 3, covers all of them:

```go
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer releaseCancel()
		if err := s.vms.ReleaseDeployEpoch(releaseCtx, name, current.Provisioning.CompletionToken); err != nil {
			log.Printf("redeploy vm %s: release deploy lease: %v", name, err)
		}
	}()
```

`Redeploy`'s success path must also stamp `HostSetupDoneAt`, the same way `Deploy` does (Task 2c). Check `internal/vm/deploy.go:130-203` for where it persists its terminal status and add it there.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/infra/api/ ./internal/vm/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/infra/api/vm_reinstall.go internal/vm/deploy.go internal/infra/api/
git commit -m "Take the deploy ownership lease on the redeploy path"
```

---

### Task 4: Decide which interrupted deploys to resume

> **DEFERRED — do not implement.** Part of the abandoned restart-recovery scope; retained only as a record of the constraints found in review.

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
			// Regression guard: this is what a successful deploy looks like once
			// its lease is released. Classifying it as work would re-finalize
			// every healthy VM every 5 seconds.
			name: "successfully deployed vm awaiting guest install is left alone",
			vm:   VirtualMachine{Phase: PhaseProvisioning, Provisioning: ProvisioningStatus{Active: true, DeadlineAt: &future, DomainObservedAt: &observed, HostSetupDoneAt: &observed, DeployEpoch: ""}},
			want: ResumeNone,
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
	// Server-side work finished. Absence of an epoch cannot mean "orphaned" on
	// its own: a successful deploy also ends with no epoch, sitting in
	// Provisioning with an active window and DomainObservedAt set
	// (internal/vm/deploy.go:119) — identical to a crash just before
	// StartDomain. Without this positive marker every healthy VM would be
	// re-finalized on every 5-second tick for the whole guest install.
	if v.Provisioning.HostSetupDoneAt != nil {
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

> **DEFERRED — do not implement.** Part of the abandoned restart-recovery scope; retained only as a record of the constraints found in review.

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

The two tests above use the default (preseed/PXE) install path with an Ubuntu fixture. Two separate dimensions still need coverage, and they are not the same thing:

**a) The curtin install path.** Copy `TestResumeInterruptedDeploysCleansUpBeforeRetry` as `TestResumeInterruptedDeploysCurtinPath` and set on the created VM:

```go
		InstallCfg: &InstallConfig{Type: InstallConfigCurtin},
```

Verify the field name and constructor against `internal/vm/types.go` before writing it; if `InstallConfig` has required companion fields, populate them. The curtin path also reads the OS image through `d.OSImages`, so populate that service or the deploy exits before creating an overlay.

**b) A genuinely non-Ubuntu image.** Changing `InstallCfg` while leaving `OSImageRef: "ubuntu-test"` does **not** satisfy the project's OS-deployment policy — it adds an install-type variant, not OS-family coverage, and cannot catch recovery code that assumes Ubuntu catalog metadata. Seed a Debian- or Red Hat-family catalog entry and run the resume through it:

```go
		OSImageRef: "debian-13-amd64-cloud",
```

That fixture name already exists in `internal/vm/cloud_image_download_test.go:34`, and `internal/vm/domain_config_test.go:34-64` carries Debian-family catalog metadata. Reuse those rather than inventing a new fixture — read the fields they set and populate the fake `OSImages` service the same way, so the family selection logic actually runs.

If the resume path is intentionally limited to one family, assert the explicit early unsupported-family error instead — the policy accepts that, but not a silent Ubuntu assumption.

Run: `go test ./internal/vm/ -run TestResumeInterruptedDeploys -v`
Expected: PASS (all three)

- [ ] **Step 6: Commit**

```bash
git add internal/vm/resume.go internal/vm/resume_test.go
git commit -m "Resume restart-interrupted VM deploys after clearing partial host state"
```

---

### Task 6: Run the resume sweep from the runtime sync loop

> **DEFERRED — do not implement.** Part of the abandoned restart-recovery scope; retained only as a record of the constraints found in review.

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

**And it must release that stamp on every exit path**, with the same deferred fresh-context release `runVMDeploy` uses (Task 3). This applies to all of: success, an early return from hypervisor resolution, an early return from teardown, a superseded-identity skip, and a `Deploy` that hit its own deadline. Removing the in-flight-set entry is not sufficient — that is process memory, while the epoch is persisted, so a worker that exits without releasing leaves the record carrying the current epoch and every subsequent sweep returns `ResumeNone` for it. The record is then stranded until the next restart.

Write this as a single `defer` at the top of the worker, before any early return can be taken, rather than at each exit site.

**Give each job its own bounded context.** A bounded worker pool alone does not bound anything if the jobs never end: passing the application-lifetime `ctx` to `resumeOne`/`failInterrupted` means one hung hypervisor call or stalled image transfer occupies a slot forever, and enough of them leave every later interrupted VM unrecovered while the tick itself keeps ticking. Each job therefore runs under `context.WithTimeout(context.Background(), resumeJobTimeout)` — derived from `Background`, not the tick context, so a tick returning does not cancel work in progress.

Size `resumeJobTimeout` from the same budget as `provisionTimeout`; a deploy that cannot finish inside its own provisioning window has nothing to gain from running longer.

Test that a job whose executor blocks past the timeout releases both its pool slot (a subsequent VM is still picked up) and its epoch (the record becomes recoverable again rather than staying stamped).

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

- [ ] **Step 3b: Give Machines the same atomic duplicate guard (required)**

This is settled, not conditional: `machine.Service.Create` calls `store.Upsert` (`internal/machine/service.go:55`), and the machine store uses `ON CONFLICT (name) DO UPDATE` (`internal/infra/sql/machine_store.go:102-113`) — the identical silent-overwrite the VM side had. Moving the UI counter does not protect against multiple tabs or direct API clients.

Mirror Task 2b and Task 3 on the machine surface: add an insert-only store operation returning `resource.ErrAlreadyExists`, a `CreateExclusive` service method, and a `409` mapping in the machine create handler. Add the same concurrent test as `TestCreateVirtualMachineRejectsDuplicateNameConcurrently`, asserting exactly one `201`, one `409`, and one record.

**Do not leak the registration token.** For a hypervisor-role Machine, `attachHypervisorRegistrationToken` persists a one-time token at `internal/infra/api/machine.go:93` — *before* the create call. Adding a `409` there means the losing request has already written a valid token row that now has no owning Machine and no deletion API, so repeated conflicts accumulate orphan credentials until expiry. Either move token creation after a successful insert, or delete the token when the insert conflicts.

If you move it after the insert, handle the opposite partial write: a token failure then leaves a committed Machine row with no credential, and a retry gets `409`. Delete the just-inserted Machine when token creation fails, or perform both in one transaction. Test that branch explicitly — inject a token-store failure and assert no orphan Machine row survives.

Extend the concurrent test to assert exactly one token was issued.

The project UI policy requires the two surfaces to behave alike; leaving Machines on a silently-overwriting create would make the guarantee VM-only.

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

> **DEFERRED — do not implement.** Part of the abandoned restart-recovery scope; retained only as a record of the constraints found in review.

`markDomainDefined` persists at `internal/vm/deploy.go:91`, but `StartDomain` (`:102`) and the network-to-`hd` boot-device switch (`:108`) run after it. A crash in either gap leaves `DomainObservedAt` set, so the resume rules skip the record — yet the runtime sync loop only observes domain state; it never starts an inactive guest or completes the boot-device switch. The VM sits until its window times out, or reboots back into PXE.

**Files:**
- Modify: `internal/vm/resume.go`, `internal/vm/resume_test.go`

- [ ] **Step 1: Implement the finalize execution path**

`DecideResumeAction` already returns `ResumeFinalize` (Task 4) for a prior-epoch record with an active window and `DomainObservedAt` set, in any of the `Pending`/`Missing`/`Provisioning` phases. This task supplies the executor.

Recovery does not redeploy: it inspects the domain and, if it is not running, calls `StartDomain` and reasserts the boot device to `hd`. Both operations are idempotent — starting a running domain and setting an already-`hd` boot device are no-ops — so the path is safe to repeat.

**Persist four things in one write before the sweep returns:**

1. `Phase = Provisioning` — a re-armed record may still read `Missing`.
2. `DomainObservedAt`, if nil. Finalize can be reached via host verification when the original marker write failed (`internal/vm/deploy.go:241`); leaving it nil would re-classify the VM as `ResumeRedeploy` next tick and destroy the running domain.
3. `DeadlineAt`, renewed from now by the original window length — the same renewal `markDomainDefined` performs (`internal/vm/deploy.go:232-236`).
4. `HostSetupDoneAt` — server-side work is now complete.

Item 3 is not optional. A slow define that outlived its deadline reaches finalize with an expired window, and `SyncAll` runs immediately after on the same tick: it marks any record with an expired active window `Error` for provisioning timeout (`internal/vm/runtime_sync.go:291-294`). Without the renewal the sweep would recover the VM and the sync loop would fail it microseconds later.

Do not fold this into `ResumeRedeploy`: tearing down a defined domain to rebuild it would discard an install that may already be under way.

- [ ] **Step 2: Test both crash windows and the re-armed Missing case**

Using the `resumeExecutor` fake:

- domain exists but is shut off → expect `StartDomain`
- domain is running with boot device still `network` → expect `SetDomainBootDevice` to `hd`
- record is `Missing` with an active re-armed window and a defined domain → expect finalize, not `ResumeNone`. This is the timeout-before-define path: `markDomainDefined` re-arms the window (`internal/vm/deploy.go:237`) without restoring the phase, and `SyncAll` maps the shut-off domain to `Provisioning` (`internal/vm/runtime_sync.go:401-405`) but never starts it.
- nil `DomainObservedAt` but the domain exists on the host → expect finalize, **not** teardown. This is the lost-checkpoint case from `internal/vm/deploy.go:241`.
- expired `DeadlineAt` reached via host verification → after finalize, run `SyncAll` and assert the VM is **not** `Error`. This proves the deadline renewal actually protects the recovered VM from the sync loop on the same tick.

Assert no volume is deleted in any of these, and that `HostSetupDoneAt` is set afterwards so the next tick returns `ResumeNone`.

- [ ] **Step 3: Commit**

```bash
git add internal/vm/resume.go internal/vm/resume_test.go
git commit -m "Complete post-definition deploy steps after an interrupted deploy"
```

---

### Task 7c: Make backing image publication atomic

> **DEFERRED — do not implement.** Part of the abandoned restart-recovery scope; retained only as a record of the constraints found in review.

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

Validate **only the rows in the table below**. Do not walk the design document's full Verification section: it still describes the deferred recovery scope (restart resume, leases, finalization, backing-image publication, the worker pool), and those items are intentionally not covered here. The design doc's status section records why.

| Item | Covered by |
|---|---|
| Duplicate names, concurrent, exactly one 201 | Task 2b Step 1 + Task 3 Step 1 |
| Machine duplicate names, concurrent | Task 7 Step 3b |
| Async response proven with a blocking deployer | Task 3 Step 1 |
| Timeout failure persisted on a fresh context | Task 3 Step 4 |
| Panic in the deploy goroutine does not kill the server | Task 3 Step 4 |
| Panic path writes a terminal audit event | Task 3 Step 4 |
| Delete still works with a nil VMDeployer | Task 2 Step 5 |
| Cleanup targets the hypervisor Deploy actually used | Task 1 Step 3b + Task 3 Step 4 |
| Deploy outcome recorded in the audit log | Task 3 Step 4 |
| Stale-deploy identity check + cleanup | Task 3 Step 4 |
| Teardown not-attempted marker preserved | Task 2 Step 5b |
| Cascade delete teardown | Task 2 Step 1 + Task 3 Step 4 |
| Existing tests updated for async completion | Task 3 Step 6 |
| Rapid clicks produce distinct VMs | Task 7 Step 4 |
| Same-name delete+recreate does not destroy the replacement | Task 3 Step 4b |
| Same-name guard does not delay the 201 | Task 3 Step 4b |
| Redeploy during an in-flight create is rejected/serialised | Task 3 Step 4b |
| Batch create respects the deploy concurrency limit | Task 3 Step 4a — **only if** a bounded pool was implemented. Step 4a permits leaving fan-out unbounded when neither cancellable RPCs nor per-hypervisor partitioning is in scope; if that fallback is taken, strike this row and record the limitation in the PR instead. Do not invent a global pool to satisfy it. |
| Panic inside Deploy cleans up on the host it selected | Task 1 Step 3c |
| Pre-mutation panic leaves existing host state untouched | Task 1 Step 3c |
| Delete works with BOTH VMDeployer and VMRuntimeDeleter nil | Task 2 Step 5 |
| Superseded audit survives a slow cleanup | Task 3 Step 4 |
| Status writes survive a delete/recreate race | Task 3 Step 3d |
| Superseded deploy records a terminal audit event | Task 3 Step 4 |
| Post-deadline success is persisted | Task 3 Step 4 |
| A stuck hypervisor does not block deploys on healthy ones | Task 3 Step 4a |
| Machine token failure leaves no orphan Machine row | Task 7 Step 3b |
| OS coverage: non-Ubuntu create through the async path | Task 3 Step 6b |
| Duplicate translation on both SQL drivers | Task 2b Step 3 |
| Machine duplicate issues exactly one registration token | Task 7 Step 3b |

Rows for the deferred recovery scope are intentionally absent — those tasks are not
being built. Do not reinstate them without revisiting the design doc's status section.

If any row has no test, write one now rather than marking this task done.

- [ ] **Step 3: Manual check against a real deploy**

This is the part automated tests cannot prove. On the GOMI server, click Quick Deploy several times in quick succession and confirm: distinct VM names, buttons usable throughout, all VMs reaching a terminal phase. Restart-mid-deploy is **not** part of this verification: automatic recovery is out of scope, and such a VM is expected to stay `Pending` until its window expires and the sync loop marks it `Missing`. Confirm that outcome rather than a resume, and confirm the operator's Redeploy action recovers it.

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
