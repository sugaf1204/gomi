#Requires -Version 5.1
# Executes the actual completion script in isolated child PowerShell processes.
# All OS/network/task interfaces are mocked; only temporary test files change.
[CmdletBinding()]
param(
    [string]$CompletionScript = (Join-Path $PSScriptRoot '..\internal\infra\pxehttp\windows\complete.ps1'),
    [switch]$Child,
    [int]$StatusCode,
    [string]$TestDirectory
)
$ErrorActionPreference = 'Stop'
if ($Child) {
    $env:ProgramData = $TestDirectory
    Add-Type -TypeDefinition @'
using System.Net;
namespace Gomi {
    public static class WindowsOOBE {
        public static bool OOBEComplete(out bool complete) { complete = true; return true; }
    }
    public class TestResponse : WebResponse {
        public HttpStatusCode StatusCode { get; set; }
        public TestResponse(int code) { StatusCode = (HttpStatusCode)code; }
    }
}
'@
    function Test-Path { param($Path) return $false }
    function Get-ItemProperty {
        param($Path)
        if ($Path -like '*Setup\State') { return [pscustomobject]@{ ImageState = 'IMAGE_STATE_COMPLETE' } }
        return [pscustomobject]@{ SystemSetupInProgress = 0; OOBEInProgress = 0 }
    }
    function Get-CimInstance {
        param($ClassName)
        return [pscustomobject]@{ IPEnabled = $true; MACAddress = '52:54:00:12:34:56'; IPAddress = @('192.0.2.10') }
    }
    function Invoke-RestMethod {
        param($Uri, $Method, $ContentType, $Body, $TimeoutSec)
        [IO.File]::WriteAllText((Join-Path $TestDirectory 'request'), [string]$StatusCode)
        $response = New-Object Gomi.TestResponse($StatusCode)
        throw (New-Object Net.WebException('Fake response', $null, [Net.WebExceptionStatus]::ProtocolError, $response))
    }
    function Unregister-ScheduledTask {
        param($TaskName, [switch]$Confirm, $ErrorAction)
        if ($TaskName -ne 'GOMI-ProvisionComplete') { throw 'Unexpected task name' }
        [IO.File]::WriteAllText((Join-Path $TestDirectory 'unregistered'), 'mock only')
    }
    $global:LASTEXITCODE = 99
    & $CompletionScript
    exit $LASTEXITCODE
}
$CompletionScript = (Resolve-Path -LiteralPath $CompletionScript).Path
$root = Join-Path ([IO.Path]::GetTempPath()) ('gomi-cleanup-test-' + [guid]::NewGuid().ToString('N'))
try {
    foreach ($code in @(404, 410, 409, 500)) {
        $case = Join-Path $root ([string]$code)
        $dir = Join-Path $case GOMI
        New-Item -ItemType Directory -Path $dir -Force | Out-Null
        '{"mac":"52:54:00:12:34:56","stateURL":"http://invalid.example/mock-only"}' | Set-Content (Join-Path $dir provision.json)
        'mock registration' | Set-Content (Join-Path $dir register.ps1)
        & (Join-Path $PSHOME powershell.exe) -NoProfile -ExecutionPolicy Bypass -File $PSCommandPath -Child -CompletionScript $CompletionScript -StatusCode $code -TestDirectory $case
        if ($LASTEXITCODE -ne 1) { throw "Unexpected callback exit for HTTP $code`: $LASTEXITCODE" }
        if ((Get-Content (Join-Path $case request)) -ne [string]$code) { throw 'Callback did not reach the fake HTTP response' }
        $terminal = $code -in @(404, 410)
        if ((Test-Path (Join-Path $case unregistered)) -ne $terminal) { throw "Incorrect task cleanup for HTTP $code" }
        foreach ($file in @('provision.json', 'register.ps1')) {
            if ((Test-Path (Join-Path $dir $file)) -eq $terminal) { throw "Incorrect $file cleanup for HTTP $code" }
        }
    }
    'PASS: actual callback cleans task/credentials for 404/410 and retains retry state for 409/500; no real tasks/network'
} finally {
    if (Test-Path -LiteralPath $root) { Remove-Item -LiteralPath $root -Recurse -Force }
}
