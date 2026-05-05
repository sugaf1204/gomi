#!/usr/bin/env python3
"""Remote runner for the GOMI KVM PXE lab.

This script is meant to run on the remote Linux/KVM host as root.
It prepares a bridge, starts a GOMI server VM, installs the GOMI Debian package,
registers Ubuntu OS images, installs prebuilt boot environments, powers on target VMs through a local
webhook helper, and verifies that the deployed machines become reachable over
SSH after the bare-metal style curtin flow completes.
"""

from __future__ import annotations

import argparse
import http.server
import json
import os
import re
import shlex
import shutil
import signal
import socket
import subprocess
import sys
import threading
import time
from pathlib import Path
from typing import Any
from urllib import error as urlerror
from urllib import parse as urlparse
from urllib import request as urlrequest


class LabError(RuntimeError):
    pass


def log(message: str) -> None:
    print(f"[gomi-kvm-lab] {message}", flush=True)


def read_json(path: Path, default: Any) -> Any:
    if not path.exists():
        return default
    return json.loads(path.read_text())


def write_json(path: Path, payload: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n")


def slugify(value: str) -> str:
    return re.sub(r"[^a-z0-9]+", "-", value.lower()).strip("-")


def pid_running(pid: int | None) -> bool:
    return bool(pid) and Path(f"/proc/{pid}").exists()


def tcp_port_open(host: str, port: int, timeout: float = 2.0) -> bool:
    sock = socket.socket()
    sock.settimeout(timeout)
    try:
        sock.connect((host, port))
    except OSError:
        return False
    finally:
        sock.close()
    return True


def wait_for(label: str, timeout: int, interval: int, fn) -> Any:
    deadline = time.time() + timeout
    last_exc: Exception | None = None
    while time.time() < deadline:
        try:
            value = fn()
            if value:
                return value
        except Exception as exc:  # noqa: BLE001
            last_exc = exc
        time.sleep(interval)
    if last_exc is not None:
        raise LabError(f"timed out waiting for {label}: {last_exc}") from last_exc
    raise LabError(f"timed out waiting for {label}")


def run(
    cmd: list[str],
    *,
    capture: bool = True,
    check: bool = True,
    cwd: str | None = None,
    env: dict[str, str] | None = None,
) -> subprocess.CompletedProcess[str]:
    log(f"$ {' '.join(cmd)}")
    result = subprocess.run(
        cmd,
        cwd=cwd,
        env=env,
        check=False,
        text=True,
        stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.STDOUT if capture else None,
    )
    if capture and result.stdout:
        sys.stdout.write(result.stdout)
    if check and result.returncode != 0:
        raise LabError(f"command failed ({result.returncode}): {' '.join(cmd)}")
    return result


def http_json(
    method: str,
    url: str,
    *,
    token: str | None = None,
    payload: Any | None = None,
) -> Any:
    data = None
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    if payload is not None:
        data = json.dumps(payload).encode()
    req = urlrequest.Request(url, data=data, headers=headers, method=method)
    try:
        with urlrequest.urlopen(req, timeout=20) as resp:
            raw = resp.read()
    except urlerror.HTTPError as exc:
        raw = exc.read().decode(errors="ignore")
        raise LabError(f"HTTP {method} {url} failed: {exc.code} {raw}") from exc
    if not raw:
        return None
    return json.loads(raw.decode())


class TargetPowerManager:
    def __init__(self, config: dict[str, Any], results_dir: Path) -> None:
        self.config = config
        self.work_dir = Path(config["work_dir"])
        self.runtime_dir = self.work_dir / "runtime" / "targets"
        self.results_dir = results_dir
        self.bridge_name = config["bridge_name"]
        self.supported_devices: set[str] | None = None

    def state_path(self, machine_id: str) -> Path:
        return self.runtime_dir / slugify(machine_id) / "state.json"

    def load_state(self, machine_id: str) -> dict[str, Any]:
        return read_json(self.state_path(machine_id), {})

    def save_state(self, machine_id: str, state: dict[str, Any]) -> None:
        write_json(self.state_path(machine_id), state)

    def qemu_supports(self, device_name: str) -> bool:
        if self.supported_devices is None:
            output = run(["qemu-system-x86_64", "-device", "help"]).stdout or ""
            names = set()
            for line in output.splitlines():
                line = line.strip()
                if not line.startswith("name "):
                    continue
                names.add(line.split()[1].rstrip(",").strip('"'))
            self.supported_devices = names
        return device_name in self.supported_devices

    def ensure_disk(self, path: Path, size_gb: int) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        if path.exists():
            return
        run(["qemu-img", "create", "-f", "qcow2", str(path), f"{size_gb}G"])

    def uefi_drives(self, target_runtime_dir: Path) -> list[str]:
        code_candidates = [
            Path("/usr/share/OVMF/OVMF_CODE_4M.fd"),
            Path("/usr/share/ovmf/OVMF.fd"),
            Path("/usr/share/qemu/OVMF.fd"),
        ]
        vars_candidates = [
            Path("/usr/share/OVMF/OVMF_VARS_4M.fd"),
            Path("/usr/share/OVMF/OVMF_VARS.fd"),
            Path("/usr/share/ovmf/OVMF.fd"),
            Path("/usr/share/qemu/OVMF.fd"),
        ]
        code = next((p for p in code_candidates if p.exists()), None)
        vars_template = next((p for p in vars_candidates if p.exists()), None)
        if code is None or vars_template is None:
            raise LabError("OVMF firmware is not available on this host")

        vars_path = target_runtime_dir / "OVMF_VARS.fd"
        if not vars_path.exists():
            shutil.copyfile(vars_template, vars_path)

        return [
            "-drive",
            f"if=pflash,format=raw,readonly=on,file={code}",
            "-drive",
            f"if=pflash,format=raw,file={vars_path}",
        ]

    def remove_tap(self, tap_name: str | None) -> None:
        if not tap_name:
            return
        run(["ip", "link", "del", tap_name], capture=False, check=False)

    def stop_machine(self, machine_id: str) -> dict[str, Any]:
        state = self.load_state(machine_id)
        pid = int(state.get("pid", 0) or 0)
        if pid_running(pid):
            os.kill(pid, signal.SIGTERM)
            try:
                wait_for(f"target qemu exit {machine_id}", 30, 1, lambda: not pid_running(pid))
            except LabError:
                os.kill(pid, signal.SIGKILL)
        self.remove_tap(state.get("tap"))
        state["status"] = "stopped"
        state["pid"] = 0
        self.save_state(machine_id, state)
        return state

    def status_machine(self, machine_id: str) -> dict[str, Any]:
        state = self.load_state(machine_id)
        pid = int(state.get("pid", 0) or 0)
        if pid_running(pid):
            status = "running"
        else:
            status = "stopped"
            state["pid"] = 0
            self.save_state(machine_id, state)
        return {"status": status, "state": state}

    def start_machine(self, payload: dict[str, Any]) -> dict[str, Any]:
        machine_id = payload["machineId"]
        state = self.load_state(machine_id)
        pid = int(state.get("pid", 0) or 0)
        if pid_running(pid):
            return {"status": "running", "state": state}

        nic_model = str(payload.get("nicModel") or "e1000e").strip()
        if not self.qemu_supports(nic_model):
            raise LabError(f"qemu device model {nic_model!r} is not available on this host")

        target_slug = slugify(machine_id)
        target_runtime_dir = self.runtime_dir / target_slug
        target_runtime_dir.mkdir(parents=True, exist_ok=True)
        disk_path = Path(payload.get("diskImage") or target_runtime_dir / "disk.qcow2")
        serial_log = self.results_dir / "logs" / f"{target_slug}.serial.log"
        pidfile = target_runtime_dir / "qemu.pid"
        tap_name = f"gt-{target_slug[:10]}"
        memory_mb = int(payload.get("memoryMB") or self.config["target_defaults"]["memory_mb"])
        cpus = int(payload.get("cpus") or self.config["target_defaults"]["cpus"])
        disk_gb = int(payload.get("diskSizeGB") or self.config["target_defaults"]["disk_gb"])
        firmware = str(payload.get("firmware") or self.config["target_defaults"].get("firmware") or "bios").strip().lower()

        self.ensure_disk(disk_path, disk_gb)
        serial_log.parent.mkdir(parents=True, exist_ok=True)
        serial_log.write_text("")
        self.remove_tap(tap_name)
        run(["ip", "tuntap", "add", "dev", tap_name, "mode", "tap"])
        run(["ip", "link", "set", tap_name, "master", self.bridge_name])
        run(["ip", "link", "set", tap_name, "up"])

        qemu_cmd = [
            "qemu-system-x86_64",
            "-machine",
            "q35",
            "-enable-kvm",
            "-cpu",
            "host",
            "-m",
            str(memory_mb),
            "-smp",
            str(cpus),
            "-drive",
            f"file={disk_path},if=virtio,format=qcow2",
            "-netdev",
            f"tap,id=net0,ifname={tap_name},script=no,downscript=no",
            "-device",
            f"{nic_model},netdev=net0,mac={payload['mac']}",
            "-boot",
            "order=n",
            "-display",
            "none",
            "-monitor",
            "none",
            "-serial",
            f"file:{serial_log}",
            "-daemonize",
            "-pidfile",
            str(pidfile),
        ]
        if firmware == "uefi":
            qemu_cmd[10:10] = self.uefi_drives(target_runtime_dir)
        run(qemu_cmd)
        pid = int(pidfile.read_text().strip())
        state = {
            "machineId": machine_id,
            "pid": pid,
            "tap": tap_name,
            "diskImage": str(disk_path),
            "serialLog": str(serial_log),
            "nicModel": nic_model,
            "firmware": firmware,
            "status": "running",
        }
        self.save_state(machine_id, state)
        return {"status": "running", "state": state}

    def cleanup_all(self) -> None:
        if not self.runtime_dir.exists():
            return
        for state_file in self.runtime_dir.glob("*/state.json"):
            state = read_json(state_file, {})
            machine_id = state.get("machineId")
            if machine_id:
                self.stop_machine(machine_id)


class PowerWebhookHandler(http.server.BaseHTTPRequestHandler):
    manager: TargetPowerManager

    def _read_payload(self) -> dict[str, Any]:
        raw = self.rfile.read(int(self.headers.get("Content-Length", "0") or "0"))
        if not raw:
            return {}
        return json.loads(raw.decode())

    def _write(self, status: int, payload: dict[str, Any]) -> None:
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt: str, *args) -> None:  # noqa: D401
        log(f"power-helper: {fmt % args}")

    def do_GET(self) -> None:  # noqa: N802
        parsed = urlparse.urlparse(self.path)
        if parsed.path != "/status":
            self._write(404, {"error": "not found"})
            return
        params = urlparse.parse_qs(parsed.query)
        machine_id = (params.get("machineId") or [""])[0]
        if not machine_id:
            self._write(400, {"error": "machineId is required"})
            return
        self._write(200, self.manager.status_machine(machine_id))

    def do_POST(self) -> None:  # noqa: N802
        try:
            payload = self._read_payload()
            if self.path == "/power/on":
                self._write(200, self.manager.start_machine(payload))
                return
            if self.path == "/power/off":
                self._write(200, self.manager.stop_machine(payload["machineId"]))
                return
            if self.path == "/boot-order":
                self._write(200, {"status": "accepted", "bootOrder": payload.get("bootOrder", [])})
                return
            self._write(404, {"error": "not found"})
        except Exception as exc:  # noqa: BLE001
            self._write(500, {"error": str(exc)})


