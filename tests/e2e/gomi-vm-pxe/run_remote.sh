#!/usr/bin/env bash
set -euo pipefail

HOST="${1:-dev.vm.canvm.jp}"
USER_NAME="${2:-ubuntu}"
ROOT_DIR=$(cd "$(dirname "$0")/../../.." && pwd)
STAMP=$(date +%Y%m%d%H%M%S)
REMOTE_DIR="/tmp/gomi-vm-lab-$STAMP"
LOCAL_BIN="/tmp/gomi-vm-lab-bin-$STAMP"
LOCAL_REMOTE_SCRIPT="$(mktemp)"

cleanup_local() {
  rm -f "$LOCAL_REMOTE_SCRIPT" "$LOCAL_BIN"
}
trap cleanup_local EXIT

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing command: $1" >&2
    exit 1
  fi
}

need_cmd ssh
need_cmd scp
need_cmd go

echo "[orchestrator] detect remote architecture"
REMOTE_UNAME=$(ssh "$USER_NAME@$HOST" "uname -m")
case "$REMOTE_UNAME" in
  x86_64|amd64)
    TARGET_GOARCH="amd64"
    ;;
  *)
    echo "unsupported remote arch: $REMOTE_UNAME (this script currently supports x86_64 only)" >&2
    exit 1
    ;;
esac

echo "[orchestrator] build gomi binary for linux/$TARGET_GOARCH"
(
  cd "$ROOT_DIR"
  CGO_ENABLED=0 GOOS=linux GOARCH="$TARGET_GOARCH" GOCACHE=/tmp/gomi-go-build-cache go build -o "$LOCAL_BIN" ./cmd/gomi
)

cat > "$LOCAL_REMOTE_SCRIPT" <<'REMOTE_SCRIPT'
#!/usr/bin/env bash
set -euo pipefail

LAB_DIR="$1"
GOMI_BIN_IN="$2"

mkdir -p "$LAB_DIR"
LOG_DIR="$LAB_DIR/logs"
mkdir -p "$LOG_DIR"
HOST_TFTP_DIR="$LAB_DIR/host-tftp"
HOST_HTTP_DIR="$LAB_DIR/host-http"
mkdir -p "$HOST_TFTP_DIR/pxelinux.cfg" "$HOST_TFTP_DIR/debian" "$HOST_HTTP_DIR"

REMOTE_USER=$(id -un)

choose_port() {
  python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
}

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
  return $rc
}
trap cleanup_on_error EXIT

if ! sudo -n true >/dev/null 2>&1; then
  echo "ERROR: passwordless sudo is required on remote host" >&2
  exit 1
fi

echo "[remote-lab] install host dependencies"
sudo -n apt-get update -y
sudo -n DEBIAN_FRONTEND=noninteractive apt-get install -y \
  qemu-system-x86 qemu-utils cloud-image-utils bridge-utils curl jq python3

echo "[remote-lab] create key for gomi vm access"
if [[ ! -f "$GOMI_VM_KEY" ]]; then
  ssh-keygen -q -t ed25519 -N '' -f "$GOMI_VM_KEY"
fi

if [[ ! -f "$BASE_IMG" ]]; then
  echo "[remote-lab] download ubuntu cloud image"
  curl -fsSL -o "$BASE_IMG" "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
fi

echo "[remote-lab] prepare cloud-init"
PUBKEY=$(cat "$GOMI_VM_KEY.pub")
cat > "$LAB_DIR/user-data" <<USERDATA
#cloud-config
ssh_authorized_keys:
  - $PUBKEY
disable_root: false
chpasswd:
  list: |
    ubuntu:ubuntu
  expire: false
users:
  - default
USERDATA

cat > "$LAB_DIR/meta-data" <<METADATA
instance-id: gomi-vm-$LAB_ID
local-hostname: gomi-vm
METADATA

cat > "$LAB_DIR/network-config" <<NETCFG
version: 2
ethernets:
  mgmt0:
    match:
      macaddress: "$GOMI_MAC_MGMT"
    set-name: mgmt0
    dhcp4: true
  pxe0:
    match:
      macaddress: "$GOMI_MAC_PXE"
    set-name: pxe0
    addresses:
      - 10.20.0.1/24
    dhcp4: false
NETCFG

cloud-localds --network-config="$LAB_DIR/network-config" "$GOMI_VM_SEED" "$LAB_DIR/user-data" "$LAB_DIR/meta-data"

echo "[remote-lab] create vm disk"
qemu-img create -f qcow2 -F qcow2 -b "$BASE_IMG" "$GOMI_VM_DISK" 24G >/dev/null

