# Hardens the Windows lab DC on purpose (lab-windows-2022): what the AD Manager must cope with. Idempotent; a reboot
# after it applies the LDAP and Kerberos settings.
#   LDAP signing required, LDAP channel binding "always", SMB signing required, the AD recycle bin on with a deleted
#   user in it, Kerberos with AES only.
param([string] $Secrets = 'C:\vagrant\.run\labs\secrets.env')
# the value of a KEY=value line of secrets.env as bash reads it: 'v', "v" or v
function Read-EnvValue([string] $line) {
  $v = ($line -split '=', 2)[1]
  if ($v -match "^'(.*)'$" -or $v -match '^"(.*)"$') { $v = $Matches[1] }
  $v
}
$ErrorActionPreference = 'Stop'
Import-Module ActiveDirectory
# the forest-wide changes (the recycle bin) need Enterprise Admins: the domain's Administrator, not the Vagrant account
$line = Get-Content $Secrets | Where-Object { $_ -match '^LAB_WIN_ADMIN_PASSWORD=' } | Select-Object -First 1
$admin = New-Object PSCredential("$((Get-ADDomain).NetBIOSName)\Administrator",
  (ConvertTo-SecureString (Read-EnvValue $line) -AsPlainText -Force))
$ntds = 'HKLM:\SYSTEM\CurrentControlSet\Services\NTDS\Parameters'
Set-ItemProperty $ntds -Name LDAPServerIntegrity -Value 2 -Type DWord             # signing required
Set-ItemProperty $ntds -Name LdapEnforceChannelBinding -Value 2 -Type DWord       # channel binding always
Set-SmbServerConfiguration -RequireSecuritySignature $true -EnableSecuritySignature $true -Force
# Kerberos: AES only (AES128 0x8 + AES256 0x10), for the DC and for the accounts created from here on
$kerb = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System\Kerberos\Parameters'
New-Item $kerb -Force | Out-Null
Set-ItemProperty $kerb -Name SupportedEncryptionTypes -Value 0x18 -Type DWord
Get-ADComputer $env:COMPUTERNAME | Set-ADComputer -KerberosEncryptionType AES128, AES256
Get-ADUser -Filter "SamAccountName -like 'lab.*'" | Set-ADUser -KerberosEncryptionType AES128, AES256
# the recycle bin with something in it: the import must not bring deleted users back
$dom = Get-ADDomain
if (-not (Get-ADOptionalFeature -Filter "Name -eq 'Recycle Bin Feature'").EnabledScopes) {
  Enable-ADOptionalFeature 'Recycle Bin Feature' -Scope ForestOrConfigurationSet -Target $dom.Forest -Confirm:$false `
    -Credential $admin -WarningAction SilentlyContinue
}
if (-not (Get-ADObject -Filter "samAccountName -eq 'lab.excluido'" -IncludeDeletedObjects)) {
  New-ADUser -SamAccountName lab.excluido -Name 'Deleted Lab' -Path "OU=Matriz,$($dom.DistinguishedName)"
  Remove-ADUser lab.excluido -Confirm:$false
}
'LDAP signing: ' + (Get-ItemProperty $ntds).LDAPServerIntegrity + ' · channel binding: ' + (Get-ItemProperty $ntds).LdapEnforceChannelBinding
'SMB signing required: ' + (Get-SmbServerConfiguration).RequireSecuritySignature
'recycle bin: ' + [bool](Get-ADOptionalFeature -Filter "Name -eq 'Recycle Bin Feature'").EnabledScopes +
  ' · deleted: ' + ((Get-ADObject -Filter 'isDeleted -eq $true -and objectClass -eq "user"' -IncludeDeletedObjects).Name -join ', ')
'Kerberos: ' + (Get-ItemProperty $kerb).SupportedEncryptionTypes + ' (0x18 = AES128 + AES256)'
