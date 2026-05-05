#!/usr/bin/env bash
set -euo pipefail

HOST="${1:-dev.vm.canvm.jp}"
USER_NAME="${2:-ubuntu}"
STAMP=$(date +%Y%m%d%H%M%S)
REMOTE_DIR="/tmp/gomi-qemu-kvm-pxe-$STAMP"
LOCAL_SCRIPT="$(mktemp)"

cleanup_local() {
  rm -f "$LOCAL_SCRIPT"
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

cat > "$LOCAL_SCRIPT" <<'REMOTE'
#!/usr/bin/env bash
set -euo pipefail

WORK_DIR="$1"
LOG_DIR="$WORK_DIR/logs"
TFTP_DIR="$WORK_DIR/tftp"
HTTP_DIR="$WORK_DIR/http"
PXELINUX_CFG_DIR="$TFTP_DIR/pxelinux.cfg"
DISK_IMG="$WORK_DIR/debian-pxe.qcow2"
INSTALL_LOG="$LOG_DIR/install-serial.log"
BOOT_LOG="$LOG_DIR/boot-serial.log"
HTTP_LOG="$LOG_DIR/http.log"
HTTP_PORT=18088
QEMU_MEM_MB=4096
QEMU_SMP=2
INSTALL_TIMEOUT=3600
BOOT_TIMEOUT=240

mkdir -p "$LOG_DIR" "$PXELINUX_CFG_DIR" "$HTTP_DIR" "$TFTP_DIR/debian"

if ! sudo -n true >/dev/null 2>&1; then
  echo "ERROR: sudo -n is unavailable. Passwordless sudo is required." >&2
  exit 1
fi

echo "[remote-pxe] install required packages"
sudo -n apt-get update -y
sudo -n DEBIAN_FRONTEND=noninteractive apt-get install -y \
  qemu-system-x86 qemu-utils pxelinux syslinux-common curl python3 ca-certificates

if [[ ! -e /dev/kvm ]]; then
  echo "ERROR: /dev/kvm does not exist. KVM is not available on this host." >&2
  exit 1
fi
if ! sudo -n test -r /dev/kvm; then
  echo "ERROR: cannot access /dev/kvm." >&2
  exit 1
fi

echo "[remote-pxe] prepare pxelinux artifacts"
PXELINUX0=$(dpkg -L pxelinux | grep '/pxelinux\.0$' | head -n1)
LDLINUX=$(dpkg -L syslinux-common | grep '/ldlinux\.c32$' | head -n1)
if [[ -z "$PXELINUX0" || -z "$LDLINUX" ]]; then
  echo "ERROR: pxelinux.0 or ldlinux.c32 was not found." >&2
  exit 1
fi
cp "$PXELINUX0" "$TFTP_DIR/pxelinux.0"
cp "$LDLINUX" "$TFTP_DIR/ldlinux.c32"

echo "[remote-pxe] download debian netboot kernel/initrd"
curl -fsSL -o "$TFTP_DIR/debian/linux" \
  "https://deb.debian.org/debian/dists/stable/main/installer-amd64/current/images/netboot/debian-installer/amd64/linux"
curl -fsSL -o "$TFTP_DIR/debian/initrd.gz" \
  "https://deb.debian.org/debian/dists/stable/main/installer-amd64/current/images/netboot/debian-installer/amd64/initrd.gz"

echo "[remote-pxe] write preseed"
cat > "$HTTP_DIR/preseed.cfg" <<PRESEED
# Locale and keyboard
d-i debian-installer/locale string en_US.UTF-8
d-i console-setup/ask_detect boolean false
d-i keyboard-configuration/xkb-keymap select us

# Networking
d-i netcfg/choose_interface select auto
d-i netcfg/get_hostname string gomi-pxe
d-i netcfg/get_domain string local

# Mirror
d-i mirror/country string manual
d-i mirror/http/hostname string deb.debian.org
d-i mirror/http/directory string /debian
d-i mirror/http/proxy string

# Users
d-i passwd/root-login boolean true
d-i passwd/root-password password gomiroot
d-i passwd/root-password-again password gomiroot
d-i passwd/user-fullname string gomi
d-i passwd/username string gomi
d-i passwd/user-password password gomi
d-i passwd/user-password-again password gomi

# Time
d-i clock-setup/utc boolean true
d-i time/zone string UTC

# Partitioning
d-i partman-auto/disk string /dev/vda
d-i partman-auto/method string regular
d-i partman-lvm/device_remove_lvm boolean true
d-i partman-md/device_remove_md boolean true
d-i partman-auto/choose_recipe select atomic
d-i partman-partitioning/confirm_write_new_label boolean true
d-i partman/choose_partition select finish
d-i partman/confirm boolean true
d-i partman/confirm_nooverwrite boolean true

# Package selection
tasksel tasksel/first multiselect standard, ssh-server
d-i pkgsel/include string qemu-guest-agent
d-i popularity-contest popularity-contest/participate boolean false

# Bootloader and serial console
d-i grub-installer/only_debian boolean true
d-i grub-installer/bootdev string /dev/vda
d-i debian-installer/add-kernel-opts string console=ttyS0,115200n8

d-i preseed/late_command string in-target systemctl enable serial-getty@ttyS0.service

# Finish
d-i finish-install/reboot_in_progress note
d-i debian-installer/exit/poweroff boolean true
PRESEED

echo "[remote-pxe] write pxelinux config"
cat > "$PXELINUX_CFG_DIR/default" <<PXECFG
DEFAULT install
PROMPT 0
TIMEOUT 20

LABEL install
  KERNEL debian/linux
  INITRD debian/initrd.gz
  APPEND auto=true priority=critical url=http://10.0.2.2:${HTTP_PORT}/preseed.cfg console=ttyS0,115200n8 ---
PXECFG

echo "[remote-pxe] start preseed http server"
python3 -m http.server "$HTTP_PORT" --bind 0.0.0.0 --directory "$HTTP_DIR" >"$HTTP_LOG" 2>&1 &
HTTP_PID=$!

cleanup() {
  kill "$HTTP_PID" >/dev/null 2>&1 || true
}
trap cleanup EXIT

if [[ ! -f "$DISK_IMG" ]]; then
  qemu-img create -f qcow2 "$DISK_IMG" 16G >/dev/null
fi

echo "[remote-pxe] start PXE installation with qemu+kvm"
set +e
timeout --foreground "$INSTALL_TIMEOUT" sudo -n qemu-system-x86_64 \
  -enable-kvm -cpu host \
  -m "$QEMU_MEM_MB" -smp "$QEMU_SMP" \
  -drive "file=$DISK_IMG,if=virtio,format=qcow2" \
  -netdev "user,id=net0,tftp=$TFTP_DIR,bootfile=pxelinux.0" \
  -device e1000,netdev=net0 \
  -boot order=n \
  -nographic -monitor none \
  -serial "file:$INSTALL_LOG"
INSTALL_RC=$?
set -e

if [[ "$INSTALL_RC" -ne 0 ]]; then
  if [[ "$INSTALL_RC" -eq 124 ]]; then
    echo "ERROR: install phase timeout (${INSTALL_TIMEOUT}s)" >&2
  else
    echo "ERROR: install phase failed rc=$INSTALL_RC" >&2
  fi
  tail -n 120 "$INSTALL_LOG" >&2 || true
  exit 1
fi

echo "[remote-pxe] boot installed OS"
set +e
timeout --foreground "$BOOT_TIMEOUT" sudo -n qemu-system-x86_64 \
  -enable-kvm -cpu host \
  -m 2048 -smp 2 \
  -drive "file=$DISK_IMG,if=virtio,format=qcow2" \
  -netdev user,id=net0 \
  -device e1000,netdev=net0 \
  -boot order=c \
  -nographic -monitor none \
  -serial "file:$BOOT_LOG"
BOOT_RC=$?
set -e

if [[ "$BOOT_RC" -ne 0 && "$BOOT_RC" -ne 124 ]]; then
  echo "ERROR: boot verification failed rc=$BOOT_RC" >&2
  tail -n 120 "$BOOT_LOG" >&2 || true
  exit 1
fi

if ! grep -Eq 'login:|Debian GNU/Linux' "$BOOT_LOG"; then
  echo "ERROR: installed OS boot log does not contain login prompt marker" >&2
  tail -n 120 "$BOOT_LOG" >&2 || true
  exit 1
fi

echo "[remote-pxe] SUCCESS: qemu+kvm PXE install verified"
echo "work_dir=$WORK_DIR"
echo "install_log=$INSTALL_LOG"
echo "boot_log=$BOOT_LOG"
echo "disk_image=$DISK_IMG"
REMOTE

echo "[remote-pxe] prepare remote directory: $USER_NAME@$HOST:$REMOTE_DIR"
ssh "$USER_NAME@$HOST" "mkdir -p '$REMOTE_DIR'"

echo "[remote-pxe] upload remote script"
scp "$LOCAL_SCRIPT" "$USER_NAME@$HOST:$REMOTE_DIR/run_pxe_kvm.sh"

echo "[remote-pxe] execute remote script"
ssh "$USER_NAME@$HOST" "chmod +x '$REMOTE_DIR/run_pxe_kvm.sh' && '$REMOTE_DIR/run_pxe_kvm.sh' '$REMOTE_DIR'"

echo "[remote-pxe] completed"
echo "[remote-pxe] cleanup command (optional): ssh $USER_NAME@$HOST 'rm -rf $REMOTE_DIR'"
