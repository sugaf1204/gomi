# gomi

gomi is a control plane for bare-metal and virtual-machine provisioning.
It provides a web console and REST API for machine lifecycle operations, PXE workflows, OS image management, hypervisor registration, and infrastructure inventory.

## What GOMI manages

- Machines for PXE-based OS deployment and lifecycle operations
- Hypervisors and VirtualMachines
- OS images and OS catalog entries
- Cloud-Init templates
- Network resources such as subnets and DHCP leases
- User accounts, roles, and audit activity

The current milestone is tracked in [docs/MILESTONE.md](docs/MILESTONE.md).

## Access

- Web console: `http://<gomi-host>:8080/`
- API base path: `/api/v1`
- Health check: `/healthz`
- OpenAPI spec: [`openapi/openapi.yaml`](openapi/openapi.yaml)

## Access roles

- `admin`: full access, including user management and secret-backed resources
- `operator`: operational write access for provisioning and lifecycle actions
- `viewer`: read-only access

## Install and first-time setup

### Install from Debian package

The Debian package installs:

- `gomi` server binary
- `gomi-hypervisor` agent binaries
- `wol-daemon`
- systemd unit: `gomi.service`
- default config file: `/etc/gomi/gomi.yaml`

Typical install flow:

```bash
sudo dpkg -i ./packages/deb/gomi_<version>_<arch>.deb
sudo systemctl status gomi
```

The package creates the `gomi` system user/group and prepares `/var/lib/gomi`.

### Configure the server

The main config file is `/etc/gomi/gomi.yaml`.
An example is kept in [`packages/debian/gomi.yaml`](packages/debian/gomi.yaml).

Important defaults:

- listen address: `0.0.0.0:8080`
- data directory: `/var/lib/gomi/data`
- database: SQLite at `/var/lib/gomi/data/gomi.db`
- DHCP mode: `full`
- boot environment source: `https://github.com/sugaf1204/gomi/releases/latest/download`

Configuration can be provided from:

1. YAML file
2. environment variables
3. CLI flags

Common environment variables:

- `GOMI_CONFIG`
- `GOMI_LISTEN_ADDR`
- `GOMI_DATA_DIR`
- `GOMI_DB_DRIVER`
- `GOMI_DB_DSN`
- `GOMI_DNS_MODE`
- `GOMI_DHCP_MODE`
- `GOMI_DHCP_IFACE`
- `GOMI_TFTP_ROOT`
- `GOMI_PXE_HTTP_BASE_URL`
- `GOMI_BOOTENV_SOURCE_URL`

See [`.env.example`](.env.example) and [`packages/debian/gomi.yaml`](packages/debian/gomi.yaml) for concrete examples.

### Create the first admin user

GOMI does not create default credentials unless you explicitly configure `admin.username` and `admin.password`.
Create the first admin on the server with:

```bash
sudo sh -c "printf '%s\n' 'change-me' > /tmp/gomi-admin-password"
sudo gomi setup admin \
  --config=/etc/gomi/gomi.yaml \
  --username=admin \
  --password-file=/tmp/gomi-admin-password
rm -f /tmp/gomi-admin-password
```

You can also use `--password-stdin`.

To make the setup idempotent in automation, use:

```bash
gomi setup admin --ignore-already-configured ...
```

### First-time checklist

1. Install the package and confirm `http://<host>:8080/healthz` responds.
2. Create the first admin user.
3. Sign in to the web console.
4. Create additional users and assign roles as needed.
5. Configure network and provisioning resources.
6. Register Machines and, if needed, Hypervisors.
7. Add OS images or install them from the OS catalog.
8. Start provisioning or power operations from the console or API.

## Core workflows

### Machine provisioning

1. Register or create an OS image.
2. Create a Machine.
3. Configure power control and networking.
4. Trigger redeploy or reinstall.
5. Let the target boot through PXE and complete the install flow.

### Virtual machine provisioning

1. Register a Hypervisor.
2. Create an OS image and Cloud-Init template if needed.
3. Create a VirtualMachine.
4. Power on, redeploy, migrate, or remove it from the console/API.

