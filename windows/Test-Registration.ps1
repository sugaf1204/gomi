#Requires -Version 5.1
# Uses the real Windows Task Scheduler XML validator with TASK_VALIDATE_ONLY.
# Does not register, modify, run, or delete any task.
[CmdletBinding()]
param([string]$RegistrationScript = (Join-Path $PSScriptRoot '..\internal\infra\pxehttp\windows\register.ps1'))
$ErrorActionPreference = 'Stop'
$script:capturedXML = $null
function Register-ScheduledTask {
    param([string]$TaskName, [string]$Xml, [string]$User, [switch]$Force)
    if ($TaskName -ne 'GOMI-ProvisionComplete' -or $User -ne 'SYSTEM' -or -not $Force) {
        throw 'Unexpected task registration contract'
    }
    $script:capturedXML = $Xml
}
. $RegistrationScript
if (-not $script:capturedXML) { throw 'Registration script supplied no XML' }
[xml]$task = $script:capturedXML
$ns = New-Object Xml.XmlNamespaceManager($task.NameTable)
$ns.AddNamespace('t', 'http://schemas.microsoft.com/windows/2004/02/mit/task')
$triggers = $task.SelectNodes('/t:Task/t:Triggers/*', $ns)
if ($triggers.Count -ne 3) { throw 'Expected registration, boot, and logon triggers' }
foreach ($name in @('RegistrationTrigger', 'BootTrigger', 'LogonTrigger')) {
    $trigger = $task.SelectSingleNode("/t:Task/t:Triggers/t:$name", $ns)
    if ($null -eq $trigger -or $trigger.Repetition.Interval -ne 'PT1M' -or $trigger.Enabled -ne 'true') {
        throw "Missing enabled minute repetition on $name"
    }
}
if ($task.SelectNodes('//t:StartBoundary | //t:EndBoundary | //t:TimeTrigger', $ns).Count) {
    throw 'Completion retries must not depend on an absolute registration-time clock'
}
$scheduler = New-Object -ComObject Schedule.Service
$scheduler.Connect()
# 1 = TASK_VALIDATE_ONLY; 5 = TASK_LOGON_SERVICE_ACCOUNT.
$null = $scheduler.GetFolder('\').RegisterTask('GOMI-ValidateOnly', $script:capturedXML, 1, 'SYSTEM', $null, 5, $null)
'PASS: actual registration XML accepted by Windows; all event triggers repeat without clock boundaries; no task registered'
