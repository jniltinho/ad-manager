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
$line = Get-Content $Secrets | Where-Object { $_ -match '^LAB_JOIN_PASSWORD=' } | Select-Object -First 1
$cred = New-Object PSCredential('DESTINO\lab.ingresso', (ConvertTo-SecureString (Read-EnvValue $line) -AsPlainText -Force))
$cs = Get-CimInstance Win32_ComputerSystem
if ($cs.PartOfDomain) {
  # a fresh secure channel, not the one already open (Test-ComputerSecureChannel kept answering True for a computer
  # whose account had been deleted): 1787 when the domain no longer knows this computer
  cmd /c "nltest /sc_reset:$($cs.Domain) >nul 2>&1" # through cmd: PowerShell 5 turns a native stderr line into a stop
  if ($LASTEXITCODE -eq 0) { "already a member of $($cs.Domain)"; return }
  # a lab domain created again (same name, new database) no longer knows this computer. Leaving it would break
  # WinRM's local logons until a reboot and a workgroup change, so the membership is repaired in place: the join
  # account recreates the computer object (it may create computers in $OU; sealed LDAP), then the secure channel is
  # repaired, which sets the machine password on the DC and here. Netlogon writes its SPNs and DNS name at the next boot.
  $parent = New-Object DirectoryServices.DirectoryEntry("LDAP://$Domain/$OU", $cred.UserName,
    $cred.GetNetworkCredential().Password, [DirectoryServices.AuthenticationTypes]'Secure,Sealing,Signing')
  try { # ADSI's own methods (PowerShell's adapter hides DirectoryEntry's .NET members); not $ou: that is [string] $OU
    $c = $parent.Create('computer', "CN=$env:COMPUTERNAME")
    $c.Put('sAMAccountName', "$env:COMPUTERNAME`$")
    $c.Put('userAccountControl', 4096) # WORKSTATION_TRUST_ACCOUNT
    $c.SetInfo()
  } catch {
    if ($_.Exception.Message -notmatch 'exist') { throw }
  }
  if (-not (Test-ComputerSecureChannel -Repair -Credential $cred)) { throw "repairing the secure channel with $($cs.Domain) failed" }
  "repaired $env:COMPUTERNAME's membership of $($cs.Domain) (the domain was created again): reboot"
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
Add-Computer -DomainName $Domain -OUPath $OU -Credential $cred -Force
"$env:COMPUTERNAME joined $Domain ($OU): reboot"
