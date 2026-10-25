## Context

The first file-serving DC VM was made from the Ubuntu 26.04 cloud image with one 200 GB disk (root and `/srv/shares`
together, ext4, no LVM). ext4 keeps all of an inode's extended attributes in the inode and at most one 4 KB block
unless the filesystem has `ea_inode`; Samba's `acl_xattr` stores a hashed NT ACL (often 1–2 KB on a folder with
inheritable entries) next to the POSIX access and default ACLs, so a few ACEs more push deep folders past the block.

## Goals / Non-Goals

**Goals**: a data filesystem that does not limit ACL size in practice; the system disk small and separate (grow or
replace the data disk alone); one documented build from scratch to an XVA; the same tools on the new VM as on the
running one.

**Non-Goals**: LVM, RAID or quotas inside the guest (the hypervisor's storage does that); changing ad-manager.

## Decisions

- **XFS for `/srv/shares`.** Extended attributes up to 64 KB per value, in their own blocks; Samba's recommended
  filesystem for file servers; online grow (`xfs_growfs`) when the VDI is enlarged. `mkfs.xfs` defaults of
  xfsprogs 6.x (crc, finobt, reflink, bigtime, inobtcount) and a `shares` label; mounted by UUID with
  `defaults,noatime` (XFS always has user xattrs and POSIX ACLs, no options needed). Rejected: ext4 with `ea_inode`
  (works, but an unusual feature for a root filesystem and not settable online on older kernels).
- **System disk 40 GB, no LVM, from the cloud image.** The `bento` box carries a 64 GB disk with LVM and VirtualBox
  cannot shrink a disk; `build-box.sh` imports the pinned cloud-image OVA, resizes its disk to 40 GB (cloud-init's
  `growpart` grows `/` at first boot), adds a seed ISO with the `vagrant` user and packages a local box.
- **Guest tools**: `xe-guest-utilities` from the xenserver GitHub releases, version and SHA-256 pinned; inert on
  VirtualBox.
- **Provisioning**: `scripts/samba-setup.py new-dc --panel --files --share-root /srv/shares` (the repository's
  standard), realm, NetBIOS name, IP and the ad-manager `.deb` given by environment variables; the administrator
  password only from a 0600 file.
- **Export**: `VBoxManage export` (OVF 1.0, both disks) → `ova2xva convert` → XVA with both VDIs; the network
  moves from the host-only NIC to the pool's network at the first boot on XCP-ng.

## Risks / Trade-offs

- [A fresh DC is a new domain SID] → swapping a running DC for the new VM means importing again (or a `dc-join`);
  the guide states it.
- [Cloud-image changes] → image and checksum pinned in `build-box.sh`.
