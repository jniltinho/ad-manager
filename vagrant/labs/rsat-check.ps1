# RSAT on the member server administers the Samba DC (lab-clients.sh), as the domain's Administrator: WinRM gives
# the local Vagrant account, which the DC does not know, so the checks run as a scheduled task with that account's
# credential (a real logon: no second hop), its password read from the lab's secrets file, never an argument.
#   ADUC's protocol (LDAP, dsquery), GPMC's (LDAP + SMB, the GroupPolicy module) and the DNS console's (RPC, dnscmd);
#   the ActiveDirectory and DnsServer PowerShell modules need ADWS and Windows DNS, which Samba does not run.
param(
  [string] $Dc = 'dstdc.destino.lab',
  [string] $Secrets = 'C:\vagrant\.run\labs\secrets.env'
)
function Read-EnvValue([string] $line) {
  $v = ($line -split '=', 2)[1]
  if ($v -match "^'(.*)'$" -or $v -match '^"(.*)"$') { $v = $Matches[1] }
  $v
}
$ErrorActionPreference = 'Stop'
$out = 'C:\Windows\Temp\rsat-check.txt'
$body = 'C:\Windows\Temp\rsat-check-task.ps1'
Set-Content $body -Encoding UTF8 -Value @"
`$r = @()
`$u = dsquery user -samid lab.ana -s $Dc
`$r += '- ADUC (dsquery -samid lab.ana): ' + `$(if (`$u) { `$u } else { 'NOTHING' })
Import-Module GroupPolicy
`$r += '- GPMC (Get-GPO): ' + ((Get-GPO -All -Domain destino.lab -Server $Dc).DisplayName -join ', ')
# /Primary: without a filter dnscmd asks for every kind of zone at once, which Samba answers with none
`$z = dnscmd $Dc /enumzones /Primary 2>&1
`$r += '- DNS (dnscmd /enumzones /Primary): ' + `$(if (`$LASTEXITCODE) { 'FAILED: ' + ((`$z | Out-String) -replace '\s+', ' ') } else { (`$z -match ' Primary ' | ForEach-Object { (`$_.Trim() -split '\s+')[0] }) -join ', ' })
`$r | Set-Content $out -Encoding UTF8
"@
Remove-Item $out -ErrorAction SilentlyContinue
$line = Get-Content $Secrets | Where-Object { $_ -match '^LAB_SAMBA_DST_PASSWORD=' } | Select-Object -First 1
$action = New-ScheduledTaskAction -Execute powershell.exe -Argument "-NoProfile -ExecutionPolicy Bypass -File $body"
Register-ScheduledTask lab-rsat-check -Action $action -User 'DESTINO\Administrator' -Password (Read-EnvValue $line) `
  -RunLevel Highest -Force | Out-Null
try {
  Start-ScheduledTask lab-rsat-check
  for ($i = 0; $i -lt 60 -and -not (Test-Path $out); $i++) { Start-Sleep 2 }
  if (-not (Test-Path $out)) { throw 'RSAT: the task did not answer within 2 minutes' }
  $lines = Get-Content $out
  $lines
  if ($lines.Count -lt 3 -or ($lines -match 'NOTHING|FAILED|: $')) { throw 'RSAT: a query to the DC failed' }
} finally { Unregister-ScheduledTask lab-rsat-check -Confirm:$false; Remove-Item $body -ErrorAction SilentlyContinue }
