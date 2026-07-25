# Quick Deploy: Asynchronous Deploy with Reload and Restart Resilience

Date: 2026-07-25
Status: **SUPERSEDED — not approved for implementation.** The restart-resilience design
(Goal 4, §2 and everything downstream) was abandoned after review; see the status
section below. The remaining sections are retained as a record of the investigation and
its constraints, not as a specification to build from. No work should start from this
document until a scope is chosen in the implementation plan's "Scope options".

**Non-normative sections:** Goal 4; §2 (Resume interrupted deploys) and its
subsections; §3's resume-specific requirements; every Verification bullet covering
restart resume, leases, finalization, backing-image publication and the worker pool.

## Problem

Clicking Quick Deploy greys out both the Quick Deploy and Preset buttons until the
VM finishes deploying. Users who want to launch several VMs in a row must wait for
each deploy to complete. The buttons are disabled because rapid clicks currently
corrupt state.

### Root causes

Four distinct defects combine to produce the observed behaviour.

| Layer | Defect | Location |
|---|---|---|
| Server | `POST /virtualmachines` runs the entire libvirt deploy inside the handler; the response returns only after the domain is started | `internal/infra/api/vm.go:110` |
| Server | The deploy uses `c.Request().Context()`, so a page reload or tab close cancels it mid-flight | `internal/infra/api/vm.go:53` |
| Server | `Upsert` uses `ON CONFLICT (name) DO UPDATE`, silently overwriting an existing VM record on a duplicate name | `internal/infra/sql/vm_store.go:118` |
| Web UI | The preset `count` is incremented only after the API call succeeds, so a second click reuses the same VM name | `web/src/components/views/virtual-machines/useVirtualMachineOperations.ts:158` |

The button disable time equals the server-side deploy time, so no UI-only change can
shorten it.

### Non-issues confirmed during investigation

- The preset, including `count`, is already persisted to `localStorage` and restored
  on load (`web/src/components/views/VirtualMachinesView.tsx:181`,
  `web/src/components/views/virtual-machines/vmFormState.ts:178`). Numbering does not
  reset across reloads.
- Deploy outcomes are already persisted server-side. Every failure path calls
  `updatePhaseOnError` → `FailDeploy`, writing `Phase=Error`, the failing action and
  the error message (`internal/vm/deploy.go:254`). Success paths write
  `Phase=Provisioning`/`Creating` plus `lastAction` (`internal/vm/deploy.go:119`).
  No new persistence is required for reporting deploy results.

## Status: restart-resilience scope is not viable as specified

Five review rounds produced 32 defects in this design. The final round produced more
than the one before it, and three of its findings are not fixable within this feature's
boundary:

- **Per-job timeouts cannot work.** 11 of the 12 `rpcExecutor` methods discard their
  context outright (`internal/libvirt/domain.go:10-86`, `internal/libvirt/storage.go:11-124`);
  only `CreateVolumeFromReader` accepts one. A hung `StartDomain` or `CreateVolume`
  outlives any deadline and holds its worker slot indefinitely. Fixing this means adding
  cancellable RPCs across the whole libvirt layer — a separate change affecting every
  deploy, migrate and power path.
- **Every deploy status write needs to become conditional.** `UpdateDeployStatus` and
  `FailDeploy` are Get-check-`writeExisting` (`internal/vm/service.go:193-215`), which a
  delete-and-recreate can lose. This touches all existing deploy paths, not just the
  asynchronous one.
- **libvirt domains carry no generation identity.** `BuildDomainConfig` stores no token,
  so host verification cannot prove a discovered domain belongs to the current deploy
  rather than a same-named predecessor.

Each is legitimate. None is about Quick Deploy. They are the cost of making deploy
recovery correct against arbitrary process death, and that cost now clearly exceeds the
feature that prompted it.

**The restart-resilience portion of this design (§2 and everything downstream) should
not be implemented as written.** The reload-resilience and rapid-click portions (§1, §4,
§5) are self-contained, were not implicated in the unresolved findings above, and
deliver the original request.

The open findings are recorded in the plan's revision log rather than patched, so a
future attempt starts from the real constraints instead of rediscovering them.

## Goals

1. Quick Deploy and Preset buttons return to an enabled state without waiting for the
   deploy to finish.
2. Repeated clicks never collide on a VM name and never overwrite an existing record.
3. A browser reload does not interrupt an in-flight deploy.
4. ~~A server restart does not strand a deploy; interrupted deploys resume automatically.~~ **(ABANDONED — see Status.)**
5. Deploy state lives in the server database as the single source of truth.

