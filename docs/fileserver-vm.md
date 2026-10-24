# File-serving DC VM (Vagrant → OVA → XCP-ng)

How to build, from scratch, an Ubuntu 26.04 VM that is a Samba DC **and** serves files, with the ad-manager panel and
agent, its shares on a **separate XFS data disk**, and move it to XCP-ng (or XenServer) as an XVA. The files are in
`vagrant/fileserver/`; the OpenSpec change is `openspec/changes/fileserver-dc-vm`. Examples use the fictitious
`domain.local`; production addresses are written as `192.0.2.x`.

## Why a separate XFS disk

Samba keeps each file's Windows ACL in the extended attribute `security.NTACL` (`vfs objects = acl_xattr`), next to
the POSIX access and default ACLs. On **ext4** without the `ea_inode` feature, all of an inode's extended attributes
must fit **one 4 KB block**. Deep folders migrated from Windows reach 3.7–3.9 KB; adding one entry to a share's
permissions (the "From another AD" import adds *Domain Admins: full control* and pushes it down the tree) then fails
on some folders with `NT_STATUS_DISK_FULL` / "disk full" (`ENOSPC`) — with gigabytes free. **XFS** stores extended
attributes up to 64 KB per value in their own blocks, is Samba's recommended filesystem for file servers and grows
online. Keeping `/srv/shares` on its own disk also lets the data be grown, snapshotted or moved apart from the system.

