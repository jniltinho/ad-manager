#!/usr/bin/env bash
# Windows Server → Samba4 labs (openspec lab-windows-2019, lab-windows-2022): origem<version>.lab on a Windows DC
# (w2019, or w2022 hardened; it also serves the shares, as many DCs do) taken to
#   path A — import into a fresh destino.lab (samba-dst, samba-dst-fs): objects, DNS (--create-zones), shares with
#            their access by name, files by the panel's sync; LDAPS validated with the lab's Enterprise CA;
#   path B — migration of the DC itself: samba-mig<version> joins (functional level read from the domain), takes the
#            FSMO roles (the DNS partitions' ones seized) and SYSVOL; the Windows DC is turned off and its metadata
#            removed on the Samba side.
#
#   LAB=1 WIN_VERSION=2019|2022 bash vagrant/labs/lab-windows.sh dc|populate|harden|destination|path-a|validate|path-b|
#     isolation|all      (harden: 2022 only)
#
# Passwords: LAB_WIN_ADMIN_PASSWORD, LAB_SAMBA_DST_PASSWORD and LAB_SAMBA_USER_PASSWORD of .run/labs/secrets.env
# (lab-lib.sh); the Windows scripts read them from that file through the synced folder.
set -euo pipefail
cd "$(dirname "$0")"
# shellcheck source=lab-lib.sh
. ./lab-lib.sh
VER=${WIN_VERSION:-2022}
case $VER in
  2022) WIN=192.168.57.22 MIGIP=192.168.57.23 ;;
  2019) WIN=192.168.57.21 MIGIP=192.168.57.24 ;;
  *) echo "WIN_VERSION: 2019 or 2022" >&2; exit 1 ;;
esac
VM=w$VER MIG=samba-mig${VER: -2} MIGNAME=MIG${VER: -2}
SRC=origem$VER.lab SRCNB=ORIGEM$VER DST=destino.lab DSTNB=DESTINO
OUT=../../.run/lab-windows-$VER
mkdir -p "$OUT"
L='C:\vagrant\vagrant\labs'

