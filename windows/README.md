# Windows PXE deployment artifacts

The initial Windows path targets **Windows 11 amd64, UEFI, WIM images**. It
applies a selected image with DISM from a PowerShell-enabled WinPE environment,
creates GPT/EFI boot configuration, then boots Windows. Activation is a separate
step: do not supply a product key to GOMI; enter it later in Windows Settings →
System → Activation, or use an existing eligible digital license. GOMI does not
purchase a license, configure KMS, or guarantee activation.

The initial implementation leaves Windows OOBE interactive. Complete the user,
region, network, and other first-run prompts using the VM console or physical
console. GOMI must remain provisioning until installed Windows reports successful
setup completion. Applying the WIM or reaching OOBE alone is not completion.
The installed callback queries Windows' documented `OOBEComplete` API and waits
for both a successful query and its completion result before checking setup state
and reporting success. Registry `ImageState`, `SystemSetupInProgress`, and
`OOBEInProgress` alone are insufficient: the 26H2 lab reached their completed
values while the Region screen was still visible. An unavailable/failed API query
keeps the deployment pending and retries.
OOBE account and Internet requirements depend on the edition/build; provide the
connectivity and account required by the selected media. GOMI does not bypass
Microsoft account, TPM, or other Windows requirements.

This first path supports exactly one fixed non-USB disk and DHCP. It creates
EFI/MSR/Windows partitions; a dedicated Windows Recovery partition is not created.
The disk must hold both the compressed WIM and its expanded Windows image during
installation, so 64 GiB is only a minimum, not a guarantee for custom images.
Before erasing disk, WinPE claims the attempt in GOMI. An interrupted or
unacknowledged claim stops automatic installer replay: inspect the failure and
start a new deployment explicitly. Even a lost claim response can require a new
attempt. After image application, complete OOBE before the provisioning deadline;
the target task retries once a minute after registration, boot, and sign-in.
These event triggers have no absolute start/end boundary: the lab observed a
specialize-time absolute trigger scheduled hours ahead after OOBE clock correction.
The sign-in trigger re-arms retries after the interactive setup and clock change.
The guest never expires credentials using its own potentially incorrect clock.
After OOBE, success or the server's HTTP 404 token rejection or HTTP 410 expiry response removes its task and
token; if OOBE is never finished, local credentials remain until that check or
operator cleanup. The initial callback uses the same deployment network trust
boundary as PXE; it does not independently attest Windows setup state.

## Build prerequisites

Use a **Windows build machine**, with administrative access and the Windows ADK
Deployment Tools plus the matching Windows PE add-on. Use a WinPE/ADK release
supported for the Windows version being deployed and apply its applicable updates.
The base WinPE WIM, optional components, architecture, and language packages must
match. A stock Windows installation ISO boot.wim does not guarantee PowerShell.
Linux wimlib can inject files, but file copying does not replace DISM package
servicing for PowerShell and its dependencies.

Provide:

- An authorized Windows installation `install.wim` (not an ISO or ESD renamed to
  WIM). Select and inspect the desired edition/index using `Get-WindowsImage`.
- A version-pinned `wimboot` binary from the upstream iPXE release, verified against
  your approved download record. The build script copies this input; it does not
  download or independently authenticate it.
- Signed amd64 WinPE NIC/storage drivers if needed. For KVM, use a NIC/storage
  model with inbox support or provide matching VirtIO drivers. Adding drivers to
  WinPE does not install those drivers into the target Windows image: prepare
  the target image separately when using VirtIO devices.

Download Windows/ADK media directly from Microsoft under applicable terms. GOMI
source does not contain or redistribute Microsoft WIMs, Windows ISO files, ADK
CABs, or product keys. Hosting generated artifacts is the operator's deployment
of their own media; public redistribution rights are not established by this
procedure. Review the terms supplied with your media before sharing it.

## Build

Open the ADK **Deployment and Imaging Tools Environment** as administrator, start
Windows PowerShell, and run:

```powershell
Get-WindowsImage -ImagePath C:\Media\sources\install.wim
.\windows\Build-WinPE.ps1 `
  -InstallWIM C:\Media\sources\install.wim `
  -ImageIndex 6 `
  -Wimboot C:\Tools\wimboot `
  -OSVersion '11-24H2' `
  -Name windows11-pro-amd64 `
  -OutputDirectory C:\Artifacts\windows11-pro-amd64
```

The example index is illustrative: use the actual index from your media.
`OutputDirectory` must not exist. Optional `-DriverDirectory C:\Drivers\WinPE`
adds drivers recursively to WinPE. `-Language` selects the matching ADK optional
component language CABs (default `en-us`); it does not select the target Windows
language. The script uses CopyPE and DISM to add WMI, NetFX, Scripting, and
PowerShell in dependency order, with corresponding language packages. It cleans
up its temporary mounted image on exit; a partial output on failure must be
removed or quarantined before another build.

Output contains `install.wim`, `boot.wim`, `BCD`, `boot.sdi`, `wimboot`,
`manifest.json`, and `checksums.txt`. The manifest records the selected index and
relative WinPE artifact paths. Preserve that directory structure. No
machine-specific startup script, deployment token, or secret is baked into the
reusable WinPE image. GOMI injects its per-attempt `startnet.cmd` through wimboot;
WinPE must execute it to initialize networking and start the supplied bootstrap.

Inspect the generated WIM with DISM before publishing. The script itself requires
Windows and has no Linux-equivalent package-servicing test. A successful artifact
build is not an E2E deployment result.

## Register and upload

