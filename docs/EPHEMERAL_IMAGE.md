# GOMI Boot Environment

## Usage

GOMI consumes deployment-time boot environments as prebuilt release-style
artifacts. The public flow is catalog driven:

```sh
curl -H "Authorization: Bearer $GOMI_TOKEN" \
  -X POST http://gomi.example/api/v1/os-catalog/debian-13-amd64-baremetal/install
```

Installing a catalog entry downloads a prebuilt raw OS image artifact into GOMI
storage and ensures the referenced boot environment exists. Release assets are
distributed as `.raw.zst` by default and expanded to local `.raw` files during
catalog install. Catalog entries are variant-qualified, for example
`ubuntu-22.04-amd64-cloud` and `ubuntu-22.04-amd64-baremetal`. The `cloud`
variant preserves the upstream cloud image package set. The `baremetal` variant
is still built offline, but preinstalls the kernel module and firmware packages
needed by physical target machines before publishing the release asset.

GOMI does not convert qcow2 images, mount raw disks, install packages into the
target OS, or otherwise mutate target OS images from the API process. Catalog
sources are raw artifacts only; image conversion, curtin-specific image
preparation, and variant-specific package installation belong in an
offline/release build path. The release image workflow injects an empty
`/curtin` directory before publishing each raw artifact so curtin can recognize
the dd-installed target root filesystem during the extract stage.

For OS image catalog artifacts, override the release asset base URL with:

```sh
GOMI_OS_IMAGE_SOURCE_URL=https://example.invalid/gomi-os-images
```

For boot environments, GOMI fetches `manifest.json`, verifies the declared
SHA256/size for each artifact, and publishes:

- `files/linux/boot-kernel`
- `files/linux/boot-initrd`
- `files/linux/rootfs.squashfs`

The boot environment builder lives in `bootenv/`. It builds Ubuntu Minimal
cloud SquashFS based kernel/initrd/SquashFS assets and emits:

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
task bootenv:plan
task bootenv:render
task bootenv:build-runner
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

Use a split kernel/initrd/SquashFS rootfs model as the first backend, not an
all-in-one initrd, Packer target-image flow, or a direct shell/Go port.

`bootenv/` owns the runtime build:

- input: a generic `BootEnvironment` spec;
- source: Debian Live ISO kernel, initrd, and SquashFS rootfs by default;
- mutation: build and inject the Go GOMI deploy runner plus declared rootfs
  files/services;
- output: release-style kernel, initrd, SquashFS, iPXE, manifest, and checksums;
- runtime: GOMI deploy agent inside a SquashFS ephemeral rootfs;
- initramfs: the thin distro live-boot initrd, not a custom all-in-one initrd;

GOMI owns only consumption:

- input: supported OS catalog entry and boot environment name;
- OS image source: `GOMI_OS_IMAGE_SOURCE_URL`, an HTTP(S) base containing
  prebuilt variant-qualified `.raw.zst` or `.raw` artifacts;
- bootenv source: `GOMI_BOOTENV_SOURCE_URL`, either a local directory or HTTP(S)
  base;
- verification: manifest schema/name plus SHA256/size for kernel/initrd/rootfs;
- output: versioned artifact directory plus stable PXE compatibility links.

This makes the GOMI use case just one consumer instead of hard-coding GOMI or
curtin assumptions into the image builder. The boot environment release asset
base URL defaults to this repository's GitHub Release download path:
`https://github.com/sugaf1204/gomi/releases/latest/download`. Local development
can still point `GOMI_BOOTENV_SOURCE_URL` at this repository's `bootenv/` build
output directory.
