# Windows lab DC (lab-windows.sh), in steps, as the Vagrant user over WinRM. Passwords come from the lab's secrets file
# in the synced folder (outside git), never from an argument:
#   windows-dc.ps1 -Step promote  -Domain origem2022.lab -NetBIOS ORIGEM2022   — new forest (reboot after it)
#   windows-dc.ps1 -Step after [-SelfSigned] -CaExport FILE                   — DNS only on the lab card, the DC's
#                                                                               certificate for LDAPS (an Enterprise CA,
#                                                                               or -SelfSigned: one for its own name),
#                                                                               the certificate to trust exported, status
param(
  [Parameter(Mandatory)] [ValidateSet('promote', 'after')] [string] $Step,
  [string] $Domain = 'origem2022.lab',
  [string] $NetBIOS = 'ORIGEM2022',
  [string] $LabIP = '192.168.57.22',
  [string] $Secrets = 'C:\vagrant\.run\labs\secrets.env',
  [switch] $SelfSigned,
  [string] $CaExport
)
# the value of a KEY=value line of secrets.env as bash reads it: 'v', "v" or v
function Read-EnvValue([string] $line) {
  $v = ($line -split '=', 2)[1]
  if ($v -match "^'(.*)'$" -or $v -match '^"(.*)"$') { $v = $Matches[1] }
  $v
}
$ErrorActionPreference = 'Stop'

function Get-Secret([string] $Name) {
  $line = Get-Content $Secrets | Where-Object { $_ -match "^$Name=" } | Select-Object -First 1
  if (-not $line) { throw "$Name is not in $Secrets" }
  ConvertTo-SecureString (Read-EnvValue $line) -AsPlainText -Force
}