echo "[remote-lab] setup bridge/tap network"
sudo -n ip link add "$BR" type bridge
sudo -n ip link set "$BR" up
sudo -n ip tuntap add dev "$TAP_G" mode tap user "$REMOTE_USER"
sudo -n ip link set "$TAP_G" master "$BR"
sudo -n ip link set "$TAP_G" up
sudo -n ip tuntap add dev "$TAP_T" mode tap user "$REMOTE_USER"
sudo -n ip link set "$TAP_T" master "$BR"
sudo -n ip link set "$TAP_T" up

echo "[remote-lab] start gomi vm"
sudo -n qemu-system-x86_64 \
  -enable-kvm -cpu host \
  -m 2048 -smp 2 \
  -drive "file=$GOMI_VM_DISK,if=virtio,format=qcow2" \
  -drive "file=$GOMI_VM_SEED,if=virtio,format=raw" \
  -netdev "user,id=mgmt0,hostfwd=tcp:127.0.0.1:${MGMT_PORT}-:22" \
  -device "virtio-net-pci,netdev=mgmt0,mac=$GOMI_MAC_MGMT" \
  -netdev "tap,id=pxe0,ifname=$TAP_G,script=no,downscript=no" \
  -device "virtio-net-pci,netdev=pxe0,mac=$GOMI_MAC_PXE" \
  -display none -monitor none \
  -serial "file:$GOMI_VM_SERIAL" \
  -daemonize \
  -pidfile "$GOMI_VM_PIDFILE"

echo "[remote-lab] wait for gomi vm ssh"
for i in {1..120}; do
  if vm_ssh "echo gomi-vm-ready" >/dev/null 2>&1; then
    break
  fi
  sleep 2
  if [[ "$i" -eq 120 ]]; then
    echo "ERROR: gomi vm ssh did not become ready" >&2
    tail -n 120 "$GOMI_VM_SERIAL" >&2 || true
    exit 1
  fi
done

echo "[remote-lab] copy gomi binary into gomi vm"
vm_scp "$GOMI_BIN_IN" "ubuntu@127.0.0.1:/tmp/gomi"
vm_ssh "sudo install -m 0755 /tmp/gomi /usr/local/bin/gomi"

echo "[remote-lab] configure gomi vm packages/services"
vm_ssh "sudo apt-get update -y"
vm_ssh "sudo DEBIAN_FRONTEND=noninteractive apt-get install -y dnsmasq pxelinux syslinux-common curl jq python3"
vm_ssh "sudo mkdir -p /srv/tftp/pxelinux.cfg /srv/tftp/debian /srv/http"
vm_ssh 'PX=$(dpkg -L pxelinux | grep "/pxelinux\\.0$" | head -n1); LD=$(dpkg -L syslinux-common | grep "/ldlinux\\.c32$" | head -n1); sudo cp "$PX" /srv/tftp/pxelinux.0; sudo cp "$LD" /srv/tftp/ldlinux.c32'
vm_ssh 'sudo curl -fsSL -o /srv/tftp/debian/linux "https://deb.debian.org/debian/dists/stable/main/installer-amd64/current/images/netboot/debian-installer/amd64/linux"'
vm_ssh 'sudo curl -fsSL -o /srv/tftp/debian/initrd.gz "https://deb.debian.org/debian/dists/stable/main/installer-amd64/current/images/netboot/debian-installer/amd64/initrd.gz"'

vm_ssh "sudo bash -c 'cat > /etc/dnsmasq.d/gomi-pxe.conf' <<'DNSCFG'
interface=pxe0
bind-interfaces
dhcp-range=10.20.0.100,10.20.0.200,255.255.255.0,12h
dhcp-option=3,10.20.0.1
dhcp-option=6,10.20.0.1
enable-tftp
tftp-root=/srv/tftp
dhcp-boot=pxelinux.0
log-dhcp
DNSCFG"
vm_ssh "sudo systemctl enable dnsmasq"
vm_ssh "sudo systemctl restart dnsmasq"
vm_ssh "sudo sysctl -w net.ipv4.ip_forward=1 >/dev/null"
vm_ssh "sudo iptables -t nat -C POSTROUTING -o mgmt0 -j MASQUERADE >/dev/null 2>&1 || sudo iptables -t nat -A POSTROUTING -o mgmt0 -j MASQUERADE"
vm_ssh "sudo iptables -C FORWARD -i pxe0 -o mgmt0 -j ACCEPT >/dev/null 2>&1 || sudo iptables -A FORWARD -i pxe0 -o mgmt0 -j ACCEPT"
vm_ssh "sudo iptables -C FORWARD -i mgmt0 -o pxe0 -m state --state RELATED,ESTABLISHED -j ACCEPT >/dev/null 2>&1 || sudo iptables -A FORWARD -i mgmt0 -o pxe0 -m state --state RELATED,ESTABLISHED -j ACCEPT"

