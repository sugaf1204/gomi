#!/usr/bin/env bash
#
# Install OS catalog entries through the GOMI API.
#
# The script intentionally does not know concrete OS names or manipulate image
# files directly. Catalog entries come from the running GOMI server.
set -euo pipefail

GOMI_URL="${1:-${GOMI_URL:-http://localhost:8080}}"
shift || true

if [[ -z "${GOMI_TOKEN:-}" ]]; then
  if [[ -z "${GOMI_USER:-}" || -z "${GOMI_PASS:-}" ]]; then
    echo "ERROR: set GOMI_TOKEN, or set both GOMI_USER and GOMI_PASS" >&2
    exit 1
  fi
  GOMI_TOKEN="$(curl -sf "${GOMI_URL}/api/v1/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"${GOMI_USER}\",\"password\":\"${GOMI_PASS}\"}" \
    | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")"
fi

api() {
  local method="$1" path="$2"
  curl -sf -X "$method" \
    -H "Authorization: Bearer ${GOMI_TOKEN}" \
    -H "Content-Type: application/json" \
    "${GOMI_URL}/api/v1${path}"
}

if [[ "$#" -gt 0 ]]; then
  names=("$@")
else
  mapfile -t names < <(api GET "/os-catalog" | python3 -c '
import json, sys
for item in json.load(sys.stdin).get("items", []):
    print(item["entry"]["name"])
')
fi

for name in "${names[@]}"; do
  echo "==> Installing OS catalog entry: ${name}"
  api POST "/os-catalog/${name}/install" >/dev/null
done
