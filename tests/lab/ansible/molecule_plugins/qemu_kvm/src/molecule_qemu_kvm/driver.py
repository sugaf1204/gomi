"""Molecule driver for the GOMI QEMU/KVM PXE lab."""

from __future__ import annotations

from pathlib import Path
from typing import TYPE_CHECKING, Any

from molecule.api import Driver

if TYPE_CHECKING:
    from molecule.config import Config


class QemuKvm(Driver):
    """Minimal QEMU/KVM driver.

    This driver keeps lifecycle work in Ansible playbooks. That matches the
    current Molecule model while giving the scenario an explicit QEMU/KVM
    driver identity and login/connection behavior.
    """

    title = "GOMI QEMU/KVM lab driver"

    def __init__(self, config: Config) -> None:
        super().__init__(config)
        self._name = "molecule-qemu-kvm"

    @property
    def name(self) -> str:
        return self._name

    @name.setter
    def name(self, value: str) -> None:
        self._name = value

    @property
    def login_cmd_template(self) -> str:
        return self.options.get("login_cmd_template", "")

    @property
    def default_safe_files(self) -> list[str]:
        return []

    @property
    def default_ssh_connection_options(self) -> list[str]:
        return [
            "-o UserKnownHostsFile=/dev/null",
            "-o StrictHostKeyChecking=no",
            "-o ControlMaster=no",
            "-o ControlPath=none",
            "-o LogLevel=ERROR",
        ]

    def login_options(self, instance_name: str) -> dict[str, str]:
        return {"instance": instance_name}

    def ansible_connection_options(self, instance_name: str) -> dict[str, Any]:
        return self.options.get("ansible_connection_options", {})

    def sanity_checks(self) -> None:
        # QEMU/KVM runs on the remote lab host, not necessarily on the local
        # Molecule controller. The remote runner performs the hard checks.
        return

    def get_playbook(self, step: str) -> str | None:
        playbook = Path(self._path, "playbooks", step + ".yml")
        if playbook.is_file():
            return str(playbook)
        return None

    def schema_file(self) -> str | None:
        return str(Path(self._path, "schema.json"))