vm_ssh "sudo bash -lc 'nohup python3 -m http.server 8088 --directory /srv/http --bind 0.0.0.0 >/var/log/gomi-http.log 2>&1 &'"

vm_ssh "sudo bash -lc 'nohup env GOMI_EMBEDDED_ETCD=false /usr/local/bin/gomi --mode=server --runtime=standalone --listen=:8080 --controller-enabled=true --namespace=default --boot-http-base-url=http://10.20.0.1:8088 >/var/log/gomi.log 2>&1 &'"

vm_ssh 'for i in {1..60}; do curl -fsS http://127.0.0.1:8080/healthz >/dev/null && exit 0; sleep 1; done; exit 1'

echo "[remote-lab] gomi api setup"
TOKEN=$(vm_ssh "curl -fsS -X POST http://127.0.0.1:8080/api/v1/setup/admin -H 'Content-Type: application/json' -d '{\"username\":\"admin\",\"password\":\"gomi-lab-admin-password\"}' | jq -r '.token'")
if [[ -z "$TOKEN" || "$TOKEN" == "null" ]]; then
  echo "ERROR: failed to get gomi token" >&2
  exit 1
fi

SUFFIX=$(date +%s)
NET_PROFILE="lab-net-${SUFFIX}"
POWER_POLICY="lab-power-${SUFFIX}"
MACHINE_NAME="test-vm-${SUFFIX}"

vm_ssh "curl -fsS -X POST 'http://127.0.0.1:8080/api/v1/network-profiles?namespace=default' -H 'Authorization: Bearer $TOKEN' -H 'Content-Type: application/json' -d '{\"metadata\":{\"name\":\"$NET_PROFILE\"},\"spec\":{\"domain\":\"lab.local\",\"nameServers\":[\"10.20.0.1\"]}}' >/dev/null"
vm_ssh "curl -fsS -X POST 'http://127.0.0.1:8080/api/v1/power-policies?namespace=default' -H 'Authorization: Bearer $TOKEN' -H 'Content-Type: application/json' -d '{\"metadata\":{\"name\":\"$POWER_POLICY\"},\"spec\":{\"type\":\"webhook\",\"webhook\":{\"powerOnURL\":\"http://127.0.0.1:19999/on\",\"powerOffURL\":\"http://127.0.0.1:19999/off\"}}}' >/dev/null"

vm_ssh "curl -fsS -X POST 'http://127.0.0.1:8080/api/v1/machines?namespace=default' -H 'Authorization: Bearer $TOKEN' -H 'Content-Type: application/json' -d '{\"metadata\":{\"name\":\"$MACHINE_NAME\"},\"spec\":{\"hostname\":\"$MACHINE_NAME\",\"mac\":\"$TEST_MAC\",\"arch\":\"amd64\",\"firmware\":\"bios\",\"powerPolicyRef\":\"$POWER_POLICY\",\"networkProfileRef\":\"$NET_PROFILE\",\"osPreset\":{\"family\":\"debian\",\"version\":\"13\",\"imageRef\":\"debian-13-netboot\"}}}' >/dev/null"

JOB_NAME=$(vm_ssh "curl -fsS -X POST 'http://127.0.0.1:8080/api/v1/machines/$MACHINE_NAME/actions/reinstall?namespace=default' -H 'Authorization: Bearer $TOKEN' -H 'Content-Type: application/json' -d '{\"confirm\":\"$MACHINE_NAME\"}' | jq -r '.job.metadata.name'")
if [[ -z "$JOB_NAME" || "$JOB_NAME" == "null" ]]; then
  echo "ERROR: failed to enqueue reinstall job" >&2
  exit 1
fi

echo "[remote-lab] wait for gomi provision job: $JOB_NAME"
for i in {1..80}; do
  PHASE=$(vm_ssh "curl -fsS 'http://127.0.0.1:8080/api/v1/jobs/$JOB_NAME?namespace=default' -H 'Authorization: Bearer $TOKEN' | jq -r '.status.phase'")
  if [[ "$PHASE" == "Succeeded" ]]; then
    break
  fi
  if [[ "$PHASE" == "Failed" ]]; then
    echo "ERROR: gomi provision job failed" >&2
    vm_ssh "curl -fsS 'http://127.0.0.1:8080/api/v1/jobs/$JOB_NAME?namespace=default' -H 'Authorization: Bearer $TOKEN'" >&2 || true
    exit 1
  fi
  sleep 1
  if [[ "$i" -eq 80 ]]; then
    echo "ERROR: gomi provision job timeout" >&2
    exit 1
  fi
