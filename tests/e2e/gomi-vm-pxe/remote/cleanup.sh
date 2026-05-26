#!/usr/bin/env bash

cleanup_on_error() {
  local rc=$?

  if [[ $rc -ne 0 ]]; then
    local vm_pid

    echo "[remote-lab] failed with rc=$rc"
    echo "[remote-lab] logs dir: $LOG_DIR"
    if [[ -f "$GOMI_VM_PIDFILE" ]]; then
      echo "[remote-lab] stopping gomi vm"
      vm_pid="$(sudo -n cat "$GOMI_VM_PIDFILE" 2>/dev/null || true)"
      if [[ -n "$vm_pid" ]]; then
        sudo -n kill "$vm_pid" >/dev/null 2>&1 || true
      fi
    fi

    sudo -n ip link del "$TAP_G" >/dev/null 2>&1 || true
    sudo -n ip link del "$TAP_T" >/dev/null 2>&1 || true
    sudo -n ip link del "$BR" type bridge >/dev/null 2>&1 || true
    if [[ -f "$HOST_HTTP_PIDFILE" ]]; then
      kill "$(cat "$HOST_HTTP_PIDFILE")" >/dev/null 2>&1 || true
    fi
  fi

  return "$rc"
}

install_failure_cleanup() {
  trap cleanup_on_error EXIT
}
