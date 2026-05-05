#!/usr/bin/env bash
set -euxo pipefail

if [[ -n "${GOMI_KERNEL_PACKAGE:-}" ]]; then
  sudo apt-get update
  sudo apt-get install -y --no-install-recommends "${GOMI_KERNEL_PACKAGE}"
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
