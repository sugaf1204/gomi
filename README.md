# gomi

gomi is a control plane for bare-metal and virtual-machine provisioning.
It provides a web console and REST API for machine lifecycle operations, PXE workflows, and infrastructure inventory.

## Access

- Web console: `http://<gomi-host>:8080/`
- API base path: `/api/v1`
- Health check: `/healthz`

On a fresh install, create the first admin user on the server with `gomi setup admin`.
GOMI does not create default credentials unless `admin.username` and `admin.password` are explicitly configured.

## Core User Workflows

1. Create and manage machines.
2. Run machine actions: redeploy, reinstall, power on, and power off.
3. Manage hypervisors and virtual machines.
4. Manage provisioning assets:
   - Cloud-Init templates
   - OS images
   - S3 image repositories
5. Manage networking and inspect DHCP leases.
6. Review audit activity.
7. Manage users and access roles.

## Access Roles

- `admin`: full access, including user management and secret-backed resources.
- `operator`: operational write access for provisioning and lifecycle actions.
- `viewer`: read-only access.

## API Reference

- OpenAPI specification: `openapi/openapi.yaml`
- First-run setup status: `GET /api/v1/setup/status`
- Login endpoint: `POST /api/v1/auth/login`

## First-Time Checklist

1. Create the first admin with `gomi setup admin --config=/etc/gomi/gomi.yaml --username admin --password-file /path/to/password`.
2. Sign in as the admin user.
3. Create user accounts with appropriate roles.
4. Configure network/provisioning resources.
5. Add machines (and hypervisors/VMs if needed).
6. Trigger provisioning or power operations from the console.
