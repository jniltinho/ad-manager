# migration-lab Specification

## Purpose
Isolated Vagrant labs (Samba 4, Windows Server 2019/2022 and domain clients) that validate the import and DC migration to Samba 4.

## Requirements

### Requirement: Samba 4 source lab
The project SHALL have an isolated Vagrant lab with a Samba 4 DC and a member file server on Ubuntu 26.04, populated
with the common dataset, and SHALL validate the import and the DC migration to another Samba 4, including the NT
ACLs (`security.NTACL`) of the files.

#### Scenario: Access preserved by name
- **WHEN** path A moves the shares from `samba-fs` to `samba-dst-fs`
- **THEN** each group and user of the dataset has the same access level (read, write, full control) on the root of
  each share, and the report lists the deny ACEs and the subfolder ACLs that were not carried over

#### Scenario: SYSVOL after the migration
- **WHEN** path B finishes with `samba-src` demoted
- **THEN** the dataset GPOs are in the target SYSVOL and `samba-tool ntacl sysvolcheck` reports no error

### Requirement: Hardened Windows Server 2022 lab
The project SHALL have an isolated Vagrant lab with a Windows Server 2022 DC populated with the common dataset and
with LDAP signing, channel binding, SMB signing and the AD Recycle Bin enabled, and SHALL validate the import and the
DC migration to Samba 4 with the same `validate.sh`.

#### Scenario: Import with the hardened AD
- **WHEN** path A runs against `origem2022.lab` with LDAP signing required
- **THEN** the import connects over validated LDAPS and `validate.sh` exits with 0

#### Scenario: Join at level 2016
- **WHEN** `samba-dst` joins as an additional DC with `--dc-level 2016`
- **THEN** the preflight passes, replication converges and a join with an incompatible level is rejected before
  changing the domain

#### Scenario: Objects in the Recycle Bin
- **WHEN** the source has users in the AD Recycle Bin
- **THEN** the import does not create them and the report lists them as "deleted at the source"

### Requirement: Windows Server 2019 lab
The project SHALL have a Vagrant lab isolated from the LAN that brings up a Windows Server 2019 DC populated with the
versioned dataset (OUs, users, nested groups, computers, GPOs, DNS, shares with folders, files and NTFS ACLs) and
SHALL automatically validate that this data reaches Samba 4 through the import and through the DC migration.

#### Scenario: Validated import
- **WHEN** the operator runs `vagrant up w2019 samba-dst` and lab path A
- **THEN** `validate.sh` exits with 0, showing only the expected import differences

#### Scenario: Validated migration
- **WHEN** path B finishes with the Windows DC demoted
- **THEN** `validate.sh` finds no difference in objects, GPOs or SYSVOL and a sample user signs in to Samba with
  Kerberos

#### Scenario: Isolation
- **WHEN** any lab VM, Linux or Windows, tries to reach 192.168.10.0/24, including after a reboot and through the
  Vagrant NAT adapter
- **THEN** the connection is blocked on the guest

### Requirement: Samba 4 domain clients
The project SHALL have an isolated Vagrant lab that joins Windows 11, Windows Server 2022 (with RSAT) and Ubuntu
26.04 to the AD Manager Samba 4 domain through an account delegated only to create computers in one OU, and SHALL
automatically validate join, DNS registration, Kerberos logon and share access on all three, GPOs on the Windows
machines and Samba administration through RSAT. A failure in any check SHALL end the step with an error.

#### Scenario: Windows 11 workstation
- **WHEN** `win11` joins `destino.lab` and a sample user logs on
- **THEN** the secure channel is active, `klist` shows the domain ticket, `gpresult` lists the OU GPOs and the share
  opens a file with the user's credential

#### Scenario: Ubuntu
- **WHEN** `ubuntu-desk` joins through `realm join` and the sample user signs in over SSH with a password
- **THEN** `id <user>@destino.lab` resolves, the home folder is created, there is a Kerberos ticket, the share opens
  with Kerberos and the machine appears in the domain DNS

#### Scenario: Delegated join account
- **WHEN** the join account tries to create a user or join Domain Admins or another group
- **THEN** the DC rejects it, and only computer creation in the delegated OU is accepted

#### Scenario: Move to an OU with a GPO
- **WHEN** support moves `win11` to an OU with a GPO in AD Manager
- **THEN** after `gpupdate` the GPO registry value appears on the machine and `gpresult` lists it

#### Scenario: Disabled computer account
- **WHEN** support disables the `win11` account in AD Manager
- **THEN** the workstation cannot re-establish the secure channel with the DC (`ERROR_NO_TRUST_SAM_ACCOUNT`), and
  re-enabling it restores the channel
