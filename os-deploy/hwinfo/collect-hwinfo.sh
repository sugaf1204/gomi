#!/bin/bash
# collect-hwinfo.sh — Collect hardware information and POST to GOMI API.
# Usage: collect-hwinfo.sh <gomi-api-base> <machine-name> [namespace]
# Example: collect-hwinfo.sh http://10.0.0.1:8080/api/v1 node-01 default
#
# Runs on PXE-booted environment or as a provisioning late-command.

set -euo pipefail

API_BASE="${1:?Usage: $0 <api-base> <machine-name> [namespace]}"
MACHINE="${2:?Usage: $0 <api-base> <machine-name> [namespace]}"
NAMESPACE="${3:-default}"

# CPU info
CPU_MODEL=$(grep -m1 'model name' /proc/cpuinfo 2>/dev/null | cut -d: -f2 | xargs || echo "unknown")
CPU_CORES=$(nproc --all 2>/dev/null || echo 1)
CPU_THREADS=$(grep -c '^processor' /proc/cpuinfo 2>/dev/null || echo "$CPU_CORES")
CPU_ARCH=$(uname -m 2>/dev/null || echo "unknown")
CPU_MHZ=$(grep -m1 'cpu MHz' /proc/cpuinfo 2>/dev/null | cut -d: -f2 | xargs || echo "0")

# Memory info
MEM_TOTAL_KB=$(grep MemTotal /proc/meminfo 2>/dev/null | awk '{print $2}' || echo 0)
MEM_TOTAL_MB=$((MEM_TOTAL_KB / 1024))

# Disk info
DISKS="[]"
if command -v lsblk >/dev/null 2>&1; then
  DISKS=$(lsblk -Jbdo NAME,SIZE,TYPE,MODEL 2>/dev/null | python3 -c '
import sys, json
data = json.load(sys.stdin)
disks = []
for d in data.get("blockdevices", []):
    if d.get("type") == "disk":
        disks.append({
            "name": d.get("name", ""),
            "sizeMB": int(d.get("size", 0)) // (1024*1024),
            "type": "disk",
            "model": (d.get("model") or "").strip()
        })
print(json.dumps(disks))
' 2>/dev/null || echo "[]")
fi

# NIC info
NICS="[]"
if [ -d /sys/class/net ]; then
  NICS=$(python3 -c '
import os, json
nics = []
for name in os.listdir("/sys/class/net"):
    if name == "lo":
        continue
    mac = ""
    try:
        with open(f"/sys/class/net/{name}/address") as f:
            mac = f.read().strip()
    except: pass
    state = ""
    try:
        with open(f"/sys/class/net/{name}/operstate") as f:
            state = f.read().strip()
    except: pass
    speed = ""
    try:
        with open(f"/sys/class/net/{name}/speed") as f:
            speed = f.read().strip() + " Mbps"
    except: pass
    nics.append({"name": name, "mac": mac, "speed": speed, "state": state})
print(json.dumps(nics))
' 2>/dev/null || echo "[]")
fi

# BIOS info
BIOS_VENDOR=""
BIOS_VERSION=""
BIOS_DATE=""
if [ -r /sys/class/dmi/id/bios_vendor ]; then
  BIOS_VENDOR=$(cat /sys/class/dmi/id/bios_vendor 2>/dev/null || true)
fi
if [ -r /sys/class/dmi/id/bios_version ]; then
  BIOS_VERSION=$(cat /sys/class/dmi/id/bios_version 2>/dev/null || true)
fi
if [ -r /sys/class/dmi/id/bios_date ]; then
  BIOS_DATE=$(cat /sys/class/dmi/id/bios_date 2>/dev/null || true)
fi

# Build JSON payload
PAYLOAD=$(cat <<EOJSON
{
  "metadata": {"namespace": "${NAMESPACE}", "name": "${MACHINE}-hwinfo"},
  "spec": {
    "machineName": "${MACHINE}",
    "cpu": {
      "model": "${CPU_MODEL}",
      "cores": ${CPU_CORES},
      "threads": ${CPU_THREADS},
      "arch": "${CPU_ARCH}",
      "mhz": "${CPU_MHZ}"
    },
    "memory": {
      "totalMB": ${MEM_TOTAL_MB}
    },
    "disks": ${DISKS},
    "nics": ${NICS},
    "bios": {
      "vendor": "${BIOS_VENDOR}",
      "version": "${BIOS_VERSION}",
      "date": "${BIOS_DATE}"
    }
  }
}
EOJSON
)

# POST to GOMI API
curl -s -X POST \
  -H "Content-Type: application/json" \
  -d "${PAYLOAD}" \
  "${API_BASE}/machines/${MACHINE}/hardware?namespace=${NAMESPACE}" \
  && echo "Hardware info reported successfully for ${MACHINE}" \
  || echo "Failed to report hardware info for ${MACHINE}" >&2
