#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
ROOT_DIR=$(cd "$SCRIPT_DIR/../../.." && pwd)

source "$SCRIPT_DIR/lib/local_transfer.sh"

HOST="${1:-dev.vm.canvm.jp}"
USER_NAME="${2:-ubuntu}"
STAMP=$(date +%Y%m%d%H%M%S)
REMOTE_DIR="/tmp/gomi-vm-lab-$STAMP"
LOCAL_BIN="/tmp/gomi-vm-lab-bin-$STAMP"

cleanup_local() {
  rm -f "$LOCAL_BIN"
}
trap cleanup_local EXIT

need_cmd ssh
need_cmd scp
need_cmd go

TARGET_GOARCH=$(detect_remote_goarch "$USER_NAME" "$HOST")
build_gomi_binary "$ROOT_DIR" "$TARGET_GOARCH" "$LOCAL_BIN"
prepare_remote_lab_dir "$USER_NAME" "$HOST" "$REMOTE_DIR"
upload_remote_lab_artifacts "$USER_NAME" "$HOST" "$REMOTE_DIR" "$LOCAL_BIN" "$SCRIPT_DIR/remote"
execute_remote_lab "$USER_NAME" "$HOST" "$REMOTE_DIR"

echo "[orchestrator] done"
