# node1 Windows lab handoff

This is the isolated lab prepared on 2026-10-17. Official Windows installation
media has been downloaded and verified. Final raw Machine and VM attempts passed
ordinary Windows PXE deployment, interactive OOBE, real completion callbacks,
and two post-completion boots/logins. Permanent activation has not been verified.

## Access and layout

- Host: `ubuntu@node1.canvm.jp` (`192.168.2.101`).
- Working directory: `/var/tmp/gomi-windows-lab` (private to the lab operator).
- Media intake: `/var/tmp/gomi-windows-lab/media`.
- Bridge: `br-gomi-win`, with no physical NIC, host IP, or host IPv4 route.
- GOMI server VM: Ubuntu 24.04, 4 vCPU, 4 GiB RAM, 100 GiB sparse disk.
- Server management: node1 `127.0.0.1:11022` (SSH), `127.0.0.1:18081` (HTTP).
- PXE network: `10.78.0.0/24`; server `10.78.0.1`, DHCP `.100`–`.150`.
- DHCP is bound to the server VM's `pxe0`. Internet NAT lives inside that VM.
- Windows target: 4 vCPU, 8 GiB RAM, 100 GiB single SATA disk, e1000e,
  amd64 UEFI, TPM 2.0, MAC `52:54:00:77:11:01`.
- Windows target completed validation and remains at the installed desktop.
  Its VNC listener is node1 `127.0.0.1:5911`.
- QEMU/OVMF/TPM start scripts and diagnostic results are in the working directory.
- Admin username/password and current session token are in `access.json`, mode
  `0600`. Do not copy that file into this repository. Re-run
  `gomi-winlab-bootstrap.py` on node1 to refresh an expired session.

An SSH tunnel was opened on the operator Mac for HTTP `127.0.0.1:18081` and VNC
`127.0.0.1:15911`. To establish an equivalent connection elsewhere:

```sh
ssh -N -L 127.0.0.1:18081:127.0.0.1:18081 \
  -L 127.0.0.1:15911:127.0.0.1:5911 ubuntu@node1.canvm.jp
```

From node1, connect to the server using:

```sh
ssh -F /var/tmp/gomi-windows-lab/ssh-config gomi-win-server
```

## VM resource path

A minimal libvirt daemon is available only on node1 `127.0.0.1:16509`; there are
no libvirt virtual networks. GOMI reaches it using QEMU user networking's
`10.0.2.2:16509`. The normal API registered Hypervisor
`hypervisors/node1-winlab`, bridge `br-gomi-win`.

The host had no prior libvirt configuration. Lab setup added the
`libvirt-qemu` system user/group, a directory storage pool named `default` at
`/var/lib/libvirt/images`, `/etc/libvirt/qemu.conf`, and transient units
`gomi-win-libvirtd` and `gomi-win-virtlogd`. This daemon uses root QEMU processes
and no libvirt security labeling, for this local test environment only. The
standalone swtpm AppArmor profile remains enabled. Do not expose its TCP port to
the LAN or reuse these settings as a general hypervisor configuration.

Raw target TPM state uses `/var/lib/swtpm/gomi-windows-lab/` because the packaged
AppArmor policy permits that directory. No AppArmor policy was disabled or edited.

## Verified before Windows media

- GOMI API health and authenticated subnet access.
- GOMI server VM reboot restores GOMI, DHCP/TFTP, and guest NAT.
- Separate Ubuntu smoke guest boots using UEFI, TPM 2.0, SATA, and e1000e.
- Smoke guest receives `10.78.0.100` from GOMI DHCP, uses `10.78.0.1` as gateway,
  and reaches external HTTPS using DHCP-provided DNS.
- Smoke guest transfers the complete `ipxe.efi` through TFTP; its SHA-256 matches
  the server file. Evidence: `smoke-result.json` in the working directory.
- Product-generated Windows UEFI/TPM/SATA/e1000e domain XML was defined and
  started on libvirt 8.0 / QEMU 6.2 without OS media. The product executor then
  removed the domain, TPM state, NVRAM, and disk; all four removals were checked.
  This exposed and fixed older-libvirt TPM flags and inactive auto-EFI NVRAM
  detection. It does not establish that Windows boots.
