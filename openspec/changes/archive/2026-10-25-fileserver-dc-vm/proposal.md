## Why

A Samba DC that also serves files keeps its shares under `/srv/shares`. When that folder sits on the system's ext4
root without `ea_inode`, every file's extended attributes (the NT ACL Samba stores in `security.NTACL` plus the POSIX
ACLs) must fit one 4 KB block. Deep trees migrated from Windows reach 3.7–3.9 KB, and adding one entry to a share's
permissions (the "From another AD" import adds Domain Admins: full control) fails on some folders with "disk full"
(ENOSPC) although the disk has free space. The file-serving VM needs its own data disk, on a filesystem made for it,
and a documented, repeatable way to build it and move it to XCP-ng.

## What Changes

- A Vagrant environment `vagrant/fileserver/` for an Ubuntu 26.04 file-serving Samba DC:
  - a 40 GB system disk, no LVM, from the official Ubuntu cloud image (a local box built by `build-box.sh`, image and
    checksum pinned), the layout of the VMs already running on XCP-ng;
  - a separate 200 GB data disk, XFS, mounted by UUID at `/srv/shares` (`noatime`): no small per-file limit on
    extended attributes, crc, reflink and big timestamps on;
  - `xe-guest-utilities` (pinned release and SHA-256) for XCP-ng;
  - the Samba DC with the ad-manager panel and agent through `scripts/samba-setup.py new-dc --panel --files`.
- The export to XCP-ng: VirtualBox OVA with both disks, converted to XVA (`ova2xva`), imported into the pool.
- `docs/fileserver-vm.md`: how to build, provision, check, export, convert, import and swap a file-serving DC VM.

## Capabilities

### New Capabilities
- `fileserver-vm`: the reproducible file-serving DC VM — disks, filesystem, guest tools, provisioning and export.

### Modified Capabilities

## Impact

- New `vagrant/fileserver/` (Vagrantfile, box builder, provisioning) and `docs/fileserver-vm.md`; no change to the
  ad-manager code.
- Hosts need VirtualBox 7, Vagrant 2.4, `genisoimage` (seed ISO) and, for the conversion, `ova2xva` (Go) or
  `qemu-img`.