### Hypervisor registration

Hypervisors register through a token-based flow.
The API exposes helper endpoints and a setup script for bringing up the agent.

## Developer setup

### Prerequisites

- Go `1.25.x` or compatible with [`go.mod`](go.mod)
- Node.js `20.19+`
- npm
- [`task`](https://taskfile.dev/)

Optional but commonly needed depending on what you are testing:

- KVM/libvirt
- `ipxe`
- `grub-efi-amd64-bin`
- Ansible
- `uv`

### Initial setup

```bash
cp .env.example .env
task web:install
```

The frontend is embedded into the Go binary from `web/dist`, so build the web app before running backend tests or packaging.

### Run locally

Run the integrated server with background sync enabled:

```bash
task run
```

Run without background sync:

```bash
task operator
```

This starts the server on `http://127.0.0.1:8080` unless you override `--listen` or `GOMI_LISTEN_ADDR`.

### Frontend development

Run the Vite dev server:

```bash
task web:dev
```

Useful frontend env vars:

- `VITE_API_BASE`
- `VITE_GOMI_BOOTSTRAP_USERNAME`
- `VITE_GOMI_BOOTSTRAP_PASSWORD`

When using the Vite dev server, point it at a running backend API.

### Backend and frontend checks

```bash
task test
task vet
task web:test
task check
```

Notes:

- `task test` and `task vet` depend on `task web:build`
- `task check` runs the standard backend verification flow
- `task web:test` runs the frontend in test mode

### Build artifacts

Build hypervisor agents:

```bash
task build:hypervisor-agent
```

Build Debian package artifacts:

```bash
task build:deb-artifacts
task build:deb
```

The Debian package build uses the version from `packages/debian/changelog`.

## Repository layout

- [`cmd/gomi`](cmd/gomi): main server entrypoint
- [`cmd/gomi-hypervisor`](cmd/gomi-hypervisor): hypervisor agent
- [`cmd/gomi-osimage`](cmd/gomi-osimage): OS image related tooling
- [`cmd/wol-daemon`](cmd/wol-daemon): Wake-on-LAN daemon
- [`internal/`](internal): application logic
- [`web/`](web): React + Vite frontend
- [`bootenv/`](bootenv): boot environment builder used by PXE deploy workflows
- [`openapi/`](openapi): REST API specification
- [`packages/`](packages): Debian packaging files
- [`tests/e2e/`](tests/e2e): end-to-end scripts
- [`tests/lab/ansible/`](tests/lab/ansible): remote KVM PXE lab automation

## Boot environment

`bootenv/` builds the lightweight PXE deploy runtime consumed by GOMI.
See [`bootenv/README.md`](bootenv/README.md) for details.

GOMI can consume boot environment artifacts from:

- a release-style URL that resolves to `manifest.json`
- a local artifact directory via `GOMI_BOOTENV_SOURCE_URL`

## End-to-end and lab testing

Repository-level smoke and lab assets live under [`tests/`](tests).

### Remote KVM PXE lab

[`tests/lab/ansible/`](tests/lab/ansible) provisions a remote KVM host for end-to-end validation, including:

- GOMI server VM
- PXE target VMs
- Ubuntu 22.04 / 24.04 coverage
- NIC matrix validation

Quick start:

```bash
cd tests/lab/ansible
uv sync
export GOMI_LAB_SSH_PASSWORD=gomi
export GOMI_LAB_SUDO_PASSWORD=gomi
uv run molecule test -s gomi-kvm-lab
```

For the full lab flow and cleanup commands, see [`tests/lab/ansible/README.md`](tests/lab/ansible/README.md).

## API and automation notes

- First-run setup status: `GET /api/v1/setup/status`
- Login endpoint: `POST /api/v1/auth/login`
- Hypervisor registration helper endpoints are exposed under `/api/v1/hypervisors/...`

For generated request/response shapes, use [`openapi/openapi.yaml`](openapi/openapi.yaml) as the source of truth.
