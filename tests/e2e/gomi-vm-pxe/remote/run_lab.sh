#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)

source "$SCRIPT_DIR/cleanup.sh"
source "$SCRIPT_DIR/setup.sh"
source "$SCRIPT_DIR/execute.sh"

LAB_DIR="${1:?usage: run_lab.sh LAB_DIR GOMI_BIN}"
GOMI_BIN_IN="${2:?usage: run_lab.sh LAB_DIR GOMI_BIN}"

choose_port() {
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

init_lab_env() {
  mkdir -p "$LAB_DIR"
  LOG_DIR="$LAB_DIR/logs"
  mkdir -p "$LOG_DIR"

  HOST_TFTP_DIR="$LAB_DIR/host-tftp"
  HOST_HTTP_DIR="$LAB_DIR/host-http"
  mkdir -p "$HOST_TFTP_DIR/pxelinux.cfg" "$HOST_TFTP_DIR/debian" "$HOST_HTTP_DIR"

  REMOTE_USER=$(id -un)
  MGMT_PORT=$(choose_port)
  LAB_ID=$(date +%M%S)

  BR="brg${LAB_ID}"
  TAP_G="tpg${LAB_ID}"
  TAP_T="tpt${LAB_ID}"
  GOMI_VM_DISK="$LAB_DIR/gomi-vm.qcow2"
  GOMI_VM_SEED="$LAB_DIR/gomi-seed.iso"
  BASE_IMG="/tmp/gomi-base-noble-server-cloudimg-amd64.img"
  GOMI_VM_PIDFILE="$LAB_DIR/gomi-vm.pid"
  GOMI_VM_SERIAL="$LOG_DIR/gomi-vm-serial.log"
  TEST_VM_DISK="$LAB_DIR/test-vm.qcow2"
  TEST_INSTALL_LOG="$LOG_DIR/test-vm-install.log"
  TEST_BOOT_LOG="$LOG_DIR/test-vm-boot.log"
  HOST_HTTP_LOG="$LOG_DIR/host-http.log"
  HOST_HTTP_PIDFILE="$LAB_DIR/host-http.pid"
  HOST_HTTP_PORT=18088
  TEST_MAC="52:54:00:bb:00:10"
  GOMI_MAC_MGMT="52:54:00:aa:00:01"
  GOMI_MAC_PXE="52:54:00:aa:00:02"
  GOMI_VM_KEY="$LAB_DIR/gomi_vm_key"

  TOKEN=""
  NET_PROFILE=""
  POWER_POLICY=""
  MACHINE_NAME=""
  JOB_NAME=""
}

vm_ssh() {
  ssh -i "$GOMI_VM_KEY" \
    -p "$MGMT_PORT" \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null \
    ubuntu@127.0.0.1 "$@"
}

vm_scp() {
  scp -i "$GOMI_VM_KEY" \
    -P "$MGMT_PORT" \
    -o StrictHostKeyChecking=no \
    -o UserKnownHostsFile=/dev/null "$@"
}

init_lab_env
install_failure_cleanup

ensure_passwordless_sudo
install_host_dependencies
prepare_gomi_vm_access
prepare_gomi_vm_cloud_init
create_gomi_vm_disk
setup_lab_network
start_gomi_vm
wait_for_gomi_vm_ssh
install_gomi_binary_in_vm
configure_gomi_vm_services
bootstrap_gomi_api

prepare_test_pxe_assets
run_test_vm_install
verify_test_vm_boot
print_success_summary

echo "[remote-lab] note: gomi vm and lab network are left running for inspection"
trap - EXIT
