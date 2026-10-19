# First provisioner of every Windows lab VM: no traffic to or from the production LANs (LAB_BLOCK, from the
# Vagrantfile), persistent firewall rules. The Vagrant NAT adapter stays (WinRM, updates), so the block is in the guest.
$ErrorActionPreference = 'Stop'
$block = if ($env:LAB_BLOCK) { $env:LAB_BLOCK } else { '192.168.10.0/24 192.168.20.0/24' }
$probe = if ($env:LAB_PROBE) { $env:LAB_PROBE } else { '192.168.10.10' }
$lans = $block -split '\s+' | Where-Object { $_ }
foreach ($dir in 'Outbound', 'Inbound') {
  $name = "labiso-$dir"
  if (-not (Get-NetFirewallRule -Name $name -ErrorAction SilentlyContinue)) {
    New-NetFirewallRule -Name $name -DisplayName "Lab: no production LAN ($dir)" -Direction $dir `
      -Action Block -RemoteAddress $lans -Profile Any | Out-Null
  }
}
Set-NetFirewallProfile -Profile Domain, Private, Public -Enabled True
$ok = Test-NetConnection $probe -Port 389 -InformationLevel Quiet -WarningAction SilentlyContinue
if ($ok) { throw 'ERROR: the production LAN is reachable' }
"isolation ok: $block blocked"