## Non-goals

- Redesigning the deploy pipeline itself or its phase model.
- Changing how the guest-side provisioning (PXE, cloud-init) completion is detected.
- Adding a client-side notification mechanism for deploy results. The VM list phase
  badge and `lastError` already carry this and survive reloads.

## Design

### 1. Detach the deploy from the request lifecycle

`CreateVirtualMachine` persists the record with `Phase=Pending` and returns `201`
immediately. `Deploy()` moves to a background goroutine using a detached context,
following the established precedent in `internal/infra/api/machine_power.go:98`:

```go
go func() {
    ctx, cancel := context.WithTimeout(context.Background(), deployTimeout)
    defer cancel()
    ...
}()
```

Because the context is not derived from the request, a reload or tab close no longer
cancels the deploy.

The concurrent-hypervisor-delete handling currently at `internal/infra/api/vm.go:116`
(re-check the record, tear down host mutations if it was swept) moves into the
goroutine. It cannot stay in the handler, which now returns before the deploy runs.

That recheck currently tests only whether a record with the same name exists. Once the
deploy is asynchronous and rapid consecutive deploys are the point, a VM can be deleted
and a new one created under the same name while the old goroutine is still running. An
existence-only check finds the new record and skips cleanup, orphaning the old
hypervisor's domain and volume. The recheck must compare deploy identity — the stored
`CompletionToken` and `HypervisorRef` against the ones this goroutine started with —
and tear down via the hypervisor it resolved before deploying whenever the record is
absent or belongs to a different deploy.

### 2. Resume interrupted deploys after a restart *(NON-NORMATIVE — abandoned; retained as a record of the constraints found)*

A goroutine dies with its process. Detaching the context alone gives no restart
resilience: a crash mid-deploy leaves a `Phase=Pending` record with no host state and
nothing to resume it.

The runtime sync loop does not repair this. While the provisioning window is still
open, `vmDeployInFlight` (`internal/vm/runtime_sync.go:323`) deliberately leaves the
record alone, since a missing domain is indistinguishable from a slow but healthy
deploy. Once the window expires, `markVMMissing` moves it to `Missing` — which records
that the domain is gone but neither retries the deploy nor removes any partial host
state the crash left behind. Either way the VM never reaches a usable state on its own.

A resume sweep is added to `runVMRuntimeSyncLoop` (`internal/app/sync.go:33`), which
already runs once at startup (line 37) and every 5 seconds thereafter. Recovery
therefore happens within about 5 seconds of a restart.

#### Distinguishing an interrupted deploy from a live one

The state a restart leaves behind — `Pending`, provisioning active, `DomainObservedAt`
nil — is exactly the state a *healthy* deploy occupies between the `201` response and
`DefineDomain`. The sweep runs every 5 seconds, so a deploy that is still creating its
volume matches on the very first tick. Resuming on that evidence alone would tear down
a live goroutine's host state and start a second concurrent `Deploy` for the same VM.

Persisted phase data cannot distinguish the two cases, because both are identical by
construction. The missing dimension is *ownership*: which process is currently
responsible for this deploy.

The design therefore adds a **deploy owner lease**. The server generates a random epoch
id at startup. `CreateVirtualMachine` stamps it on the record before returning `201`,
and the sweep skips records carrying the current epoch. Records from a previous epoch
are unambiguously orphaned.

**The lease must be released, not merely taken.** "Current epoch" means "a live
goroutine owns this" only if every goroutine clears the stamp before exiting. A deploy
that hits its context deadline before defining a domain would otherwise leave the
record stamped forever: `Deploy` attempts `FailDeploy` on the already-cancelled
context, so even the `Error` write can fail, and the sweep would then treat a dead
worker's record as live indefinitely — reintroducing the stuck-`Pending` failure this
design exists to remove.

Every exit path of the deploy goroutine therefore clears `DeployEpoch`, using a fresh
context rather than the expired one so the release survives its own timeout. Releasing
the lease is what makes the record eligible for recovery on the next sweep.

This adds one persisted field. `ProvisioningStatus` already carries the deploy's
identity via `CompletionToken`, so the epoch belongs alongside it rather than in a new
table.

#### Ownership does not survive dispatch

