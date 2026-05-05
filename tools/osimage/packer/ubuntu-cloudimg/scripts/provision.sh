#!/usr/bin/env bash
set -euxo pipefail

if [[ -n "${GOMI_KERNEL_PACKAGE:-}" ]]; then
  sudo apt-get update
  if [[ "${GOMI_KERNEL_PACKAGE_RESOLVE:-}" == "ubuntu-generic-modules" && "${GOMI_KERNEL_PACKAGE}" == "linux-image-generic" ]]; then
    image_pkg="$(apt-cache depends linux-image-generic | awk '/Depends: linux-image-[0-9].*-generic/ { print $2; exit }')"
    if [[ -z "${image_pkg}" ]]; then
      echo "failed to resolve linux-image-generic dependency" >&2
      exit 1
    fi
    kernel_version="${image_pkg#linux-image-}"
    sudo apt-get install -y --no-install-recommends \
      "${image_pkg}" \
      "linux-modules-${kernel_version}" \
      "linux-modules-extra-${kernel_version}"
  else
    sudo apt-get install -y --no-install-recommends "${GOMI_KERNEL_PACKAGE}"
  fi
  sudo mkdir -p /curtin
  printf '%s' "${GOMI_KERNEL_PACKAGE}" | sudo tee /curtin/CUSTOM_KERNEL >/dev/null
else
  sudo mkdir -p /curtin
fi

sudo rm -f /etc/netplan/50-cloud-init.yaml
sudo sed -i 's/^root:[^:]*/root:*/' /etc/shadow
sudo rm -rf /root/.ssh /root/.cache
sudo rm -f /etc/ssh/ssh_host_*
sudo apt-get clean
sudo rm -rf /var/lib/apt/lists/*
sudo cloud-init clean --logs
sudo truncate -s 0 /etc/machine-id
sudo rm -f /var/lib/dbus/machine-id
