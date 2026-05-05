#!/usr/bin/env bash
set -euo pipefail

WORK_DIR="/tmp/gomi-pxe-full-$(date +%Y%m%d%H%M%S)"
STATUS_FILE="$WORK_DIR/status"
RUN_LOG="$WORK_DIR/run.log"
INSTALL_LOG="$WORK_DIR/install-serial.log"
BOOT_LOG="$WORK_DIR/boot-serial.log"
DISK_IMG="$WORK_DIR/full-install.qcow2"
MAC="52:54:00:aa:20:01"
INSTALL_TIMEOUT=5400
BOOT_TIMEOUT=300

mkdir -p "$WORK_DIR"
echo "$WORK_DIR" > /tmp/gomi-pxe-full-last-dir
echo "RUNNING" > "$STATUS_FILE"
exec > >(tee -a "$RUN_LOG") 2>&1

echo "[full-verify] work_dir=$WORK_DIR"
echo "[full-verify] mac=$MAC"

if ! sudo -n true >/dev/null 2>&1; then
  echo "[full-verify] ERROR: passwordless sudo is required"
  echo "FAIL" > "$STATUS_FILE"
  exit 1
fi

sudo -n DEBIAN_FRONTEND=noninteractive apt-get install -y qemu-system-x86 qemu-utils >/dev/null

TAP="tfi$(printf '%05d' $((RANDOM%100000)))"
cleanup_tap() {
  sudo -n ip link del "$TAP" >/dev/null 2>&1 || true
}

qemu-img create -f qcow2 "$DISK_IMG" 24G >/dev/null

echo "[full-verify] setup tap $TAP on br-eth0"
sudo -n ip tuntap add dev "$TAP" mode tap user "$(id -un)"
sudo -n ip link set "$TAP" master br-eth0
sudo -n ip link set "$TAP" up

echo "[full-verify] start PXE install (BIOS)"
set +e
sudo -n timeout --foreground "$INSTALL_TIMEOUT" qemu-system-x86_64 \
  -enable-kvm -cpu host \
  -m 3072 -smp 2 \
  -drive "file=$DISK_IMG,if=virtio,format=qcow2" \
  -netdev "tap,id=net0,ifname=$TAP,script=no,downscript=no" \
  -device "e1000,netdev=net0,mac=$MAC" \
  -boot order=n \
  -no-reboot \
  -nographic -monitor none \
  -serial "file:$INSTALL_LOG"
INSTALL_RC=$?
set -e

cleanup_tap

echo "[full-verify] install rc=$INSTALL_RC"
if [[ "$INSTALL_RC" -ne 0 ]]; then
  if [[ "$INSTALL_RC" -eq 124 ]]; then
    echo "[full-verify] ERROR: install timeout (${INSTALL_TIMEOUT}s)"
  else
    echo "[full-verify] ERROR: install failed rc=$INSTALL_RC"
  fi
  tail -n 120 "$INSTALL_LOG" || true
  echo "FAIL" > "$STATUS_FILE"
  exit 1
fi

echo "[full-verify] boot installed disk"
set +e
sudo -n timeout --foreground "$BOOT_TIMEOUT" qemu-system-x86_64 \
  -enable-kvm -cpu host \
  -m 2048 -smp 2 \
  -drive "file=$DISK_IMG,if=virtio,format=qcow2" \
  -netdev user,id=net0 \
  -device "e1000,netdev=net0,mac=$MAC" \
  -boot order=c \
  -nographic -monitor none \
  -serial "file:$BOOT_LOG"
BOOT_RC=$?
set -e

echo "[full-verify] boot rc=$BOOT_RC"
if [[ "$BOOT_RC" -ne 0 && "$BOOT_RC" -ne 124 ]]; then
  echo "[full-verify] ERROR: boot verify failed rc=$BOOT_RC"
  tail -n 120 "$BOOT_LOG" || true
  echo "FAIL" > "$STATUS_FILE"
  exit 1
fi

if ! grep -Eq 'login:|Debian GNU/Linux' "$BOOT_LOG"; then
  echo "[full-verify] ERROR: boot log does not contain login marker"
  tail -n 120 "$BOOT_LOG" || true
  echo "FAIL" > "$STATUS_FILE"
  exit 1
fi

echo "SUCCESS" > "$STATUS_FILE"
echo "[full-verify] SUCCESS"
echo "[full-verify] install_log=$INSTALL_LOG"
echo "[full-verify] boot_log=$BOOT_LOG"
echo "[full-verify] disk=$DISK_IMG"
