# GOMI bootenv

`bootenv/` builds the lightweight PXE deploy runtime used by GOMI. It lives in
the GOMI monorepo, while GOMI server still consumes verified release-style
artifacts instead of rebuilding SquashFS locally.

The default boot environment is based on the Ubuntu Minimal cloud SquashFS. The
builder installs `cloud-initramfs-rooturl`, `cloud-initramfs-copymods`, and
`overlayroot` into the rootfs, generates a PXE-capable initrd, and publishes
`kernel/initrd/rootfs` as separate release assets.

The deploy logic injected into the rootfs is `cmd/gomi-deploy-runner`, a static
Go binary. It posts `api/inventory.HardwareInventory` JSON to GOMI, downloads
the curtin config returned by `/pxe/inventory`, and executes `curtin -c
<config> install`.

## Local commands

```sh
make test
make build-runner
make validate
make render
make build
```

If TinyGo is installed, the runner can also be built with:

```sh
make build-runner-tinygo
```

The legacy Debian Live definition remains available:

```sh
make build SPEC=bootenvs/debian-live-standard-amd64.yaml OUT=dist/debian-live-standard-amd64
```

A build emits:

- `boot-kernel`
- `boot-initrd`
- `rootfs.squashfs`
- `boot.ipxe`
- `manifest.json`
- `checksums.txt`

GOMI can consume either the directory path or a URL that resolves to `manifest.json` via `GOMI_BOOTENV_SOURCE_URL`.