win() { # its exit status is the command's: a failed step stops the lab
  local out rc=0
  out=$(vagrant winrm "$VM" -s powershell -c "$1" 2>&1) || rc=$?
  grep -vE '^\s*$|^#< CLIXML|<Objs Version' <<<"$out" || true
  # a terminating PowerShell error does not always reach WinRM's exit status
  grep -q 'FullyQualifiedErrorId' <<<"$out" && rc=${rc/#0/1}
  return "$rc"
}
wait_win() { # the machine answers WinRM (and its AD, once a DC) with the synced folder mounted (after a reboot)
  local probe='if ((Get-CimInstance Win32_ComputerSystem).DomainRole -ge 4) { Get-ADDomain | Out-Null }; "ok"'
  for _ in $(seq 60); do
    if vagrant winrm "$VM" -s powershell -c "$probe" 2>/dev/null | grep -q ok; then
      # a reboot from inside (the promotion) leaves the synced folder unmounted: Vagrant mounts it on a reload
      vagrant winrm "$VM" -s powershell -c 'Test-Path C:\vagrant\vagrant\labs' 2>/dev/null | grep -q True && return
      vagrant reload "$VM" >/dev/null
      continue
    fi
    sleep 15
  done
  echo "$VM did not come back" >&2
  return 1
}

dc() {
  wait_win
  log "$VM: forest $SRC, reboot, DNS only on the lab card, Enterprise CA (LDAPS)"
  win "& $L\\windows-dc.ps1 -Step promote -Domain $SRC -NetBIOS $SRCNB -LabIP $WIN"
  sleep 120 # the scheduled promotion runs and reboots the server
  wait_win
  local self=; [ "$VER" = 2019 ] && self=-SelfSigned # AD CS setup fails on the 2019 box (0x80072082)
  win "& $L\\windows-dc.ps1 -Step after -Domain $SRC -NetBIOS $SRCNB -LabIP $WIN $self -CaExport C:\\vagrant\\.run\\lab-windows-$VER\\ca.cer" | tee "$OUT/dc.log"
  openssl x509 -inform der -in "$OUT/ca.cer" -out "$OUT/ca.pem"
  openssl x509 -in "$OUT/ca.pem" -noout -subject
}

populate() {
  log "$VM: data set (populate-windows.ps1)"
  win "& $L\\populate-windows.ps1" | tee "$OUT/populate.log"
}

harden() {
  log "$VM: hardening (LDAP signing, channel binding, SMB signing, recycle bin, AES only), reboot"
  win "& $L\\harden-2022.ps1" | tee "$OUT/harden.log"
  vagrant reload "$VM" >/dev/null
  wait_win
}

destination() {
  log "path A: a fresh destino.lab (samba-dst, samba-dst-fs) by the .deb and the wizard"
  vagrant destroy -f samba-dst samba-dst-fs >/dev/null 2>&1 || true
  vagrant up samba-dst samba-dst-fs >"$OUT/up-dst.log" 2>&1
  install samba-dst samba-dst-fs
  wizard samba-dst "$LAB_SAMBA_DST_PASSWORD" "{\"role\":\"new-dc\",\"realm\":\"$DST\",\"domain\":\"$DSTNB\",\"ip\":\"192.168.57.40\",\"files\":false}"
  wizard samba-dst-fs "$LAB_SAMBA_DST_PASSWORD" "{\"role\":\"member\",\"realm\":\"$DST\",\"domain\":\"$DSTNB\",\"dns_dc\":\"192.168.57.40\",\"share_root\":\"/srv/shares\"}"
  register samba-dst "$LAB_SAMBA_DST_PASSWORD" samba-dst-fs dstfs 192.168.57.41
}

path_a() {
  # the member's agent restarts itself at the end of its setup: the import needs it answering
  vm samba-dst 'for _ in $(seq 60); do curl -sk -o /dev/null -w "%{http_code}" https://192.168.57.41:9443/api/health | grep -qE "^(2|401)" && { echo "samba-dst-fs agent up"; exit 0; }; sleep 5; done; echo "samba-dst-fs agent down" >&2; exit 1'
  # LDAPS validated: the lab CA trusted on the destination, the DC by the name in its certificate
  vm samba-dst "sudo install -m644 /vagrant/.run/lab-windows-$VER/ca.pem /usr/local/share/ca-certificates/origem$VER-lab-ca.crt && sudo update-ca-certificates >/dev/null && (grep -q '$VM.$SRC' /etc/hosts || echo '$WIN $VM.$SRC $VM' | sudo tee -a /etc/hosts >/dev/null) && sudo systemctl restart ad-manager && echo 'lab CA trusted on samba-dst'"
  sleep 5
  log "path A: import of $SRC into $DST (LDAPS validated, objects, DNS with --create-zones, the DC's shares)"
  printf '%s\n%s\n' "$LAB_SAMBA_DST_PASSWORD" "$LAB_WIN_ADMIN_PASSWORD" | vm samba-dst "$api_prelude
VER=$VER; WIN=$WIN"'
read -r SPW; export SPW
A=$(api GET /agents | jq ".[]|select(.name==\"dstfs\")|.id")
body() { jq -n --argjson a "$A" "{url:\"ldaps://w$VER.origem$VER.lab\",domain:\"origem$VER.lab\",user:\"Administrator\",password:env.SPW,insecure:false,objects:true,dns:true,create_zones:true,shares:{host:\"$WIN\",agent:\$a,map:\"C:\\\\shares=/srv/shares\"}}"; }
body | api POST /import/ad/plan - | jq -c "{plan: ([.plan[]?|.kind]|group_by(.)|map({(.[0]):length})|add), error}"
body | api POST /import/ad/apply - | jq -c "{failed: [.plan[]?|select(.ok==false and .kind!=\"skip\")|{kind,target,result}], error}"' | tee "$OUT/path-a-import.log"
  printf '%s\n' "$LAB_SAMBA_USER_PASSWORD" | vm samba-dst "sudo bash /vagrant/vagrant/labs/populate-samba.sh passwords $DSTNB"
  log "path A: files by the panel's sync ($VM → samba-dst-fs, NTLM, signed SMB, the target's inheritance)"
  printf '%s\n%s\n' "$LAB_SAMBA_DST_PASSWORD" "$LAB_WIN_ADMIN_PASSWORD" | vm samba-dst "$api_prelude
VER=$VER; WIN=$WIN"'
read -r SPW; export SPW
A=$(api GET /agents | jq ".[]|select(.name==\"dstfs\")|.id")
for share in Financeiro Diretoria TI Publico; do
  id=$(tr "[:upper:]" "[:lower:]" <<<"w${VER: -2}-$share")
  jq -n --arg id "$id" --arg s "$share" "{id:\$id,name:(\"$VER \"+\$s),source:{host:\"$WIN\",share:\$s,auth:\"ntlm\",user:\"Administrator\",domain:\"ORIGEM$VER\"},password:env.SPW,dest:(\"/srv/shares/\"+\$s),mode:\"copy\",reinherit:true,max_delete:0,max_delete_pct:0,enabled:false}" |
    api PUT /agents/$A/syncs/$id - | jq -c "{saved:.id,error}"
  run=$(api POST /agents/$A/syncs/$id/run | jq -r ".run // .id")
  for _ in $(seq 60); do st=$(api GET /agents/$A/syncs | jq -r --arg r "$run" ".runs[]|select(.id==\$r)|.status"); [ "$st" = running ] || [ "$st" = queued ] || break; sleep 3; done
  echo "sync $share: $(api GET /agents/$A/syncs | jq -c --arg r "$run" ".runs[]|select(.id==\$r)|{status,error,transfers:.result.transfers}")"
done' | tee "$OUT/path-a-sync.log"
}

validate() {
  log "path A validation: access by name (source NTFS × target ACL), recycle bin, SMB, files"
  local report="$OUT/path-a-validate.md"
  # the source's access by name at each share's root: allow entries of the domain, as ad-manager agent acl names them
  local ps_norm='param($p) ((Get-Acl $p).Access | Where-Object { $_.AccessControlType -eq "Allow" -and $_.IdentityReference -like "ORIGEM'$VER'\*" } |
    ForEach-Object { $r = $_.FileSystemRights.ToString(); $a = if ($r -match "FullControl") { "full" } elseif ($r -match "Modify") { "rw" } elseif ($r -match "ReadAndExecute") { "ro" } else { "custom" }
      "{0}={1}" -f ($_.IdentityReference.Value.Split("\")[1].ToLower()), $a } | Sort-Object -Unique) -join " "'
  local norm='[.aces[]|select(.type=="allow")|{n:(.name|split("\\")|last|ascii_downcase),a:.access}|select(.n|test("^(system|administrators|domain admins|creator owner|enterprise admins)$")|not)|"\(.n)=\(.a)"]|unique|join(" ")'
  {
    echo "# Path A — access by name (origem$VER.lab, Windows $VER → destino.lab)"
    echo
    echo "| Share | Source (NTFS) | Target | Same |"
    echo "|---|---|---|---|"
    grep -v '^#' data/folders.csv | while IFS=';' read -r share path _; do
      s=$(vagrant winrm "$VM" -s powershell -c "& { $ps_norm } C:\\shares\\$path" </dev/null 2>/dev/null | tr -d '\r' | grep -v '^$' | tail -1)
      d=$(vm samba-dst-fs "sudo ad-manager agent acl --json /srv/shares/$path | jq -r '$norm'" </dev/null | tr -d '\r')
      echo "| $share | $s | $d | $([ "$s" = "$d" ] && echo yes || echo **NO**) |"
    done
    echo
    echo "## Not carried over by the current path (expected)"
    echo
    echo "- A subfolder's own ACL and deny entry on \`Financeiro/Restrito\`: at the source $(vagrant winrm "$VM" -s powershell -c '((Get-Acl C:\shares\Financeiro\Restrito).Access | Where-Object { -not $_.IsInherited } | ForEach-Object { "{0} {1}" -f $_.AccessControlType, $_.IdentityReference.Value.Split("\")[1] }) -join ", "' </dev/null 2>/dev/null | tr -d '\r' | grep -v '^$' | tail -1); at the target: $(vm samba-dst-fs "sudo ad-manager agent acl --json /srv/shares/Financeiro/Restrito | jq -r 'if .protected then \"protected\" else \"inherits from the root\" end'" </dev/null | tr -d '\r')"
    echo "- AD recycle bin: deleted at the source: $(vagrant winrm "$VM" -s powershell -c '(Get-ADObject -Server localhost -IncludeDeletedObjects -Filter { isDeleted -eq $true -and objectClass -eq "user" } -Properties samAccountName).samAccountName -join ", "' </dev/null 2>/dev/null | tr -d '\r' | grep -v '^$' | tail -1); at the target: $(vm samba-dst 'sudo samba-tool user show lab.excluido >/dev/null 2>&1 && echo "CREATED (error)" || echo "not created"' </dev/null | tr -d '\r')"
    echo
    echo "## Effective access over SMB at the target"
    echo
    printf '%s\n' "$LAB_SAMBA_USER_PASSWORD" | vm samba-dst-fs 'read -r PASSWD; export PASSWD; echo ok > /tmp/t.txt
try() { r=$(smbclient "//127.0.0.1/$2" -U "DESTINO\\$1" -c "$3" 2>&1 | grep -oE "NT_STATUS_[A-Z_]+" | head -1); echo "- $1 on $2 ($4): ${r:-ok}"; }
try lab.ana Financeiro "put /tmp/t.txt t-ana.txt; del t-ana.txt" "writes: expected ok"
try lab.caio Financeiro "ls" "reads: expected ok"
try lab.caio Financeiro "put /tmp/t.txt t-caio.txt" "writes: expected ACCESS_DENIED"
try lab.edu Financeiro "ls" "expected ACCESS_DENIED"
try lab.edu Publico "ls" "reads: expected ok"
try lab.dani Diretoria "ls" "expected ACCESS_DENIED"
try lab.dani TI "put /tmp/t.txt t-dani.txt; del t-dani.txt" "writes: expected ok"'
    echo
    echo "## Files (SHA-256)"
    echo
    for p in Financeiro/data.bin Publico/readme.txt; do
      w=$(vagrant winrm "$VM" -s powershell -c "(Get-FileHash C:\\shares\\${p//\//\\}).Hash.Substring(0,16).ToLower()" </dev/null 2>/dev/null | tr -d '\r' | grep -v '^$' | tail -1)
      echo "- $p: source $w, target $(vm samba-dst-fs "sudo sha256sum /srv/shares/$p 2>/dev/null | cut -c1-16" </dev/null | tr -d '\r')"
    done
    echo
    echo "## Attributes (login|title|department|email|enabled|groups)"
    echo
    win "& $L\\user-attrs.ps1" </dev/null | tr -d '\r' | sort >"$OUT/attrs-src.txt"
    vm samba-dst "sudo bash /vagrant/vagrant/labs/samba-dc.sh user-attrs" </dev/null | tr -d '\r' | sort >"$OUT/attrs-dst.txt"
    if diff -u "$OUT/attrs-src.txt" "$OUT/attrs-dst.txt" >"$OUT/attrs.diff"; then
      echo "- the same for the $(wc -l <"$OUT/attrs-src.txt") users of the data set"
    else
      echo "- **DIFFERENCES**:"; echo; sed 's/^/    /' "$OUT/attrs.diff"
    fi
    echo
    echo "## Kerberos"
    echo
    printf '%s\n' "$LAB_SAMBA_USER_PASSWORD" | vm samba-dst 'read -r PW; export KRB5CCNAME=FILE:/tmp/krb-lab; echo "$PW" | kinit lab.ana@DESTINO.LAB >/dev/null 2>&1 && echo "- kinit lab.ana@DESTINO.LAB: ok ($(klist | grep -c krbtgt) TGT)" || echo "- kinit lab.ana@DESTINO.LAB: FAILED"; kdestroy >/dev/null 2>&1 || true'
  } | tee "$report"
}

isolation() {
  log "isolation after a reboot: the production LAN blocked on every lab VM"
  local m probe=${LAB_PROBE:-192.168.10.10} cmd
  vagrant reload "$VM" >/dev/null && wait_win
  cmd='"${env:COMPUTERNAME}: PROBE:389 " + $(if (Test-NetConnection PROBE -Port 389 -InformationLevel Quiet -WarningAction SilentlyContinue) { "REACHABLE (error)" } else { "blocked" })'
  win "${cmd//PROBE/$probe}" | tee "$OUT/isolation.log"
  for m in samba-dst samba-dst-fs "$MIG"; do
    vagrant status "$m" 2>/dev/null | grep -q running || continue
    vagrant reload "$m" >/dev/null
    cmd='echo "$(hostname): PROBE:389 $(timeout 3 bash -c "</dev/tcp/PROBE/389" 2>/dev/null && echo "REACHABLE (error)" || echo blocked)"'
    vm "$m" "${cmd//PROBE/$probe}" </dev/null | tee -a "$OUT/isolation.log"
  done
  # after path B the old Windows DC stays off (its metadata is gone from the domain)
  if vagrant status "$MIG" 2>/dev/null | grep -q running; then vagrant halt "$VM" >/dev/null; fi
  ! grep -q REACHABLE "$OUT/isolation.log"
}

path_b() {
  log "path B: $MIG joins $SRC (the domain's level), takes over FSMO and SYSVOL; the Windows DC is turned off"
  vagrant up "$MIG" >"$OUT/up-$MIG.log" 2>&1
  install "$MIG"
  wizard "$MIG" "$LAB_WIN_ADMIN_PASSWORD" "{\"role\":\"dc-join\",\"realm\":\"$SRC\",\"domain\":\"$SRCNB\",\"ip\":\"$MIGIP\",\"src_dc\":\"$WIN\",\"files\":false}"
  grep -E "functional level|joins with" "$OUT/setup-$MIG.log" || true
  local d=/vagrant/vagrant/labs/samba-dc.sh
  {
    printf '%s\n' "$LAB_WIN_ADMIN_PASSWORD" | vm "$MIG" "sudo bash $d replicate $VM.$SRC"
    printf '%s\n' "$LAB_WIN_ADMIN_PASSWORD" | vm "$MIG" "sudo bash $d take-roles"
    # the DNS partitions' roles: Windows refuses their transfer (samba-tool: UNWILLING_TO_PERFORM; the holder writing
    # fSMORoleOwner itself: "the role owner attribute could not be read"); seized, as the holder is demoted next
    vm "$MIG" "sudo bash $d seize-dns" </dev/null
    printf '%s\n' "$LAB_WIN_ADMIN_PASSWORD" | vm "$MIG" "sudo bash $d sysvol-from $WIN"
    # Windows does not demote with a Samba DC as the only other holder of the DNS partitions (1355, "could not find
    # another Active Directory Domain Controller to transfer the remaining data in DC=DomainDnsZones"), nor by force
    # here: as the Microsoft procedure for a DC that cannot be demoted, it is turned off and its metadata removed
    log "$VM: turned off; metadata removed by Samba (docs/migration-runbook.md, step 8)"
    vagrant halt "$VM" >/dev/null
    printf '%s\n' "$LAB_WIN_ADMIN_PASSWORD" | vm "$MIG" "sudo bash $d remove-dead ${VM^^}"
    vm "$MIG" "sudo bash $d cleanup" </dev/null
    vm "$MIG" "sudo bash $d report" </dev/null
  } 2>&1 | tee "$OUT/path-b.log"
}

case "${1:-}" in
  dc) dc ;;
  populate) populate ;;
  harden) harden ;;
  destination) destination ;;
  path-a) path_a ;;
  validate) validate ;;
  path-b) path_b ;;
  isolation) isolation ;;
  all) # one after the other: in an "a && b" list bash would not stop at a failure inside a
    dc
    populate
    [ "$VER" = 2022 ] && harden # the 2019 lab keeps the defaults
    destination
    path_a
    validate
    path_b
    isolation
    ;;
  *) sed -n '2,12p' "$0"; exit 1 ;;
esac