class LabRunner:
    def __init__(self, config: dict[str, Any]) -> None:
        self.config = config
        self.work_dir = Path(config["work_dir"])
        self.cache_dir = self.work_dir / "cache"
        self.runtime_dir = self.work_dir / "runtime"
        self.results_dir = self.work_dir / "results"
        self.logs_dir = self.results_dir / "logs"
        self.gomi_vm_runtime = self.runtime_dir / "gomi-vm"
        self.gomi_vm_pidfile = self.gomi_vm_runtime / "qemu.pid"
        self.gomi_vm_serial_log = self.logs_dir / "gomi-vm-serial.log"
        self.gomi_tap = "gt-gomi-pxe"
        self.bridge_name = config["bridge_name"]
        self.gomi_vm_key = self.cache_dir / "gomi_vm_key"
        self.api_token = ""
        self.summary: dict[str, Any] = {"results": []}
        self.helper_server: http.server.ThreadingHTTPServer | None = None
        self.helper_thread: threading.Thread | None = None
        self.target_manager = TargetPowerManager(config, self.results_dir)

    @property
    def gomi_vm(self) -> dict[str, Any]:
        return self.config["gomi_vm"]

    def check_root(self) -> None:
        if os.geteuid() != 0:
            raise LabError("this runner must execute as root")

    def check_commands(self) -> None:
        required = [
            "cloud-localds",
            "curl",
            "ip",
            "iptables",
            "qemu-img",
            "qemu-system-x86_64",
            "scp",
            "ssh",
            "ssh-keygen",
        ]
        for name in required:
            if shutil.which(name) is None:
                raise LabError(f"required command not found: {name}")
        if not Path("/dev/kvm").exists():
            raise LabError("/dev/kvm is not available on this host")

    def prepare_dirs(self) -> None:
        for path in [self.cache_dir, self.runtime_dir, self.results_dir, self.logs_dir, self.gomi_vm_runtime]:
            path.mkdir(parents=True, exist_ok=True)

    def cleanup_runtime(self, *, remove_results: bool = False) -> None:
        self.stop_power_helper()
        self.target_manager.cleanup_all()

        if self.gomi_vm_pidfile.exists():
            pid = int(self.gomi_vm_pidfile.read_text().strip() or "0")
            if pid_running(pid):
                os.kill(pid, signal.SIGTERM)
                try:
                    wait_for("gomi vm shutdown", 30, 1, lambda: not pid_running(pid))
                except LabError:
                    os.kill(pid, signal.SIGKILL)

        run(["ip", "link", "del", self.gomi_tap], capture=False, check=False)
        run(["ip", "link", "del", self.bridge_name], capture=False, check=False)

        if self.runtime_dir.exists():
            shutil.rmtree(self.runtime_dir)
        if remove_results and self.results_dir.exists():
            shutil.rmtree(self.results_dir)

    def start_power_helper(self) -> None:
        PowerWebhookHandler.manager = self.target_manager
        address = ("0.0.0.0", int(self.config["helper_port"]))
        self.helper_server = http.server.ThreadingHTTPServer(address, PowerWebhookHandler)
        self.helper_thread = threading.Thread(target=self.helper_server.serve_forever, daemon=True)
        self.helper_thread.start()
        wait_for(
            "power helper port",
            timeout=10,
            interval=1,
            fn=lambda: tcp_port_open("127.0.0.1", int(self.config["helper_port"])),
        )
        log(f"power helper listening on {address[0]}:{address[1]}")

    def stop_power_helper(self) -> None:
        if self.helper_server is not None:
            self.helper_server.shutdown()
            self.helper_server.server_close()
            self.helper_server = None
        if self.helper_thread is not None:
            self.helper_thread.join(timeout=5)
            self.helper_thread = None

    def ensure_bridge(self) -> None:
        run(["ip", "link", "del", self.bridge_name], capture=False, check=False)
        run(["ip", "link", "add", self.bridge_name, "type", "bridge"])
        run(["ip", "link", "set", self.bridge_name, "up"])

    def ensure_ssh_key(self) -> None:
        if self.gomi_vm_key.exists():
            return
        run(["ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", str(self.gomi_vm_key)])

    def gomi_vm_ssh_base(self) -> list[str]:
        return [
            "ssh",
            "-i",
            str(self.gomi_vm_key),
            "-p",
            str(self.gomi_vm["ssh_port"]),
            "-o",
            "StrictHostKeyChecking=no",
            "-o",
            "UserKnownHostsFile=/dev/null",
            "-o",
            "LogLevel=ERROR",
            "ubuntu@127.0.0.1",
        ]

    def vm_ssh(self, command: str, *, capture: bool = True) -> subprocess.CompletedProcess[str]:
        return run(self.gomi_vm_ssh_base() + [command], capture=capture)

    def vm_scp_to(self, src: str, dest: str) -> None:
        run(
            [
                "scp",
                "-i",
                str(self.gomi_vm_key),
                "-P",
                str(self.gomi_vm["ssh_port"]),
                "-o",
                "StrictHostKeyChecking=no",
                "-o",
                "UserKnownHostsFile=/dev/null",
                "-o",
                "LogLevel=ERROR",
                src,
                f"ubuntu@127.0.0.1:{dest}",
            ]
        )

    def vm_scp_dir_to(self, src: str, dest: str) -> None:
        run(
            [
                "scp",
                "-r",
                "-i",
                str(self.gomi_vm_key),
                "-P",
                str(self.gomi_vm["ssh_port"]),
                "-o",
                "StrictHostKeyChecking=no",
                "-o",
                "UserKnownHostsFile=/dev/null",
                "-o",
                "LogLevel=ERROR",
                src,
                f"ubuntu@127.0.0.1:{dest}",
            ]
        )

    def ensure_cache_file(self, url: str, dest: Path) -> None:
        if dest.exists():
            return
        dest.parent.mkdir(parents=True, exist_ok=True)
        run(["curl", "-fL", "--retry", "3", "-o", str(dest), url], capture=False)

    def write_file(self, path: Path, content: str) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)

    def create_gomi_vm_seed(self) -> Path:
        vm_dir = self.gomi_vm_runtime
        user_data = vm_dir / "user-data"
        meta_data = vm_dir / "meta-data"
        network_config = vm_dir / "network-config"
        seed_iso = vm_dir / "seed.iso"
        ssh_key = (self.gomi_vm_key.with_suffix(".pub")).read_text().strip()

        self.write_file(
            user_data,
            "\n".join(
                [
                    "#cloud-config",
                    "ssh_authorized_keys:",
                    f"  - {ssh_key}",
                    "disable_root: false",
                    "chpasswd:",
                    "  list: |",
                    "    ubuntu:ubuntu",
                    "  expire: false",
                    "users:",
                    "  - default",
                    "",
                ]
            ),
        )
        self.write_file(
            meta_data,
            "\n".join(
                [
                    "instance-id: gomi-kvm-lab",
                    "local-hostname: gomi-lab",
                    "",
                ]
            ),
        )
        self.write_file(
            network_config,
            "\n".join(
                [
                    "version: 2",
                    "ethernets:",
                    "  mgmt0:",
                    "    match:",
                    f"      macaddress: \"{self.gomi_vm['mgmt_mac']}\"",
                    "    set-name: mgmt0",
                    "    dhcp4: true",
                    "  pxe0:",
                    "    match:",
                    f"      macaddress: \"{self.gomi_vm['pxe_mac']}\"",
                    "    set-name: pxe0",
                    f"    addresses: [{self.gomi_vm['pxe_address']}]",
                    "    dhcp4: false",
                    "",
                ]
            ),
        )
        run(["cloud-localds", "--network-config", str(network_config), str(seed_iso), str(user_data), str(meta_data)])
        return seed_iso

    def start_gomi_vm(self) -> None:
        base_url = self.gomi_vm["base_image_url"]
        base_path = self.cache_dir / Path(urlparse.urlparse(base_url).path).name
        overlay_path = self.gomi_vm_runtime / "disk.qcow2"
        seed_iso = self.create_gomi_vm_seed()
        self.ensure_cache_file(base_url, base_path)

        if overlay_path.exists():
            overlay_path.unlink()
        run(
            [
                "qemu-img",
                "create",
                "-f",
                "qcow2",
                "-F",
                "qcow2",
                "-b",
                str(base_path),
                str(overlay_path),
                f"{self.gomi_vm['disk_gb']}G",
            ]
        )

        run(["ip", "tuntap", "add", "dev", self.gomi_tap, "mode", "tap"])
        run(["ip", "link", "set", self.gomi_tap, "master", self.bridge_name])
        run(["ip", "link", "set", self.gomi_tap, "up"])

        qemu_cmd = [
            "qemu-system-x86_64",
            "-machine",
            "q35",
            "-enable-kvm",
            "-cpu",
            "host",
            "-m",
            str(self.gomi_vm["memory_mb"]),
            "-smp",
            str(self.gomi_vm["cpus"]),
            "-drive",
            f"file={overlay_path},if=virtio,format=qcow2",
            "-drive",
            f"file={seed_iso},if=virtio,format=raw",
            "-netdev",
            (
                f"user,id=mgmt0,"
                f"hostfwd=tcp:127.0.0.1:{self.gomi_vm['ssh_port']}-:22,"
                f"hostfwd=tcp:127.0.0.1:{self.gomi_vm['api_port']}-:8080"
            ),
            "-device",
            f"virtio-net-pci,netdev=mgmt0,mac={self.gomi_vm['mgmt_mac']}",
            "-netdev",
            f"tap,id=pxe0,ifname={self.gomi_tap},script=no,downscript=no",
            "-device",
            f"virtio-net-pci,netdev=pxe0,mac={self.gomi_vm['pxe_mac']}",
            "-display",
            "none",
            "-monitor",
            "none",
            "-serial",
            f"file:{self.gomi_vm_serial_log}",
            "-daemonize",
            "-pidfile",
            str(self.gomi_vm_pidfile),
        ]
        run(qemu_cmd)

    def wait_for_gomi_vm(self) -> None:
        wait_for(
            "gomi vm ssh",
            timeout=300,
            interval=5,
            fn=lambda: run(self.gomi_vm_ssh_base() + ["echo ready"], check=False).returncode == 0,
        )

    def configure_gomi_vm(self) -> None:
        self.vm_scp_to(self.config["gomi_deb_path"], "/tmp/gomi.deb")

        self.vm_ssh("sudo mkdir -p /opt/gomi-lab", capture=False)
        self.vm_scp_to(str(self.gomi_vm_key), "/tmp/gomi-target-key")
        self.vm_ssh("sudo install -m 0600 -o ubuntu -g ubuntu /tmp/gomi-target-key /opt/gomi-lab/target_key", capture=False)

        self.vm_ssh(
            (
                "sudo systemctl disable --now apt-daily.timer apt-daily-upgrade.timer "
                "apt-daily.service apt-daily-upgrade.service unattended-upgrades.service || true"
            ),
            capture=False,
        )
        self.vm_ssh(
            (
                "sudo sed -i "
                "'s#http://archive.ubuntu.com/ubuntu/#http://ftp.riken.jp/Linux/ubuntu/#g; "
                "s#http://archive.ubuntu.com/ubuntu#http://ftp.riken.jp/Linux/ubuntu#g; "
                "s#http://security.ubuntu.com/ubuntu/#http://ftp.riken.jp/Linux/ubuntu/#g; "
                "s#http://security.ubuntu.com/ubuntu#http://ftp.riken.jp/Linux/ubuntu#g' "
                "/etc/apt/sources.list /etc/apt/sources.list.d/*.sources 2>/dev/null || true"
            ),
            capture=False,
        )
        self.vm_ssh(
            "printf '%s\\n' 'Acquire::Languages \"none\";' 'Acquire::Retries \"3\";' 'Acquire::http::Timeout \"30\";' | sudo tee /etc/apt/apt.conf.d/99gomi-lab >/dev/null",
            capture=False,
        )
        self.vm_ssh("sudo apt-get update -y", capture=False)
        self.vm_ssh("sudo DEBIAN_FRONTEND=noninteractive apt-get install -y /tmp/gomi.deb", capture=False)
        self.vm_ssh("sudo systemctl disable --now gomi.service || true", capture=False)

        subnet_yaml = (
            f"cidr: {self.config['subnet']['cidr']}\n"
            f"pxeInterface: {self.config['subnet']['pxeInterface']}\n"
            f"pxeAddressRange:\n"
            f"  start: {self.config['subnet']['pxeAddressRange']['start']}\n"
            f"  end: {self.config['subnet']['pxeAddressRange']['end']}\n"
            f"defaultGateway: {self.config['subnet']['defaultGateway']}\n"
            "dnsServers:\n"
            + "".join(f"  - {item}\n" for item in self.config["subnet"]["dnsServers"])
            + f"domainName: {self.config['subnet']['domainName']}\n"
            + f"leaseTime: {self.config['subnet']['leaseTime']}\n"
        )
        self.write_file(self.gomi_vm_runtime / "subnet.yaml", subnet_yaml)
        self.vm_scp_to(str(self.gomi_vm_runtime / "subnet.yaml"), "/tmp/subnet.yaml")

        self.vm_ssh(
            (
                "sudo mkdir -p /var/lib/gomi/data/images /var/lib/gomi/data/files "
                "/var/lib/gomi/data/tftp/ubuntu "
                "/var/lib/gomi/data/cache /var/lib/gomi/data/build /opt/gomi-lab"
            ),
            capture=False,
        )
        self.vm_ssh("sudo install -m 0644 /tmp/subnet.yaml /var/lib/gomi/data/subnet.yaml", capture=False)
        self.vm_ssh("sudo ln -sfn /var/lib/gomi/data/images /var/lib/gomi/data/tftp/images", capture=False)
        self.vm_ssh("sudo chown -R gomi:gomi /var/lib/gomi/data || true", capture=False)
        self.vm_ssh(
            "bash -lc "
            + shlex.quote(
                "set -euo pipefail; "
                "PX=$(dpkg -L ipxe | grep '/undionly\\.kpxe$' | head -n1); "
                "EFI=$(dpkg -L ipxe | grep '/ipxe\\.efi$' | head -n1 || true); "
                "[ -n \"$PX\" ] && [ -f \"$PX\" ]; "
                "sudo cp \"$PX\" /var/lib/gomi/data/tftp/undionly.kpxe; "
                "if [ -n \"$EFI\" ] && [ -f \"$EFI\" ]; then "
                "sudo cp \"$EFI\" /var/lib/gomi/data/tftp/ipxe.efi; "
                "fi"
            ),
            capture=False,
        )
        self.vm_ssh("sudo sysctl -w net.ipv4.ip_forward=1 >/dev/null", capture=False)
        self.vm_ssh(
            "sudo iptables -t nat -C POSTROUTING -o mgmt0 -j MASQUERADE >/dev/null 2>&1 || sudo iptables -t nat -A POSTROUTING -o mgmt0 -j MASQUERADE",
            capture=False,
        )
        self.vm_ssh(
            "sudo iptables -C FORWARD -i pxe0 -o mgmt0 -j ACCEPT >/dev/null 2>&1 || sudo iptables -A FORWARD -i pxe0 -o mgmt0 -j ACCEPT",
            capture=False,
        )
        self.vm_ssh(
            "sudo iptables -C FORWARD -i mgmt0 -o pxe0 -m state --state RELATED,ESTABLISHED -j ACCEPT >/dev/null 2>&1 || sudo iptables -A FORWARD -i mgmt0 -o pxe0 -m state --state RELATED,ESTABLISHED -j ACCEPT",
            capture=False,
        )

        service = "\n".join(
            [
                "[Unit]",
                "Description=GOMI KVM Lab",
                "After=network-online.target",
                "",
                "[Service]",
                "Type=simple",
                "Environment=GOMI_DATA_DIR=/var/lib/gomi/data",
                "Environment=GOMI_DHCP_MODE=full",
                "Environment=GOMI_DHCP_IFACE=pxe0",
                "Environment=GOMI_TFTP_ROOT=/var/lib/gomi/data/tftp",
                "Environment=GOMI_PXE_HTTP_BASE_URL=http://10.77.0.1:8080/pxe",
                "Environment=GOMI_BOOT_HTTP_BASE_URL=http://10.77.0.1:8080",
                f"Environment=GOMI_BOOTENV_SOURCE_URL={self.config['bootenv_source_url']}",
                f"Environment=GOMI_OS_IMAGE_SOURCE_URL={self.config['os_image_source_url']}",
                "Environment=GOMI_CURTIN_UBUNTU_MIRROR=http://ftp.riken.jp/Linux/ubuntu",
                "Environment=GOMI_PXE_SERIAL_CONSOLE=1",
                "ExecStart=/usr/bin/gomi --listen=:8080 --background-sync-enabled=true",
                "Restart=always",
                "",
                "[Install]",
                "WantedBy=multi-user.target",
                "",
            ]
        )
        self.write_file(self.gomi_vm_runtime / "gomi-lab.service", service)
        self.vm_scp_to(str(self.gomi_vm_runtime / "gomi-lab.service"), "/tmp/gomi-lab.service")
        self.vm_ssh("sudo install -m 0644 /tmp/gomi-lab.service /etc/systemd/system/gomi-lab.service", capture=False)
        self.vm_ssh("sudo systemctl daemon-reload && sudo systemctl enable --now gomi-lab.service", capture=False)

        wait_for(
            "gomi api",
            timeout=180,
            interval=3,
            fn=lambda: tcp_port_open("127.0.0.1", int(self.gomi_vm["api_port"])),
        )
        wait_for(
            "gomi health",
            timeout=180,
            interval=3,
            fn=lambda: http_json("GET", f"http://127.0.0.1:{self.gomi_vm['api_port']}/healthz").get("status") == "ok",
        )

    def login_api(self) -> None:
        try:
            http_json(
                "POST",
                f"http://127.0.0.1:{self.gomi_vm['api_port']}/api/v1/setup/admin",
                payload={"username": "admin", "password": "gomi-lab-admin-password"},
            )
        except LabError as exc:
            if "409" not in str(exc):
                raise
        response = http_json(
            "POST",
            f"http://127.0.0.1:{self.gomi_vm['api_port']}/api/v1/auth/login",
            payload={"username": "admin", "password": "gomi-lab-admin-password"},
        )
        token = response.get("token", "")
        if not token:
            raise LabError("failed to obtain API token")
        self.api_token = token
        self.ensure_lab_subnet()
        self.ensure_lab_ssh_key()

    def ensure_lab_subnet(self) -> None:
        http_json(
            "PUT",
            f"http://127.0.0.1:{self.gomi_vm['api_port']}/api/v1/subnets/{urlparse.quote(self.config['subnet_name'])}",
            token=self.api_token,
            payload={
                "name": self.config["subnet_name"],
                "spec": self.config["subnet"],
            },
        )

    def ensure_lab_ssh_key(self) -> None:
        public_key = self.gomi_vm_key.with_suffix(".pub").read_text().strip()
        http_json(
            "POST",
            f"http://127.0.0.1:{self.gomi_vm['api_port']}/api/v1/ssh-keys",
            token=self.api_token,
            payload={"name": "lab-runner", "publicKey": public_key},
        )

    def ensure_os_image_registered(
        self,
        image: dict[str, Any],
        local_path: str,
        size_bytes: int,
        image_format: str,
        manifest: dict[str, Any] | None = None,
    ) -> None:
        payload = {
            "name": image["image_ref"],
            "osFamily": "ubuntu",
            "osVersion": image["os_version"],
            "arch": "amd64",
            "format": image_format,
            "source": "url",
            "url": image["cloud_image_url"],
            "sizeBytes": size_bytes,
            "description": f"Ubuntu {image['os_version']} cloud image for the GOMI KVM lab",
        }
        if manifest is not None:
            payload["manifest"] = manifest
        http_json(
            "POST",
            f"http://127.0.0.1:{self.gomi_vm['api_port']}/api/v1/os-images",
            token=self.api_token,
            payload=payload,
        )
        http_json(
            "PATCH",
            f"http://127.0.0.1:{self.gomi_vm['api_port']}/api/v1/os-images/{image['image_ref']}/status",
            token=self.api_token,
            payload={"ready": True, "localPath": local_path, "error": ""},
        )

    def prepare_vm_suite_apt(self, suite: str) -> None:
        lists_dir = f"/tmp/gomi-{suite}-lists"
        archives_dir = f"/tmp/gomi-{suite}-archives"
        preferences_path = f"/tmp/gomi-{suite}.preferences"
        sources = "\n".join(
            [
                f"deb http://archive.ubuntu.com/ubuntu {suite} main restricted universe multiverse",
                f"deb http://archive.ubuntu.com/ubuntu {suite}-updates main restricted universe multiverse",
                f"deb http://security.ubuntu.com/ubuntu {suite}-security main restricted universe multiverse",
                "",
            ]
        )
        conf = "\n".join(
            [
                f'Dir::Etc::sourcelist "/tmp/gomi-{suite}.list";',
                'Dir::Etc::sourceparts "/tmp/gomi-empty-sourceparts";',
                'Dir::Etc::main "/dev/null";',
                f'Dir::Etc::preferences "{preferences_path}";',
                'Dir::Etc::preferencesparts "/tmp/gomi-empty-preferencesparts";',
                f'Dir::State::lists "{lists_dir}";',
                f'Dir::Cache::archives "{archives_dir}";',
                'APT::Get::List-Cleanup "0";',
                "",
            ]
        )
        preferences = "\n".join(
            [
                "Package: *",
                f"Pin: release n={suite}",
                "Pin-Priority: 1001",
                "",
            ]
        )
        self.write_file(self.gomi_vm_runtime / f"{suite}.list", sources)
        self.write_file(self.gomi_vm_runtime / f"{suite}.conf", conf)
        self.write_file(self.gomi_vm_runtime / f"{suite}.preferences", preferences)
        self.vm_scp_to(str(self.gomi_vm_runtime / f"{suite}.list"), f"/tmp/gomi-{suite}.list")
        self.vm_scp_to(str(self.gomi_vm_runtime / f"{suite}.conf"), f"/tmp/gomi-{suite}.conf")
        self.vm_scp_to(str(self.gomi_vm_runtime / f"{suite}.preferences"), preferences_path)
        self.vm_ssh(
            (
                "sudo mkdir -p "
                f"/tmp/gomi-empty-sourceparts /tmp/gomi-empty-preferencesparts "
                f"{lists_dir}/partial {archives_dir}/partial"
            ),
            capture=False,
        )
        self.vm_ssh(f"sudo APT_CONFIG=/tmp/gomi-{suite}.conf apt-get update -y", capture=False)

    def prepare_os_assets(self) -> None:
        for image in self.config.get("os_images", self.config.get("ubuntu_images", [])):
            image_ref = image.get("catalog_ref", image["image_ref"])
            log(f"installing OS catalog entry {image_ref}")
            http_json(
                "POST",
                f"http://127.0.0.1:{self.gomi_vm['api_port']}/api/v1/os-catalog/{urlparse.quote(image_ref)}/install",
                token=self.api_token,
                payload={},
            )

            def catalog_ready() -> dict[str, Any] | None:
                response = http_json(
                    "GET",
                    f"http://127.0.0.1:{self.gomi_vm['api_port']}/api/v1/os-catalog",
                    token=self.api_token,
                )
                for item in response.get("items", []):
                    entry = item.get("entry", {})
                    if entry.get("name") != image_ref:
                        continue
                    bootenv = item.get("bootEnvironment", {})
                    if item.get("osImageError"):
                        raise LabError(f"OS image install failed for {image_ref}: {item['osImageError']}")
                    if bootenv.get("phase") == "error":
                        raise LabError(f"boot environment build failed for {image_ref}: {bootenv.get('message', '')}")
                    if item.get("osImageReady") and bootenv.get("phase") == "ready":
                        return item
                    return None
                raise LabError(f"OS catalog entry not found: {image_ref}")

            wait_for(f"os catalog install {image_ref}", timeout=3600, interval=10, fn=catalog_ready)

    def activate_os_assets(self, image_ref: str) -> None:
        self.vm_ssh("sudo test -s /var/lib/gomi/data/files/linux/boot-kernel", capture=False)
        self.vm_ssh("sudo test -s /var/lib/gomi/data/files/linux/boot-initrd", capture=False)
        self.vm_ssh("sudo test -s /var/lib/gomi/data/files/linux/rootfs.squashfs", capture=False)

    def api_machine(self, name: str) -> dict[str, Any] | None:
        try:
            return http_json(
                "GET",
                f"http://127.0.0.1:{self.gomi_vm['api_port']}/api/v1/machines/{name}",
                token=self.api_token,
            )
        except LabError as exc:
            if "404" in str(exc):
                return None
            raise

    def delete_machine_if_exists(self, name: str) -> None:
        machine = self.api_machine(name)
        if machine is None:
            return
        http_json(
            "DELETE",
            f"http://127.0.0.1:{self.gomi_vm['api_port']}/api/v1/machines/{name}",
            token=self.api_token,
        )

    def create_machine(self, name: str, mac: str, image: dict[str, Any], nic: dict[str, Any], case_index: int) -> None:
        image_ref = image["image_ref"]
        firmware = str(image.get("firmware") or self.config["target_defaults"]["firmware"])
        disk_path = f"{self.runtime_dir}/targets/{slugify(name)}/disk.qcow2"
        payload = {
            "name": name,
            "hostname": name,
            "mac": mac,
            "arch": self.config["target_defaults"]["arch"],
            "firmware": firmware,
            "power": {
                "type": "webhook",
                "webhook": {
                    "powerOnURL": f"http://10.0.2.2:{self.config['helper_port']}/power/on",
                    "powerOffURL": f"http://10.0.2.2:{self.config['helper_port']}/power/off",
                    "statusURL": f"http://10.0.2.2:{self.config['helper_port']}/status?machineId={urlparse.quote(name)}",
                    "bootOrderURL": f"http://10.0.2.2:{self.config['helper_port']}/boot-order",
                    "bodyExtras": {
                        "nicLabel": nic["name"],
                        "nicModel": nic["qemu_model"],
                        "diskImage": disk_path,
                        "memoryMB": str(self.config["target_defaults"]["memory_mb"]),
                        "cpus": str(self.config["target_defaults"]["cpus"]),
                        "diskSizeGB": str(self.config["target_defaults"]["disk_gb"]),
                        "caseIndex": str(case_index),
                        "firmware": firmware,
                    },
                },
            },
            "subnetRef": self.config["subnet_name"],
            "ipAssignment": "dhcp",
            "network": {"domain": self.config["domain"]},
            "sshKeyRefs": ["lab-runner"],
            "osPreset": {
                "family": image.get("os_family", "ubuntu"),
                "version": image["os_version"],
                "imageRef": image_ref,
            },
        }
        login_user = str(image.get("login_user") or "").strip()
        if login_user:
            payload["loginUser"] = {"username": login_user}
        http_json(
            "POST",
            f"http://127.0.0.1:{self.gomi_vm['api_port']}/api/v1/machines",
            token=self.api_token,
            payload=payload,
        )

    def power_on_machine(self, name: str) -> None:
        http_json(
            "POST",
            f"http://127.0.0.1:{self.gomi_vm['api_port']}/api/v1/machines/{name}/actions/power-on",
            token=self.api_token,
            payload={},
        )

    def power_off_machine(self, name: str) -> None:
        http_json(
            "POST",
            f"http://127.0.0.1:{self.gomi_vm['api_port']}/api/v1/machines/{name}/actions/power-off",
            token=self.api_token,
            payload={},
        )

    def wait_for_machine_ready(self, name: str) -> dict[str, Any]:
        def probe() -> dict[str, Any] | None:
            machine = self.api_machine(name)
            if machine and machine.get("phase") == "Error":
                raise LabError(f"machine {name} entered Error phase: {machine.get('lastError', '')}")
            if machine and machine.get("phase") == "Ready" and machine.get("ip"):
                return machine
            return None

        return wait_for(f"machine ready {name}", timeout=1800, interval=5, fn=probe)

    def verify_target(self, ip_addr: str, login_user: str) -> dict[str, str]:
        guest_cmd = (
            "set -euo pipefail; "
            "iface=$(ls /sys/class/net | grep -v '^lo$' | head -n1); "
            "driver=$(basename \"$(readlink -f /sys/class/net/${iface}/device/driver)\"); "
            ". /etc/os-release; "
            "printf 'VERSION_ID=%s\\nDRIVER=%s\\nIFACE=%s\\n' \"$VERSION_ID\" \"$driver\" \"$iface\""
        )
        ssh_cmd = (
            "ssh -i /opt/gomi-lab/target_key -o BatchMode=yes "
            "-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR "
            f"{login_user}@{ip_addr} "
            + shlex.quote(f"bash -lc {shlex.quote(guest_cmd)}")
        )
        output = self.vm_ssh(ssh_cmd).stdout.strip().splitlines()
        result: dict[str, str] = {}
        for line in output:
            if "=" not in line:
                continue
            key, value = line.split("=", 1)
            result[key] = value
        if "VERSION_ID" not in result:
            raise LabError(f"target verification returned unexpected output: {output}")
        return result

    def run_case(self, image: dict[str, Any], nic: dict[str, Any], case_index: int) -> dict[str, Any]:
        name = f"lab-{slugify(image['image_ref'])}-{nic['name']}"
        login_user = str(image.get("login_user") or "ubuntu")
        mac = f"52:54:00:77:{case_index // 256:02x}:{case_index % 256:02x}"
        if not self.target_manager.qemu_supports(str(nic["qemu_model"])):
            return {
                "name": name,
                "mac": mac,
                "status": "unsupported",
                "phase": "",
                "ip": "",
                "requested_os_version": image["os_version"],
                "actual_os_version": "",
                "requested_nic": nic["name"],
                "qemu_model": nic["qemu_model"],
                "actual_driver": "",
                "iface": "",
                "notes": [
                    f"qemu device model {nic['qemu_model']!r} is not available on this host",
                ],
            }
        self.activate_os_assets(image["image_ref"])
        self.delete_machine_if_exists(name)
        self.target_manager.stop_machine(name)
        self.create_machine(name, mac, image, nic, case_index)
        self.power_on_machine(name)

        machine = self.wait_for_machine_ready(name)
        verification = wait_for(
            f"target ssh {name}",
            timeout=900,
            interval=10,
            fn=lambda: self.verify_target(machine["ip"], login_user),
        )

        status = "passed"
        notes: list[str] = []
        if verification.get("VERSION_ID") != image["os_version"]:
            status = "failed"
            notes.append(
                f"expected VERSION_ID={image['os_version']}, got {verification.get('VERSION_ID', '')}"
            )
        if nic.get("exact") and nic.get("expected_driver") and verification.get("DRIVER") != nic["expected_driver"]:
            status = "failed"
            notes.append(
                f"expected driver={nic['expected_driver']}, got {verification.get('DRIVER', '')}"
            )
        elif not nic.get("exact"):
            notes.append(
                nic.get(
                    "note",
                    "driver is recorded for inspection; exact driver match is not enforced for this case",
                )
            )

        result = {
            "name": name,
            "mac": mac,
            "status": status,
            "phase": machine.get("phase"),
            "ip": machine.get("ip", ""),
            "requested_os_version": image["os_version"],
            "actual_os_version": verification.get("VERSION_ID", ""),
            "requested_nic": nic["name"],
            "qemu_model": nic["qemu_model"],
            "actual_driver": verification.get("DRIVER", ""),
            "iface": verification.get("IFACE", ""),
            "notes": notes,
        }
        try:
            self.power_off_machine(name)
        except Exception as exc:  # noqa: BLE001
            result.setdefault("notes", []).append(f"power-off failed: {exc}")
        return result

    def write_summary(self) -> None:
        payload = {
            "generatedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
            "workDir": str(self.work_dir),
            "results": self.summary["results"],
        }
        write_json(self.results_dir / "summary.json", payload)

    def run_matrix(self) -> None:
        case_index = 1
        for image in self.config.get("os_images", self.config.get("ubuntu_images", [])):
            for nic in self.config["target_nics"]:
                log(f"running case os={image['image_ref']} nic={nic['name']}")
                try:
                    result = self.run_case(image, nic, case_index)
                except Exception as exc:  # noqa: BLE001
                    result = {
                        "name": f"lab-{image['os_version'].replace('.', '')}-{nic['name']}",
                        "requested_os_version": image["os_version"],
                        "requested_nic": nic["name"],
                        "qemu_model": nic["qemu_model"],
                        "status": "failed",
                        "notes": [str(exc)],
                    }
                self.summary["results"].append(result)
                self.write_summary()
                case_index += 1

    def run(self) -> None:
        self.check_root()
        self.check_commands()
        self.cleanup_runtime(remove_results=True)
        self.prepare_dirs()
        self.ensure_bridge()
        self.ensure_ssh_key()
        self.start_power_helper()
        self.start_gomi_vm()
        self.wait_for_gomi_vm()
        self.configure_gomi_vm()
        self.login_api()
        self.prepare_os_assets()
        self.run_matrix()
        self.write_summary()
        if self.config.get("cleanup_on_success"):
            self.cleanup_runtime(remove_results=False)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", required=True)
    parser.add_argument("--cleanup", action="store_true")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    config = json.loads(Path(args.config).read_text())
    runner = LabRunner(config)
    try:
        if args.cleanup:
            runner.cleanup_runtime(remove_results=False)
            return 0
        runner.run()
        return 0
    except Exception as exc:  # noqa: BLE001
        log(f"ERROR: {exc}")
        runner.write_summary()
        return 1
    finally:
        runner.stop_power_helper()


if __name__ == "__main__":
    raise SystemExit(main())