- The normal GOMI API registered the official Ubuntu 24.04 cloud image with its
  SHA-256, created a Linux VM on `hypervisors/node1-winlab`, received its `curtin`
  completion callback, and reached `Running`. SSH login as `ubuntu` and guest
  access to GOMI succeeded. Normal API deletion returned 204, subsequent GET
  returned 404, and the domain/VM disk were removed. The reusable image and SSH
  key remain registered for subsequent regression checks; the Hypervisor is Ready.
  Evidence: `linux-api-smoke-final.jsonl` and `linux-api-delete.json`.
- node1 kubelet and CRI-O remained active; its physical NIC and IPv4 routes were
  not changed. No packages or services of the existing cluster were restarted.

These establish environment readiness and Linux VM cloud-image deployment,
not Windows PXE E2E or Linux Machine/curtin PXE E2E.
The host lab processes and transient libvirt units do not automatically recover
after a node1 reboot. The server VM's own services do recover after a guest reboot.

## Media and artifact verification

The host-wide ISO search found no existing Windows media. The official Windows
11 Enterprise Evaluation x64 en-US ISO was therefore downloaded into
`downloads/windows11-enterprise-eval-en-us-x64.iso` (8,225,329,152 bytes).
Both aria2 verification and an independent `sha256sum` matched Microsoft's
published SHA-256:
`BC3F24086EBADC94489066B5AD78089E2CF5C3491E90E790BB81A2B199C10E38`.
Sources: [Microsoft evaluation download](https://www.microsoft.com/en-us/evalcenter/download-windows-11-enterprise)
and [published ISO checksums](https://support.microsoft.com/en-us/servicing/os/windows/docs/2026/09/verify-the-authenticity-of-a-windows-11-enterprise-evaluation-iso-file).
Read-only mounted WIM metadata reports one image, index 1, EnterpriseEval,
x86_64, build 26300.9457. The verified marker is
`downloads/windows-iso.sha256-verified`.

A separate temporary `gomi-windows-builder` QEMU VM installed this ISO and
reached Audit mode to run the official ADK and matching WinPE add-on. It has its own
100 GiB disk, 4 vCPU, 8 GiB RAM, TPM 2.0, and user networking; it is not connected
to the PXE bridge. Its VNC is host loopback port 5913. Builder-only unattended
setup is not product deployment or OOBE completion evidence. Private builder
credentials remain under `builder/`, outside the repository. A token-protected
loopback upload receiver (`gomi-win-builder-receiver`, port 18082) received the
artifacts into `media/`; inspect `media/builder-result.json` for build status.
That receiver is stopped now.

The first builder reached Audit logon but its bootstrap did not start.
`Windows/Panther/UnattendGC/setuperr.log` identified an invalid
`RunSynchronousCommand/Path` (`0x80004005`); the encoded command was 622
characters, beyond the [documented 259-character limit](https://learn.microsoft.com/en-us/windows-hardware/customize/desktop/unattend/microsoft-windows-deployment-runsynchronous-runsynchronouscommand-path).
The temporary answer-file generator now uses a 185-character command and asserts
that limit. Recovery runs that short bootstrap from the temporary builder's
administrative console, without reinstalling Windows. This is builder recovery,
not product deployment evidence; the product's specialize command is shorter
than the limit.

Artifact build and transfer completed at `2026-10-17T14:49:32Z`. The Microsoft
signed ADK and matching WinPE add-on are version `10.1.26100.9457`. DISM added
the WMI, NetFX, Scripting, and PowerShell optional components and committed the
image successfully. Actual Windows PowerShell 5.1 parsing found no syntax errors
in `install.ps1`, `complete.ps1`, or `Build-WinPE.ps1`. The mounted WinPE's
`Windows/INF/net1ix64.inf` includes `PCI\VEN_8086&DEV_10D3` for e1000e; no
additional driver was injected.
The temporary builder and its dedicated TPM were subsequently shut down; disks,
firmware, and evidence are retained. Its auxiliary ISO was regenerated only
after shutdown, with the corrected 185-character startup command.

Independent node1 SHA-256 verification matched all five assets and the manifest:

- `install.wim`: 7,333,902,541 bytes,
  `ce6b8251e401d7f5c7c03c0d6cb5447b5aa67b2960d98b9f08da206ee267a1a9`.
- `boot.wim`: 499,165,909 bytes,
  `827c217869c5194804fd5971655130cc4fb4d36f654f1d5d674cfa1d62be4756`.

Evidence is under `builder/`: `artifact-verification.json`,
`powershell-parser-evidence.json`, `winpe-driver-evidence.json`, and
`unattend-path-error.png`; the uploaded transcript is `media/builder.log`.
These prove artifact construction, not product PXE deployment.

The first product PXE runs reached WinPE, passed the NIC/disk checks, created
GPT partitions, and claimed their attempts. WIM download then wrote only about
0.3 MB/s. A Windows-only change sets PowerShell's `ProgressPreference` to
`SilentlyContinue`; native diskpart/DISM output and GOMI stage callbacks remain
enabled. After deploying binary
`6129a31399b9b1dac40b88de8402564200dba0218718530fd4662e5b42631c91`, both
targets restarted through normal redeploy APIs with fresh attempts. During the
new WIM download, VM disk writes measured approximately 44.7 MB/s, and the raw
Machine target measured 47.87 MiB/s over 20.42 seconds. These are disk write
counter measurements during download, not independent network throughput.
Evidence: `windows-target/download-performance.json` and the VM comparison
`windows-vm-progress-comparison.json`. PXE HTTP tests passed; the preference is
confined to the Windows installer script and does not change Linux paths or
native tool output.

Two later runtime findings are not successful acceptance results:

- A Windows VM's live QEMU initially permitted only network boot. Changing its
  persistent XML to disk boot did not change the live process, and its installer
  reboot eventually reached UEFI Shell. Windows VM creation now allows network
  followed by disk; Linux keeps its existing boot order. Boot-device rewriting
  removes duplicate entries. libvirt/VM/API and full Go tests passed; normal
  redeployment must verify the resulting live boot sequence.
- The raw Machine reported Ready while interactive OOBE was still displayed.
  That run is explicitly FAIL (`windows-target/oobe-false-positive.json`), not
  completed Windows deployment. During the actual license screen, a read-only
  native API probe returned `OOBEComplete=False` while the old predicates were
  `SystemSetupInProgress=0`, `OOBEInProgress=0`, and
  `ImageState=IMAGE_STATE_COMPLETE`. The callback now additionally requires a
  successful, true result from Microsoft's OOBE API and retries on query failure.
  A fresh ordinary Machine attempt then remained Provisioning for more than a
  minute at interactive OOBE. After normal local-account/privacy setup and the
  offered Update later action, it reached the desktop and reported Ready at
  `2026-10-17T15:59:11.421663862Z`, with completion source `windows` and attempt
  `attempt-246a6b01fe0e829ad2af9fa3c366ee16` unchanged. Evidence:
  `windows-target/oobe-pending-native-api.json`,
  `windows-target/oobe-pending-native-api.ppm`,
  `windows-target/desktop-after-oobe.ppm`, and
  `windows-machine-oobe-api-status.jsonl`. This supersedes the failed run only
  for the OOBE/callback check; VM acceptance remains a separate check.

The raw Machine subsequently completed two normal guest restarts and password
logins without entering WinPE or changing its attempt/completion timestamp.
`Get-Partition` reports 260 MiB System, 16 MiB Reserved, and 99.73 GiB C: Basic;
`Win32_OperatingSystem` reports build 26300 / version 10.0.26300. DHCP gives
10.78.0.101, gateway 10.78.0.1, and DNS 1.1.1.1 / 8.8.8.8. A read-only native
query reports `OOBEComplete=True`. EnterpriseEval reports LicenseStatus 1
(Licensed), with the desktop explicitly identifying the 90-day evaluation;
this is not permanent product-key activation evidence. Evidence:
`windows-target/reboot1-desktop.ppm`, `reboot2-lockscreen.ppm`,
`reboot1-status.jsonl`, `reboot2-status.jsonl`, `final-status.jsonl`,
`installed-os-partitions.ppm`, and `installed-network-licensing-oobe.ppm`.
The normal Settings > System > Activation > Change product key action opens
the manual entry dialog (`windows-target/manual-product-key-dialog.ppm`). No
key was entered, and the dialog was cancelled. This verifies availability of
the post-deploy entry flow, not successful activation with a purchased key.

The VM then exposed a separate completion-timer bug. Its task existed and had
run during OOBE, but after clock correction the next run was hours ahead:
`StartBoundary=2026-10-17T15:44:24-07:00` while the guest displayed about 09:15.
This was already offset-qualified, so changing only the timezone suffix would
not fix the wrong registration-time clock. A normal guest restart invoked the
startup trigger and produced the real callback at
`2026-10-17T16:22:44.783411875Z`; this is diagnostic evidence, not acceptance
of the delayed notification. Evidence: `windows-vm-task-timezone-diagnostic.ppm`,
`windows-vm-task-boundary-diagnostic.ppm`, and
`windows-vm-startup-diagnostic-callback.json`.

Registration now uses repeating Registration/Boot/Logon event triggers with no
absolute boundary. The guest also relies on server expiry rather than its
potentially incorrect wall clock. Windows Task Scheduler's native
`TASK_VALIDATE_ONLY` accepted the actual XML without creating a task (evidence
`windows-target/registration-xml-native-validator-pass.ppm`). Fresh ordinary
Machine and VM attempts using binary
`b1dea03f8a1c8c9cecfa0c5186adfa01be6a525f94edda1567aa7010f3f0c414`
completed acceptance; the embedded UI serves `/assets/index-b7acXfpB.js`.

## Final acceptance

Both normal deployments used the verified Windows 11 Enterprise Evaluation WIM,
without a supplied product key or OOBE bypass. Neither required a manual task run
or an extra restart to produce its completion callback.

| Target | Attempt | Real completion UTC | Final state |
| --- | --- | --- | --- |
| Machine `winlab-raw` | `attempt-afbf5a72a603fe084aa0e7251c3ec837` | 2026-10-17 16:57:26.075444863Z | Ready, 10.78.0.101 |
| VM `winlab-winvm` | `attempt-6085064df3d0acc887a569f2ed48ac2f` | 2026-10-17 17:01:29.793304815Z | Running, 10.78.0.100 |

OOBE pending-state holds passed for both targets. The Machine then completed two
normal restarts/logins. The VM completed a normal guest restart/login and a clean
Windows shutdown followed by normal GOMI `:powerOn` and login. Attempt IDs and
completion timestamps remained unchanged; no disk wipe or installation replay
occurred. VM XML proves initial boot order `network, hd` and cold-start order
`hd`. Summaries: `windows-target/final-acceptance.json` and
`windows-vm-final-e2e-summary.json`, with screenshots and API readbacks listed
inside those files.

The final server binary SHA-256 is
`97099e31e1bde1d1bb7798bfdaf3c3502ebd30ac17ac5884a040493504483760`.
Follow-ups after the full deploy runs only added terminal HTTP 404 cleanup and
early rejection of whitespace-padded Windows family metadata. Actual Windows
PowerShell 5 tests execute the completion script with fake OS/task/HTTP interfaces:
404/410 remove credentials/tasks, while 409/500 retain retry state. They passed
without touching real tasks or sending callbacks. Full Go tests, 266 frontend
tests, frontend build, native task XML validation, and diff checks passed.

The notification changes are Windows-only; Windows boot fallback is selected by
typed install configuration. Linux boot defaults remain unchanged. The earlier
Ubuntu API deployment/delete smoke and Fedora/other-family regression tests
cover representative unaffected paths; full Linux Machine/curtin PXE E2E was not
run. Permanent product-key activation, physical hardware, and Secure Boot remain
outside this live acceptance coverage.

Both installed targets and the GOMI server remain available for inspection.
The temporary builder/TPM, upload receiver, and diagnostic HTTP service are
stopped. No credentials or installation media are stored in this repository.

## Deploy the verified artifacts

The verified artifacts already reside in `media`: `install.wim`, `boot.wim`,
`BCD`, `boot.sdi`, `wimboot`, and `manifest.json`.
Use the normal image registration/upload procedure in
[`windows/README.md`](../../../windows/README.md). All five assets must be
present and the image must be Ready. Do not edit the database or force Ready.

The image is already registered as `osImages/windows11-enterprise-eval-amd64`
and Ready. The raw target is registered as `machines/winlab-raw`, with DHCP and
manual power control. New target registration starts an attempt, so register
only after the image is ready. For an existing target, use the normal redeploy
action and manually select **UEFI PXE IPv4** for that boot in its firmware Boot
Manager. Reset alone can boot the existing Windows disk instead; manual power
control cannot change firmware on the operator's behalf. Complete OOBE using
VNC. GOMI must remain provisioning until
the installed Windows callback succeeds. Activation is a separate manual step.

For the VM resource path, create a separate Windows VM through GOMI using
`hypervisors/node1-winlab`. Do not register the same MAC as both a Machine and
a VirtualMachine. Follow the acceptance checks in [`WINDOWS.md`](WINDOWS.md).
