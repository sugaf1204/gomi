# Milestones

## Priority: High

### 1. OS Image Creation with mkosi
Use `mkosi` for OS image creation. Separate the GitHub Actions workflow for image creation and artifact storage into another repository.

### 2. Fast OS Installation / Curtin-less Mode
Speed up the `curtin` installation time. Eliminate `apt-get` operations during the curtin phase. Alternatively, design and implement a curtin-less mode and conduct performance measurement experiments.

### 3. Universal Netplan Support
Adopt `netplan` for network configuration on non-Ubuntu OSes as well.

### 4. Change Netplan File Prefix
Change the generated netplan file's number prefix to `50` (e.g., `50-gomi-network.yaml`).

### 5. Improve Deploy Timeline Design
Improve the visual design and UX of the deploy timeline in the web UI.

### 6. Fix DNS Name Resolution Timeout
Investigate and fix the issue where DNS name resolution times out during operations.

## Priority: Medium
### 6. Hypervisor Bridge Management

- Allow a hypervisor to have **multiple bridges** (currently limited to one).
- When creating a machine, provide a **checkbox to auto-create a bridge**.
  - If the checkbox is enabled, the system automatically provisions the bridge
    on the target hypervisor before attaching the machine.

### 7. VirtualMachine Creation — Bridge Selection

- When a hypervisor is **explicitly specified**, present a **selectable list of
  its bridges** so the user can pick which bridge to attach the VM to.
- When no hypervisor is specified (random placement), the system selects a
  hypervisor and bridge automatically.

### 8. VLAN Support

- Add VLAN awareness to the networking layer.
- Allow bridges and/or subnets to be associated with a VLAN ID.
- Ensure VLAN tagging is correctly applied when provisioning network interfaces.

### 9. DNS integration
integrate with RFC2136 and PowerDNS

### 10. live migration
live migration is implemented but not tested.

### 11. vnc
you can use vnc console on web ui.

### 12. cli
create gmctl that can operate gomi.

### 13. metadata
add tag to machine which can be used filtering

### 14. Support Desktop Ubuntu/Debian, and other linux distribution
Support deploying desktop Ubuntu/Debian

### 15. cleanup qcow2
At deleting VirtualMachine, delete vm image qcow2 file on hypervisor.

### 16. Register Hypervisor
When deploying machine, register it as hypervisor with selected hypervisor checkbox

### 17. Log severity
make gomi server log severity variable setting

### 18. don't use pkg pattern
rearchitecte gomi go source.

### 19. install OS images from web ui
List up OS Image that supported gomi, and you can install there by click install button.

### 20. Storage layout
wip

### 21. apt proxy cache
implemet apt proxy on gomi

### 22. Separate API DTOs from internal models

Machine and VM API handlers currently return internal domain models directly.
Introduce explicit request/response DTOs so persisted fields, runtime-only
fields, and external API fields can evolve independently. This should include
redaction rules for sensitive or internal-only fields and focused tests for list,
detail, create, update, and redeploy responses.

### 23. deploy workflow
use taskfile

### 24. Embed Hypervisor setup script

Move Hypervisor setup-and-register shell script out of Go raw strings and serve
it with go:embed. Keep libvirt TCP auth setup in that script, avoid complex
escaped regex in Go code, and add script-focused tests such as response content
checks and bash syntax validation. For the current lab/dev TCP flow, configure
libvirt with auth_tcp = "none" directly and revisit SSH/TLS/SASL support before
production use.

### 25. Redesign API following Google AIP guidelines

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

### 26. Target OS rootfs and hardware bundle deployment

Reduce target OS image size without losing bare-metal driver support. Do not mix
the PXE runtime OS with the installed target OS.

- Keep the PXE OS minimal: deploy runner, curtin, inventory collection, and
  artifact download/apply logic only.
- Store target OS hardware support as versioned GOMI artifacts instead of
  baking everything into the target image or PXE rootfs.
