# Next Milestone: Bridge, Subnet, and VLAN Management

## Overview

Introduce flexible network topology management by allowing hypervisors to own
multiple bridges, enabling per-machine bridge selection (with optional
auto-creation), and adding VLAN support.

---

## 1. Hypervisor Bridge Management

- Allow a hypervisor to have **multiple bridges** (currently limited to one).
- When creating a machine, provide a **checkbox to auto-create a bridge**.
  - If the checkbox is enabled, the system automatically provisions the bridge
    on the target hypervisor before attaching the machine.

## 2. VirtualMachine Creation — Bridge Selection

- When a hypervisor is **explicitly specified**, present a **selectable list of
  its bridges** so the user can pick which bridge to attach the VM to.
- When no hypervisor is specified (random placement), the system selects a
  hypervisor and bridge automatically.

## 3. VLAN Support

- Add VLAN awareness to the networking layer.
- Allow bridges and/or subnets to be associated with a VLAN ID.
- Ensure VLAN tagging is correctly applied when provisioning network interfaces.

## 4. DNS integration
integrate with RFC2136 and PowerDNS

## 5. live migration
live migration is implemented but not tested.

## 6. vnc
you can use vnc console on web ui.

## 7. cli
create gmctl that can operate gomi.

## 8. metadata
add tag to machine which can be used filtering

## 9. Support Desktop Ubuntu/Debian, and other linux distribution
Support deploying desktop Ubuntu/Debian

## 10. cleanup qcow2
At deleting VirtualMachine, delete vm image qcow2 file on hypervisor.

## 11. Register Hypervisor
When deploying machine, register it as hypervisor with selected hypervisor checkbox

## 12. Log severity
make gomi server log severity variable setting

## 13. don't use pkg pattern
rearchitecte gomi go source.

## 14. install OS images from web ui
List up OS Image that supported gomi, and you can install there by click install button.

## 15. Storage layout
wip

## 16. apt proxy cache
implemet apt proxy on gomi

## 17. Separate API DTOs from internal models

Machine and VM API handlers currently return internal domain models directly.
Introduce explicit request/response DTOs so persisted fields, runtime-only
fields, and external API fields can evolve independently. This should include
redaction rules for sensitive or internal-only fields and focused tests for list,
detail, create, update, and redeploy responses.

## 18. deploy workflow
use taskfile

## 19. Embed Hypervisor setup script

Move Hypervisor setup-and-register shell script out of Go raw strings and serve
it with go:embed. Keep libvirt TCP auth setup in that script, avoid complex
escaped regex in Go code, and add script-focused tests such as response content
checks and bash syntax validation. For the current lab/dev TCP flow, configure
libvirt with auth_tcp = "none"; revisit SSH/TLS/SASL support before production
use.

## 20. Redesign API following Google AIP guidelines

Redesign the REST API to align with Google AIP (API Improvement Proposals:
https://google.aip.dev/). Priority issues identified:

- **Pagination** (AIP-158): All list endpoints (`/virtual-machines`, `/machines`,
  etc.) currently return all records in a single response. Introduce cursor-based
  or offset-based pagination with `limit`, `pageToken`/`after`, and `hasMore`
  fields.
- **Server-side filtering** (AIP-160): Add query parameter filters to list
  endpoints (e.g., `?hypervisorRef=hv1`, `?phase=running`) so clients do not
  need to fetch all records and filter locally.
- **Immutable resource identifiers** (AIP-122): Evaluate replacing mutable `name`
  path parameters with stable UUIDs to prevent URL breakage if names change.
- **Deduplicate actions**: `reinstall` and `redeploy` are identical on both
  `/machines` and `/virtual-machines`; consolidate to a single endpoint.
