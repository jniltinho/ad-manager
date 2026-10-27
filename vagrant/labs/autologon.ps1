# Sign the vagrant user in at boot (Winlogon AutoAdminLogon), for the desktop app's tests on the win11 workstation.
# The password comes from the Vagrantfile (LAB_WIN_PASSWORD, the box's public default); throwaway lab VMs only.
$ErrorActionPreference = "Stop"
$key = "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon"
Set-ItemProperty $key AutoAdminLogon "1"
Set-ItemProperty $key DefaultUserName "vagrant"
Set-ItemProperty $key DefaultDomainName $env:COMPUTERNAME
Set-ItemProperty $key DefaultPassword $env:LAB_WIN_PASSWORD
Remove-ItemProperty $key AutoLogonCount -ErrorAction SilentlyContinue
# no lock screen after idle: the tests drive the desktop
powercfg /change monitor-timeout-ac 0
New-Item -Force "HKLM:\SOFTWARE\Policies\Microsoft\Windows\Personalization" | Out-Null
Set-ItemProperty "HKLM:\SOFTWARE\Policies\Microsoft\Windows\Personalization" NoLockScreen 1 -Type DWord
Write-Output "autologon ok"
