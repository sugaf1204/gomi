#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/../../.." && pwd)
TMP_DIR=$(mktemp -d)
API_PORT="${GOMI_TEST_API_PORT:-18080}"
WEBHOOK_PORT="${GOMI_TEST_WEBHOOK_PORT:-19090}"
NAMESPACE="${GOMI_TEST_NAMESPACE:-default}"
API_BASE="http://127.0.0.1:${API_PORT}"
GO_BUILD_CACHE="${GOMI_TEST_GOCACHE:-/tmp/gomi-go-build-cache}"
GOMI_BIN="${GOMI_TEST_GOMI_BIN:-}"

SERVER_PID=""
WEBHOOK_PID=""

cleanup() {
  if [[ -n "$SERVER_PID" ]]; then
    kill "$SERVER_PID" >/dev/null 2>&1 || true
    wait "$SERVER_PID" >/dev/null 2>&1 || true
  fi
  if [[ -n "$WEBHOOK_PID" ]]; then
    kill "$WEBHOOK_PID" >/dev/null 2>&1 || true
    wait "$WEBHOOK_PID" >/dev/null 2>&1 || true
  fi
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing command: $1" >&2
    exit 1
  fi
}

need_cmd curl
need_cmd jq
need_cmd python3
if [[ -z "$GOMI_BIN" ]]; then
  need_cmd go
fi

echo "[smoke] temporary dir: $TMP_DIR"
mkdir -p "$GO_BUILD_CACHE"
if [[ -n "${GOMI_TEST_GOMODCACHE:-}" ]]; then
  mkdir -p "$GOMI_TEST_GOMODCACHE"
fi

echo "[smoke] starting webhook mock"
python3 "$ROOT_DIR/tests/e2e/provisioning-smoke/mock_power_server.py" \
  --listen 127.0.0.1 \
  --port "$WEBHOOK_PORT" \
  --log-file "$TMP_DIR/webhook.log" >"$TMP_DIR/webhook.out" 2>&1 &
WEBHOOK_PID=$!

sleep 1

echo "[smoke] starting gomi server"
if [[ -n "$GOMI_BIN" ]]; then
  (
    GOMI_EMBEDDED_ETCD=false \
    "$GOMI_BIN" \
      --mode=server \
      --runtime=standalone \
      --listen=":$API_PORT" \
      --controller-enabled=true \
      --namespace="$NAMESPACE" \
      --boot-http-base-url="http://127.0.0.1:8088" \
      >"$TMP_DIR/gomi.out" 2>&1
  ) &
else
  (
    cd "$ROOT_DIR"
    if [[ -n "${GOMI_TEST_GOMODCACHE:-}" ]]; then
      export GOMODCACHE="$GOMI_TEST_GOMODCACHE"
    fi
    GOMI_EMBEDDED_ETCD=false \
    GOCACHE="$GO_BUILD_CACHE" \
    go run ./cmd/gomi \
      --mode=server \
      --runtime=standalone \
      --listen=":$API_PORT" \
      --controller-enabled=true \
      --namespace="$NAMESPACE" \
      --boot-http-base-url="http://127.0.0.1:8088" \
      >"$TMP_DIR/gomi.out" 2>&1
  ) &
fi
SERVER_PID=$!

for i in {1..40}; do
  if curl -fsS "$API_BASE/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
  if [[ "$i" -eq 40 ]]; then
    echo "[smoke] gomi server failed to start" >&2
    sed -n '1,200p' "$TMP_DIR/gomi.out" >&2 || true
    exit 1
  fi
done

echo "[smoke] setup first admin"
TOKEN=$(curl -fsS -X POST "$API_BASE/api/v1/setup/admin" \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"gomi-smoke-admin-password"}' | jq -r '.token')

if [[ -z "$TOKEN" || "$TOKEN" == "null" ]]; then
  echo "[smoke] failed to get token" >&2
  exit 1
fi

RESOURCE_SUFFIX=$(date +%s)
NETWORK_NAME="np-${RESOURCE_SUFFIX}"
POWER_NAME="pp-${RESOURCE_SUFFIX}"
MACHINE_NAME="vm-${RESOURCE_SUFFIX}"

