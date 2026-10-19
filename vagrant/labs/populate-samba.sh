#!/usr/bin/env bash
# Fills the Samba source of the lab (lab-samba.sh populate) with the common data set (vagrant/labs/data), as root
# on the machine:
#   populate-samba.sh dc DOMAIN   — on the DC: objects by the ad-manager CLI, users' passwords, GPOs, DNS;
#                                   stdin: the administrator's password, then the users' password
#   populate-samba.sh fs DOMAIN   — on the file server: folders with their Windows ACLs (ad-manager agent acl set),
#                                   a subfolder ACL of its own, files
#   populate-samba.sh passwords   — on a DC: the data set's users get the lab password (stdin); the disabled ones
#                                   stay disabled
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "run as root" >&2; exit 1; }
DATA=/vagrant/vagrant/labs/data
role=$1 nb=$2

# rows of a ;-separated file of the data set, without the comment line (empty fields kept)
rows() { grep -v '^#' "$DATA/$1"; }

# set_passwords: every user of users.csv gets $UPW through Samba's API (never an argument); setting a password
# enables the account, so the ones the data set has disabled are disabled again
set_passwords() {
  # shellcheck disable=SC2046
  python3 - $(tail -n +2 "$DATA/users.csv" | cut -d';' -f1) <<'PY'
import os, sys
from samba.auth import system_session
from samba.param import LoadParm
from samba.samdb import SamDB
lp = LoadParm()
lp.load_default()
db = SamDB(url=lp.samdb_url(), session_info=system_session(), lp=lp)
for u in sys.argv[1:]:
    db.setpassword(f"(sAMAccountName={u})", os.environ["UPW"], force_change_at_next_login=False, username=u)
    print("password of", u)
PY
  tail -n +2 "$DATA/users.csv" | awk -F';' '$10 == "no" {print $1}' | while read -r u; do
    samba-tool user disable "$u" >/dev/null && echo "$u stays disabled"
  done
}

case $role in
dc)
  read -r pw
  read -r upw
  export PASSWD=$pw # samba-tool's password, never an argument
  rm -rf /root/lab-data && cp -r "$DATA" /root/lab-data
  AD_USER=Administrator AD_PASSWORD=$pw ad-manager import -d /root/lab-data --apply | tail -2
  UPW=$upw set_passwords
  for g in "Lab - Proxy" "Lab - Wallpaper"; do
    samba-tool gpo listall | grep -q "display name : $g$" || samba-tool gpo create "$g" -U Administrator | tail -1
  done
  rows dns.csv | cut -d';' -f1 | sort -u | while read -r z; do
    samba-tool dns zonelist 127.0.0.1 -U Administrator | grep -q "pszZoneName *: $z$" ||
      samba-tool dns zonecreate 127.0.0.1 "$z" -U Administrator | tail -1
  done
  rows dns.csv | while IFS=';' read -r z n t d; do
    samba-tool dns add 127.0.0.1 "$z" "$n" "$t" "$d" -U Administrator 2>&1 | grep -v ALREADY_EXISTS | tail -1 || true
  done
  ;;
fs)
  rows folders.csv | while IFS=';' read -r _share path rw ro full sub; do
    args=()
    for g in ${rw//,/ }; do args+=(--rw "$nb\\$g"); done
    for g in ${ro//,/ }; do args+=(--ro "$nb\\$g"); done
    for g in ${full//,/ }; do args+=(--full "$nb\\$g"); done
    ad-manager agent acl set "/srv/shares/$path" "${args[@]}" >/dev/null && echo "folder $path"
    printf 'contents of %s\n' "$path" >"/srv/shares/$path/readme.txt"
    head -c 2097152 /dev/urandom >"/srv/shares/$path/data.bin"
    if [ -n "$sub" ]; then
      ad-manager agent acl set "/srv/shares/${sub%%:*}" --rw "$nb\\${sub#*:}" >/dev/null && echo "subfolder ${sub%%:*}"
      echo restricted >"/srv/shares/${sub%%:*}/sigiloso.txt"
    fi
  done
  ;;
passwords)
  read -r upw
  UPW=$upw set_passwords
  ;;
*)
  sed -n '2,9p' "$0"
  exit 1
  ;;
esac
