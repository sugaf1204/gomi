#!/usr/bin/env bash
#
# upload-os-images.sh — Upload release/catalog raw OS image artifacts to GOMI.
#
# Usage:
#   ./tools/upload-os-images.sh [GOMI_URL] [ARTIFACT_SOURCE]
#
# Environment:
#   GOMI_URL                 Base URL (default: http://localhost:8080)
#   GOMI_OS_IMAGE_SOURCE_URL Artifact base URL or local directory
#                            (default: GitHub latest release assets)
#   GOMI_TOKEN               Auth token (auto-obtained via login if not set)
#   GOMI_USER                Admin username for login fallback
#   GOMI_PASS                Admin password for login fallback
#   DOWNLOAD_DIR             Local cache dir (default: /tmp/gomi-images)
#
set -euo pipefail

GOMI_URL="${1:-${GOMI_URL:-http://localhost:8080}}"
ARTIFACT_SOURCE="${2:-${GOMI_OS_IMAGE_SOURCE_URL:-https://github.com/sugaf1204/gomi/releases/latest/download}}"
DOWNLOAD_DIR="${DOWNLOAD_DIR:-/tmp/gomi-images}"

# ── Auth ───────────────────────────────────────────────────────────────────
if [[ -z "${GOMI_TOKEN:-}" ]]; then
    if [[ -z "${GOMI_USER:-}" || -z "${GOMI_PASS:-}" ]]; then
        echo "ERROR: set GOMI_TOKEN, or set both GOMI_USER and GOMI_PASS" >&2
        exit 1
    fi
    echo "==> Logging in as ${GOMI_USER}..."
    GOMI_TOKEN=$(curl -sf "${GOMI_URL}/api/v1/auth/login" \
        -H "Content-Type: application/json" \
        -d "{\"username\":\"${GOMI_USER}\",\"password\":\"${GOMI_PASS}\"}" \
        | python3 -c "import sys,json; print(json.load(sys.stdin)['token'])")
    if [[ -z "$GOMI_TOKEN" ]]; then
        echo "ERROR: Login failed" >&2
        exit 1
    fi
    echo "    OK"
fi

# Format: NAME|OS_FAMILY|OS_VERSION|ARCH|ARTIFACT
IMAGES=(
    "ubuntu-22.04-amd64|ubuntu|22.04|amd64|ubuntu-22.04-amd64.raw.zst"
    "ubuntu-24.04-amd64|ubuntu|24.04|amd64|ubuntu-24.04-amd64.raw.zst"
    "debian-13-amd64|debian|13|amd64|debian-13-amd64.raw.zst"
)

api() {
    local method="$1" path="$2"; shift 2
    curl -sf -X "$method" \
        -H "Authorization: Bearer ${GOMI_TOKEN}" \
        -H "Content-Type: application/json" \
        "${GOMI_URL}/api/v1${path}" "$@"
}

api_upload() {
    local path="$1" file="$2"
    curl -sf -X POST \
        -H "Authorization: Bearer ${GOMI_TOKEN}" \
        -F "file=@${file}" \
        "${GOMI_URL}/api/v1${path}"
}

size_bytes() {
    stat -f%z "$1" 2>/dev/null || stat -c%s "$1"
}

artifact_ref() {
    local artifact="$1"
    case "$ARTIFACT_SOURCE" in
        http://*|https://*)
            printf '%s/%s\n' "${ARTIFACT_SOURCE%/}" "$artifact"
            ;;
        file://*)
            printf '%s/%s\n' "${ARTIFACT_SOURCE#file://}" "$artifact"
            ;;
        *)
            printf '%s/%s\n' "${ARTIFACT_SOURCE%/}" "$artifact"
            ;;
    esac
}

fetch_artifact() {
    local artifact="$1" dest="$2" ref
    ref="$(artifact_ref "$artifact")"
    case "$ref" in
        http://*|https://*)
            echo "  Downloading ${ref}..."
            curl -fL --progress-bar -o "${dest}.tmp" "$ref"
            mv "${dest}.tmp" "$dest"
            ;;
        *)
            if [[ ! -f "$ref" ]]; then
                echo "  ERROR: artifact not found: ${ref}" >&2
                return 1
            fi
            cp "$ref" "${dest}.tmp"
            mv "${dest}.tmp" "$dest"
            ;;
    esac
}

prepare_raw() {
    local name="$1" artifact_path="$2" raw_path="$3"
    case "$artifact_path" in
        *.raw.zst|*.zst)
            command -v zstd >/dev/null 2>&1 || {
                echo "  ERROR: zstd is required to expand ${artifact_path}" >&2
                return 1
            }
            echo "  Expanding zstd artifact..."
            zstd -dc "$artifact_path" > "${raw_path}.tmp"
            mv "${raw_path}.tmp" "$raw_path"
            ;;
        *.raw)
            cp "$artifact_path" "${raw_path}.tmp"
            mv "${raw_path}.tmp" "$raw_path"
            ;;
        *)
            echo "  ERROR: unsupported artifact for ${name}: ${artifact_path}" >&2
            echo "         expected .raw or .raw.zst" >&2
            return 1
            ;;
    esac
}

echo "==> OS Image Upload"
echo "    GOMI:            ${GOMI_URL}"
echo "    Artifact source: ${ARTIFACT_SOURCE}"
echo "    Download dir:    ${DOWNLOAD_DIR}"
echo ""

mkdir -p "${DOWNLOAD_DIR}"

total=${#IMAGES[@]}
current=0

for entry in "${IMAGES[@]}"; do
    IFS='|' read -r name family version arch artifact <<< "$entry"
    current=$((current + 1))
    artifact_path="${DOWNLOAD_DIR}/${artifact}"
    raw_path="${DOWNLOAD_DIR}/${name}.raw"

    echo "[${current}/${total}] ${name}"
    fetch_artifact "$artifact" "$artifact_path"
    prepare_raw "$name" "$artifact_path" "$raw_path"

    if api GET "/os-images/${name}" >/dev/null 2>&1; then
        echo "  Replacing existing entry..."
        api DELETE "/os-images/${name}" >/dev/null
    fi

    bytes="$(size_bytes "$raw_path")"
    payload=$(NAME="$name" FAMILY="$family" VERSION="$version" ARCH="$arch" SIZE_BYTES="$bytes" python3 - <<'PY'
import json
import os

print(json.dumps({
    "name": os.environ["NAME"],
    "osFamily": os.environ["FAMILY"],
    "osVersion": os.environ["VERSION"],
    "arch": os.environ["ARCH"],
    "format": "raw",
    "source": "upload",
    "description": f"Prebuilt raw {os.environ['FAMILY']} {os.environ['VERSION']} image ({os.environ['ARCH']})",
    "sizeBytes": int(os.environ["SIZE_BYTES"]),
}))
PY
)
    api POST "/os-images" -d "$payload" >/dev/null
    echo "  Registered"

    echo "  Uploading raw image ($(du -h "$raw_path" | cut -f1))..."
    api_upload "/os-images/${name}/upload" "$raw_path" >/dev/null
    echo "  Done"
    echo ""
done

echo "==> ${total} images uploaded"
echo ""
echo "Registered images:"
api GET "/os-images" \
    | python3 -c "
import sys, json
items = json.load(sys.stdin).get('items', [])
for img in items:
    print(f'  {img[\"name\"]:<24} {img[\"osFamily\"]} {img[\"osVersion\"]}  format={img[\"format\"]}')
" 2>/dev/null || echo "  (could not list images)"
