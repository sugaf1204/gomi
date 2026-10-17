$ErrorActionPreference = 'Stop'
# Windows PowerShell progress rendering can throttle multi-gigabyte WIM
# downloads. Native diskpart/DISM output and GOMI stage reports remain visible.
$ProgressPreference = 'SilentlyContinue'
$config = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('__GOMI_CONFIG__')) | ConvertFrom-Json

function Invoke-Native([string]$Program, [string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed (exit $LASTEXITCODE)" }
}

function Send-State([string]$Stage) {
    $body = @{stage=$Stage} | ConvertTo-Json -Compress
    for ($attempt = 0; $attempt -lt 12; $attempt++) {
        try {
            Invoke-RestMethod -Uri $config.stateURL -Method Post -ContentType 'application/json' -Body $body -TimeoutSec 20 | Out-Null
            return
        } catch { if ($attempt -eq 11) { throw }; Start-Sleep -Seconds 5 }
    }
}

try {
    # Never run this destructive installer in an installed Windows system.
    if ($env:SystemDrive -ne 'X:' -or !(Test-Path 'HKLM:\SYSTEM\CurrentControlSet\Control\MiniNT')) {
        throw 'GOMI Windows installer must run inside WinPE'
    }
    Invoke-Native -Program 'wpeutil.exe' -Arguments @('UpdateBootInfo')
    if ((Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control').PEFirmwareType -ne 2) {
        throw 'Only UEFI Windows deployment is supported'
    }
    $nics = @(Get-WmiObject Win32_NetworkAdapterConfiguration | Where-Object {
        $_.IPEnabled -and ($_.MACAddress -replace ':','').ToLowerInvariant() -eq ($config.mac -replace ':','').ToLowerInvariant()
    })
    if ($nics.Count -ne 1) { throw 'The deployment NIC is unavailable in WinPE; check drivers and DHCP' }
    # A single fixed disk is the explicit initial safety contract. USB disks
    # and multi-disk hosts require a future serial-selected deployment mode.
    $allDisks = @(Get-WmiObject Win32_DiskDrive)
    if ($allDisks.Count -ne 1 -or $allDisks[0].InterfaceType -eq 'USB' -or $allDisks[0].MediaType -notmatch 'Fixed') {
        throw 'Windows deployment requires exactly one fixed non-USB disk; no disks were erased'
    }
    $disk = $allDisks[0]
    if ([uint64]$disk.Size -lt 64GB) { throw 'Windows deployment requires at least 64 GiB of disk space' }
    if ((Test-Path 'W:\') -or (Test-Path 'S:\')) { throw 'Reserved WinPE drive letters W: or S: are already in use' }
    # Check source reachability before erasing the selected disk.
    Invoke-WebRequest -UseBasicParsing -Method Head -Uri $config.imageURL -TimeoutSec 30 | Out-Null
    # Claim this destructive attempt before touching disk. A reboot/power loss
    # from here on requires an explicit fresh deploy, never an automatic wipe.
    Send-State 'install_started'
    $diskpart = @"
select disk $($disk.Index)
clean
convert gpt
create partition efi size=260
format quick fs=fat32 label=System
assign letter=S
create partition msr size=16
create partition primary
format quick fs=ntfs label=Windows
assign letter=W
exit
"@
    $diskpart | Set-Content X:\gomi-diskpart.txt -Encoding ASCII
    Invoke-Native -Program 'diskpart.exe' -Arguments @('/s', 'X:\gomi-diskpart.txt')
    if (!(Test-Path 'W:\') -or !(Test-Path 'S:\')) { throw 'Partition creation failed' }
    # Download to disk rather than the WinPE RAM drive: installation WIMs may
    # exceed RAM. The temporary file is removed after DISM completes.
    Invoke-WebRequest -UseBasicParsing -Uri $config.imageURL -OutFile W:\gomi-install.wim -TimeoutSec 3600
    if ($config.sha256 -and (Get-FileHash W:\gomi-install.wim -Algorithm SHA256).Hash -ne $config.sha256) {
        throw 'Installation WIM checksum mismatch'
    }
    New-Item W:\gomi-scratch -ItemType Directory -Force | Out-Null
    Invoke-Native -Program 'dism.exe' -Arguments @('/Apply-Image', '/ImageFile:W:\gomi-install.wim', "/Index:$($config.imageIndex)", '/ApplyDir:W:\', '/ScratchDir:W:\gomi-scratch', '/CheckIntegrity')
    Remove-Item W:\gomi-install.wim -Force
    Remove-Item W:\gomi-scratch -Recurse -Force
    if (!(Test-Path W:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe)) { throw 'Target image requires Windows PowerShell' }

    $dir = 'W:\ProgramData\GOMI'
    New-Item $dir -ItemType Directory -Force | Out-Null
    $config | ConvertTo-Json -Compress | Set-Content "$dir\provision.json" -Encoding UTF8
    [IO.File]::WriteAllBytes("$dir\complete.ps1", [Convert]::FromBase64String('__GOMI_COMPLETE__'))
    # ACL on the installed filesystem: the callback token is SYSTEM/admin only.
    Invoke-Native -Program 'icacls.exe' -Arguments @($dir, '/inheritance:r', '/grant:r', '*S-1-5-18:(OI)(CI)F', '*S-1-5-32-544:(OI)(CI)F')
    [IO.File]::WriteAllBytes("$dir\register.ps1", [Convert]::FromBase64String('__GOMI_REGISTER__'))
    # No ProductKey and no user/password: OOBE remains interactive. A task is
    # registered during specialize, independent of SetupComplete OEM behavior.
    $name = [Security.SecurityElement]::Escape([string]$config.hostname)
    $unattend = @"
<?xml version="1.0" encoding="utf-8"?>
<unattend xmlns="urn:schemas-microsoft-com:unattend">
  <settings pass="specialize">
    <component name="Microsoft-Windows-Shell-Setup" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS"><ComputerName>$name</ComputerName></component>
    <component name="Microsoft-Windows-Deployment" processorArchitecture="amd64" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
      <RunSynchronous><RunSynchronousCommand xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State" wcm:action="add"><Order>1</Order><Path>powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\ProgramData\GOMI\register.ps1</Path></RunSynchronousCommand></RunSynchronous>
    </component>
  </settings>
</unattend>
"@
    New-Item W:\Windows\Panther -ItemType Directory -Force | Out-Null
    $unattend | Set-Content W:\Windows\Panther\unattend.xml -Encoding UTF8
    Invoke-Native -Program 'bcdboot.exe' -Arguments @('W:\Windows', '/s', 'S:', '/f', 'UEFI')
    # Persist the no-reinstall guard before reboot. A lost acknowledgement
    # stops here; it must never lead to an automatic second disk wipe.
    Send-State 'image_applied'
    Invoke-Native -Program 'wpeutil.exe' -Arguments @('reboot')
} catch {
    Write-Host ('GOMI Windows deployment failed: ' + $_.Exception.Message)
    try { Send-State 'failed' } catch { Write-Host 'GOMI could not record failure; stop the VM before retrying' }
    exit 1
}
