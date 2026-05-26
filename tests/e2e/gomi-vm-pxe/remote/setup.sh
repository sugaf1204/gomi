#!/usr/bin/env bash

ensure_passwordless_sudo() {
  if ! sudo -n true >/dev/null 2>&1; then
    echo "ERROR: passwordless sudo is required on remote host" >&2
    exit 1
  fi
}

install_host_dependencies() {
  echo "[remote-lab] install host dependencies"
  sudo -n apt-get update -y
  sudo -n DEBIAN_FRONTEND=noninteractive apt-get install -y \
    qemu-system-x86 qemu-utils cloud-image-utils bridge-utils curl jq python3
}

prepare_gomi_vm_access() {
  echo "[remote-lab] create key for gomi vm access"
  if [[ ! -f "$GOMI_VM_KEY" ]]; then
    ssh-keygen -q -t ed25519 -N '' -f "$GOMI_VM_KEY"
  fi

  if [[ ! -f "$BASE_IMG" ]]; then
    echo "[remote-lab] download ubuntu cloud image"
    curl -fsSL -o "$BASE_IMG" "https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img"
  fi
}

prepare_gomi_vm_cloud_init() {
  local pubkey

  echo "[remote-lab] prepare cloud-init"
  pubkey=$(cat "$GOMI_VM_KEY.pub")
  cat > "$LAB_DIR/user-data" <<USERDATA
#cloud-config
ssh_authorized_keys:
  - $pubkey
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
}

create_gomi_vm_disk() {
  echo "[remote-lab] create vm disk"
  qemu-img create -f qcow2 -F qcow2 -b "$BASE_IMG" "$GOMI_VM_DISK" 24G >/dev/null
}

setup_lab_network() {
  echo "[remote-lab] setup bridge/tap network"
  sudo -n ip link add "$BR" type bridge
  sudo -n ip link set "$BR" up
  sudo -n ip tuntap add dev "$TAP_G" mode tap user "$REMOTE_USER"
  sudo -n ip link set "$TAP_G" master "$BR"
  sudo -n ip link set "$TAP_G" up
  sudo -n ip tuntap add dev "$TAP_T" mode tap user "$REMOTE_USER"
  sudo -n ip link set "$TAP_T" master "$BR"
  sudo -n ip link set "$TAP_T" up
}

start_gomi_vm() {
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
}

wait_for_gomi_vm_ssh() {
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
}

install_gomi_binary_in_vm() {
  echo "[remote-lab] copy gomi binary into gomi vm"
  vm_scp "$GOMI_BIN_IN" "ubuntu@127.0.0.1:/tmp/gomi"
  vm_ssh "sudo install -m 0755 /tmp/gomi /usr/local/bin/gomi"
}

configure_gomi_vm_services() {
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
}

bootstrap_gomi_api() {
  local suffix

  echo "[remote-lab] gomi api setup"
  TOKEN=$(vm_ssh "curl -fsS -X POST http://127.0.0.1:8080/api/v1/setup/admin -H 'Content-Type: application/json' -d '{\"username\":\"admin\",\"password\":\"gomi-lab-admin-password\"}' | jq -r '.token'")
  if [[ -z "$TOKEN" || "$TOKEN" == "null" ]]; then
    echo "ERROR: failed to get gomi token" >&2
    exit 1
  fi

  suffix=$(date +%s)
  NET_PROFILE="lab-net-${suffix}"
  POWER_POLICY="lab-power-${suffix}"
  MACHINE_NAME="test-vm-${suffix}"

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
}