The sweep classifies a record and then hands it to a worker, so the record it acts on
is a snapshot. Between classification and execution the VM can be deleted and
recreated under the same name — the same race the create goroutine guards against.
A stale worker would then tear down the *replacement's* domain and volume, or leave
orphans on the old hypervisor.

Token-gated status writes do not help: they protect database rows, not host mutations.
The worker must therefore re-read the record and confirm the `CompletionToken` still
matches its snapshot immediately before any host call, and repeat the same stale check
after deploying that `runVMDeploy` performs.

#### Decision logic

#### Recovery needs a positive record of unfinished work

Inferring "orphaned" from the *absence* of an epoch does not work, because a
successfully finished deploy also has no epoch — the lease is released on exit. A
completed deploy ends at `Phase=Provisioning` with an active window and
`DomainObservedAt` set (`internal/vm/deploy.go:119`), which is indistinguishable from a
deploy that crashed just before `StartDomain`. Treating that as recoverable would
re-finalize every healthy VM on every 5-second tick for the whole install.

The deploy therefore records when its **server-side work is complete** — a
`HostSetupDoneAt` stamp written after `StartDomain` and the boot-device switch succeed.
This is a positive assertion that nothing remains for the server to do; what follows is
the guest's own install, which the provisioning window and completion signal already
govern.

Recovery considers only records that have an epoch from a previous process **or** are
missing `HostSetupDoneAt`. A record with neither an epoch nor the stamp is a genuine
crash victim; a record with the stamp is finished server-side and is never touched.

#### Decision logic

Candidates are records with an active, uncompleted provisioning window in a
non-terminal phase — `Pending`, `Missing` or `Provisioning`.

```
DeployEpoch == currentEpoch -> a live goroutine owns it        -> leave alone
HostSetupDoneAt != nil      -> server-side work finished       -> leave alone
DomainObservedAt != nil      -> domain defined, setup unfinished -> finalize
DeadlineAt passed            -> clean up host state, then FailDeploy(Phase=Error)
otherwise                    -> domain not yet defined -> clean up, then re-run Deploy()
```

The last two branches are host-verified, not marker-verified: before tearing anything
down the worker checks whether the domain actually exists, and finalizes instead if it
does. This covers the case where the marker write failed after a successful define.

Note that the expired branch also cleans up: a crash after volume creation but before
domain definition leaves an orphan volume, and marking the record `Error` without
removing it would make a later redeploy under the same name fail with the same
"already exists" error this design exists to prevent.

`DomainObservedAt` distinguishes the define gap from a domain removed from the host
(`internal/vm/types.go:104-108`).

#### Relationship to `vmDeployInFlight`

`internal/vm/runtime_sync.go:323` already implements the same predicate — active,
not timed out, `DomainObservedAt == nil` — to stop the runtime sync loop from marking
a mid-deploy VM as `Missing`. Its comment explicitly anticipates a "crashed server,
killed worker".

The resume logic must not duplicate it. `DecideResumeAction` reuses `vmDeployInFlight`
for the shared part and adds only the epoch and deadline dimensions on top.

#### Ordering against the runtime sync

`SyncAll` and the resume sweep read the same records, and `SyncAll` mutates the state
the sweep depends on: for an expired window with no domain, `vmDeployInFlight` returns
false and `markVMMissing` sets `Phase=Missing` with `Active=false`. A sweep running
afterwards sees neither `Pending` nor an active window, so the expired deploy would
never be failed or cleaned up.

The resume sweep therefore runs **before** `SyncAll` on each tick.

### 3. Clean up before re-running a deploy

Re-running `Deploy()` is not safe without cleanup, because the libvirt operations
differ in idempotency:

| Operation | Re-run behaviour | Location |
|---|---|---|
| `DefineDomain` | Idempotent; `DomainDefineXMLFlags` overwrites an existing inactive domain | `internal/libvirt/domain.go:16` |
| `CreateVolume` | Fails; `StorageVolCreateXML` errors when the volume exists | `internal/libvirt/storage.go:32` |
| `CreateOverlayVolume` | Fails, same reason | `internal/libvirt/storage.go:62` |

A restart between volume creation and domain definition leaves an orphan volume. A
naive re-run would fail with "already exists" and mark every such VM `Error` —
turning automatic recovery into a guaranteed failure.

The sweep therefore tears down leftover host state before re-running. The existing
`teardownVMRuntimeOnHypervisor` (`internal/infra/api/vm.go:276`) already performs
`DestroyDomain` → `UndefineDomain` → `DeleteVolume` with not-found errors tolerated,
so it is reused rather than reimplemented.

