packer {
  required_plugins {
    qemu = {
      source  = "github.com/hashicorp/qemu"
      version = ">= 1.1.0"
    }
  }
}

variable "architecture" {
  type    = string
  default = "amd64"
}

variable "disk_size" {
  type    = string
  default = "8G"
}

variable "headless" {
  type    = bool
  default = true
}

variable "image_name" {
  type = string
}

variable "kernel_package" {
  type    = string
  default = ""
}

variable "output_directory" {
  type = string
}

variable "ovmf_code" {
  type = string
}

variable "ovmf_vars" {
  type = string
}

variable "seed_iso" {
  type = string
}

variable "ssh_password" {
  type    = string
  default = "packer"
}

variable "ssh_username" {
  type    = string
  default = "packer"
}

variable "timeout" {
  type    = string
  default = "30m"
}

variable "ubuntu_series" {
  type    = string
  default = "jammy"
}

locals {
  qemu_arch = {
    amd64 = "x86_64"
    arm64 = "aarch64"
  }
  qemu_cpu = {
    amd64 = "host"
    arm64 = "max"
  }
  qemu_machine = {
    amd64 = "accel=kvm"
    arm64 = "virt"
  }
}

source "qemu" "cloudimg" {
  boot_wait              = "10s"
  cpus                   = 2
  disk_image             = true
  disk_size              = var.disk_size
  format                 = "qcow2"
  headless               = var.headless
  iso_checksum           = "file:https://cloud-images.ubuntu.com/${var.ubuntu_series}/current/SHA256SUMS"
  iso_url                = "https://cloud-images.ubuntu.com/${var.ubuntu_series}/current/${var.ubuntu_series}-server-cloudimg-${var.architecture}.img"
  memory                 = 2048
  output_directory       = var.output_directory
  qemu_binary            = "qemu-system-${lookup(local.qemu_arch, var.architecture, "")}"
  shutdown_command       = "sudo -S shutdown -P now"
  ssh_handshake_attempts = 500
  ssh_password           = var.ssh_password
  ssh_timeout            = var.timeout
  ssh_username           = var.ssh_username
  ssh_wait_timeout       = var.timeout
  use_backing_file       = true
  vm_name                = "${var.image_name}.qcow2"
  vnc_bind_address       = "127.0.0.1"

  qemu_img_args {
    create = ["-F", "qcow2"]
  }

  qemuargs = [
    ["-machine", lookup(local.qemu_machine, var.architecture, "")],
    ["-cpu", lookup(local.qemu_cpu, var.architecture, "")],
    ["-device", "virtio-gpu-pci"],
    ["-drive", "if=pflash,format=raw,id=ovmf_code,readonly=on,file=${var.ovmf_code}"],
    ["-drive", "if=pflash,format=raw,id=ovmf_vars,file=${var.ovmf_vars}"],
    ["-drive", "file=${var.output_directory}/${var.image_name}.qcow2,format=qcow2"],
    ["-drive", "file=${var.seed_iso},format=raw,if=virtio"],
  ]
}

build {
  sources = ["source.qemu.cloudimg"]

  provisioner "shell" {
    environment_vars = [
      "DEBIAN_FRONTEND=noninteractive",
      "GOMI_KERNEL_PACKAGE=${var.kernel_package}",
    ]
    scripts = ["${path.root}/scripts/provision.sh"]
  }
}
