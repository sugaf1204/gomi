# GOMI KVM PXE Lab

`tests/lab/ansible` contains Ansible playbooks for automatically building the following test environment on a remote KVM host.

- One VM for the GOMI server
- PXE target VMs
- A bare-metal-like deployment flow based on Machine registration
- Ubuntu 22.04 / 24.04 matrix
- NIC matrix: `igc`, `e1000e`, `r8169`

## Prerequisites

- The target host is a Linux machine with KVM support.
- The Ansible control machine can SSH to the target host.
- The SSH user has sudo privileges.
- If Ansible uses password authentication, `sshpass` is installed on the control machine.
- `cmd/gomi` in this repository can be built locally.

This setup automatically performs the following steps.

1. Build `gomi` locally for `linux/amd64`.
2. Create a bridge and helper service on the remote host.
3. Start a GOMI server VM based on Ubuntu 24.04.
4. Install Ubuntu 22.04 / 24.04 catalog entries through the GOMI API.
5. Let GOMI build the Debian Live based SquashFS boot environment in process.
6. Register OSImage records and publish PXE boot resources through GOMI.
7. Register Machines with webhook-based power control.
8. Power on target VMs as Machines and verify PXE, curtin deployment, first boot, and SSH connectivity.

## Run

```bash
cd tests/lab/ansible
export GOMI_LAB_SSH_PASSWORD=gomi
export GOMI_LAB_SUDO_PASSWORD=gomi
ansible-playbook playbooks/run-gomi-kvm-lab.yml
```

The result summary is fetched to `tests/lab/ansible/artifacts/<inventory_hostname>/summary.json`.

## Ubuntu Desktop PXE experiment

Use `vars/ubuntu-desktop.yml` to narrow the matrix to a single Ubuntu 24.04 Desktop qcow2 image. The image must be a GOMI bare-metal qcow2 artifact, such as the `root.qcow2` output from `sugaf1204/os-images` `ubuntu-24.04-desktop`.

```bash
cd tests/lab/ansible
export GOMI_LAB_SSH_USER=gomi
export GOMI_LAB_SSH_PASSWORD=gomi
export GOMI_LAB_SUDO_PASSWORD=gomi
export GOMI_LAB_DEB=/path/to/gomi_*.deb
export GOMI_LAB_BOOTENV_SOURCE_URL=http://example.local/bootenv
export GOMI_LAB_DESKTOP_IMAGE_URL=http://example.local/root.qcow2
ansible-playbook playbooks/run-gomi-kvm-lab.yml -e @vars/ubuntu-desktop.yml
```

The Desktop case additionally verifies that `ubuntu-desktop-minimal` is installed and a display manager is enabled after first boot. It records display-manager active state, but does not fail on inactive state because the QEMU target is headless.

## Molecule smoke test

Molecule runs the existing playbooks with the local `molecule-qemu-kvm` driver from `molecule_plugins/qemu_kvm`. The default scenario is a smoke test that creates one GOMI server VM and one PXE target VM on the same KVM host.

```bash
cd tests/lab/ansible
uv sync
export GOMI_LAB_SSH_PASSWORD=gomi
export GOMI_LAB_SUDO_PASSWORD=gomi
uv run molecule test -s gomi-kvm-lab
```

The driver is intentionally small: lifecycle work stays in Ansible playbooks, while the remote runner performs QEMU/KVM checks on the Linux lab host. To keep runtime manageable, the Molecule smoke test narrows the scenario playbooks to one Ubuntu 24.04 + `e1000e` case. Run `ansible-playbook playbooks/run-gomi-kvm-lab.yml` directly when you need the full Ubuntu 22.04 / 24.04 and NIC matrix.

## cleanup

```bash
cd tests/lab/ansible
export GOMI_LAB_SSH_PASSWORD=gomi
export GOMI_LAB_SUDO_PASSWORD=gomi
ansible-playbook playbooks/cleanup-gomi-kvm-lab.yml
```

## Notes

- The default inventory host is `192.168.2.103`.
- The default SSH user is `gomi`. Override it with `GOMI_LAB_SSH_USER` if needed.
- QEMU does not provide an exact device model for `r8169`, so the default lab treats `rtl8139` as the closest built-in Realtek case. The runner does not require an exact driver match for this case; it records the actual guest driver in the summary.
- Ubuntu image catalog entries use official Ubuntu cloud images.
