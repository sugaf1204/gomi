# Windows PXE lab (manual activation)

This is a manual E2E procedure, not evidence of a completed Windows boot. Use the
ordinary OSImage and Machine APIs/UI; direct database edits do not qualify.
The existing Linux lab runner uses virtio disks and must not be reused for a
Windows target without a separate target configuration.

## Prerequisites and isolation

- A dedicated GOMI server VM connected only to `br-gomi-lab` for PXE/DHCP, as in
  the existing lab. Never bind its DHCP service to the physical LAN.
- Existing-host KVM, QEMU, OVMF, swtpm, sufficient memory, and 64 GiB free disk.
  Installing host packages or changing bridges requires reviewing the host
  impact first. Do not run the full Linux lab playbook just to probe readiness.
- Operator-supplied Windows amd64 `install.wim`, its image index, and compatible
  WinPE `boot.wim`, BCD, `boot.sdi`, plus wimboot. Prepare boot.wim using the
  [Windows boot environment instructions](../../../windows/README.md). GOMI does not download Windows media.
- Use authorized Windows media. A key is optional during deploy; activation and
  licensing must be handled on the installed Windows machine afterwards.

Initial read-only node1 check on 2026-10-17, before lab setup: `node1.canvm.jp` is physical, CPU exposes
`svm`, `/dev/kvm` exists (root:kvm, 0660), passwordless sudo works. The `ubuntu`
user is not in kvm; QEMU and virsh were absent. No ISO/WIM/SDI/wimboot was found
under `/var/tmp`, `/tmp`, `/opt` to depth 4. This is a limited inventory, not an
exhaustive filesystem search. No packages, bridge or VM were changed during that
initial check. The subsequently prepared environment is described in
[`WINDOWS_NODE1.md`](WINDOWS_NODE1.md).

## Register media through GOMI

In OS Images create an image with OS family `windows`, architecture `amd64`,
format `wim`, and this manifest (substitute the correct edition index):

```json
{
  "bootModes": ["uefi"],
  "capabilities": {"deployTargets": ["vm", "baremetal"]},
  "root": {"format": "wim", "path": "install.wim"},
  "windows": {
    "imageIndex": 1,
    "winPE": {"wimboot": "wimboot", "bcd": "BCD", "bootSDI": "boot.sdi", "bootWIM": "boot.wim"}
  }
}
```

Select Upload, install.wim and the four separate boot assets. Windows URL
registration is unsupported. Verify the image reports Ready before deploy. The API equivalents are POST `/api/v1/os-images`,
POST `/api/v1/os-images/{id}/upload` (multipart `file`), and POST
`/api/v1/os-images/{id}/upload-artifact` (multipart `path` and `file`) once per
boot asset. Authenticate normally; never write access tokens to tracked files.

## Isolated Machine target

Use a new disposable VM disk and a dedicated MAC, for example
`52:54:00:77:11:01`. Register a Machine through the normal form with amd64,
UEFI, the Windows image, the lab subnet, DHCP, and manual power control. Do not
set Cloud-Init, SSH keys, Linux login user, hypervisor role or static networking.
Leave Target Disk empty. The Windows deployment contract requires exactly one
fixed disk, selects that disk, and erases it. Additional fixed disks cause an
early failure; no Linux device-path selector is supported.

The following target configuration illustrates the required device choices.
It assumes the already-reviewed isolated bridge exists. Run in a dedicated
lab directory as an account allowed to use KVM and TAP; substitute OVMF paths
for the installed distribution. Ports and TAP names must be unused.

```bash
mkdir -p windows-target/tpm
qemu-img create -f qcow2 windows-target/disk.qcow2 64G
cp /usr/share/OVMF/OVMF_VARS_4M.fd windows-target/OVMF_VARS.fd
swtpm socket --tpm2 --tpmstate dir=windows-target/tpm \
  --ctrl type=unixio,path=windows-target/tpm.sock --daemon
sudo ip tuntap add dev gwindows0 mode tap
sudo ip link set gwindows0 master br-gomi-lab
sudo ip link set gwindows0 up
sudo qemu-system-x86_64 -enable-kvm -machine q35 -cpu host -smp 4 -m 8192 \
  -drive if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd \
  -drive if=pflash,format=raw,file=windows-target/OVMF_VARS.fd \
  -drive if=none,id=osdisk,format=qcow2,file=windows-target/disk.qcow2 \
  -device ide-hd,drive=osdisk,bus=ide.0 \
  -netdev tap,id=pxe,ifname=gwindows0,script=no,downscript=no \
  -device e1000e,netdev=pxe,mac=52:54:00:77:11:01 \
  -chardev socket,id=tpm,path=windows-target/tpm.sock \
  -tpmdev emulator,id=tpm0,chardev=tpm -device tpm-tis,tpmdev=tpm0 \
  -boot order=cn -vnc 127.0.0.1:11 -monitor unix:windows-target/monitor.sock,server,nowait \
  -serial file:windows-target/serial.log
```

An empty disk falls through to network boot, while a successfully installed
Windows disk boots first after restart. Access VNC through an SSH tunnel; do not
bind it publicly. Do not disable TPM or bypass Windows hardware requirements.

## Acceptance evidence

1. Initiate deploy through the normal GOMI Machine operation. Capture Machine
   status and ordered deployment events; no key entry should be requested.
2. Observe WinPE startup, selected disk and image index, installation, reboot,
   and handoff to installed Windows. WIM transfer alone is not success; GOMI
   must remain provisioning while interactive initial setup is unfinished.
3. Complete Windows initial setup on the console, then wait for its real
   completion callback. Record OS version/edition, disk layout, network
   connectivity and GOMI completion separately.
4. Reboot twice. Verify local Windows boot and no repeated disk clearing/PXE
   installation. Record activation state separately; enter the key manually
   from Windows Settings to test the supported activation workflow.
5. Test missing boot artifact, invalid image index and interrupted transfer;
   failures must not produce Ready. Confirm a retry has explicit destructive
   semantics and cannot restart a completed deployment unexpectedly.
6. Run an existing Linux target through its ordinary lab procedure to cover
   the unaffected PXE path. VM resource deployment is a separate test from this
   Machine-on-KVM procedure and must not be claimed from this result.

Stop the disposable QEMU and its swtpm process, then remove only `gwindows0` and
this target's directory after collecting evidence. Preserve the server VM,
bridge and other existing targets unless they were created for this run.
