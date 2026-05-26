#!/usr/bin/env bash

write_test_preseed() {
  vm_ssh "curl -fsS 'http://127.0.0.1:8080/api/v1/jobs/$JOB_NAME?namespace=default' -H 'Authorization: Bearer $TOKEN' | jq -r '.status.artifacts[\"installConfig.inline\"]'" > "$HOST_HTTP_DIR/preseed.cfg"
  cat >> "$HOST_HTTP_DIR/preseed.cfg" <<'PRESEED_EXTRA'
d-i netcfg/disable_autoconfig boolean true
d-i mirror/country string manual
d-i mirror/http/hostname string deb.debian.org
d-i mirror/http/directory string /debian
d-i mirror/http/proxy string
d-i apt-setup/insecure-repositories boolean true
d-i debian-installer/allow_unauthenticated boolean true
d-i partman-auto/disk string /dev/vda
d-i partman-auto/method string regular
d-i partman-lvm/device_remove_lvm boolean true
d-i partman-md/device_remove_md boolean true
d-i partman-auto/choose_recipe select atomic
d-i partman-partitioning/confirm_write_new_label boolean true
d-i partman/choose_partition select finish
d-i partman/confirm boolean true
d-i partman/confirm_nooverwrite boolean true
d-i partman-lvm/confirm boolean true
d-i partman-lvm/confirm_nooverwrite boolean true
d-i grub-installer/bootdev string /dev/vda
d-i grub-installer/with_other_os boolean true
d-i cdrom-detect/eject boolean false
PRESEED_EXTRA
}

copy_pxe_artifacts_from_gomi_vm() {
  vm_scp "ubuntu@127.0.0.1:/srv/tftp/pxelinux.0" "$HOST_TFTP_DIR/pxelinux.0"
  vm_scp "ubuntu@127.0.0.1:/srv/tftp/ldlinux.c32" "$HOST_TFTP_DIR/ldlinux.c32"
  vm_scp "ubuntu@127.0.0.1:/srv/tftp/debian/linux" "$HOST_TFTP_DIR/debian/linux"
  vm_scp "ubuntu@127.0.0.1:/srv/tftp/debian/initrd.gz" "$HOST_TFTP_DIR/debian/initrd.gz"
}

write_pxelinux_config() {
  cat > "$HOST_TFTP_DIR/pxelinux.cfg/default" <<PXECFG
DEFAULT install
PROMPT 0
TIMEOUT 50

LABEL install
  KERNEL debian/linux
  INITRD debian/initrd.gz
  APPEND auto=true priority=critical url=http://10.0.2.2:${HOST_HTTP_PORT}/preseed.cfg console=ttyS0,115200n8 ipv6.disable=1 ---
PXECFG
}

start_host_preseed_server() {
  sudo -n pkill -f "^python3 -m http.server ${HOST_HTTP_PORT}( |$)" >/dev/null 2>&1 || true
  nohup python3 -m http.server "$HOST_HTTP_PORT" --bind 0.0.0.0 --directory "$HOST_HTTP_DIR" >"$HOST_HTTP_LOG" 2>&1 &
  echo $! > "$HOST_HTTP_PIDFILE"
}

prepare_test_pxe_assets() {
  write_test_preseed
  copy_pxe_artifacts_from_gomi_vm
  write_pxelinux_config
  start_host_preseed_server
}

run_test_vm_install() {
  local install_rc

  echo "[remote-lab] create and PXE install test vm"
  qemu-img create -f qcow2 "$TEST_VM_DISK" 16G >/dev/null

  set +e
  timeout --foreground 3600 sudo -n qemu-system-x86_64 \
    -enable-kvm -cpu host \
    -m 3072 -smp 2 \
    -drive "file=$TEST_VM_DISK,if=virtio,format=qcow2" \
    -netdev "user,id=net0,tftp=$HOST_TFTP_DIR,bootfile=pxelinux.0" \
    -device "e1000,netdev=net0,mac=$TEST_MAC" \
    -boot order=n \
    -no-reboot \
    -nographic -monitor none \
    -serial "file:$TEST_INSTALL_LOG"
  install_rc=$?
  set -e

  if [[ "$install_rc" -ne 0 ]]; then
    if [[ "$install_rc" -eq 124 ]]; then
      echo "ERROR: test vm install timeout" >&2
    else
      echo "ERROR: test vm install failed rc=$install_rc" >&2
    fi
    tail -n 120 "$TEST_INSTALL_LOG" >&2 || true
    exit 1
  fi
}

verify_test_vm_boot() {
  local boot_rc

  echo "[remote-lab] boot installed test vm"
  set +e
  timeout --foreground 240 sudo -n qemu-system-x86_64 \
    -enable-kvm -cpu host \
    -m 2048 -smp 2 \
    -drive "file=$TEST_VM_DISK,if=virtio,format=qcow2" \
    -netdev user,id=net0 \
    -device "e1000,netdev=net0,mac=$TEST_MAC" \
    -boot order=c \
    -nographic -monitor none \
    -serial "file:$TEST_BOOT_LOG"
  boot_rc=$?
  set -e

  if [[ "$boot_rc" -ne 0 && "$boot_rc" -ne 124 ]]; then
    echo "ERROR: test vm boot verify failed rc=$boot_rc" >&2
    tail -n 120 "$TEST_BOOT_LOG" >&2 || true
    exit 1
  fi

  if ! grep -Eq 'login:|Debian GNU/Linux' "$TEST_BOOT_LOG"; then
    echo "ERROR: boot log does not show login/debian marker" >&2
    tail -n 120 "$TEST_BOOT_LOG" >&2 || true
    exit 1
  fi
}

print_success_summary() {
  echo "[remote-lab] SUCCESS"
  echo "lab_dir=$LAB_DIR"
  echo "bridge=$BR tap_gomi=$TAP_G tap_test=$TAP_T"
  echo "gomi_vm_ssh_port_on_host=$MGMT_PORT"
  echo "gomi_job=$JOB_NAME machine=$MACHINE_NAME"
  echo "gomi_vm_serial=$GOMI_VM_SERIAL"
  echo "test_install_log=$TEST_INSTALL_LOG"
  echo "test_boot_log=$TEST_BOOT_LOG"
  echo "gomi_vm_access: ssh -i $GOMI_VM_KEY -p $MGMT_PORT ubuntu@127.0.0.1"
}
