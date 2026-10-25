## 1. Environment

- [x] 1.1 `vagrant/fileserver/build-box.sh`: pinned cloud image → 40 GB local box (seed ISO, `vagrant` user)
- [x] 1.2 `vagrant/fileserver/Vagrantfile`: the box, a 200 GB data disk, host-only IP, provisioners by env
- [x] 1.3 `provision/data-disk.sh`: XFS on the empty second disk, by UUID at `/srv/shares` (`noatime`)
- [x] 1.4 `provision/xe-tools.sh`: `xe-guest-utilities`, pinned version and SHA-256
- [x] 1.5 `provision/samba.sh`: `samba-setup.py new-dc --panel --files` from env (realm, NetBIOS, IP, `.deb`)

## 2. Documentation

- [x] 2.1 `docs/fileserver-vm.md`: build, provision, verify, export OVA, convert XVA, import, swap a running DC
- [x] 2.2 Link from `docs/README.md` and `docs/dc-file-server.md`

## 3. Validation

- [x] 3.1 Build the VM, check the disks, an ACL over 4 KB on `/srv/shares`, Samba and ad-manager
- [x] 3.2 Export the OVA, convert and validate the XVA (two VDIs)
- [x] 3.3 Codex review of the change and the guide
