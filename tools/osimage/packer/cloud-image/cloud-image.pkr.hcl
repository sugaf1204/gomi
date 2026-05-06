packer {
  required_plugins {
    qemu = {
      source  = "github.com/hashicorp/qemu"
      version = ">= 1.1.0"
    }
  }
}

variable "architecture" {
  type = string
}

variable "apt_packages" {
  type    = list(string)
  default = []
}

variable "curtin_kernel_package" {
  type    = string
  default = ""
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

variable "output_directory" {
  type = string
}

variable "ovmf_code" {
  type = string
}

variable "ovmf_vars" {
  type = string
}

variable "qemu_cpu" {
  type = string
}

variable "qemu_machine" {
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

variable "source_checksum" {
  type = string
}

variable "source_format" {
  type    = string
  default = "qcow2"
}

variable "source_url" {
  type = string
}

variable "timeout" {
  type    = string
  default = "30m"
}

locals {
  qemu_arch = {
    amd64 = "x86_64"
    arm64 = "aarch64"
  }
}

source "qemu" "cloud_image" {
  boot_wait              = "10s"
  cpus                   = 2
  disk_image             = true
  disk_size              = var.disk_size
  format                 = "qcow2"
  headless               = var.headless
  iso_checksum           = var.source_checksum
  iso_url                = var.source_url
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
    create = ["-F", var.source_format]
  }

  qemuargs = [
    ["-machine", var.qemu_machine],
    ["-cpu", var.qemu_cpu],
    ["-device", "virtio-gpu-pci"],
    ["-drive", "if=pflash,format=raw,id=ovmf_code,readonly=on,file=${var.ovmf_code}"],
    ["-drive", "if=pflash,format=raw,id=ovmf_vars,file=${var.ovmf_vars}"],
    ["-drive", "file=${var.output_directory}/${var.image_name}.qcow2,format=qcow2"],
    ["-drive", "file=${var.seed_iso},format=raw,if=virtio"],
  ]
}

build {
  sources = ["source.qemu.cloud_image"]

  provisioner "shell" {
    environment_vars = [
      "DEBIAN_FRONTEND=noninteractive",
      "GOMI_APT_PACKAGES=${join(" ", var.apt_packages)}",
      "GOMI_CURTIN_KERNEL_PACKAGE=${var.curtin_kernel_package}",
    ]
    scripts = ["${path.root}/scripts/provision.sh"]
  }
}
