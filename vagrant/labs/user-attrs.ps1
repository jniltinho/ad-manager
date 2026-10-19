# The data set's users as the lab compares them, one line each:
#   login|title|department|email|enabled|the data set's groups (sorted)
# from a Windows DC (the Samba side: samba-dc.sh user-attrs prints the same).
param([string] $Data = 'C:\vagrant\vagrant\labs\data')
$ErrorActionPreference = 'Stop'
Import-Module ActiveDirectory
$groups = (Import-Csv (Join-Path $Data 'groups.csv') -Delimiter ';' -Encoding UTF8).group
foreach ($u in Import-Csv (Join-Path $Data 'users.csv') -Delimiter ';' -Encoding UTF8) {
  $a = Get-ADUser $u.login -Server localhost -Properties Title, Department, EmailAddress, Enabled, MemberOf
  $g = ($a.MemberOf | ForEach-Object { ($_ -split ',')[0] -replace '^CN=', '' } | Where-Object { $groups -contains $_ } |
    ForEach-Object { $_.ToLower() } | Sort-Object) -join ','
  '{0}|{1}|{2}|{3}|{4}|{5}' -f $u.login, $a.Title, $a.Department, $a.EmailAddress, $(if ($a.Enabled) { 'yes' } else { 'no' }), $g
}