From a shell with `curl` and `jq`, after copying the generated artifact directory
to that machine, set `GOMI_URL` to the server origin and `GOMI_TOKEN` to an
existing authenticated operator/admin bearer token. Run inside the artifact
directory (the following is Bash, not Windows PowerShell):

```bash
set -euo pipefail
jq '{name, osFamily, osVersion, arch, format: "wim", source: "upload",
     variant: "desktop", manifest: .}' manifest.json > image-request.json
image_name=$(jq -r '.name' manifest.json)
api="${GOMI_URL%/}/api/v1"
curl --fail-with-body -sS -H "Authorization: Bearer $GOMI_TOKEN" \
  -H 'Content-Type: application/json' --data-binary @image-request.json \
  "$api/os-images"
curl --fail-with-body -sS -H "Authorization: Bearer $GOMI_TOKEN" \
  -F file=@install.wim "$api/os-images/$image_name/upload"
for asset in wimboot BCD boot.sdi boot.wim; do
  curl --fail-with-body -sS -H "Authorization: Bearer $GOMI_TOKEN" \
    -F "path=$asset" -F "file=@$asset" \
    "$api/os-images/$image_name/upload-artifact"
done
curl --fail-with-body -sS -H "Authorization: Bearer $GOMI_TOKEN" \
  "$api/os-images/$image_name" | jq '{name, ready, error}'
```

Use the generated simple image name without URL path separators. These operations
register and upload media; they do not start a deployment. A partial upload remains
not ready; retry the missing companion upload. Windows URL-source registration
is not the initial supported companion-upload workflow.

## Validation before deployment

Use an isolated, disposable UEFI KVM VM with adequate disk/RAM, TPM 2.0, and the
Windows 11 hardware prerequisites. Keep the target disk unambiguous; deployment
is destructive. Validate using normal GOMI image registration and deploy actions:

1. Register an OS image with `source: "upload"`, `format: "wim"`,
   `osFamily: "windows"`, `arch: "amd64"`, and the generated JSON as its
   `manifest`. Use the API image registration action `POST /os-images`.
   Upload `install.wim` using `POST /os-images/:name/upload` (multipart field
   `file`). Upload each of `wimboot`, `BCD`, `boot.sdi`, and `boot.wim` using
   `POST /os-images/:name/upload-artifact` (multipart fields `path` and `file`);
   `path` must exactly match its manifest entry. These are routes relative to
   the authenticated API base. Verify image `ready` becomes true only after all
   required files are present.
2. Confirm PXE → wimboot → WinPE networking → selected disk/image application.
3. Confirm local Windows boot, complete OOBE, and wait for GOMI's completion report.
4. Reboot again and verify no PXE-triggered reinstallation occurs.
5. Enter a key through Windows Activation if desired. Record activation status
   separately from installation success.
6. Exercise missing driver/artifact, wrong WIM index, transfer failure, and
   completion callback retry; none may produce a successful deployment.
7. Verify existing Linux deployment paths independently before shipping changes.

`wimboot` supports signed UEFI boot, but the **whole chain** matters: the selected
iPXE binary must also support Secure Boot and the firmware must trust its signer.
An arbitrary distribution's `ipxe.efi` is not proof of Secure Boot compatibility.
Record the actual firmware, Secure Boot setting, iPXE/wimboot versions, WinPE
build, WIM edition/index, and device models used in any live result.

The focused gate regression test runs in Windows PowerShell without changing
machine state or contacting GOMI:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\windows\Test-OOBEComplete.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\windows\Test-Registration.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\windows\Test-CompletionCleanup.ps1
```

It tests incomplete OOBE, completed OOBE, and failed API queries (including a stale
true output). This fake-API test does not replace checking the real API before and
after interactive OOBE during E2E verification. The registration test checks the
actual embedded registration script and asks Windows Task Scheduler to validate
its XML with `TASK_VALIDATE_ONLY`; it registers no task. The cleanup test executes
the actual callback in child PowerShell processes with fake native/registry/network
responses and temporary credentials, checking that 404/410 clean up while 409/500
retain retry state. It does not contact a server or operate real tasks.

## Primary references

- [Create WinPE media](https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/winpe-create-usb-bootable-drive?view=windows-11)
- [Add PowerShell support to WinPE](https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/winpe-adding-powershell-support-to-windows-pe?view=windows-11)
- [WinPE optional components and matching versions](https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/winpe-add-packages--optional-components-reference?view=windows-11)
- [Capture and apply Windows WIMs](https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/capture-and-apply-windows-using-a-single-wim?view=windows-11)
- [Windows OOBEComplete API](https://learn.microsoft.com/en-us/windows/win32/api/oobenotification/nf-oobenotification-oobecomplete)
- [Task Scheduler event triggers and repetition](https://learn.microsoft.com/en-us/windows/win32/taskschd/taskschedulerschema-repetition-triggerbasetype-element)
- [Task validation without registration](https://learn.microsoft.com/en-us/windows/win32/api/taskschd/ne-taskschd-task_creation)
- [Windows setup states](https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/windows-setup-states?view=windows-11)
- [Activation and product keys](https://learn.microsoft.com/en-us/windows-hardware/manufacture/desktop/work-with-product-keys-and-activation-auth-phases?view=windows-11)
- [OEM keys disable SetupComplete.cmd](https://learn.microsoft.com/en-us/troubleshoot/mem/configmgr/os-deployment/os-deployment-task-sequence-not-continue)
- [iPXE wimboot and injected files](https://ipxe.org/wimboot)
- [iPXE Secure Boot support](https://ipxe.org/secboot)
