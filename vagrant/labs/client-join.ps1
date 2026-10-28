# A Windows client of the lab joins destino.lab (lab-clients.sh), by the delegated join account (lab.ingresso: it
# only creates computers in OU=Estacoes), its password read from the lab's secrets file, never from an argument.
# Reboot after it. -Rsat also installs the tools that administer the Samba DC (ADUC, GPMC, DNS) on a server.
param(
  [string] $Domain = 'destino.lab',
  [string] $DnsServer = '192.168.57.40',
  [string] $OU = 'OU=Estacoes,DC=destino,DC=lab',
  [string] $Secrets = 'C:\vagrant\.run\labs\secrets.env',
  [switch] $Rsat
)
function Read-EnvValue([string] $line) {
  $v = ($line -split '=', 2)[1]
  if ($v -match "^'(.*)'$" -or $v -match '^"(.*)"$') { $v = $Matches[1] }
  $v
}
$ErrorActionPreference = 'Stop'
$cs = Get-CimInstance Win32_ComputerSystem
if ($cs.PartOfDomain) {
  # a fresh secure channel, not the one already open (Test-ComputerSecureChannel kept answering True for a computer
  # whose account had been deleted): 1787 when the domain no longer knows this computer
  cmd /c "nltest /sc_reset:$($cs.Domain) >nul 2>&1" # through cmd: PowerShell 5 turns a native stderr line into a stop
  if ($LASTEXITCODE -eq 0) { "already a member of $($cs.Domain)"; return }
  # a lab domain created again (same name, new database) no longer knows this computer: leave it here (no account
  # to disable there). Left like that, local logons fail with STATUS_NO_LOGON_SERVERS (WinRM included) until the
  # workgroup is set again after a reboot, so a startup task does that once, then reboots (checked on win11 and
  # ws2022m by hand); lab-clients.sh reboots, waits for WinRM and runs this again
  $act = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument ('-NoProfile -Command "Add-Computer -WorkgroupName WORKGROUP -Force; ' +
    'Unregister-ScheduledTask -TaskName lab-workgroup -Confirm:$false; Restart-Computer -Force"')
  Register-ScheduledTask -TaskName lab-workgroup -Action $act -Trigger (New-ScheduledTaskTrigger -AtStartup) -User SYSTEM -RunLevel Highest -Force | Out-Null
  $r = $cs | Invoke-CimMethod -MethodName UnjoinDomainOrWorkgroup -Arguments @{ FUnjoinOptions = [uint32]0 }
  if ($r.ReturnValue -ne 0) { throw "leaving $($cs.Domain) failed: $($r.ReturnValue)" }
  "left the stale domain $($cs.Domain): reboot and run again"
  return
}
# every card asks the domain's DC: the join finds it through DNS
Get-NetAdapter | ForEach-Object { Set-DnsClientServerAddress -InterfaceIndex $_.ifIndex -ServerAddresses $DnsServer }
# Vagrant's NAT card (10.0.2.15 on every VM) stays out of the domain's DNS: only the lab network's address
Get-NetIPAddress -AddressFamily IPv4 -IPAddress 10.0.2.* -ErrorAction SilentlyContinue |
  ForEach-Object { Set-DnsClient -InterfaceIndex $_.InterfaceIndex -RegisterThisConnectionsAddress $false }
Clear-DnsClientCache
if ($Rsat) {
  Install-WindowsFeature RSAT-AD-Tools, GPMC, RSAT-DNS-Server -IncludeAllSubFeature | Out-Null
  'RSAT installed (ADUC, GPMC, DNS)'
}
$line = Get-Content $Secrets | Where-Object { $_ -match '^LAB_JOIN_PASSWORD=' } | Select-Object -First 1
$cred = New-Object PSCredential('DESTINO\lab.ingresso', (ConvertTo-SecureString (Read-EnvValue $line) -AsPlainText -Force))
Add-Computer -DomainName $Domain -OUPath $OU -Credential $cred -Force
"$env:COMPUTERNAME joined $Domain ($OU): reboot"