echo "[smoke] create network profile: $NETWORK_NAME"
curl -fsS -X POST "$API_BASE/api/v1/network-profiles?namespace=$NAMESPACE" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"metadata\":{\"name\":\"$NETWORK_NAME\"},\"spec\":{\"domain\":\"e2e.local\",\"nameServers\":[\"8.8.8.8\"]}}" >/dev/null

echo "[smoke] create power policy (webhook): $POWER_NAME"
curl -fsS -X POST "$API_BASE/api/v1/power-policies?namespace=$NAMESPACE" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"metadata\":{\"name\":\"$POWER_NAME\"},\"spec\":{\"type\":\"webhook\",\"webhook\":{\"powerOnURL\":\"http://127.0.0.1:$WEBHOOK_PORT/power/on\",\"powerOffURL\":\"http://127.0.0.1:$WEBHOOK_PORT/power/off\",\"headers\":{\"X-Gomi-Test\":\"1\"}}}}" >/dev/null

echo "[smoke] create machine: $MACHINE_NAME"
curl -fsS -X POST "$API_BASE/api/v1/machines?namespace=$NAMESPACE" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"metadata\":{\"name\":\"$MACHINE_NAME\"},\"spec\":{\"hostname\":\"$MACHINE_NAME\",\"mac\":\"00:11:22:33:44:55\",\"arch\":\"amd64\",\"firmware\":\"uefi\",\"powerPolicyRef\":\"$POWER_NAME\",\"networkProfileRef\":\"$NETWORK_NAME\",\"osPreset\":{\"family\":\"ubuntu\",\"version\":\"24.04\",\"imageRef\":\"ubuntu-24.04\"}}}" >/dev/null

echo "[smoke] request reinstall"
JOB_NAME=$(curl -fsS -X POST "$API_BASE/api/v1/machines/$MACHINE_NAME/actions/reinstall?namespace=$NAMESPACE" \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{\"confirm\":\"$MACHINE_NAME\"}" | jq -r '.job.metadata.name')

if [[ -z "$JOB_NAME" || "$JOB_NAME" == "null" ]]; then
  echo "[smoke] job enqueue failed" >&2
  exit 1
fi

echo "[smoke] waiting for job completion: $JOB_NAME"
JOB_PHASE=""
for i in {1..40}; do
  JOB_PHASE=$(curl -fsS "$API_BASE/api/v1/jobs/$JOB_NAME?namespace=$NAMESPACE" \
    -H "Authorization: Bearer $TOKEN" | jq -r '.status.phase')
  if [[ "$JOB_PHASE" == "Succeeded" ]]; then
    break
  fi
  if [[ "$JOB_PHASE" == "Failed" ]]; then
    echo "[smoke] job failed" >&2
    curl -fsS "$API_BASE/api/v1/jobs/$JOB_NAME?namespace=$NAMESPACE" -H "Authorization: Bearer $TOKEN" >&2
    exit 1
  fi
  sleep 1
  if [[ "$i" -eq 40 ]]; then
    echo "[smoke] job timed out" >&2
    exit 1
  fi
done

echo "[smoke] invoke power on/off"
curl -fsS -X POST "$API_BASE/api/v1/machines/$MACHINE_NAME/actions/power-on?namespace=$NAMESPACE" \
  -H "Authorization: Bearer $TOKEN" >/dev/null
curl -fsS -X POST "$API_BASE/api/v1/machines/$MACHINE_NAME/actions/power-off?namespace=$NAMESPACE" \
  -H "Authorization: Bearer $TOKEN" >/dev/null

sleep 1

WEBHOOK_EVENTS=$(wc -l < "$TMP_DIR/webhook.log" | tr -d ' ')
if [[ "$WEBHOOK_EVENTS" -lt 2 ]]; then
  echo "[smoke] expected at least 2 webhook events but got $WEBHOOK_EVENTS" >&2
  cat "$TMP_DIR/webhook.log" >&2 || true
  exit 1
fi

ARTIFACT_KEYS=$(curl -fsS "$API_BASE/api/v1/jobs/$JOB_NAME?namespace=$NAMESPACE" \
  -H "Authorization: Bearer $TOKEN" | jq -r '.status.artifacts | keys | join(",")')

echo "[smoke] success"
echo "job=$JOB_NAME phase=$JOB_PHASE artifacts=$ARTIFACT_KEYS webhook_events=$WEBHOOK_EVENTS"

echo "[smoke] sample webhook events"
sed -n '1,5p' "$TMP_DIR/webhook.log"