It currently lives in `internal/infra/api` but is needed by two callers: the deploy
goroutine (§1) and the resume sweep (§2, in `internal/app`). It moves to
`internal/vm`, alongside the deploy logic it complements, and both callers invoke it
from there. `internal/vm` already owns `BuildLibvirtConfig`, `IsIgnorableDestroyError`
and `SkipHostStorageCleanup`, which the helper depends on, so this removes an
api→vm indirection rather than adding a new dependency edge.

The move must preserve `ErrVMTeardownNotAttempted` (`internal/infra/api/vm.go:237`).
`DeleteVirtualMachine` relies on it: a `Missing` VM whose hypervisor is unreachable
falls back to a record-only delete precisely because teardown never touched the host
(`internal/infra/api/vm.go:217`). Dropping the marker would turn that case into a
`502` that strands the record. The sentinel moves to `internal/vm` with the function.

#### The post-definition crash window

`markDomainDefined` persists at `internal/vm/deploy.go:91`, but `StartDomain`
(`:102`) and the network-to-`hd` boot device switch (`:108`) run after it. A crash in
between leaves `DomainObservedAt` set with the guest never started, or started but
still set to PXE-boot on next reboot.

Such a record is excluded from resume by the rule above, and the runtime sync loop
only observes domain state — it neither starts an inactive guest nor completes the
boot-device switch. The VM would sit until its provisioning window times out.

Both remaining operations are idempotent (`StartDomain` on a running domain and
setting an already-`hd` boot device are both no-ops or trivially repeatable), so
recovery completes them rather than redeploying: for a record with `DomainObservedAt`
set whose domain exists but is not running, start it and reassert the boot device.
This is a distinct, cheaper recovery path than a full redeploy.

**The marker is not durable on its own.** `markDomainDefined` logs and returns when
its store write fails (`internal/vm/deploy.go:241`), while `DefineDomain` has already
succeeded. A restart after that leaves a prior-epoch record with a nil marker and a
domain that exists — which the rules above classify as `ResumeRedeploy`, destroying a
defined or running domain instead of finalizing it.

Recovery therefore must not treat a nil marker as proof that no domain exists. Before
any destructive teardown, the resumed worker inspects the host: if the domain is
already defined, it takes the finalize path regardless of what the marker says. The
persisted marker becomes an optimisation, not the sole source of truth.

**Finalization is not limited to `Pending`.** When pre-domain work outlives the
original deadline, the runtime sync loop marks the record `Missing`, and
`markDomainDefined` then re-arms the window (`internal/vm/deploy.go:237`) without
restoring the phase. A crash immediately after leaves a prior-epoch record that is
`Missing` with an active window and `DomainObservedAt` set. `SyncAll` maps its
shut-off domain to `Provisioning` (`internal/vm/runtime_sync.go:401-405`) but never
starts it, and a `Pending`-only rule ignores it — so the VM never boots.

Finalization is therefore selected by the provisioning window and domain marker, not
by phase: any recoverable record with an active, uncompleted window whose domain is
defined is a finalize candidate, whether its phase reads `Pending`, `Missing` or
`Provisioning`.

**Finalization must renew the window, not just the phase.** A slow define that
outlived the original deadline reaches finalize with an expired `DeadlineAt` — and
`SyncAll`, which runs immediately after on the same tick, marks any record with an
expired active window `Error` for provisioning timeout
(`internal/vm/runtime_sync.go:291-294`). Restoring only the phase would hand the sync
loop a VM that was just recovered and let it fail it immediately.

Finalization therefore persists, in one write before the sweep returns: the
`DomainObservedAt` marker (which may be missing if the original write failed), a
deadline renewed from now by the original window length — the same renewal
`markDomainDefined` performs (`internal/vm/deploy.go:232-236`) — the restored
`Provisioning` phase, and `HostSetupDoneAt`.

#### Every path that arms a window takes the lease

The lease is not a create-specific mechanism. The sweep's candidate set is defined by
*the provisioning window*, so **every code path that arms one must claim ownership**,
or the sweep will treat live work as orphaned.