switch ($Step) {
  'promote' {
    if ((Get-CimInstance Win32_ComputerSystem).DomainRole -ge 4) { 'already a DC'; return }
    $pw = Get-Secret 'LAB_WIN_ADMIN_PASSWORD'
    # the local Administrator becomes the domain's: it gets the lab password first
    Set-LocalUser -Name Administrator -Password $pw -PasswordNeverExpires $true
    Enable-LocalUser -Name Administrator
    Install-WindowsFeature AD-Domain-Services, DNS -IncludeManagementTools | Out-Null
    # the promotion turns the local accounts into the domain's and drops this WinRM session: it runs as SYSTEM in a
    # scheduled task that reboots at the end; the caller waits for the domain to answer
    $job = 'C:\Windows\Temp\lab-promote.ps1'
    @"
`$line = Get-Content '$Secrets' | Where-Object { `$_ -match '^LAB_WIN_ADMIN_PASSWORD=' } | Select-Object -First 1
`$pw = ConvertTo-SecureString ((`$line -split '=', 2)[1].Trim([char]39, [char]34)) -AsPlainText -Force # 'v', "v" or v
Import-Module ADDSDeployment
Install-ADDSForest -DomainName '$Domain' -DomainNetbiosName '$NetBIOS' -SafeModeAdministratorPassword `$pw -InstallDns -Force *> C:\Windows\Temp\lab-promote.log
"@ | Set-Content $job -Encoding UTF8
    $action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument "-NoProfile -ExecutionPolicy Bypass -File $job"
    Register-ScheduledTask -TaskName lab-promote -Action $action -User SYSTEM -RunLevel Highest -Force | Out-Null
    Start-ScheduledTask -TaskName lab-promote
    "promotion of $Domain started (scheduled task; the server reboots at the end)"
  }
  'after' {
    # only the lab card in DNS: the Vagrant NAT card's address would be what other machines get for the DC
    $lab = Get-NetIPAddress -IPAddress $LabIP
    Get-NetAdapter | Where-Object { $_.ifIndex -ne $lab.InterfaceIndex } |
      ForEach-Object { Set-DnsClient -InterfaceIndex $_.ifIndex -RegisterThisConnectionsAddress $false }
    dnscmd /ResetListenAddresses $LabIP | Out-Null
    Register-DnsClient
    Restart-Service Netlogon # the DC's names registered again, now without the NAT card
    Start-Sleep 15
    foreach ($zone in $Domain, "_msdcs.$Domain") { # what was registered before: out
      Get-DnsServerResourceRecord -ZoneName $zone -RRType A -ErrorAction SilentlyContinue |
        Where-Object { $_.RecordData.IPv4Address.IPAddressToString -like '10.0.2.*' } |
        Remove-DnsServerResourceRecord -ZoneName $zone -Force
    }
    $fqdn = "$env:COMPUTERNAME.$Domain".ToLower()
    if ($SelfSigned) {
      # a certificate for its own name in the computer's store: the DC answers LDAPS with it (the 2019 lab: AD CS
      # setup fails there with 0x80072082)
      $cert = Get-ChildItem Cert:\LocalMachine\My | Where-Object { $_.Subject -eq "CN=$fqdn" } | Select-Object -First 1
      if (-not $cert) {
        $cert = New-SelfSignedCertificate -DnsName $fqdn -CertStoreLocation Cert:\LocalMachine\My -NotAfter (Get-Date).AddYears(5) `
          -KeyUsage DigitalSignature, KeyEncipherment -TextExtension @('2.5.29.37={text}1.3.6.1.5.5.7.3.1')
      }
      # the DC serves LDAPS only with a certificate it trusts itself: its own one among the trusted roots
      if (-not (Get-ChildItem Cert:\LocalMachine\Root | Where-Object Thumbprint -eq $cert.Thumbprint)) {
        $tmp = Join-Path $env:TEMP 'lab-dc.cer'
        Export-Certificate -Cert $cert -FilePath $tmp -Force | Out-Null
        Import-Certificate -FilePath $tmp -CertStoreLocation Cert:\LocalMachine\Root | Out-Null
        Remove-Item $tmp
        Restart-Service NTDS -Force
        for ($i = 0; $i -lt 30; $i++) { # AD answers again (and its web services, for the cmdlets that follow)
          try { Get-ADDomain -ErrorAction Stop | Out-Null; break } catch { Start-Sleep 10 }
        }
      }
      $trust = $cert
    } else {
      # Enterprise CA: the DC enrolls a Domain Controller certificate by itself, and LDAPS (636) answers with it
      if (-not (Get-Service CertSvc -ErrorAction SilentlyContinue)) {
        Install-WindowsFeature ADCS-Cert-Authority -IncludeManagementTools | Out-Null
      }
      if ((Get-Service CertSvc).Status -ne 'Running') {
        # an Enterprise CA writes to the configuration partition: the domain's Administrator (Enterprise Admins), not
        # the Vagrant account
        $admin = New-Object PSCredential("$NetBIOS\Administrator", (Get-Secret 'LAB_WIN_ADMIN_PASSWORD'))
        Install-AdcsCertificationAuthority -CAType EnterpriseRootCA -CACommonName "$NetBIOS Lab CA" -Credential $admin `
          -OverwriteExistingKey -OverwriteExistingDatabase -Force | Out-Null
      }
      certutil -pulse | Out-Null
      for ($i = 0; $i -lt 30; $i++) {
        $cert = Get-ChildItem Cert:\LocalMachine\My | Where-Object { $_.EnhancedKeyUsageList.FriendlyName -contains 'Server Authentication' }
        if ($cert) { break }
        Start-Sleep 10
        certutil -pulse | Out-Null
      }
      $trust = Get-ChildItem Cert:\LocalMachine\Root | Where-Object Subject -like "*$NetBIOS Lab CA*" | Select-Object -First 1
    }
    if ($CaExport -and $trust) { Export-Certificate -Cert $trust -FilePath $CaExport -Force | Out-Null }
    $ldaps = Test-NetConnection $LabIP -Port 636 -InformationLevel Quiet -WarningAction SilentlyContinue
    "DC: $((Get-ADDomain).DNSRoot) · forest/domain levels: $((Get-ADForest).ForestMode)/$((Get-ADDomain).DomainMode)"
    "DC certificate: $(if ($cert) { $cert[0].Subject } else { 'NOT YET' }) · LDAPS 636: $ldaps"
    "the DC's DNS (server): $((Resolve-DnsName "$env:COMPUTERNAME.$Domain" -Type A -Server $LabIP -DnsOnly).IPAddress -join ', ')"
  }
}
