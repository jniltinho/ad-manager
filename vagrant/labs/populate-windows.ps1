# Fills a Windows lab DC with the common data set (vagrant/labs/data), as the Samba source gets it from
# populate-samba.sh: OUs, groups, users (the lab users' password from the secrets file), members, 2 GPOs linked to the
# top OU, DNS with a reverse zone, shares on the DC with NTFS ACLs (a subfolder with its own ACL and a deny entry: what
# the import does not carry), files. Idempotent.
param(
  [string] $Data = 'C:\vagrant\vagrant\labs\data',
  [string] $Secrets = 'C:\vagrant\.run\labs\secrets.env',
  [string] $Root = 'C:\shares'
)
# the value of a KEY=value line of secrets.env as bash reads it: 'v', "v" or v
function Read-EnvValue([string] $line) {
  $v = ($line -split '=', 2)[1]
  if ($v -match "^'(.*)'$" -or $v -match '^"(.*)"$') { $v = $Matches[1] }
  $v
}
$ErrorActionPreference = 'Stop'
Import-Module ActiveDirectory
$dom = Get-ADDomain
$dn, $fqdn, $nb = $dom.DistinguishedName, $dom.DNSRoot, $dom.NetBIOSName
function Rows([string] $file) { Import-Csv (Join-Path $Data $file) -Delimiter ';' -Encoding UTF8 }
# files whose first line is a comment: the columns by position
function RowsBy([string] $file, [string[]] $cols) {
  Get-Content (Join-Path $Data $file) -Encoding UTF8 | Where-Object { $_ -and $_ -notmatch '^#' } |
    ConvertFrom-Csv -Delimiter ';' -Header $cols
}
function Secret([string] $Name) {
  $line = Get-Content $Secrets | Where-Object { $_ -match "^$Name=" } | Select-Object -First 1
  ConvertTo-SecureString (Read-EnvValue $line) -AsPlainText -Force
}
$upw = Secret 'LAB_SAMBA_USER_PASSWORD'

# OUs, parents first
foreach ($o in (Rows 'ous.csv' | Sort-Object { ($_.ou -split ',').Count })) {
  $name = ($o.ou -split ',')[0] -replace '^OU=', ''
  $parent = (($o.ou -split ',') | Select-Object -Skip 1) -join ','
  $path = if ($parent) { "$parent,$dn" } else { $dn }
  if (-not (Get-ADOrganizationalUnit -Filter "Name -eq '$name'" -SearchBase $path -SearchScope OneLevel)) {
    New-ADOrganizationalUnit -Name $name -Path $path -Description $o.description -ProtectedFromAccidentalDeletion $false
  }
}
foreach ($g in Rows 'groups.csv') {
  if (-not (Get-ADGroup -Filter "Name -eq '$($g.group)'")) {
    New-ADGroup -Name $g.group -GroupScope Global -GroupCategory Security -Path "$($g.ou),$dn" -Description $g.description
  }
}
foreach ($u in Rows 'users.csv') {
  if (-not (Get-ADUser -Filter "SamAccountName -eq '$($u.login)'")) {
    New-ADUser -SamAccountName $u.login -Name $u.name -GivenName $u.given_name -Surname $u.surname `
      -UserPrincipalName "$($u.login)@$fqdn" -EmailAddress $u.email -Title $u.title -Department $u.department `
      -Description $u.description -Path "$($u.ou),$dn" -AccountPassword $upw -Enabled ($u.enabled -ne 'no') `
      -ChangePasswordAtLogon $false
  }
}
foreach ($g in Rows 'groups.csv') {
  $m = $g.members -split '\|' | Where-Object { $_ }
  if ($m) { Add-ADGroupMember -Identity $g.group -Members $m }
}
'objects ok'

foreach ($name in 'Lab - Proxy', 'Lab - Wallpaper') {
  if (-not (Get-GPO -Name $name -ErrorAction SilentlyContinue)) {
    New-GPO -Name $name | New-GPLink -Target "OU=Matriz,$dn" | Out-Null
  }
}
'GPOs ok'

foreach ($r in RowsBy 'dns.csv' 'zona', 'nome', 'tipo', 'dado') {
  $zone = $r.zona -replace 'origem-samba\.lab', $fqdn
  $value = $r.dado -replace 'origem-samba\.lab', $fqdn # not $data: PowerShell names ignore case ($Data)
  if (-not (Get-DnsServerZone -Name $zone -ErrorAction SilentlyContinue)) {
    Add-DnsServerPrimaryZone -Name $zone -ReplicationScope Domain -DynamicUpdate Secure
  }
  if (Get-DnsServerResourceRecord -ZoneName $zone -Name $r.nome -ErrorAction SilentlyContinue) { continue }
  switch ($r.tipo) {
    'A' { Add-DnsServerResourceRecordA -ZoneName $zone -Name $r.nome -IPv4Address $value }
    'CNAME' { Add-DnsServerResourceRecordCName -ZoneName $zone -Name $r.nome -HostNameAlias $value }
    'PTR' { Add-DnsServerResourceRecordPtr -ZoneName $zone -Name $r.nome -PtrDomainName $value }
  }
}
'DNS ok'

# shares on the DC (as many DCs serve them): NTFS decides, the share lets everyone in
function Set-Folder([string] $path, [string[]] $rw, [string[]] $ro, [string[]] $full) {
  New-Item -ItemType Directory -Force $path | Out-Null
  $grants = @('SYSTEM:(OI)(CI)F', 'Administrators:(OI)(CI)F')
  $grants += $rw | Where-Object { $_ } | ForEach-Object { "$nb\${_}:(OI)(CI)M" }
  $grants += $ro | Where-Object { $_ } | ForEach-Object { "$nb\${_}:(OI)(CI)RX" }
  $grants += $full | Where-Object { $_ } | ForEach-Object { "$nb\${_}:(OI)(CI)F" }
  icacls $path /inheritance:r /grant:r $grants /Q | Out-Null
}
foreach ($p in RowsBy 'folders.csv' 'share', 'path', 'rw', 'ro', 'full', 'sub') {
  $path = Join-Path $Root $p.path
  Set-Folder $path ($p.rw -split ',') ($p.ro -split ',') ($p.full -split ',')
  Set-Content (Join-Path $path 'readme.txt') "contents of $($p.path)" -Encoding UTF8
  $bin = Join-Path $path 'data.bin'
  if (-not (Test-Path $bin)) { $b = New-Object byte[] 2097152; (New-Object Random).NextBytes($b); [IO.File]::WriteAllBytes($bin, $b) }
  if ($p.sub) {
    $sp, $sg = $p.sub -split ':'
    $subPath = Join-Path $Root ($sp -replace '/', '\')
    Set-Folder $subPath @($sg) @() @()
    icacls $subPath /deny "$nb\Diretoria:(OI)(CI)RX" /Q | Out-Null # a deny entry: not carried by the import
    Set-Content (Join-Path $subPath 'sigiloso.txt') 'restricted' -Encoding UTF8
  }
  if (-not (Get-SmbShare -Name $p.share -ErrorAction SilentlyContinue)) {
    New-SmbShare -Name $p.share -Path $path -FullAccess 'Everyone' | Out-Null
  }
}
'shares ok'
