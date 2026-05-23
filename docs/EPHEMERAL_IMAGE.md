# GOMI Boot Environment

## Usage

GOMI consumes deployment-time boot environments as prebuilt release-style
artifacts. OS images are registered separately as prebuilt artifacts:

```sh
curl -H "Authorization: Bearer $GOMI_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"debian-13","osFamily":"debian","osVersion":"13","arch":"amd64","format":"qcow2","source":"url","url":"https://images.example/debian-13-amd64.qcow2"}' \
  http://gomi.example/api/v1/os-images
```

GOMI does not build target OS images. Target OS images are produced outside this
repository, for example by the mkosi-based `os-images` pipeline, then registered
with the OS image API as `qcow2`. Bare-metal `qcow2` images are whole-disk
images and require manifest partition metadata so the deploy runtime can inject
NoCloud seed data after writing the disk.

For boot environments, GOMI fetches `manifest.json`, verifies the declared
SHA256/size for each artifact, and publishes:

- `files/linux/boot-kernel`
- `files/linux/boot-initrd`
- `files/linux/rootfs.squashfs`

The boot environment builder lives in `bootenv/`. It uses mkosi plus shell
scripts to build kernel/initrd/SquashFS assets and emits:

- `manifest.json`
- `checksums.txt`
- `boot-kernel`
- `boot-initrd`
- `rootfs.squashfs`

Point GOMI at a local build directory or release asset base URL with:

```sh
GOMI_BOOTENV_SOURCE_URL=bootenv/dist/ubuntu-minimal-cloud-amd64
```

The GOMI Taskfile delegates convenience commands to `bootenv/`:

```sh
task bootenv:validate
task bootenv:test
task bootenv:build
```

## Scope

The target is the lightweight image that runs during PXE deployment. It is not
the final image written to disk.

## Findings

- The useful pattern is split PXE boot artifacts: a normal kernel, a normal
  initrd, and a compressed SquashFS root filesystem used only for deployment.
  GOMI should own that contract directly instead of depending on another
  provisioning system's image distribution format.
- Debian Live provides an upstream distro live-boot stack where `boot=live`
  activates live boot and `fetch=` can retrieve the compressed filesystem over
  HTTP:
  https://manpages.debian.org/unstable/live-boot-doc/live-boot.7.en.html
- Debian publishes official amd64 live ISO images with a basic `standard`
  flavor and checksum files:
  https://cdimage.debian.org/debian-cd/current-live/amd64/iso-hybrid/
- Tinkerbell explicitly has HookOS, a lightweight in-memory OS used to run
  provisioning workflows:
  https://tinkerbell.org/homepage/
- HookOS uses LinuxKit to build a swappable kernel/initramfs OSIE. It is a good
  precedent for an in-memory deploy runtime, but an all-in-one initrd is not the
  right default for GOMI:
  https://github.com/tinkerbell/hook
- LinuxKit is a toolkit for building minimal immutable Linux systems from a YAML
  file made of a kernel, init system, onboot containers, services, and files:
  https://github.com/linuxkit/linuxkit
- OpenStack Ironic has the same deploy-ramdisk model and can boot user-provided
  ramdisks/ISOs. Its builder path is tied to Ironic/DIB elements:
  https://docs.openstack.org/ironic/wallaby/admin/ramdisk-boot.html
- `bootc-image-builder` now has a `pxe-tar-xz` stateless PXE output, but it is
  centered on bootc/RPM image mode:
  https://osbuild.org/docs/bootc/
- Debian live-build and KIWI NG can build netboot/KIS/live images, but each is
  distro-family-specific and does not provide a generic deployment-agent
  contract:
  https://live-team.pages.debian.net/live-manual/html/live-manual.en.html
  https://documentation.suse.com/en-us/appliance/kiwi-9/html/kiwi/building-types.html

## Decision

Use a split kernel/initrd/SquashFS rootfs model for the deploy runtime, not an
all-in-one initrd.

`bootenv/` owns the runtime build:

- input: `mkosi.conf` plus shell scripts;
- mutation: install the shell GOMI deploy runner and initramfs timing hooks;
- output: release-style kernel, initrd, SquashFS, iPXE, manifest, and checksums;
- runtime: GOMI deploy agent inside a SquashFS ephemeral rootfs;

GOMI owns only consumption:

- input: registered prebuilt `squashfs` or `qcow2` OS images;
- bootenv source: `GOMI_BOOTENV_SOURCE_URL`, either a local directory or HTTP(S)
  base;
- verification: manifest name plus SHA256/size for kernel/initrd/rootfs;
- output: versioned artifact directory plus stable PXE compatibility links.

This makes the GOMI use case just one consumer instead of hard-coding GOMI or
curtin assumptions into the image builder. The boot environment release asset
base URL defaults to this repository's GitHub Release download path:
`https://github.com/sugaf1204/gomi/releases/latest/download`. Local development
can still point `GOMI_BOOTENV_SOURCE_URL` at this repository's `bootenv/` build
output directory.
