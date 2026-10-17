#Requires -Version 5.1
# Run with Windows PowerShell. Tests only the completion gate, with a fake API;
# no task, registry, deployment token, or network endpoint is modified.
[CmdletBinding()]
param([string]$CompletionScript = (Join-Path $PSScriptRoot '..\internal\infra\pxehttp\windows\complete.ps1'))
$ErrorActionPreference = 'Stop'
$tokens = $null
$parseErrors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile(
    (Resolve-Path -LiteralPath $CompletionScript).Path, [ref]$tokens, [ref]$parseErrors)
if ($parseErrors.Count) { throw ($parseErrors | Out-String) }
$gate = $ast.Find({ param($node)
    $node -is [Management.Automation.Language.FunctionDefinitionAst] -and
    $node.Name -eq 'Test-GomiOOBEComplete'
}, $true)
if ($null -eq $gate) { throw 'Completion gate not found' }
Add-Type -TypeDefinition @'
namespace Gomi {
    public static class WindowsOOBE {
        public static bool QuerySucceeded;
        public static bool Completed;
        public static bool OOBEComplete(out bool completed) {
            completed = Completed;
            return QuerySucceeded;
        }
    }
}
'@
. ([scriptblock]::Create($gate.Extent.Text))

[Gomi.WindowsOOBE]::QuerySucceeded = $true
[Gomi.WindowsOOBE]::Completed = $false
if (Test-GomiOOBEComplete) { throw 'Interactive OOBE was incorrectly accepted' }
[Gomi.WindowsOOBE]::Completed = $true
if (-not (Test-GomiOOBEComplete)) { throw 'Completed OOBE was incorrectly rejected' }
# Even a stale TRUE output cannot authorize completion when the API failed.
[Gomi.WindowsOOBE]::QuerySucceeded = $false
foreach ($completed in @($false, $true)) {
    [Gomi.WindowsOOBE]::Completed = $completed
    $threw = $false
    try { $null = Test-GomiOOBEComplete } catch { $threw = $true }
    if (-not $threw) { throw 'Failed OOBE query did not fail closed' }
}
Write-Output 'PASS: pending OOBE, completed OOBE, and failed queries with both outputs'