done

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

vm_scp "ubuntu@127.0.0.1:/srv/tftp/pxelinux.0" "$HOST_TFTP_DIR/pxelinux.0"
vm_scp "ubuntu@127.0.0.1:/srv/tftp/ldlinux.c32" "$HOST_TFTP_DIR/ldlinux.c32"
vm_scp "ubuntu@127.0.0.1:/srv/tftp/debian/linux" "$HOST_TFTP_DIR/debian/linux"
vm_scp "ubuntu@127.0.0.1:/srv/tftp/debian/initrd.gz" "$HOST_TFTP_DIR/debian/initrd.gz"

cat > "$HOST_TFTP_DIR/pxelinux.cfg/default" <<PXECFG
DEFAULT install
PROMPT 0
TIMEOUT 50

LABEL install
  KERNEL debian/linux
  INITRD debian/initrd.gz
  APPEND auto=true priority=critical url=http://10.0.2.2:${HOST_HTTP_PORT}/preseed.cfg console=ttyS0,115200n8 ipv6.disable=1 ---
PXECFG

sudo -n pkill -f "^python3 -m http.server ${HOST_HTTP_PORT}( |$)" >/dev/null 2>&1 || true
nohup python3 -m http.server "$HOST_HTTP_PORT" --bind 0.0.0.0 --directory "$HOST_HTTP_DIR" >"$HOST_HTTP_LOG" 2>&1 &
echo $! > "$HOST_HTTP_PIDFILE"

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
INSTALL_RC=$?
set -e

if [[ "$INSTALL_RC" -ne 0 ]]; then
  if [[ "$INSTALL_RC" -eq 124 ]]; then
    echo "ERROR: test vm install timeout" >&2
  else
    echo "ERROR: test vm install failed rc=$INSTALL_RC" >&2
  fi
  tail -n 120 "$TEST_INSTALL_LOG" >&2 || true
  exit 1
fi

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
BOOT_RC=$?
set -e

if [[ "$BOOT_RC" -ne 0 && "$BOOT_RC" -ne 124 ]]; then
  echo "ERROR: test vm boot verify failed rc=$BOOT_RC" >&2
  tail -n 120 "$TEST_BOOT_LOG" >&2 || true
  exit 1
fi

if ! grep -Eq 'login:|Debian GNU/Linux' "$TEST_BOOT_LOG"; then
  echo "ERROR: boot log does not show login/debian marker" >&2
  tail -n 120 "$TEST_BOOT_LOG" >&2 || true
  exit 1
fi

echo "[remote-lab] SUCCESS"
echo "lab_dir=$LAB_DIR"
echo "bridge=$BR tap_gomi=$TAP_G tap_test=$TAP_T"
echo "gomi_vm_ssh_port_on_host=$MGMT_PORT"
echo "gomi_job=$JOB_NAME machine=$MACHINE_NAME"
echo "gomi_vm_serial=$GOMI_VM_SERIAL"
echo "test_install_log=$TEST_INSTALL_LOG"
echo "test_boot_log=$TEST_BOOT_LOG"
echo "gomi_vm_access: ssh -i $GOMI_VM_KEY -p $MGMT_PORT ubuntu@127.0.0.1"

echo "[remote-lab] note: gomi vm and lab network are left running for inspection"
trap - EXIT
REMOTE_SCRIPT

echo "[orchestrator] prepare remote dir: $USER_NAME@$HOST:$REMOTE_DIR"
ssh "$USER_NAME@$HOST" "mkdir -p '$REMOTE_DIR'"

echo "[orchestrator] upload artifacts"
scp "$LOCAL_REMOTE_SCRIPT" "$USER_NAME@$HOST:$REMOTE_DIR/run_lab.sh"
scp "$LOCAL_BIN" "$USER_NAME@$HOST:$REMOTE_DIR/gomi"

echo "[orchestrator] execute remote lab"
ssh "$USER_NAME@$HOST" "chmod +x '$REMOTE_DIR/run_lab.sh' && '$REMOTE_DIR/run_lab.sh' '$REMOTE_DIR' '$REMOTE_DIR/gomi'"

echo "[orchestrator] done"
