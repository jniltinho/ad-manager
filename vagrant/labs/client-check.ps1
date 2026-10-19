# Checks of a Windows client of destino.lab (lab-clients.sh), as the local administrator over WinRM:
#   -Step status        — secure channel, the computer's OU, the lab GPO's registry value, gpresult (computer)
#   -Step user -User u  — the domain user u logs on (a network logon through WinRM on this machine: Kerberos from
#                         the DC) and lists its tickets; then this session maps a share as u and reads a file (inside
#                         u's WinRM session the share would be a second hop; -Share '' skips it). Its password from
#                         the secrets file. A failure throws.
param(
  [Parameter(Mandatory)] [ValidateSet('status', 'user')] [string] $Step,
  [string] $User = 'lab.ana',
  [string] $Share = '\\dstfs.destino.lab\Publico',
  [string] $Secrets = 'C:\vagrant\.run\labs\secrets.env'
)
function Read-EnvValue([string] $line) {
  $v = ($line -split '=', 2)[1]
  if ($v -match "^'(.*)'$" -or $v -match '^"(.*)"$') { $v = $Matches[1] }
  $v
}
$ErrorActionPreference = 'Stop'
switch ($Step) {
  'status' {
    $sc = (nltest /sc_query:destino.lab 2>&1 | Select-String 'Trusted DC Connection Status|Status =' | Select-Object -First 1)
    "secure channel: $($sc -replace '\s+', ' ')"
    $gp = gpupdate /target:computer /force 2>&1 | Out-String # the policies first, then what they set
    if ($LASTEXITCODE -ne 0) { throw "gpupdate failed: $($gp.Trim())" }
    $value = (Get-ItemProperty 'HKLM:\SOFTWARE\Policies\AdManagerLab' -ErrorAction SilentlyContinue).Estacao
    "Lab - Estações GPO value: $(if ($null -ne $value) { $value } else { 'absent' })"
    # the XML report: the text one comes in the console's code page and garbles accented names
    $xml = Join-Path $env:TEMP gpresult.xml
    gpresult /scope computer /x $xml /f | Out-Null
    $applied = ([xml](Get-Content $xml -Raw)).Rsop.ComputerResults.GPO | Where-Object { $_.IsValid -eq 'true' -and $_.FilterAllowed -eq 'true' -and $_.AccessDenied -ne 'true' } | ForEach-Object Name
    Remove-Item $xml
    "computer GPOs (gpresult): $(($applied | Where-Object { $_ -ne 'Local Group Policy' }) -join '; ')"
  }
  'user' {
    # the user may log on here through WinRM (a network logon on this machine)
    Add-LocalGroupMember -Group 'Remote Management Users' -Member "DESTINO\$User" -ErrorAction SilentlyContinue
    $line = Get-Content $Secrets | Where-Object { $_ -match '^LAB_SAMBA_USER_PASSWORD=' } | Select-Object -First 1
    $plain = Read-EnvValue $line
    $cred = New-Object PSCredential("DESTINO\$User", (ConvertTo-SecureString $plain -AsPlainText -Force))
    try {
      Invoke-Command -ComputerName $env:COMPUTERNAME -Credential $cred -Authentication Kerberos -ScriptBlock {
        "logon: $(whoami)"
        "tickets: $((klist | Select-String 'Server: ').Count)"
      }
    } catch { throw "logon of $User refused: $($_.Exception.Message.Split([Environment]::NewLine)[0])" }
    if (-not $Share) { return }
    try {
      New-SmbMapping -RemotePath $Share -UserName "DESTINO\$User" -Password $plain -ErrorAction Stop | Out-Null
      "share ${Share} read as ${User}: $(Get-Content "$Share\readme.txt" -ErrorAction Stop)"
    } catch { throw "share ${Share} as ${User}: $($_.Exception.Message.Split([Environment]::NewLine)[0])" }
    finally { Remove-SmbMapping -RemotePath $Share -Force -ErrorAction SilentlyContinue }
  }
}
