#!/usr/bin/env bash
# The data disk: one GPT partition, XFS labelled "shares", mounted by UUID at /srv/shares with noatime. XFS keeps
# extended attributes up to 64 KB per value in their own blocks (ext4 without ea_inode has one 4 KB block for all of
# an inode's: Samba's NT ACL plus the POSIX ACLs of a deep migrated tree overflow it, "disk full"). xfsprogs 6.x
# defaults: crc, finobt, reflink, bigtime, inobtcount. Never formats a disk that already has a partition table or a
# filesystem signature.
set -euo pipefail
MNT=/srv/shares
GB=${DATA_GB:-200}
if grep -q " $MNT " /etc/fstab; then
  mount -a
  findmnt "$MNT" >/dev/null && { echo "data disk ok (already set up)"; findmnt -no SOURCE,FSTYPE,OPTIONS "$MNT"; exit 0; }
fi
command -v mkfs.xfs >/dev/null || DEBIAN_FRONTEND=noninteractive apt-get install -y -qq xfsprogs >/dev/null
root=$(lsblk -no PKNAME "$(findmnt -no SOURCE /)" | tail -1)
dev=$(lsblk -dnbo NAME,SIZE,TYPE | awk -v r="$root" -v s=$((GB * 1024 * 1024 * 1024)) \
  '$3=="disk" && $1!=r && $2==s {print "/dev/"$1}')
[ -n "$dev" ] && [ "$(echo "$dev" | wc -l)" = 1 ] || { echo "ERROR: no single unused ${GB} GiB disk (found: '$dev')" >&2; exit 1; }
if [ -n "$(blkid -p "$dev" 2>/dev/null)" ] || [ -n "$(lsblk -no NAME "$dev" | tail -n +2)" ]; then
  echo "ERROR: $dev is not empty; refusing to format" >&2; exit 1
fi
parted -s "$dev" mklabel gpt mkpart shares xfs 1MiB 100%
udevadm settle
part=$(lsblk -lnpo NAME,TYPE "$dev" | awk '$2=="part"{print $1; exit}')
mkfs.xfs -q -L shares "$part"
mkdir -p "$MNT"
echo "UUID=$(blkid -s UUID -o value "$part") $MNT xfs defaults,noatime 0 2" >> /etc/fstab
systemctl daemon-reload
mount "$MNT"
xfs_info "$MNT" | grep -oE "(crc|finobt|reflink|bigtime|inobtcount)=[01]" | tr '\n' ' '; echo
df -h "$MNT"