- Treat firmware and kernel modules differently:
  - Firmware can be selected by hardware inventory and copied into the target
    rootfs when needed.
  - Kernel modules must match the target kernel ABI, so modules are provided as
    target-kernel-specific bundles.
- Extend OS image manifests to describe the target kernel and available hardware
  bundles, including module names, firmware paths, checksums, and sizes.
- During deploy, use hardware inventory to select only the required bundles,
  download them from the GOMI server, and inject them into curtin's target rootfs
  after extract.
- After injection, run target-side `depmod` and `update-initramfs` for the target
  kernel so NIC/storage drivers are available after reboot.
- Keep Packer optional. Standard GOMI usage should work with catalog images plus
  manifest-driven hardware bundles, without requiring users to build images on
  the GOMI server.

### 27. Bug: loginUser cannot override default cloud user password

When deploying a cloud-image VirtualMachine with `loginUser.username=ubuntu` and
`loginUser.password` set, password SSH login does not work, while a separate
custom login user such as `gomi/gomi` works. This likely comes from cloud-init
`users: [default, ubuntu]` merge behavior when the requested login user matches
the distribution's default cloud user. Fix the cloud-init generation so
password-backed login works for both default users and custom users, and add an
integration-style test for Ubuntu cloud images.

### 28. Secure production libvirt authentication

The current VM deploy implementation still uses libvirt TCP from the GOMI
server to the hypervisor. The lab-only `auth_tcp = "none"` setup must not be the
production default. Define and implement a production-safe connection/auth model,
such as SSH transport, TLS client certificates, SASL, or a `gomi-hypervisor`
agent API, and update the setup/register flow so unauthenticated libvirt TCP is
clearly limited to local lab/dev testing.

### 29. Multi-OS curtin and artifact deploy design

The current curtin/rootfs deployment path was validated primarily with Ubuntu
and must not grow into Ubuntu-specific behavior hidden inside generic deploy
logic. Refactor curtin config generation so OS-family-specific behavior is
explicitly selected by catalog metadata, image/rootfs artifact capabilities, or
typed OS family branches.

- Keep curtin YAML generation centralized in a small builder layer rather than
  scattering `sources`, `storage`, `stages`, `late_commands`, and shell snippets
  across deploy handlers.
- Treat rootfs SquashFS artifacts, qcow2 cloud images, ISO installers, and
  future filesystem artifacts as distinct deploy capabilities instead of
  inferring behavior from filenames or one Ubuntu example.
- Isolate Ubuntu/Debian-specific cloud-init, netplan, bootloader, package, and
  post-install assumptions behind clearly named helpers.
- For Red Hat-family, Fedora, Debian, and Ubuntu paths, either implement the
  correct behavior or fail early with an explicit unsupported-family error.
- Add tests that cover at least one non-Ubuntu path, or the explicit
  unsupported-family error, whenever OS deploy behavior changes.

## 25. Curtin config cleanup and deploy acceleration

The current curtin path still carries install-time behavior that is unnecessary
when GOMI deploys a completed target OS image. Remove redundant package-manager
and kernel-install work so completed images are written and finalized with the
minimum required steps.

- For completed rootfs or filesystem-image based deploys, stop asking curtin to
  install or replace the target kernel package unless the image metadata
  explicitly says the image is incomplete and requires it.
- Remove unnecessary `apt update`, package refresh, or equivalent target-side
  package-manager work from curtin/preseed completion flows when the deployed
  image already contains the required packages.
- Keep only the install steps that are still required for the selected artifact
  type: storage layout, image extraction, bootloader/fstab integration,
  cloud-init seed injection, and narrowly scoped target finalization.
- Add timing-oriented tests or lab validation that prove the optimized path
  reduces deploy time without regressing already working raw, squashfs, or
  direct cloud-image flows.

## 26. Future macOS host support

Keep the architecture open so the GOMI server itself can run on macOS in the
future, not only on Linux. This goal is limited to server runtime support; OS
image build and other Linux-specific artifact build workflows do not need to
run on macOS.
