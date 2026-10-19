#!/usr/bin/env bash
# The destination DC's side of the clients lab (lab-clients.sh), as root on samba-dst; on stdin the domain
# administrator's password, then the join account's:
#   clients-dc.sh prepare      — OU=Estacoes; lab.ingresso, delegated only to create and manage computers there (as
#                                Windows' "Delegate Control" does for computer accounts); the GPO "Lab - Estações"
#                                with one machine registry value, linked to OU=Matriz
#   clients-dc.sh delegation   — what lab.ingresso cannot do (a user in its OU, itself in Domain Admins or another
#                                group) and what it can (a computer in its OU)
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }
read -r admin_pw
read -r join_pw
export PASSWD=$admin_pw # samba-tool's password, never an argument
SAM=/var/lib/samba/private/sam.ldb
base=$(ldbsearch -H "$SAM" -s base -b "" defaultNamingContext 2>/dev/null | sed -n 's/^defaultNamingContext: //p')
ou="OU=Estacoes,$base"
computer=bf967a86-0de6-11d0-a285-00aa003049e2 # the computer class

case $1 in
prepare)
  ldbsearch -H "$SAM" -b "$ou" -s base dn >/dev/null 2>&1 || samba-tool ou add "$ou" --description="Workstations (lab)" | tail -1
  if ! samba-tool user show lab.ingresso >/dev/null 2>&1; then
    samba-tool user add lab.ingresso --random-password --description="computer joins (lab)" >/dev/null
  fi
  UPW=$join_pw python3 - <<'PY'
import os
from samba.auth import system_session
from samba.param import LoadParm
from samba.samdb import SamDB
lp = LoadParm()
lp.load_default()
db = SamDB(url=lp.samdb_url(), session_info=system_session(), lp=lp)
db.setpassword("(sAMAccountName=lab.ingresso)", os.environ["UPW"], force_change_at_next_login=False, username="lab.ingresso")
PY
  sid=$(ldbsearch -H "$SAM" "(sAMAccountName=lab.ingresso)" objectSid 2>/dev/null | sed -n 's/^objectSid: //p')
  for ace in "(OA;CI;CCDC;$computer;;$sid)" "(OA;CIIO;GA;;$computer;$sid)"; do
    samba-tool dsacl set -H "$SAM" --objectdn="$ou" --sddl="$ace" >/dev/null
  done
  echo "lab.ingresso: create and manage computers in $ou"
  name="Lab - Estações"
  # grep finds nothing on a fresh domain: not an error here
  gpo_guid() { samba-tool gpo listall | { grep -B1 "display name : $name\$" || true; } | sed -n 's/^GPO *: //p'; }
  guid=$(gpo_guid)
  if [ -z "$guid" ]; then
    samba-tool gpo create "$name" -U Administrator >/dev/null
    guid=$(gpo_guid)
  fi
  # the value and the link every time: a prepare cut halfway is completed by the next one
  j=$(mktemp)
  printf '[{"keyname":"Software\\\\Policies\\\\AdManagerLab","valuename":"Estacao","class":"MACHINE","type":"REG_DWORD","data":1}]\n' >"$j"
  samba-tool gpo load "$guid" --content="$j" -U Administrator >/dev/null
  rm -f "$j"
  if ! samba-tool gpo getlink "OU=Matriz,$base" -U Administrator 2>/dev/null | grep -qiF "$guid"; then
    samba-tool gpo setlink "OU=Matriz,$base" "$guid" -U Administrator >/dev/null
  fi
  echo "GPO $name $guid: HKLM\\Software\\Policies\\AdManagerLab\\Estacao = 1, linked to OU=Matriz"
  ;;
delegation)
  export PASSWD=$join_pw
  try() { if "$@" >/dev/null 2>&1; then echo "ALLOWED (error)"; else echo "denied"; fi; }
  echo "- create a user in OU=Estacoes: $(try samba-tool user add lab.intruso --random-password --userou=OU=Estacoes -H ldap://localhost -U lab.ingresso)"
  echo "- join Domain Admins: $(try samba-tool group addmembers 'Domain Admins' lab.ingresso -H ldap://localhost -U lab.ingresso)"
  echo "- join the Financeiro group: $(try samba-tool group addmembers Financeiro lab.ingresso -H ldap://localhost -U lab.ingresso)"
  echo "- create a computer in OU=Estacoes: $(if samba-tool computer add LABTESTE --computerou=OU=Estacoes -H ldap://localhost -U lab.ingresso >/dev/null 2>&1; then
    samba-tool computer delete LABTESTE -H ldap://localhost -U lab.ingresso >/dev/null 2>&1; echo allowed; else echo "DENIED (error)"; fi)"
  ;;
*)
  sed -n '2,8p' "$0"
  exit 1
  ;;
esac