That includes redeploy, which this design does not otherwise touch. The reinstall
handler persists an active `Provisioning` window (`internal/infra/api/vm_reinstall.go:98-104`)
and then runs `Redeploy` synchronously for as long as it takes to recreate the volume
and redefine the domain. Without an epoch that record matches the sweep exactly —
active window, no owner, no `HostSetupDoneAt`, no domain yet — so a tick landing
mid-redeploy would tear down host state underneath a live operation. Redeploy
therefore stamps the epoch when it arms the window, clears `HostSetupDoneAt` (its
previous deploy's completion no longer applies), sets `HostSetupDoneAt` again when it
finishes, and releases the lease on exit.

The same obligation applies to any future path that arms a provisioning window.

#### Every worker releases its lease

A resumed worker also stamps the current epoch when it starts, and must clear it on
every exit path — success, early return from hypervisor resolution or teardown, or a
`Deploy` that hit its own deadline — using a fresh context. Omitting the release on any
path strands the record permanently, because subsequent sweeps see the current epoch
and skip it.

Each recovery job also runs under its **own bounded context**, not the application
lifetime context. A hung hypervisor or image transfer would otherwise occupy a worker
slot forever, and with enough of them every later interrupted VM stays unrecovered even
though the sync tick itself keeps running. The per-job timeout is what guarantees the
slot and the lease are both eventually released.

#### Cleanup after a timed-out deploy

Both the stale-identity recheck and the teardown that follows it must use a fresh,
bounded context rather than the deploy context. When `Deploy` returns because the
provisioning timeout expired, the deploy context is already cancelled: the identity
read fails with `context deadline exceeded` (matching neither stale condition, so a
concurrently deleted VM goes unnoticed) and `TeardownHostState` cannot open a
connection. The lease release already uses a fresh context; the recheck and cleanup
need the same treatment.

#### Conditional writes, not read-modify-write

Releasing the lease by reading the record, comparing the token, and writing it back is
a race: between the read and the write the VM can be deleted and recreated under the
same name, and the stale worker's snapshot then overwrites the replacement's row.
`writeExisting` does not prevent this — it only checks that *a* record exists
(`internal/vm/store.go:32`), and falls back to a plain `Upsert` on stores without
`ExistingUpdater`.

The release must be a single store-level conditional update: clear the epoch **where**
the name and completion token both still match. The same applies to any other
token-guarded mutation introduced by this design.

#### Backing image integrity

`prepareCloudImageBacking` treats `VolumeExists` as proof that a previous upload
finished (`internal/vm/cloud_image.go:47-52`). A crash during `StorageVolUpload`
leaves the shared hashed backing volume allocated but partially written, and
`TeardownHostState` will not remove it because it is keyed by image hash, not VM name.
A resumed deploy would then build an overlay on corrupt data.

Publication must become atomic: upload to a temporary volume name and rename, or
record a completion marker that `prepareCloudImageBacking` verifies before reuse.
This is shared infrastructure — a partially uploaded backing already affects any
deploy that reuses the image, not only resumed ones.

### 4. Reject duplicate VM names on the server

`CreateVirtualMachine` returns `409 Conflict` when the name is taken. This is the last
line of defence for concurrent requests that bypass the UI (multiple tabs, direct API
calls).

A `Get`-then-`Upsert` check in the handler does not achieve this. Two concurrent
requests can both observe the name as absent and both proceed to upsert, so one
silently overwrites the other and both return success — the exact failure the guard is
meant to prevent. The check must be atomic at the store layer.

The VM store exposes only `Upsert`, whose `ON CONFLICT (name) DO UPDATE`
(`internal/infra/sql/vm_store.go:118`) is what makes the overwrite possible. The
design adds an insert-only store operation that lets the primary key reject the
duplicate, and maps that rejection to `409`. Tests must issue the competing requests
concurrently; sequential requests do not exercise the race.

This is a breaking change: the endpoint previously overwrote the existing record and
returned success.

### 5. Increment the preset count optimistically in the UI

The `count` increment moves from after the API response to the moment of the click,
so the next click always produces a different VM name. `quickDeploying` narrows to
cover only the brief request dispatch. Since `count` is already written to
`localStorage` on change, numbering survives a reload.

## Failure reporting

No new mechanism. The deploy result is read from the persisted VM record: the phase
badge and `lastError`, already rendered in the VM list. Because `FailDeploy` runs on
the detached context, error details are written even when the client has disconnected,
and they remain visible after a reload or restart.

## Verification

Correctness checks required before this is considered done:

- **Live deploy is not disturbed**: a normal deploy that is still in its pre-definition
  stage survives one or more sync ticks untouched. This is the primary regression risk
  of the whole design.
- **Idempotency**: re-running a deploy after cleanup succeeds for both the curtin
  overlay path and the non-curtin `CreateVolume` path, with the fake executor
  implementing the volume-creation methods so the retry actually executes.
- **Reload**: a deploy started and then interrupted by a page reload still completes.
- **Restart**: a deploy interrupted by a server restart resumes and reaches a terminal
  phase; a deploy past its deadline is cleaned up and marked `Error` rather than
  retried forever or left with an orphan volume.
- **Sweep ordering**: the expired-deploy path is exercised through the real
  `syncVMRuntimeStates` tick, not by calling the resume function directly, so the
  ordering against `SyncAll` is actually covered.
- **Post-definition crash windows**: a crash after `DefineDomain` but before
  `StartDomain`, and after `StartDomain` but before the boot-device switch, both
  recover to a running VM booting from disk.
- **Healthy VMs are never re-finalized**: a successfully deployed VM sitting in
  `Provisioning` with its lease released survives many sweep ticks with no libvirt
  calls at all. This is the primary regression risk of the recovery design.
- **A live redeploy is not disturbed**: a sweep tick landing while the synchronous
  reinstall handler is recreating a volume or redefining a domain performs no host
  operations against that VM.
- **Stuck recovery jobs release their slot**: a recovery job whose hypervisor calls
  hang hits its own timeout, frees its worker slot and releases its lease, so later
  interrupted VMs are still recovered.
- **Lease release on timeout**: a deploy goroutine that hits its context deadline
  releases its epoch, so the next sweep recovers the record instead of treating a dead
  worker as live forever. Resumed workers release on every exit path too.
- **Release is atomic**: a delete-and-recreate racing a lease release does not let the
  stale worker overwrite the replacement's row.
- **Expired-context cleanup**: a deploy that returns at its provisioning timeout still
  detects a concurrently deleted VM and tears down its host state.
- **Finalize renews the deadline**: a VM recovered by finalization is not immediately
  failed by the `SyncAll` that runs on the same tick.
- **Dispatch race**: a VM deleted and recreated between sweep classification and worker
  execution is not torn down by the stale worker.
- **Lost checkpoint**: a successful `DefineDomain` whose marker write failed, followed
  by a restart, finalizes the existing domain rather than destroying it.
- **Re-armed Missing**: a deploy whose pre-domain work outlived the deadline, marked
  `Missing` and then re-armed by `markDomainDefined`, still gets finalized after a
  crash rather than being ignored.
- **Duplicate names**: *concurrent* creates with the same name yield exactly one
  record, the loser receiving `409`.
- **Async response**: the create test uses a deployer that blocks, proving the `201`
  arrives while the deploy is still in flight rather than passing vacuously with a nil
  deployer.
- **Teardown marker**: an unreachable hypervisor still allows a `Missing` VM to be
  deleted record-only rather than returning `502`.
- **Stale-deploy cleanup**: a VM deleted and recreated under the same name while the
  original deploy is running leaves no orphan domain or volume on the old hypervisor.
- **Backing image integrity**: a partially uploaded backing volume is not reused as if
  complete.
- **Rapid clicks**: N consecutive Quick Deploy clicks produce N distinct VMs.
- **OS coverage**: the resume sweep is exercised with a genuinely non-Ubuntu image —
  a Debian- or Red Hat-family catalog entry, not an Ubuntu fixture with a different
  install type — so recovery code that assumes Ubuntu catalog metadata is caught. Where
  a family is intentionally unsupported, assert the explicit early error instead (per
  project OS-deployment policy).
- **Existing tests**: identify and update tests that assume `POST /virtual-machines`
  completes the deploy synchronously.
- **Cascade delete**: the relocated concurrent-hypervisor-delete handling still tears
  down host state correctly.
- **Sync loop responsiveness**: recovery work must not block the runtime sync tick. A
  slow backing-image transfer for one VM must not delay status updates for every other
  VM, so resumed deploys run off the loop with a bounded worker count. This also bounds
  the restart storm when many interrupted records exist at startup.

## Cross-surface consistency

The Machines side has an equivalent Quick Deploy with the same optimistic-count and
duplicate-name concerns (`web/src/components/views/MachinesView.tsx:97`,
`web/src/components/views/machines/MachinesWorkspace.tsx:60`). Per the project UI
policy, the UI changes are applied to both surfaces. Whether the machine PXE deploy
path needs the same async/resume treatment is assessed during planning; it is not
assumed to be identical to the VM path.
