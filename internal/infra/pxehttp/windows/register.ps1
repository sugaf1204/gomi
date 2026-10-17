$ErrorActionPreference = 'Stop'
# An absolute trigger created during specialize can remain hours in the future
# after OOBE corrects the clock. Event triggers have no wall-clock boundary.
# Logon also re-arms retries at the first post-OOBE sign-in after that correction.
$taskXML = @'
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers>
    <RegistrationTrigger><Enabled>true</Enabled><Repetition><Interval>PT1M</Interval><StopAtDurationEnd>false</StopAtDurationEnd></Repetition></RegistrationTrigger>
    <BootTrigger><Enabled>true</Enabled><Repetition><Interval>PT1M</Interval><StopAtDurationEnd>false</StopAtDurationEnd></Repetition></BootTrigger>
    <LogonTrigger><Enabled>true</Enabled><Repetition><Interval>PT1M</Interval><StopAtDurationEnd>false</StopAtDurationEnd></Repetition></LogonTrigger>
  </Triggers>
  <Principals><Principal id="System"><UserId>S-1-5-18</UserId><RunLevel>HighestAvailable</RunLevel></Principal></Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <ExecutionTimeLimit>PT2M</ExecutionTimeLimit>
  </Settings>
  <Actions Context="System"><Exec><Command>%SystemRoot%\System32\WindowsPowerShell\v1.0\powershell.exe</Command><Arguments>-NoProfile -ExecutionPolicy Bypass -File C:\ProgramData\GOMI\complete.ps1</Arguments></Exec></Actions>
</Task>
'@
Register-ScheduledTask -TaskName GOMI-ProvisionComplete -Xml $taskXML -User SYSTEM -Force | Out-Null
