#Requires -Version 5.1
#Requires -RunAsAdministrator
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$InstallWIM,
    [Parameter(Mandatory = $true)][ValidateRange(1, 999)][int]$ImageIndex,
    [Parameter(Mandatory = $true)][string]$Wimboot,
    [Parameter(Mandatory = $true)][string]$OutputDirectory,
    [Parameter(Mandatory = $true)][string]$OSVersion,
    [string]$Name = 'windows-amd64',
    [string]$Language = 'en-us',
    [string]$DriverDirectory,
    [string]$ADKRoot = "${env:ProgramFiles(x86)}\Windows Kits\10\Assessment and Deployment Kit"
)
$ErrorActionPreference = 'Stop'

function Invoke-Checked {
    param([string]$Program, [string[]]$Arguments)
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed with exit code $LASTEXITCODE" }
}
function Require-File {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "Required file missing: $Path" }
}

$InstallWIM = (Resolve-Path -LiteralPath $InstallWIM).Path
$Wimboot = (Resolve-Path -LiteralPath $Wimboot).Path
Require-File $InstallWIM
Require-File $Wimboot
$winPE = Join-Path $ADKRoot 'Windows Preinstallation Environment'
$copyPE = Join-Path $winPE 'copype.cmd'
$ocRoot = Join-Path $winPE 'amd64\WinPE_OCs'
Require-File $copyPE
$packages = @('WinPE-WMI', 'WinPE-NetFX', 'WinPE-Scripting', 'WinPE-PowerShell')
foreach ($package in $packages) {
    Require-File (Join-Path $ocRoot "$package.cab")
    Require-File (Join-Path $ocRoot "$Language\${package}_${Language}.cab")
}
# Validate the selected source image before creating or modifying artifacts.
$image = Get-WindowsImage -ImagePath $InstallWIM -Index $ImageIndex
if ($image.Architecture -ne 9 -and $image.Architecture -ne 'x64') {
    throw 'Only amd64 Windows images are supported.'
}
if (Test-Path -LiteralPath $OutputDirectory) { throw 'OutputDirectory must not already exist.' }
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
$work = Join-Path ([IO.Path]::GetTempPath()) ('gomi-winpe-' + [guid]::NewGuid().ToString('N'))
$mounted = $false
try {
    # Run from the elevated ADK Deployment and Imaging Tools Environment, which
    # initializes the architecture-specific DISM and CopyPE dependencies.
    Invoke-Checked -Program $copyPE -Arguments @('amd64', $work)
    $bootWIM = Join-Path $work 'media\sources\boot.wim'
    $mount = Join-Path $work 'mount'
    Invoke-Checked -Program 'dism.exe' -Arguments @('/Mount-Image', "/ImageFile:$bootWIM", '/Index:1', "/MountDir:$mount")
    $mounted = $true
    foreach ($package in $packages) {
        Invoke-Checked -Program 'dism.exe' -Arguments @("/Image:$mount", '/Add-Package', "/PackagePath:$(Join-Path $ocRoot "$package.cab")")
        Invoke-Checked -Program 'dism.exe' -Arguments @("/Image:$mount", '/Add-Package', "/PackagePath:$(Join-Path $ocRoot "$Language\${package}_${Language}.cab")")
    }
    if ($DriverDirectory) {
        $drivers = (Resolve-Path -LiteralPath $DriverDirectory).Path
        Invoke-Checked -Program 'dism.exe' -Arguments @("/Image:$mount", '/Add-Driver', "/Driver:$drivers", '/Recurse')
    }
    # Per-attempt startnet.cmd is injected by GOMI/wimboot; no deployment token
    # or machine-specific secret is embedded in this reusable boot image.
    Invoke-Checked -Program 'dism.exe' -Arguments @('/Unmount-Image', "/MountDir:$mount", '/Commit')
    $mounted = $false
    New-Item -ItemType Directory -Path $OutputDirectory | Out-Null
    $files = [ordered]@{
        'install.wim' = $InstallWIM
        'wimboot' = $Wimboot
        'BCD' = (Join-Path $work 'media\Boot\BCD')
        'boot.sdi' = (Join-Path $work 'media\Boot\boot.sdi')
        'boot.wim' = $bootWIM
    }
    foreach ($entry in $files.GetEnumerator()) {
        Require-File $entry.Value
        Copy-Item -LiteralPath $entry.Value -Destination (Join-Path $OutputDirectory $entry.Key)
    }
    $manifest = [ordered]@{
        name = $Name; osFamily = 'windows'; osVersion = $OSVersion; arch = 'amd64'
        bootModes = @('uefi')
        capabilities = @{ deployTargets = @('vm', 'baremetal') }
        root = @{ format = 'wim'; path = 'install.wim'; sha256 = (Get-FileHash $InstallWIM -Algorithm SHA256).Hash.ToLowerInvariant() }
        windows = @{
            imageIndex = $ImageIndex
            winPE = @{ wimboot = 'wimboot'; bcd = 'BCD'; bootSDI = 'boot.sdi'; bootWIM = 'boot.wim' }
        }
    }
    $utf8 = New-Object System.Text.UTF8Encoding($false)
    [IO.File]::WriteAllText((Join-Path $OutputDirectory 'manifest.json'), ($manifest | ConvertTo-Json -Depth 8), $utf8)
    $checksums = foreach ($file in Get-ChildItem -LiteralPath $OutputDirectory -File) {
        '{0}  {1}' -f (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant(), $file.Name
    }
    [IO.File]::WriteAllLines((Join-Path $OutputDirectory 'checksums.txt'), [string[]]$checksums, $utf8)
    Write-Host "Artifacts created in $OutputDirectory. Verify the image and drivers in a disposable UEFI VM before deployment."
}
finally {
    if ($mounted) {
        & dism.exe /Unmount-Image "/MountDir:$(Join-Path $work 'mount')" /Discard
        $mounted = ($LASTEXITCODE -ne 0)
    }
    if ($mounted) {
        Write-Warning "DISM could not unmount the image. Preserving $work for manual recovery."
    } elseif (Test-Path -LiteralPath $work) {
        Remove-Item -LiteralPath $work -Recurse -Force
    }
}
