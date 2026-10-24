#!/usr/bin/env bash
# A new Samba DC that also serves files, with the ad-manager panel and agent (scripts/samba-setup.py new-dc
# --panel --files) from the .deb the Vagrantfile copied to /tmp/fileserver. The administrator password comes from
# the 0600 file copied there and is removed afterwards; it never goes on a command line.
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive   # krb5-config's debconf screen would otherwise wait
D=/tmp/fileserver
: "${FS_REALM:?}" "${FS_DOMAIN:?}" "${FS_IP:?}" "${FS_DEB:?}"
[ -f "$D/$FS_DEB" ] && [ -f "$D/admin.pw" ] && [ -f "$D/samba-setup.py" ] || {
  echo "ERROR: run with FS_DEB and FS_ADMIN_PW_FILE set, provisioners setup-files, deb-file, pw-file, samba" >&2; exit 1; }
chmod 600 "$D/admin.pw"
findmnt /srv/shares >/dev/null || { echo "ERROR: /srv/shares is not mounted (data-disk first)" >&2; exit 1; }
nic=$(ip -o -4 addr show | awk -v ip="$FS_IP" '$4 ~ "^"ip"/" {print $2}')
AD_PASSWORD_FILE=$D/admin.pw python3 "$D/samba-setup.py" new-dc --realm "$FS_REALM" --domain "$FS_DOMAIN" \
  --ip "$FS_IP" --interfaces "lo $nic" --forwarder "${FS_FORWARDER:-1.1.1.1}" \
  --panel --files --share-root /srv/shares ${FS_IMPORT_READY:+--import-ready} --deb "$D/$FS_DEB"
shred -u "$D/admin.pw" 2>/dev/null || rm -f "$D/admin.pw"
python3 "$D/samba-setup.py" verify
