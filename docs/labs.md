# Installation and migration labs (`vagrant/labs`)

Isolated Vagrant machines to prove ad-manager end to end: the `.deb`, the setup wizard, the import from
another AD, folder sync and DC migration.

- **Network**: host-only `192.168.57.0/24`. The first provisioner of every VM (`isolate-linux.sh`) blocks the
  production LANs (`192.168.10.0/24` and `192.168.20.0/24`) and fails if they respond.
- **Guard**: every command requires `LAB=1` (`vagrant/labs/Vagrantfile` and the scripts stop without it).
- **Passwords**: kept in `.run/labs/secrets.env`, outside git, and reach the VMs through standard input or an
  environment variable. They never go into a process argument, a versioned file or a log.
- **Installation**: as an operator would do it. The newest `.deb` in `dist/` (`make deb`) is installed, the services
  are enabled and each machine goes through the setup wizard via the API (`setup-wizard.sh`): login with the one-time
  token, preview, execution and log.

| VM | Address | Role |
|---|---|---|
| `setup-test` | 192.168.57.50 | wizard on a clean machine (`initial-setup`), DC + files |
| `restore-test` | 192.168.57.51 | "Restore a DC" role with the DC backup from `setup-test` |
| `samba-src` | 192.168.57.30 | DC of `origem-samba.lab`, with the panel |
| `samba-fs` | 192.168.57.31 | file server of `origem-samba.lab` (member with the agent) |
| `samba-mig` | 192.168.57.32 | path B: additional DC of `origem-samba.lab` that takes over the domain |
| `samba-dst` | 192.168.57.40 | path A: DC of `destino.lab`, with the panel |
| `samba-dst-fs` | 192.168.57.41 | path A: file server of `destino.lab` |
| `w2019` | 192.168.57.21 | Windows Server 2019 DC of `origem2019.lab` (Windows defaults), which also serves the shares |
| `samba-mig19` | 192.168.57.24 | path B for 2019 |
| `w2022` | 192.168.57.22 | hardened Windows Server 2022 DC of `origem2022.lab`, which also serves the shares |
| `samba-mig22` | 192.168.57.23 | Windows path B: Samba DC that joins `origem2022.lab` and takes over the domain |
| `win11` | 192.168.57.61 | Windows 11 workstation of `destino.lab`; signs `vagrant` in at boot (`autologon.ps1`, the box's public password or `LAB_WIN_PASSWORD`) and never locks, for the desktop app's tests |
| `ws2022m` | 192.168.57.62 | Windows Server 2022 member of `destino.lab`, with RSAT |
| `ubuntu-desk` | 192.168.57.63 | Ubuntu 26.04 client of `destino.lab` (realmd/sssd) |

## Samba4 → Samba4 (`lab-samba.sh`)

```bash
make deb
cd vagrant/labs && LAB=1 vagrant up samba-src samba-fs samba-mig samba-dst samba-dst-fs && cd ../..
LAB=1 bash vagrant/labs/lab-samba.sh all     # or: setup, populate, path-a, validate, path-b
```

The dataset lives in `vagrant/labs/data`. It is synthetic data, with nothing from production:

- `ous.csv`, `groups.csv`, `users.csv`, `computers.csv`: the `ad-manager export` format, imported through the CLI;
- `folders.csv`: shares and levels per group, plus a subfolder with its own ACL;
- `dns.csv`: sample records and a reverse zone that the destination does not have.

| Step | What it does |
|---|---|
| `setup` | `.deb` and wizard on the 5 VMs: `new-dc` (src, dst), `member` (fs, dst-fs), `dc-join` (mig); registers the agents in the panels |
| `populate` | on the source: objects (`ad-manager import`), passwords, 2 GPOs, DNS with the reverse zone (`populate-samba.sh dc`); folders with Windows ACLs and files (`populate-samba.sh fs`); shares through the panel |
| `path-a` | in the destination panel: "From another AD" import with objects, DNS with `create_zones` and shares; files through Sync (NTLM, destination inheritance) |
| `validate` | access by name at the root of each share, source × destination; what is not carried over; real SMB access with destination users; file hashes |
| `path-b` | `samba-mig` replicates, receives the 7 FSMO roles and SYSVOL (copy with xattrs and `sysvolreset`); `samba-src` is demoted and stopped (`samba-dc.sh`) |

Output in `.run/lab-samba/` (setup logs of each machine, `path-a-validate.md`, `path-b.log`).

### Result (2026-10-19, from scratch, `all` with that day's `.deb`)

- **Setup through the wizard**: all 5 machines `ok` (`new-dc` ×2, `member` ×2, `dc-join`), agents registered.
- **Path A**: the "From another AD" import planned and wrote without failures 3 OUs, 4 groups, 6 users,
  6 memberships, 1 computer, 2 zones (`zone.create`), 4 records and 4 shares. Sync brought the files over
  (4 jobs `ok`).
- **Validation**:
  - access by name **identical** on the 4 shares;
  - 7 SMB tests with destination users as expected (write, read, `ACCESS_DENIED`);
  - file hashes identical on source and destination;
  - not carried over (expected): the subfolder `Financeiro/Restrito`'s own ACL, which inherits from the root on the
    destination.
- **Path B**: replication of the 5 partitions, the 7 FSMO roles on `samba-mig`, SYSVOL copied and `sysvolcheck` ok,
  `samba-src` demoted, dataset GPOs on the remaining DC, `dbcheck` with 0 errors after `cleanup`.

### What the lab fixed in the product

- **Shares without administrators**: `valid users` with only the chosen groups locked out Domain Admins
  (the "From another AD" import got `ACCESS_DENIED` on a server managed by ad-manager). Now the domain group
  (resolved by RID 512) is always included and does not appear in the list.
- **Member**: `net rpc rights grant` right after the join failed (`NO_LOGON_SERVERS`). The script now waits for
  `wbinfo -t` and retries.
- **DC with more than one network card**: the DC registered the NAT card's address in DNS, and replication went
  through it. Now `new-dc`, `dc-join` and `restore-dc` bind Samba only to the `--ip` card (`interfaces`,
  `bind interfaces only`).
- **DC `/etc/hosts`**: the server name on 127.x (Vagrant) sent the DRS tools to a closed port. Now the short name
  and the FQDN point to `--ip`.
- **Wizard translations**: 10 server messages in the setup had no pt-BR entry in the catalog.

## Windows Server → Samba4 (`lab-windows.sh`: 2019 and hardened 2022)

```bash
make deb
cd vagrant/labs && LAB=1 vagrant up w2022 && cd ../..
LAB=1 WIN_VERSION=2022 bash vagrant/labs/lab-windows.sh all   # or WIN_VERSION=2019 (without harden)
```

The Windows boxes have a pinned version in `vagrant/labs/Vagrantfile`. The 2019 one is the last published, because
the maintainer stopped publishing that version. On the Linux VMs, Vagrant replaces the default key on first boot.
On the Windows ones, the WinRM password stays the box's, protected by the isolated host-only network.

The Windows VMs have their own isolation (`isolate-windows.ps1`, Windows Firewall rules). The PowerShell scripts
read the passwords from `secrets.env` through the shared folder and are saved as UTF-8 with BOM, because
Windows PowerShell 5.1 reads a file without BOM as ANSI.

| Step | What it does |
|---|---|
| `dc` | promotes `w2022` to DC of `origem2022.lab` (scheduled task as SYSTEM, which reboots by itself); only the lab network card in DNS; enterprise CA, so the DC has a certificate and answers LDAPS; exports the CA certificate |
| `populate` | the same dataset (`populate-windows.ps1`): OUs, groups, users, linked GPOs, DNS with a reverse zone, shares on the DC itself with NTFS, a subfolder with its own ACL and a deny entry |
| `harden` | LDAP signing required, channel binding "always", SMB signing required, AD Recycle Bin with a deleted user, Kerberos AES only (`harden-2022.ps1`) |
| `destination` | new `destino.lab` (`samba-dst`, `samba-dst-fs`) through the `.deb` and the wizard |
| `path-a` | the lab CA trusted on `samba-dst`; "From another AD" import over **validated LDAPS** (`insecure: false`) with objects, DNS with `create_zones` and shares; files through Sync (NTLM, signed SMB) |
| `validate` | access by name (source NTFS × destination ACL), recycle bin, SMB with destination users, file hashes, **attributes** of each user (title, department, email, status, groups: `user-attrs.ps1` × `samba-dc.sh user-attrs`), **Kerberos** (`kinit` of a migrated user) |
| `path-b` | `samba-mig<version>` joins (functional level read from the domain: 2016), replicates, receives the FSMO roles and SYSVOL; Windows is shut down and its metadata is removed by Samba |
| `isolation` | reboots each VM and checks that the production LAN is still blocked on it |

### 2022 result (2026-10-19)

- **Path A**:
  - import without failures: 3 OUs, 4 groups, 7 users, 8 memberships, 2 zones, 4 records and 4 shares; the 10
    Windows built-in groups were skipped (see the fix below);
  - access by name **identical** on the 4 shares;
  - 7 SMB tests as expected;
  - identical hashes;
  - `lab.excluido`, which was in the source's recycle bin, was **not** created;
  - not carried over (expected): the own ACL and the deny entry of `Financeiro/Restrito`.
- **Channel binding** (`LdapEnforceChannelBinding = 2`):

  | Bind | Result |
  |---|---|
  | simple over LDAPS (AD Manager's) | ok |
  | simple without TLS | refused: `Strong(er) authentication required` |
  | GSSAPI over LDAPS without token (`SASL_CBINDING=none`) | refused: `80090346` (SEC_E_BAD_BINDINGS) |
  | GSSAPI over LDAPS with token (`tls-endpoint`) | ok |

  AD Manager does a simple bind over LDAPS and is not affected. With LDAP signing required, LDAPS with a trusted CA
  is mandatory.
- **Path B**:
  - the join used the 2016 level read from the domain, and replication of the 5 partitions gave `[ALL GOOD]`;
  - 5 FSMO roles by transfer; the 2 DNS partition roles by `seize`, because Windows refuses the transfer
    (`UNWILLING_TO_PERFORM`);
  - SYSVOL with the 4 GPOs and `sysvolcheck` ok;
  - Windows did not demote: it refused Samba as the destination for the DNS partition (1355), even when forced. It
    was shut down and its metadata removed (`--remove-other-dead-server`);
  - at the end: `MIG22` the only DC, 7/7 roles, dataset GPOs, `dbcheck` 0 errors.

### 2019 result (2026-10-19, from scratch)

- **DC**: promoted by a scheduled task (WinRM drops during promotion). The functional level is 2016, because 2019 did
  not create a new level. AD CS fails on this box (`0x80072082`), so LDAPS uses a certificate of the DC itself, which
  it and the destination trust.
- **Path A**:
  - import without failures: only the 4 dataset groups, with the built-in ones skipped;
  - access by name identical on the 4 shares;
  - 7 SMB tests as expected;
  - identical hashes;
  - **identical attributes on the 6 users**, including the disabled one;
  - `kinit lab.ana@DESTINO.LAB` ok.
- **Path B**:
  - join at 2016 and replication `[ALL GOOD]`;
  - 5 roles by transfer and 2 DNS ones by `seize`;
  - SYSVOL with 4 GPOs and `sysvolcheck` ok;
  - Windows shut down and metadata removed;
  - at the end: `MIG19` the only DC, 7/7 roles, `dbcheck` 0 errors (4 fixed).
- **Isolation**: after rebooting `w2019`, `samba-dst`, `samba-dst-fs` and `samba-mig19`, the production LAN is still
  blocked on all of them.

### What the Windows lab fixed or documented

- **Import**: groups in the Windows Builtin container (Hyper-V Administrators, Remote Management Users, RDS…)
  came in as domain local, and those with a fixed RID (Key Admins, Cloneable Domain Controllers) as regular groups.
  The import created them on the destination with a new SID and without the special role. Now they come out as
  `builtin` and are skipped when missing on the destination.
- **`dc-join` setup**: reads the domain's functional level and joins with it (before it was always `2008_R2`, and it
  could not join a 2016 domain). A `--dc-level` below the domain level is refused before touching the domain.
- **Runbook** (`docs/migration-runbook.md`): AD CS and DNS before the demotion, `seize` of the DNS roles and the way
  out of error 1355 (shut down and clean up the metadata from Samba).

## Domain clients (`lab-clients.sh`)

Proves that everyday machines work with Samba4 after the migration: Windows 11, Windows Server 2022 with
RSAT and Ubuntu join `destino.lab` (`samba-dst` with `samba-dst-fs`, built by `lab-samba.sh` or by
`lab-windows.sh`).

```bash
LAB=1 bash vagrant/labs/lab-clients.sh all   # prepare, up, join, validate, roteiro
```

- **prepare** (`clients-dc.sh` on the DC): creates the `Estacoes` OU, the `lab.ingresso` account with delegation only
  to create and manage computers in it (like Windows' "Delegate Control") and the "Lab - Estações" GPO (a value in
  `HKLM\Software\Policies\AdManagerLab`) linked to `OU=Matriz`. Checks that the join account cannot create users
  or join groups.
- **join**: the Windows machines via `Add-Computer` (`client-join.ps1`) and Ubuntu via `realm join`
  (`ubuntu-client.sh`), all three with `lab.ingresso` in the `Estacoes` OU. ws2022m installs RSAT (ADUC, GPMC and
  DNS).
- **validate**: the computers in the panel and in DNS; on Windows, secure channel, `gpupdate`/`gpresult`, Kerberos
  logon of a user and reading the share with their credentials; RSAT querying Samba; on Ubuntu, `id`,
  SSH logon with password (home folder and sssd ticket), share with Kerberos and ADSys.
- **roteiro**: AD Manager in real use (below).

Reports go to `.run/lab-clients/`; at the end of `validate` and `roteiro` the report is checked against the
expected results, and the step fails saying what was missing.

### Result (2026-10-19)

- **Delegation**: `lab.ingresso` cannot create users, cannot join Domain Admins or any other group; it creates
  computers in the `Estacoes` OU.
- **Join**: all three in the `Estacoes` OU, in the panel with OS and DNS name, and with an A record in the domain's
  DNS (192.168.57.61, .62 and .63).
- **Windows 11 and Server 2022**: secure channel `NERR_Success`; Kerberos logon of `lab.ana`; the share
  `\\dstfs.destino.lab\Publico` read with her credentials.
- **RSAT on ws2022m**, as the domain Administrator: ADUC (`dsquery`) finds `lab.ana`, the GroupPolicy module (the
  GPMC one) lists the 3 GPOs and `dnscmd` lists the zones.
- **Ubuntu**: `id lab.ana@destino.lab` resolves; SSH logon creates the home folder and gets the
  `krbtgt/DESTINO.LAB` ticket; the share opens with Kerberos; ADSys 0.16 shows the Default Domain Policy applied to
  the machine.
- **Walkthrough with the panel**:
  - move WIN11 to `OU=Matriz` → after `gpupdate` the GPO value shows up and `gpresult` lists
    "Lab - Estações";
  - a share opened on WIN11 shows up under the file server's Sessions;
  - disable WIN11 → the workstation cannot re-establish the secure channel (`nltest /sc_reset`: 1787
    `ERROR_NO_TRUST_SAM_ACCOUNT`); enable it → it comes back (`NERR_Success`).

### What the clients lab showed

- **Disabling a computer** cuts the secure channel, and with it NTLM logon and the first interactive logon of users
  without a cache. It is not immediate: an established channel stays valid until it is re-established (reboot or
  `nltest /sc_reset`). And Samba's KDC keeps issuing service tickets for the disabled workstation: network Kerberos
  logon on it keeps working.
- **RSAT against Samba**: ADUC, GPMC and the DNS console work (LDAP, SMB and RPC). The PowerShell modules
  `ActiveDirectory` and `DnsServer` do not, because they need ADWS and Windows DNS. `dnscmd /enumzones` without a
  filter comes back empty from Samba; with `/Primary` it lists the zones.
- **Client DNS**: Windows also registers the address of Vagrant's NAT card (10.0.2.15, the same on every VM),
  and sssd, without `dyndns_iface`, registers nothing. The join disables registration on Windows' NAT card and
  points sssd's `dyndns_iface` to the card that reaches the DC (and needs `nsupdate`, from the `bind9-dnsutils`
  package).
- **Outside the walkthrough**: changing the password at first logon needs an interactive logon (console or RDP),
  which WinRM does not provide.
