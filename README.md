# gomi

> Bare-metal and virtual machine provisioning control plane for homelab and small-scale infrastructure.

gomi manages the full lifecycle of physical machines and VMs from a single web console and REST API: PXE boot, OS install, power control, hypervisor management, and network resource tracking.

**What gomi manages**

- Physical machines — PXE-based OS deployment, power control, lifecycle phases
- Hypervisors and virtual machines — create, redeploy, migrate, remove
- OS images and the OS catalog
- Cloud-Init templates
- Network resources — subnets, DHCP leases
- Users, roles, and audit activity

The active roadmap lives in [docs/MILESTONE.md](docs/MILESTONE.md).

---

## Quick Start

Install from the Debian package:

```bash
sudo dpkg -i gomi_<version>_<arch>.deb
sudo systemctl start gomi
```

Create the first admin user:

```bash
sudo sh -c "printf '%s\n' 'change-me' > /tmp/gomi-admin-password"
sudo gomi setup admin \
  --config=/etc/gomi/gomi.yaml \
  --username=admin \
  --password-file=/tmp/gomi-admin-password
rm -f /tmp/gomi-admin-password
```

Open the web console at `http://<host>:8080/` and sign in.

### First-time checklist

1. Confirm `http://<host>:8080/healthz` responds.
2. Create the first admin user (above).
3. Sign in and create additional users with appropriate roles.
4. Configure network and provisioning resources.
5. Register Machines and, if needed, Hypervisors.
6. Add OS images or install them from the OS catalog.
7. Start provisioning or power operations from the console or API.

---

## Access

| Endpoint | URL |
|---|---|
| Web console | `http://<host>:8080/` |
| API base | `/api/v1` |
| Health check | `/healthz` |
| OpenAPI spec | [`openapi/openapi.yaml`](openapi/openapi.yaml) |

### Roles

| Role | Access |
|---|---|
| `admin` | Full access including user management and secret-backed resources |
| `operator` | Operational write access for provisioning and lifecycle actions |
| `viewer` | Read-only |

---

## Core workflows

### Machine provisioning

1. Register or create an OS image.
2. Create a Machine and configure power control and networking.
3. Trigger redeploy or reinstall.
4. The target boots through PXE and completes the install flow automatically.

### Virtual machine provisioning

1. Register a Hypervisor.
2. Create an OS image and Cloud-Init template if needed.
3. Create a VirtualMachine and power it on.

### Hypervisor registration

Hypervisors register through a token-based flow. The API exposes helper endpoints and a setup script for bringing up the agent.

---

## Configuration

The main config file is `/etc/gomi/gomi.yaml`. An annotated example is at [`packages/debian/gomi.yaml`](packages/debian/gomi.yaml).

Configuration is loaded from three sources in order (later sources override earlier ones):

1. YAML file
2. Environment variables
3. CLI flags

**Defaults**

| Key | Default |
|---|---|
| Listen address | `0.0.0.0:8080` |
| Data directory | `/var/lib/gomi/data` |
| Database | SQLite at `/var/lib/gomi/data/gomi.db` |
| DHCP mode | `full` |
| Boot environment source | `https://github.com/sugaf1204/gomi/releases/latest/download` |

**Common environment variables**

```
GOMI_CONFIG
GOMI_LISTEN_ADDR
GOMI_DATA_DIR
GOMI_DB_DRIVER
GOMI_DB_DSN
GOMI_DNS_MODE
GOMI_DHCP_MODE
GOMI_DHCP_IFACE
GOMI_TFTP_ROOT
GOMI_PXE_HTTP_BASE_URL
GOMI_BOOTENV_SOURCE_URL
```

See [`.env.example`](.env.example) for a full example.

---

## Developer setup

### Prerequisites

**Required**

- Go `1.25.x` (see [`go.mod`](go.mod))
- Node.js `20.19+` and npm
- [`task`](https://taskfile.dev/)

**Optional** (needed for specific workflows)

- KVM / libvirt
- `ipxe`, `grub-efi-amd64-bin`
- Ansible, `uv`

### Initial setup

```bash
cp .env.example .env
task web:install
```

The frontend (`web/dist`) is embedded into the Go binary at build time, so build the web app before running backend tests or packaging.

### Run locally

```bash
# with background sync
task run

# without background sync
task operator
```

The server starts on `http://127.0.0.1:8080` unless overridden via `--listen` or `GOMI_LISTEN_ADDR`.

### Frontend development

```bash
task web:dev
```

Point the Vite dev server at a running backend API. Useful frontend env vars:

```
VITE_API_BASE
VITE_GOMI_BOOTSTRAP_USERNAME
VITE_GOMI_BOOTSTRAP_PASSWORD
```

### Tests and checks

```bash
task test        # backend tests (depends on task web:build)
task vet         # backend vet (depends on task web:build)
task web:test    # frontend tests
task check       # standard backend verification flow
```

### Build artifacts

```bash
task build:hypervisor-agent   # hypervisor agent binaries
task build:deb-artifacts      # all deb build inputs
task build:deb                # Debian package
```

The Debian package version is read from `packages/debian/changelog`.

---

## Repository layout

```
cmd/
  gomi/                 main server entrypoint
  gomi-hypervisor/      hypervisor agent
  gomi-osimage/         OS image tooling
  wol-daemon/           Wake-on-LAN daemon
internal/               application logic
web/                    React + Vite frontend
bootenv/                PXE deploy boot environment builder
openapi/                REST API specification
packages/               Debian packaging files
tests/
  e2e/                  end-to-end scripts
  lab/ansible/          remote KVM PXE lab automation
```

---

## Boot environment

`bootenv/` builds the lightweight PXE deploy runtime consumed by gomi. See [`bootenv/README.md`](bootenv/README.md) for details.

gomi resolves boot environment artifacts from:

- a release-style URL that serves `manifest.json` (default)
- a local artifact directory via `GOMI_BOOTENV_SOURCE_URL`

---

## Lab and end-to-end testing

[`tests/lab/ansible/`](tests/lab/ansible) provisions a remote KVM host for full end-to-end validation:

- gomi server VM + PXE target VMs
- Ubuntu 22.04 and 24.04 coverage
- NIC matrix validation

```bash
cd tests/lab/ansible
uv sync
export GOMI_LAB_SSH_PASSWORD=gomi
export GOMI_LAB_SUDO_PASSWORD=gomi
uv run molecule test -s gomi-kvm-lab
```

See [`tests/lab/ansible/README.md`](tests/lab/ansible/README.md) for the full lab flow.

---

## API reference

- First-run setup status: `GET /api/v1/setup/status`
- Login: `POST /api/v1/auth/login`
- Hypervisor registration helpers: `/api/v1/hypervisors/...`

For full request/response shapes, use [`openapi/openapi.yaml`](openapi/openapi.yaml) as the source of truth.
