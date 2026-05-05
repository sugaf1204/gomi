#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
template_dir="${repo_root}/tools/osimage/packer/ubuntu-cloudimg"

image_name="${IMAGE_NAME:-ubuntu-22.04-amd64-baremetal}"
ubuntu_series="${UBUNTU_SERIES:-jammy}"
architecture="${ARCHITECTURE:-amd64}"
kernel_package="${KERNEL_PACKAGE:-linux-image-generic}"
out_dir="${OUT_DIR:-${repo_root}/dist/os-images}"
work_dir="${WORK_DIR:-${repo_root}/tmp/osimage-packer/${image_name}}"
timeout="${PACKER_TIMEOUT:-30m}"
max_release_asset_size="${MAX_RELEASE_ASSET_SIZE:-2147483647}"

require_command() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "ERROR: required command not found: $1" >&2
    exit 1
  }
}

find_ovmf_code() {
  for path in \
    /usr/share/OVMF/OVMF_CODE_4M.fd \
    /usr/share/OVMF/OVMF_CODE.fd
  do
    if [[ -r "$path" ]]; then
      printf '%s\n' "$path"
      return 0
    fi
  done
  echo "ERROR: OVMF_CODE fd not found" >&2
  return 1
}

find_ovmf_vars() {
  for path in \
    /usr/share/OVMF/OVMF_VARS_4M.fd \
    /usr/share/OVMF/OVMF_VARS.fd
  do
    if [[ -r "$path" ]]; then
      printf '%s\n' "$path"
      return 0
    fi
  done
  echo "ERROR: OVMF_VARS fd not found" >&2
  return 1
}

require_command cloud-localds
require_command packer
require_command qemu-img
require_command zstd

rm -rf "$work_dir"
mkdir -p "$work_dir" "$out_dir"

seed_dir="${work_dir}/seed"
mkdir -p "$seed_dir"
cat >"${seed_dir}/meta-data" <<EOF
instance-id: ${image_name}
local-hostname: ${image_name}
EOF
cat >"${seed_dir}/user-data" <<'EOF'
#cloud-config
users:
  - name: root
    lock_passwd: false
    plain_text_passwd: packer
    ssh_redirect_user: false
ssh_pwauth: true
disable_root: false
chpasswd:
  expire: false
preserve_hostname: true
runcmd:
  - sed -i -e '/^[#]*PermitRootLogin/s/^.*$/PermitRootLogin yes/' /etc/ssh/sshd_config
  - systemctl restart ssh
EOF

seed_iso="${work_dir}/seed.iso"
cloud-localds "$seed_iso" "${seed_dir}/user-data" "${seed_dir}/meta-data"

ovmf_code="${work_dir}/OVMF_CODE.fd"
ovmf_vars="${work_dir}/OVMF_VARS.fd"
cp "$(find_ovmf_code)" "$ovmf_code"
cp "$(find_ovmf_vars)" "$ovmf_vars"

packer_work="${work_dir}/packer"
cp -a "$template_dir" "$packer_work"

output_dir="${work_dir}/output"
cache_dir="${work_dir}/packer-cache"
mkdir -p "$cache_dir"

echo "==> Building ${image_name} with Packer"
PACKER_CACHE_DIR="$cache_dir" packer init "$packer_work"
PACKER_CACHE_DIR="$cache_dir" packer build \
  -var "image_name=${image_name}" \
  -var "ubuntu_series=${ubuntu_series}" \
  -var "architecture=${architecture}" \
  -var "kernel_package=${kernel_package}" \
  -var "output_directory=${output_dir}" \
  -var "ovmf_code=${ovmf_code}" \
  -var "ovmf_vars=${ovmf_vars}" \
  -var "seed_iso=${seed_iso}" \
  -var "ssh_username=root" \
  -var "ssh_password=packer" \
  -var "timeout=${timeout}" \
  "$packer_work"

qcow2="${output_dir}/${image_name}.qcow2"
raw="${out_dir}/${image_name}.raw"
zst="${raw}.zst"

if [[ ! -s "$qcow2" ]]; then
  echo "ERROR: expected Packer output not found: ${qcow2}" >&2
  find "$output_dir" -maxdepth 2 -type f -print >&2 || true
  exit 1
fi

echo "==> Converting ${qcow2} to raw"
qemu-img convert -O raw "$qcow2" "$raw"

echo "==> Compressing ${raw}"
zstd -T0 -19 -f "$raw" -o "$zst"
rm -f "$raw"

size="$(stat -c%s "$zst" 2>/dev/null || stat -f%z "$zst")"
sha256="$(
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$zst" | awk '{print $1}'
  else
    shasum -a 256 "$zst" | awk '{print $1}'
  fi
)"

if (( size > max_release_asset_size )); then
  echo "ERROR: $(basename "$zst") is ${size} bytes; GitHub release assets must be <= ${max_release_asset_size} bytes" >&2
  exit 1
fi

du -h "$zst"
printf '%s  %s\n' "$sha256" "$(basename "$zst")"
