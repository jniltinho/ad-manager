#!/usr/bin/env bash
# Steps of the DC migration of the Samba lab (lab-samba.sh path-b), as root on the machine; the domain
# administrator's password on stdin when the step needs it:
#   samba-dc.sh replicate SOURCE   — pull every partition from the source DC now, then the summary
#   samba-dc.sh take-roles         — transfer the seven FSMO roles to this DC
#   samba-dc.sh seize-dns          — seize the DNS partitions' roles (a Windows holder refuses their transfer); only
#                                    when that holder is demoted right after
#   samba-dc.sh sysvol-from HOST   — the SYSVOL of a Windows DC copied here over SMB (Samba does not take part in
#                                    DFS-R), then sysvol-reset
#   samba-dc.sh sysvol-reset       — after the SYSVOL copy: its ACLs as the domain wants them, then the check
#   samba-dc.sh demote             — demote this DC (only with no role left here), then stop and disable it
#   samba-dc.sh remove-dead NAME   — the metadata of a DC removed by force (a Windows DC demoted with -ForceRemoval)
#   samba-dc.sh cleanup            — on the DC that stays: the references to the demoted one (dbcheck --fix)
#   samba-dc.sh report             — roles, DCs, GPOs, SYSVOL and database checks
#   samba-dc.sh user-attrs         — the data set's users as the lab compares them (user-attrs.ps1 prints the same
#                                    from a Windows DC): login|title|department|email|enabled|groups
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }
step=$1
needpw() { read -r pw; export PASSWD=$pw; } # samba-tool's password, never an argument
me=$(hostname -s | tr '[:lower:]' '[:upper:]')
base=$(ldbsearch -H /var/lib/samba/private/sam.ldb -s base -b "" defaultNamingContext 2>/dev/null | sed -n 's/^defaultNamingContext: //p')

case $step in
replicate)
  needpw
  for nc in "$base" "CN=Configuration,$base" "CN=Schema,CN=Configuration,$base" "DC=DomainDnsZones,$base" "DC=ForestDnsZones,$base"; do
    samba-tool drs replicate "${me,,}" "$2" "$nc" -U Administrator 2>&1 | tail -1
  done
  samba-tool drs showrepl --summary 2>&1 | tail -2
  ;;
take-roles)
  needpw
  samba-tool fsmo transfer --role=all -U Administrator 2>&1 | grep -iE "transfer|already|error" || true
  echo "roles held by $me: $(samba-tool fsmo show | grep -c "CN=$me,")/7"
  ;;
seize-dns)
  for role in domaindns forestdns; do
    samba-tool fsmo show | grep -iE "^${role}.*CN=$me," >/dev/null && continue
    samba-tool fsmo seize --role=$role --force 2>&1 | grep -iE "seiz|role|error" | head -2
  done
  echo "roles held by $me: $(samba-tool fsmo show | grep -c "CN=$me,")/7"
  ;;
sysvol-from)
  needpw
  realm=$(testparm -s --parameter-name=realm 2>/dev/null | tr '[:upper:]' '[:lower:]')
  dom=$(testparm -s --parameter-name=workgroup 2>/dev/null)
  # through a file: tar stops reading before smbclient ends its archive, and the SIGPIPE would fail the pipe
  t=$(mktemp)
  smbclient "//$2/SYSVOL" -U "$dom\\Administrator" -m SMB3 -Tc "$t" "$realm" >/dev/null 2>&1
  tar -C /var/lib/samba/sysvol -xf "$t"
  rm -f "$t"
  echo "SYSVOL of $2: $(find "/var/lib/samba/sysvol/$realm/Policies" -maxdepth 1 -mindepth 1 | wc -l) GPOs"
  samba-tool ntacl sysvolreset
  samba-tool ntacl sysvolcheck && echo "sysvolcheck: ok"
  ;;
sysvol-reset)
  samba-tool ntacl sysvolreset
  samba-tool ntacl sysvolcheck && echo "sysvolcheck: ok"
  ;;
demote)
  needpw
  if samba-tool fsmo show | grep -q "CN=$me,"; then
    echo "this DC still holds FSMO roles: transfer them before demoting" >&2
    exit 1
  fi
  samba-tool domain demote -U Administrator 2>&1 | tail -2
  systemctl disable --now samba-ad-dc >/dev/null 2>&1
  echo "$me demoted and stopped"
  ;;
remove-dead)
  needpw
  samba-tool domain demote --remove-other-dead-server="$2" -U Administrator 2>&1 | tail -2
  ;;
cleanup)
  # dbcheck exits non-zero when it found something, fixed or not: the second run says (and returns) what is left
  samba-tool dbcheck --cross-ncs --fix --yes 2>&1 | tail -1 || true
  rc=0
  out=$(samba-tool dbcheck --cross-ncs 2>&1) || rc=$?
  echo "after the fix: $(tail -1 <<<"$out")"
  [ "$rc" = 0 ] || exit "$rc"
  ;;
report)
  echo "roles held by $me: $(samba-tool fsmo show | grep -c "CN=$me,")/7"
  echo "DCs: $(ldbsearch -H /var/lib/samba/private/sam.ldb -b "OU=Domain Controllers,$base" "(objectClass=computer)" cn 2>/dev/null | sed -n 's/^cn: //p' | tr '\n' ' ')"
  echo "GPOs: $(samba-tool gpo listall | sed -n 's/^display name *: //p' | tr '\n' ';')"
  samba-tool ntacl sysvolcheck >/dev/null 2>&1 && echo "sysvolcheck: ok" || echo "sysvolcheck: ERROR"
  echo "dbcheck: $(samba-tool dbcheck --cross-ncs 2>&1 | tail -1)"
  ;;
user-attrs)
  data=/vagrant/vagrant/labs/data
  groups=$(tail -n +2 "$data/groups.csv" | cut -d';' -f1 | tr '[:upper:]' '[:lower:]')
  tail -n +2 "$data/users.csv" | cut -d';' -f1 | while read -r u; do
    e=$(ldbsearch -H /var/lib/samba/private/sam.ldb "(sAMAccountName=$u)" title department mail userAccountControl memberOf 2>/dev/null)
    v() { # a value as LDIF writes it: "attr: text", or "attr:: base64" when it is not plain ASCII (accents)
      local l
      l=$(grep -m1 "^$1:" <<<"$e" || true)
      case $l in
      "$1:: "*) base64 -d <<<"${l#"$1":: }" ;;
      "$1: "*) echo "${l#"$1": }" ;;
      esac
    }
    uac=$(v userAccountControl)
    g=$(sed -n 's/^memberOf: CN=\([^,]*\),.*/\1/p' <<<"$e" | tr '[:upper:]' '[:lower:]' | { grep -xF "$groups" || true; } | sort | paste -sd, -)
    echo "$u|$(v title)|$(v department)|$(v mail)|$([ $((${uac:-0} & 2)) = 0 ] && echo yes || echo no)|$g"
  done
  ;;
*)
  sed -n '2,17p' "$0"
  exit 1
  ;;
esac
