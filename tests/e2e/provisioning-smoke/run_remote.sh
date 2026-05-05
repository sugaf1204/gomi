#!/usr/bin/env bash
set -euo pipefail

HOST="${1:-dev.vm.canvm.jp}"
USER_NAME="${2:-ubuntu}"
ROOT_DIR=$(cd "$(dirname "$0")/../../.." && pwd)
STAMP=$(date +%Y%m%d%H%M%S)
REMOTE_DIR="/tmp/gomi-e2e-$STAMP"
ARCHIVE_NAME="gomi-e2e-$STAMP.tar.gz"
LOCAL_ARCHIVE="/tmp/$ARCHIVE_NAME"
LOCAL_BIN="/tmp/gomi-e2e-$STAMP-bin"

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing command: $1" >&2
    exit 1
  fi
}

need_cmd ssh
need_cmd scp
need_cmd tar
need_cmd go

echo "[remote] detecting remote architecture"
REMOTE_UNAME=$(ssh "$USER_NAME@$HOST" "uname -m")
case "$REMOTE_UNAME" in
  x86_64|amd64)
    TARGET_GOARCH="amd64"
    ;;
  aarch64|arm64)
    TARGET_GOARCH="arm64"
    ;;
  *)
    echo "unsupported remote arch: $REMOTE_UNAME" >&2
    exit 1
    ;;
esac

echo "[remote] creating source archive: $LOCAL_ARCHIVE"
tar -C "$ROOT_DIR" \
  --exclude='web/node_modules' \
  --exclude='web/dist' \
  --exclude='data' \
  -czf "$LOCAL_ARCHIVE" .

echo "[remote] building gomi binary (linux/$TARGET_GOARCH)"
(
  cd "$ROOT_DIR"
  CGO_ENABLED=0 GOOS=linux GOARCH="$TARGET_GOARCH" GOCACHE=/tmp/gomi-go-build-cache go build -o "$LOCAL_BIN" ./cmd/gomi
)

echo "[remote] preparing remote directory: $USER_NAME@$HOST:$REMOTE_DIR"
ssh "$USER_NAME@$HOST" "mkdir -p '$REMOTE_DIR'"

echo "[remote] uploading archive"
scp "$LOCAL_ARCHIVE" "$USER_NAME@$HOST:$REMOTE_DIR/"
echo "[remote] uploading gomi binary"
scp "$LOCAL_BIN" "$USER_NAME@$HOST:$REMOTE_DIR/gomi"

echo "[remote] running smoke test on remote host"
ssh "$USER_NAME@$HOST" "
  set -euo pipefail
  cd '$REMOTE_DIR'
  tar -xzf '$ARCHIVE_NAME'
  chmod +x '$REMOTE_DIR/gomi'
  chmod +x tests/e2e/provisioning-smoke/run_smoke.sh
  GOMI_TEST_GOMI_BIN='$REMOTE_DIR/gomi' ./tests/e2e/provisioning-smoke/run_smoke.sh
"

echo "[remote] smoke test completed"
echo "[remote] cleanup command (optional): ssh $USER_NAME@$HOST 'rm -rf $REMOTE_DIR'"
