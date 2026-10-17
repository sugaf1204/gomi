$ErrorActionPreference = 'Stop'
$dir = Join-Path $env:ProgramData GOMI
$config = Get-Content (Join-Path $dir provision.json) -Raw | ConvertFrom-Json

# The setup registry can already say COMPLETE while Windows Welcome is still
# showing Region/account screens. Query the documented OOBE notification API.
function Test-GomiOOBEComplete {
    if (-not ('Gomi.WindowsOOBE' -as [type])) {
        Add-Type -TypeDefinition @'
using System.Runtime.InteropServices;
namespace Gomi {
    public static class WindowsOOBE {
        [DllImport("kernel32.dll", SetLastError = true)]
        [return: MarshalAs(UnmanagedType.Bool)]
        public static extern bool OOBEComplete(
            [MarshalAs(UnmanagedType.Bool)] out bool isOOBEComplete);
    }
}
'@
    }
    $completed = $false
    if (-not [Gomi.WindowsOOBE]::OOBEComplete([ref]$completed)) {
        throw 'Windows could not determine OOBE completion state'
    }
    return $completed
}

function Remove-GomiTask {
    Unregister-ScheduledTask -TaskName GOMI-ProvisionComplete -Confirm:$false -ErrorAction SilentlyContinue
    Remove-Item (Join-Path $dir provision.json) -Force -ErrorAction SilentlyContinue
    Remove-Item (Join-Path $dir register.ps1) -Force -ErrorAction SilentlyContinue
}

try {
    # The guest clock can be corrected during OOBE; expiry is authoritative
    # only from server expiry/token rejection, never from the guest wall clock.
    # Never report WinPE, specialize, or interactive OOBE as a ready machine.
    if (Test-Path 'HKLM:\SYSTEM\CurrentControlSet\Control\MiniNT') { exit 0 }
    if (-not (Test-GomiOOBEComplete)) { exit 0 }
    $state = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Setup\State'
    $setup = Get-ItemProperty 'HKLM:\SYSTEM\Setup'
    if ($state.ImageState -ne 'IMAGE_STATE_COMPLETE' -or $null -eq $setup.SystemSetupInProgress -or $setup.SystemSetupInProgress -ne 0 -or $null -eq $setup.OOBEInProgress -or $setup.OOBEInProgress -ne 0) { exit 0 }
    $nic = @(Get-CimInstance Win32_NetworkAdapterConfiguration | Where-Object {
        $_.IPEnabled -and ($_.MACAddress -replace ':','').ToLowerInvariant() -eq ($config.mac -replace ':','').ToLowerInvariant()
    })
    $ip = @($nic | ForEach-Object { $_.IPAddress } | Where-Object { $_ -match '^\d+\.\d+\.\d+\.\d+$' -and $_ -notmatch '^169\.254\.' -and $_ -notmatch '^127\.' }) | Select-Object -First 1
    if (!$ip) { exit 0 }
    $body = @{stage='complete'; ip=$ip; mac=$config.mac; hostname=$env:COMPUTERNAME} | ConvertTo-Json -Compress
    Invoke-RestMethod -Uri $config.stateURL -Method Post -ContentType 'application/json' -Body $body -TimeoutSec 30 | Out-Null
    'Windows initial setup completed. Activation is managed manually in Windows Settings.' | Set-Content (Join-Path $dir status.txt)
    Remove-GomiTask
} catch {
    if ($null -ne $_.Exception.Response -and [int]$_.Exception.Response.StatusCode -in @(404, 410)) {
        'Provisioning token is no longer valid on the server; start a new deployment in GOMI.' | Set-Content (Join-Path $dir status.txt)
        Remove-GomiTask
        exit 1
    }
    # Scheduler retries once a minute. Do not log the bearer URL or token.
    'Waiting for Windows setup, DHCP, or GOMI callback.' | Set-Content (Join-Path $dir status.txt)
    exit 1
}
