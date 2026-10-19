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
if ((Get-CimInstance Win32_ComputerSystem).PartOfDomain) { "already a member of $((Get-CimInstance Win32_ComputerSystem).Domain)"; return }
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