| Disk | Size | Layout |
|------|------|--------|
| system (`sda`, XCP-ng `xvda`) | 40 GB | ext4 `/`, **no LVM** (the official cloud image's layout) |
| data (`sdb`, XCP-ng `xvdb`) | 200 GB | one GPT partition, **XFS** labelled `shares`, by UUID at `/srv/shares`, `defaults,noatime` |

XFS options: the xfsprogs 6.x defaults (`crc=1`, `finobt=1`, `reflink=1`, `bigtime=1`, `inobtcount=1`); XFS always
has user extended attributes and POSIX ACLs, so no mount option is needed for Samba.

## Requirements (host)

- VirtualBox 7, Vagrant 2.4, `genisoimage`, `curl`.
- For XCP-ng: [`ova2xva`](https://github.com/jniltinho/ova2xva) (Go, converts OVA → XVA without `xe`) or `qemu-img`, and access to the
  pool master (Xen Orchestra or XAPI).

## 1. The box (40 GB system disk, once per host)

```bash
vagrant/fileserver/build-box.sh
```

It downloads the **pinned** Ubuntu 26.04 cloud image (`ubuntu-26.04-server-cloudimg-amd64.ova`, release folder and
SHA-256 in the script; `IMAGE_FILE=/path/to.ova` reuses a local copy, still checked), resizes its disk to 40 GB,
boots it once with a cloud-init seed that creates the `vagrant` user (Vagrant's public insecure keys, replaced at the
first `vagrant up`), sets DHCP on the first NIC and disables cloud-init, and packages the box
`local/ubuntu-26.04-40g`. The VM has a SATA controller with the system disk on port 0 and port 1 free for the data
disk. (The `bento` boxes carry a 64 GB disk with LVM, and VirtualBox cannot shrink a disk.)

## 2. The VM (disks and guest tools)

```bash
cd vagrant/fileserver
FS_NAME=dc01 FS_IP=192.168.56.20 vagrant up
```

- `data-disk.sh`: finds the single empty disk of `FS_DATA_GB` (200) GiB, refuses to format a disk that already has a
  partition table or a filesystem signature, creates the XFS and its `/etc/fstab` line by UUID.
- `xe-tools.sh`: `xe-guest-utilities` from the xenserver GitHub releases (version and SHA-256 pinned; inert on
  VirtualBox, active on XCP-ng: IP, memory and disks in the pool's UI).

Other settings: `FS_DATA_GB`, `FS_MEMORY` (8192), `FS_CPUS` (2), `FS_BOX`. The data disk is
`vagrant/fileserver/.vagrant/<name>-data.vdi`. **`vagrant destroy` deletes it with the VM** (VirtualBox removes the
attached disks); to keep the shares, detach it first:
`VBoxManage storageattach <name> --storagectl SATA --port 1 --device 0 --medium none`.

## 3. Samba DC + ad-manager

```bash
install -m 600 /dev/null ~/dc01-admin.pw && $EDITOR ~/dc01-admin.pw        # the new domain's administrator password
FS_REALM=domain.local FS_DOMAIN=DOMAIN FS_DEB=$PWD/../../dist/ad-manager_X_amd64.deb FS_ADMIN_PW_FILE=~/dc01-admin.pw \
  vagrant provision --provision-with setup-files,deb-file,pw-file,samba
```

With `FS_IMPORT_READY=1` the agent also gets the GPO-import and password-migration write modes for the panel's
*From another AD* import (`--import-ready`: `import_modes = true`, the administrator credential encrypted in the panel's
database, no password on the VM; remove it in *Settings* after the import; see
[password-migration.md](password-migration.md)) — without it, ticking **Passwords** fails with "no agent on a
domain controller of this domain with the password migration mode on".

`samba.sh` runs `scripts/samba-setup.py new-dc --panel --files --share-root /srv/shares` (see
[dc-file-server.md](dc-file-server.md)); the password is read from the 0600 file copied into the VM and erased after,
never from a command line. For a custom certificate (a local CA, LDAPS and the panel), install it as
[deployment.md](deployment.md) describes.

## 4. Check

```bash
vagrant ssh -c 'findmnt /srv/shares; findmnt /; lsblk; xfs_info /srv/shares | head -3'
vagrant ssh -c 'sudo samba-tool domain info 127.0.0.1; systemctl is-active samba-ad-dc ad-manager ad-manager-agent'
# extended attributes over one 4 KB block on a folder (access + default ACLs, ~6.5 KB): fine on XFS, ENOSPC on ext4
vagrant ssh -c 'sudo bash -c "mkdir -p /srv/shares/acltest && for i in \$(seq 2000 2400); do setfacl -m u:\$i:rwx,d:u:\$i:rwx /srv/shares/acltest; done && getfattr -d -m ^system.posix_acl -e hex /srv/shares/acltest | awk -F= \"/=/{s+=length(\\\$2)/2-1} END{print s, (s>4096 ? \\\"bytes: over 4 KB, OK\\\" : \\\"bytes: test too small\\\")}\" && rm -rf /srv/shares/acltest"'
```

The panel answers at `https://<FS_IP>:8443`.

## 5. Export (OVA) and convert (XVA)

Prepare the VM for its first boot on XCP-ng **before** exporting: there it has one NIC, `enX0`, none of the
VirtualBox network files match it, cloud-init is off and the `vagrant` user has no password — without these steps it
boots with no network and no login.

```bash
vagrant ssh -c 'sudo passwd root'                    # a console login on XCP-ng (or add an admin user with its key)
vagrant ssh -c 'sudo install -m 600 /dev/stdin /etc/netplan/99-production.yaml' < 99-production.yaml   # section 6
# Samba binds to named interfaces (bind interfaces only = yes): add enX0 now, VirtualBox ignores a missing one
vagrant ssh -c "sudo sed -i -E 's/^([[:space:]]*interfaces[[:space:]]*=.*)\$/\1 enX0/' /etc/samba/smb.conf && sudo testparm -s >/dev/null"
```

The production netplan only names `enX0`, so it does nothing on VirtualBox. Then:

```bash
vagrant halt dc01
VBoxManage export dc01 --ovf10 -o dc01.ova                  # both disks, streamOptimized VMDKs
ova2xva convert -o dc01.xva --name dc01 --memory 8192 --vcpus 2 dc01.ova
ova2xva validate dc01.xva                                    # structure and SHA-1 checks: two VDIs
```

## 6. Import on XCP-ng

- **Xen Orchestra**: *Import → VM*, the XVA, the pool's storage repository (SR) for both VDIs; or
  `OVA2XVA_HOST=<pool master> ova2xva import --sr <sr-uuid> --start dc01.xva`.
- Give the VM one VIF on the server network, keep the name (it is the DC's hostname), mode HVM with PV drivers
  (`xe-guest-utilities` reports IP and memory).
- First boot (the console with the root password, or SSH once the netplan of section 5 is up): disable the VirtualBox
  files `01-first-nic.yaml` and Vagrant's host-only one, set `interfaces = lo enX0` in `smb.conf` if it still names
  the VirtualBox NIC, point `/etc/hosts` and the DC's DNS A/PTR records to the new address (`samba_dnsupdate`),
  restart `samba-ad-dc`, and check `samba-tool dbcheck`, the panel and the agent.

```yaml
# /etc/netplan/99-production.yaml (0600)
network:
  version: 2
  ethernets:
    enX0:
      addresses: [192.0.2.20/24]
      routes: [{to: default, via: 192.0.2.1}]
      nameservers: {addresses: [127.0.0.1], search: [domain.local]}
```

## 7. Replacing a running file-serving DC with the new VM

A VM built with `new-dc` is a **new domain** (new SID), even with the same realm and name. To take the place of a
running DC:

1. Build and check the new VM (sections 1–4) with the **same hostname** as the running one.
2. With the new VM still on its lab address, run the panel's *Import/export → From another AD* against the running DC
   (objects, DNS, GPOs, shares; with `FS_IMPORT_READY=1`, the passwords too) and copy the files with the agent's sync
   jobs or an SMB copy that writes each share into **its share path** (the import maps `E:\X` to `/srv/shares/X`).
   To keep the domain SID instead, do **not** run section 3: join the unprovisioned VM as an additional DC with its
   own, unique hostname (`samba-setup.py dc-join`), move the roles and demote the old DC (see
   [migration-runbook.md](migration-runbook.md)); a VM that already ran `new-dc` cannot join.
3. Export, convert and import it (sections 5–6), stop the old VM, give the new one the old address (DNS records,
   `/etc/hosts`), start it and repeat the checks; keep the old VM stopped (not deleted) until the clients are verified.
