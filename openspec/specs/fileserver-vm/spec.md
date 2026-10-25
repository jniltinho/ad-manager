# fileserver-vm Specification

## Purpose
TBD - created by archiving change fileserver-dc-vm. Update Purpose after archive.

## Requirements

### Requirement: Separate XFS data disk
The file-serving DC VM SHALL keep `/srv/shares` on its own data disk formatted XFS and mounted by UUID with
`noatime`, apart from a 40 GB system disk without LVM.

#### Scenario: Disks after provisioning
- **WHEN** the VM is built with `vagrant up`
- **THEN** `findmnt /srv/shares` shows an XFS filesystem labelled `shares` on the second disk, and `/` is ext4 on
  the 40 GB first disk without LVM

#### Scenario: Large ACLs
- **WHEN** a folder under `/srv/shares` gets an ACL whose extended attributes exceed 4 KB
- **THEN** the change succeeds (no ENOSPC), unlike on ext4 without `ea_inode`

#### Scenario: Never formats a used disk
- **WHEN** the data disk already has a partition table or a filesystem signature
- **THEN** provisioning stops without formatting it

### Requirement: Ready for XCP-ng
The VM SHALL carry `xe-guest-utilities` (pinned version and SHA-256) and SHALL export to an OVA with both disks that
converts to an XVA importable into XCP-ng.

#### Scenario: Export and convert
- **WHEN** the operator exports the VM and runs `ova2xva convert`
- **THEN** `ova2xva validate` accepts the XVA, which holds two VDIs (system and data)

### Requirement: Documented provisioning
The repository SHALL document how to build, provision, verify, export, convert, import and swap the VM, with a
fictitious domain only.

#### Scenario: From scratch
- **WHEN** an operator follows `docs/fileserver-vm.md` on a host with VirtualBox and Vagrant
- **THEN** they obtain a running file-serving DC with the panel and the agent, and its XVA
