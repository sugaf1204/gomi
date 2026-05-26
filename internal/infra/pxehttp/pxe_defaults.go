package pxehttp

// Deprecated: preseed-based provisioning is being phased out in favor of
// curtin/cloud-init. The default below intentionally creates no user; an
// inline preseed must be supplied by the caller for any usable install.
const defaultDebianPreseed = `# Locale and keyboard
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

# Users (root locked, no default user; supply via inline preseed)
d-i passwd/root-login boolean false
d-i passwd/make-user boolean false

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
d-i pkgsel/include string qemu-guest-agent sudo curl wget ca-certificates vim less net-tools iproute2 git tmux htop dnsutils
d-i popularity-contest popularity-contest/participate boolean false

# Bootloader and serial console
d-i grub-installer/only_debian boolean true
d-i grub-installer/bootdev string /dev/vda
d-i debian-installer/add-kernel-opts string console=ttyS0,115200n8

d-i preseed/late_command string in-target systemctl enable serial-getty@ttyS0.service

# Finish
d-i finish-install/reboot_in_progress note
d-i debian-installer/exit/poweroff boolean true
`

const defaultLinuxCurtinUserData = `#cloud-config
hostname: gomi-pxe
manage_etc_hosts: true
users:
  - default
ssh_pwauth: false
runcmd:
  - systemctl enable serial-getty@ttyS0.service || true
`

const defaultAutoinstallUserData = `#cloud-config
autoinstall:
  version: 1
  locale: en_US.UTF-8
  keyboard:
    layout: us
  identity:
    hostname: gomi-pxe
    username: ubuntu
    password: "!"
  ssh:
    install-server: true
    allow-pw: false
  storage:
    layout:
      name: direct
`

const defaultNoCloudVendorData = `#cloud-config
{}
`
