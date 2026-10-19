#!/usr/bin/env bash
# Domain clients lab (openspec: migration-lab): Windows 11 (win11), a Windows Server 2022 member with RSAT
# (ws2022m) and Ubuntu (ubuntu-desk) join destino.lab (samba-dst, samba-dst-fs: made by lab-windows.sh or
# lab-samba.sh) by a delegated account, then the everyday checks and the AD Manager in real use.
#
#   LAB=1 bash vagrant/labs/lab-clients.sh prepare|up|join|validate|roteiro|all
#
# Passwords: LAB_SAMBA_DST_PASSWORD, LAB_JOIN_PASSWORD and LAB_SAMBA_USER_PASSWORD of .run/labs/secrets.env
# (lab-lib.sh); the Windows scripts read them from that file through the synced folder.
set -euo pipefail
cd "$(dirname "$0")"
# shellcheck source=lab-lib.sh
. ./lab-lib.sh
OUT=../../.run/lab-clients
mkdir -p "$OUT"
L='C:\vagrant\vagrant\labs'

wwin() { # wwin VM COMMAND: PowerShell on a Windows client; its exit status is the command's
  local out rc=0
  out=$(vagrant winrm "$1" -s powershell -c "$2" 2>&1) || rc=$?
  grep -vE '^\s*$|^#< CLIXML|<Objs Version' <<<"$out" || true
  grep -q 'FullyQualifiedErrorId' <<<"$out" && rc=${rc/#0/1} # a PowerShell error WinRM's status may not carry
  return "$rc"
}
wait_winrm() { # the machine answers WinRM again (after a reboot)
  for _ in $(seq 60); do vagrant winrm "$1" -s powershell -c '"ok"' 2>/dev/null | grep -q ok && return; sleep 10; done
  echo "$1 did not come back" >&2
  return 1
}
# panel METHOD PATH [BODY]: the destino.lab panel, as Administrator; an HTTP error fails (its body on the output)
panel() { printf '%s\n' "$LAB_SAMBA_DST_PASSWORD" | vm samba-dst "$api_prelude
H+=(--fail-with-body); api $1 '$2' ${3:+'$3'}"; }

prepare() {
  log "samba-dst: OU=Estacoes, delegated account lab.ingresso, GPO Lab - Estações linked to OU=Matriz"
  printf '%s\n%s\n' "$LAB_SAMBA_DST_PASSWORD" "$LAB_JOIN_PASSWORD" | vm samba-dst "sudo bash /vagrant/vagrant/labs/clients-dc.sh prepare"
  log "minimal delegation: what the join account cannot do"
  printf '%s\n%s\n' "$LAB_SAMBA_DST_PASSWORD" "$LAB_JOIN_PASSWORD" | vm samba-dst "sudo bash /vagrant/vagrant/labs/clients-dc.sh delegation" | tee "$OUT/delegation.log"
  ! grep -qE "ALLOWED|DENIED" "$OUT/delegation.log"
}

up() {
  vagrant up win11 ws2022m ubuntu-desk >"$OUT/up.log" 2>&1
}

join() {
  log "the clients join by the delegated account"
  wwin win11 "& $L\\client-join.ps1"
  wwin ws2022m "& $L\\client-join.ps1 -Rsat"
  vagrant reload win11 ws2022m >/dev/null
  wait_winrm win11
  wait_winrm ws2022m
  printf '%s\n' "$LAB_JOIN_PASSWORD" | vm ubuntu-desk "sudo bash /vagrant/vagrant/labs/ubuntu-client.sh join"
}

# expect REPORT REGEX...: every regex matches a line of the report, or the step fails naming what is missing
expect() {
  local report=$1 re miss=0
  shift
  for re in "$@"; do grep -qE -- "$re" "$report" || { echo "MISSING from the report: $re" >&2; miss=1; }; done
  return "$miss"
}

validate() {
  local report="$OUT/validate.md"
  {
    echo "# Clients of destino.lab"
    echo
    echo "## In the panel (Computers and DNS)"
    echo
    panel GET /computers | tr -d '\r' | jq -r '.[] | select(.name|test("^(WIN11|WS2022M|UDESK)$";"i")) | "- \(.name): \(.dn | split(",")[1]) · \(.os // "?") · DNS \(.dns_name // "?") · \(if .enabled then "enabled" else "disabled" end)"'
    for h in win11 ws2022m udesk; do
      echo "- DNS $h.destino.lab: $(vm samba-dst "host -t A $h.destino.lab 127.0.0.1 | sed -n 's/.* has address //p' | paste -sd' '" </dev/null | tr -d '\r')"
    done
    for m in win11 ws2022m; do
      echo
      echo "## $m"
      echo
      wwin "$m" "& $L\\client-check.ps1 -Step status" </dev/null | tr -d '\r' | sed 's/^/- /'
      wwin "$m" "& $L\\client-check.ps1 -Step user -User lab.ana" </dev/null | tr -d '\r' | sed 's/^/- /'
    done
    echo
    echo "## ws2022m: RSAT administering Samba"
    echo
    wwin ws2022m "& $L\\rsat-check.ps1" </dev/null | tr -d '\r'
    echo
    echo "## ubuntu-desk"
    echo
    printf '%s\n' "$LAB_SAMBA_USER_PASSWORD" | vm ubuntu-desk "sudo bash /vagrant/vagrant/labs/ubuntu-client.sh check" | tr -d '\r' | sed 's/^/- /'
    vm ubuntu-desk "sudo bash /vagrant/vagrant/labs/ubuntu-client.sh adsys" </dev/null | tr -d '\r' | sed 's/^/- /'
  } | tee "$report"
  expect "$report" '^- WIN11: OU=' '^- WS2022M: OU=' '^- UDESK: OU=' \
    'DNS win11\.destino\.lab: 192\.168\.57\.61$' 'DNS ws2022m\.destino\.lab: 192\.168\.57\.62$' \
    'DNS udesk\.destino\.lab: 192\.168\.57\.63$' 'ADUC .*CN=Ana Lab' 'GPMC .*Lab - Estações' \
    'dnscmd .*destino\.lab' 'ticket: krbtgt/DESTINO\.LAB' 'share \(Kerberos\): .*contents of Publico' \
    'home folder: /home/lab\.ana@destino\.lab$'
  [ "$(grep -cE 'secure channel: .*NERR_Success' "$report")" = 2 ] || { echo "MISSING: the secure channel on both Windows clients" >&2; return 1; }
  [ "$(grep -cE 'read as lab\.ana: contents of Publico' "$report")" = 2 ] || { echo "MISSING: the share on both Windows clients" >&2; return 1; }
}

roteiro() {
  local report="$OUT/roteiro.md"
  {
    echo "# The AD Manager in real use"
    echo
    echo "## Move to an OU with a GPO (panel) → gpupdate on Windows"
    echo
    # the starting point, so it can run again: WIN11 where it joined, out of the GPO's reach
    panel POST /computers/WIN11%24/move '{"ou":"OU=Estacoes,DC=destino,DC=lab"}' >/dev/null
    echo "- before: $(wwin win11 "& $L\\client-check.ps1 -Step status" </dev/null | grep 'GPO value' | tr -d '\r')"
    panel POST /computers/WIN11%24/move '{"ou":"OU=Matriz,DC=destino,DC=lab"}' | tr -d '\r' | head -c 200
    echo "- moved by the panel to OU=Matriz"
    wwin win11 "& $L\\client-check.ps1 -Step status" </dev/null | tr -d '\r' | sed 's/^/- after: /'
    echo
    echo "## SMB session in the panel"
    echo
    # the mapping lives as long as that WinRM logon: the panel sees the session while it lasts
    wwin win11 '$l = Get-Content C:\vagrant\.run\labs\secrets.env | Where-Object { $_ -match "^LAB_SAMBA_USER_PASSWORD=" }; $p = (($l -split "=", 2)[1]).Trim([char]39, [char]34); New-SmbMapping -RemotePath \\dstfs.destino.lab\Publico -UserName DESTINO\lab.ana -Password $p | Out-Null; "mapped"' </dev/null | tr -d '\r' | sed 's/^/- win11: /'
    panel GET '/computers/sessions?host=dstfs.destino.lab' | tr -d '\r' | jq -r '"- sessions on dstfs: " + ([.sessions[]? | "\(.user) from \(.client)"] | join(", "))'
    echo
    echo "## Disable the computer (panel) → secure channel"
    echo
    # what disabling does: the station can no longer set up its secure channel (logons that need the DC through it,
    # NTLM and the first interactive logon, fail). The channel already up lasts until it is set up again (a reboot,
    # or nltest /sc_reset here), and Samba's KDC still gives Kerberos service tickets for the disabled station.
    # cmd /c: nltest's error text on stderr would make PowerShell report an error of its own
    trap 'panel POST /computers/WIN11%24/enable >/dev/null' EXIT # enabled again whatever fails below
    sc() { wwin win11 'cmd /c "nltest /sc_reset:destino.lab 2>&1"' </dev/null | tr -d '\r' | grep -m1 -E 'Status =|failed' || true; }
    panel POST /computers/WIN11%24/disable >/dev/null
    echo "- WIN11 disabled by the panel"
    echo "- secure channel set up again: $(sc)"
    wwin win11 "& $L\\client-check.ps1 -Step user -User lab.caio -Share ''" </dev/null | tr -d '\r' | sed 's/^/- lab.caio (Kerberos network logon): /'
    panel POST /computers/WIN11%24/enable >/dev/null
    trap - EXIT
    echo "- WIN11 enabled again; secure channel set up again: $(sc)"
  } | tee "$report"
  expect "$report" 'before: Lab - Estações GPO value: absent' 'after: Lab - Estações GPO value: 1$' \
    'after: computer GPOs .*Lab - Estações' 'sessions on dstfs: .*lab\.ana from 192\.168\.57\.61' \
    '^- secure channel set up again: .*1787' 'enabled again; secure channel set up again: .*NERR_Success'
}

case "${1:-}" in
  prepare) prepare ;;
  up) up ;;
  join) join ;;
  validate) validate ;;
  roteiro) roteiro ;;
  all) # one after the other: in an "a && b" list bash would not stop at a failure inside a
    prepare
    up
    join
    validate
    roteiro
    ;;
  *) sed -n '2,9p' "$0"; exit 1 ;;
esac
