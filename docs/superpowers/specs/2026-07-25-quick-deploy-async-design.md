# Quick Deploy: Asynchronous Deploy with Reload and Restart Resilience

Date: 2026-07-25
Status: Approved for planning

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

## Goals

1. Quick Deploy and Preset buttons return to an enabled state without waiting for the
   deploy to finish.
2. Repeated clicks never collide on a VM name and never overwrite an existing record.
3. A browser reload does not interrupt an in-flight deploy.
4. A server restart does not strand a deploy; interrupted deploys resume automatically.
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

### 2. Resume interrupted deploys after a restart

A goroutine dies with its process. Detaching the context alone gives no restart
resilience: a crash mid-deploy leaves a `Phase=Pending` record that nothing resumes.
`Pending` cannot transition to `Missing` under the transition table
(`internal/vm/phase.go:4`), so the runtime sync loop will not reclaim it either. Such
a record would stay `Pending` forever.

A resume sweep is added to `runVMRuntimeSyncLoop` (`internal/app/sync.go:33`), which
already runs once at startup (line 37) and every 5 seconds thereafter. Recovery
therefore happens within about 5 seconds of a restart.

Decision logic for each VM with `Phase == Pending` and `Provisioning.Active`:

```
DeadlineAt passed          -> FailDeploy(Phase=Error, "deploy interrupted by restart")
DomainObservedAt == nil    -> deploy did not reach domain definition -> clean up, then re-run Deploy()
DomainObservedAt != nil    -> domain is defined; guest-side provisioning pending -> leave to existing sync loop
```

All three inputs are already persisted on `ProvisioningStatus`
(`internal/vm/types.go:96`). `DomainObservedAt` exists precisely to distinguish the
define gap from a domain removed from the host (`internal/vm/types.go:104-108`). No
new database columns or struct fields are required.

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

### 4. Reject duplicate VM names on the server

`CreateVirtualMachine` checks for an existing record before `Upsert` and returns
`409 Conflict` when the name is taken. This is the last line of defence for
concurrent requests that bypass the UI (multiple tabs, direct API calls).

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

- **Idempotency**: re-running a deploy after cleanup succeeds for both the curtin
  overlay path and the non-curtin `CreateVolume` path.
- **Reload**: a deploy started and then interrupted by a page reload still completes.
- **Restart**: a deploy interrupted by a server restart resumes and reaches a terminal
  phase; a deploy past its deadline is marked `Error` rather than retried forever.
- **Duplicate names**: concurrent creates with the same name yield exactly one record,
  the loser receiving `409`.
- **Rapid clicks**: N consecutive Quick Deploy clicks produce N distinct VMs.
- **OS coverage**: the resume sweep covers non-curtin and non-Ubuntu deploy paths, not
  only the curtin/Ubuntu case (per project OS-deployment policy).
- **Existing tests**: identify and update tests that assume `POST /virtualmachines`
  completes the deploy synchronously (for example
  `internal/infra/api/handler_hypervisor_vm_test.go`).
- **Cascade delete**: the relocated concurrent-hypervisor-delete handling still tears
  down host state correctly.
- **Restart storm**: bound the number of deploys resumed concurrently when many
  `Pending` records exist at startup.

## Cross-surface consistency

The Machines side has an equivalent Quick Deploy with the same optimistic-count and
duplicate-name concerns (`web/src/components/views/MachinesView.tsx:97`,
`web/src/components/views/machines/MachinesWorkspace.tsx:60`). Per the project UI
policy, the UI changes are applied to both surfaces. Whether the machine PXE deploy
path needs the same async/resume treatment is assessed during planning; it is not
assumed to be identical to the VM path.
