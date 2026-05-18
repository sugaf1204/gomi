# GOMI bootenv

`bootenv/` builds the lightweight PXE deploy runtime used by GOMI. The GOMI
server consumes the generated release-style artifacts and does not rebuild the
SquashFS locally.

The boot environment is built with mkosi using `mkosi.conf`. The deploy logic
inside the rootfs is `scripts/gomi-deploy-runner`, a shell script that posts
hardware inventory, then either runs curtin for SquashFS target images or writes
a qcow2 whole-disk image directly with `qemu-img convert`.

## Local commands

```sh
make test
make validate
make build
```

A build emits:

- `boot-kernel`
- `boot-initrd`
- `rootfs.squashfs`
- `boot.ipxe`
- `manifest.json`
- `checksums.txt`

GOMI can consume either the directory path or a URL that resolves to
`manifest.json` via `GOMI_BOOTENV_SOURCE_URL`.
