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
  `nltest /sc_reset`). Service tickets for the disabled workstation depend on the KDC: older Samba kept issuing them
  (network Kerberos logon kept working); on 2026-10-28 (Samba 4.23.6) the first try was refused, as on Windows. The
  walkthrough reports either and asserts the secure channel.
- **RSAT against Samba**: ADUC, GPMC and the DNS console work (LDAP, SMB and RPC). The PowerShell modules
  `ActiveDirectory` and `DnsServer` do not, because they need ADWS and Windows DNS. `dnscmd /enumzones` without a
  filter comes back empty from Samba; with `/Primary` it lists the zones.
- **Client DNS**: Windows also registers the address of Vagrant's NAT card (10.0.2.15, the same on every VM),
  and sssd, without `dyndns_iface`, registers nothing. The join disables registration on Windows' NAT card and
  points sssd's `dyndns_iface` to the card that reaches the DC (and needs `nsupdate`, from the `bind9-dnsutils`
  package). sssd 2.10 (Ubuntu 26.04) may still not send the update after the join: the join then registers the
  address itself with a secure update (`nsupdate -g`) and the machine's keytab, as a Windows member does.
- **Outside the walkthrough**: changing the password at first logon needs an interactive logon (console or RDP),
  which WinRM does not provide.

## Full run of 2026-10-28 (`.deb` 0.27.1 with the Entra-sync work)

Every lab from scratch except the reused `destino.lab` noted below; the scripts' new checks fail a step instead of
only printing a difference.

| Lab | Result |
|---|---|
| Windows 2022 (`lab-windows.sh`, hardened) | import with no failure, 4 syncs `ok`, access by name identical on 4 shares, 7 SMB tests as expected, hashes and attributes identical, `kinit` ok; path B: `MIG22` 7/7 roles, `dbcheck` 0 errors; isolation ok |
| Windows 2019 (`lab-windows.sh`) | the same, with `MIG19`; the KDC had stayed stopped after the DC step (fixed in `windows-dc.ps1`) |
| Clients (`lab-clients.sh`) | win11, ws2022m and ubuntu-desk joined by the delegated account; secure channel, logon, tickets, share by name; RSAT (ADUC, GPMC, DNS) against Samba; GPO by moving to an OU in the panel; SMB session in the panel; disable/enable cuts and restores the secure channel |
| Samba → Samba (`lab-samba.sh`) | 5 machines through the wizard, access by name identical, 7 SMB tests as expected, path B `MIGDC` 7/7 roles, `dbcheck` 0 errors; run onto the `destino.lab` of the 2022 lab, so one reverse-zone `dns.conflict` was expected |

### What this run fixed

- **Product — sync refused after a reboot** (`openspec/changes/sync-confinement-covered-mounts`): with `/vagrant`
  mounted twice (at boot and by Vagrant), systemd left the covered mount `rw` inside the worker's unit, and the
  confinement check refused every sync ("/vagrant is writable to the sync worker"). Covered mounts are unreachable
  and no longer count; the lab re-ran the syncs on the same machine: `ok`, identical hashes.
- **Labs — failures that only printed**: `path-a` now fails when a sync is not `ok` (`check_syncs`) and `validate`
  when hashes, SMB results, access, attributes or `kinit` differ (`check_report`). The failed sync above had gone
  through with exit 0.
- **Labs — first boot that hangs**: a fresh Ubuntu 26.04 guest sometimes stalls (kernel RCU stalls under
  VirtualBox 7.2) before SSH; `up_retry` power-cycles it once and runs its provisioners (isolation included) again.
- **Labs — KDC stopped on the 2019 DC** after the NTDS restart of the DC step: the step now starts it with retries
  and reports `Kerberos 88`.
- **Labs — Ubuntu client DNS**: the registration fallback above.

KVM (QEMU, `dockur/windows`) and VirtualBox cannot run VMs at the same time on this host: halt the Vagrant labs
before the containers.

### Windows in a container (`dockur/windows`)

- **Windows 10 joining the test Samba** (`make samba-up`, `empresa.test`): a `dockurr/windows` container on the
  compose network with `--dns` at the DC and `DOMAIN=empresa.test` joined during its unattended install. The CLI
  listed it (`ad-manager computer list`: OS and last logon filled), moved it to a new OU, disabled and enabled it
  (each in `audit.log`); the client registered itself in the DC's DNS (secure update); on the client the domain
  logon (`empresatest\administrator`) holds Kerberos tickets from `dc1.empresa.test` (`cifs/`, `LDAP/`, AES-256).
  The secure channel checks out (`Test-ComputerSecureChannel`: `True`, "in good condition"; the OEM script's
  `nltest /sc_verify:empresa.test` → `NERR_Success`).
- **Windows Server 2008 R2 lab DC** (`VERSION=2008`, forest `lab2008.test` by `dcpromo /unattend` from `/oem`,
  plus fixtures: `plain`, a user without rights, and `nopw`, created without a password). Three things the image
  needs for 2008 R2:
  - **The answer file on a CD.** Its setup reads `Autounattend.xml` from no USB drive (dockur's `setup.img`), so it
    stopped at the language screen. Copy `setup.img`'s files into an ISO (`genisoimage -J -joliet-long -r`) and attach
    it with `ARGUMENTS="-drive file=/storage/au.iso,media=cdrom,readonly=on"`: the install runs unattended.
  - **`SAMBA=N`.** dockur keeps 139/445 for its own shared folder, so SMB never reached the DC (`preflight`
    blocker, no `migrate shares`).
  - **The clock.** The guest came up 7 h ahead (Pacific time over a UTC clock): Kerberos failed with
    `KRB_AP_ERR_SKEW`. `tzutil /s utc` and `time` in the guest fixed it.
- **2008 R2 results** (destination: the all-in-one image built from this tree, `AD_DOMAIN=lab2008.test`, on the same
  Docker network):
  - DRSUAPI hash read (`internal/gosamba/drs`, from Ubuntu 26.04 and Debian 13): Administrator's hash, `nopw` → no
    hash, unknown GUID refused, `plain` refused (`ERROR_DS_DRA_BAD_DN`), wrong password refused. LDAP reads
    (`TestSearchForSync`) over `ldap://` + NTLM.
  - CLI over `ldap://` + NTLM: `migrate preflight` with no blocker (functional level 2008 R2, schema 47, FSMO),
    `user list`, `migrate inventory`, `export`. A write (`user disable`) is refused: "changes require a TLS
    connection" (the 2008 R2 LDAPS speaks only TLS 1.0/1.1).
  - New-domain cutover: `import ad --only objects --apply` created `labadmin`, `nopw` and `plain`;
    `migrate passwords --apply` set `plain` and `labadmin` (`ntlm_only`: no Kerberos keys that Samba takes) and
    `nopw` → `no_password`; the second run reported `same`, exit 0. `plain`'s production password works on the
    target over SMB and LDAP; `kinit` waits for the next password change (documented for `ntlm_only`).
  - `migrate shares` from `\\LABADMI-EFDUN97\Financeiro`: the target agent created `/srv/shares/Financeiro` with
    `Domain Users` write (NTFS `0x1301bf`).
- **2008 R2 → Samba by `dc-join`** (the lab's path B): an Ubuntu 26.04 container with systemd as PID 1 (`mig2008`,
  standing in for the `samba-dst` VM: KVM and VirtualBox do not run together) on the DC's network, with a route to the
  guest's own address (the DC registers `172.30.96.x`, behind dockur's NAT), the `.deb` of this tree installed. Its
  setup wizard (`setup-wizard.sh`, role `dc-join`) joined at the domain's level (2008_R2); `samba-dc.sh` then
  replicated the five partitions (`[ALL GOOD]`), transferred five FSMO roles and seized the two DNS ones (7/7), copied
  SYSVOL (2 GPOs, `sysvolcheck` ok); the Windows DC was turned off and its metadata removed (`remove-dead`):
  `dbcheck` 0 errors, `MIG2008` the only DC, SRV records on it. After the cutover: `plain` signs in with its 2008
  password over Kerberos (AES) and NTLM; the CLI writes over LDAPS (OU, user, group, member, move, disable/enable,
  password reset, delete — all audited); the panel signs in and lists the users.
  - **RC4-only accounts.** `Administrator` and `labadmin` had their passwords set while still local accounts (before
    `dcpromo`): they carry the NT hash only, no AES keys, on the 2008 R2 DC as on Samba after the join. Samba's KDC
    serves them RC4, but Ubuntu 26.04's MIT client refuses RC4 by default (`kinit`: "KDC has no support for
    encryption type"). `migrate preflight` now warns about them (`RC4-only accounts`: `pwdLastSet` older than the
    Read-only Domain Controllers group); on the migrated domain it listed `Administrator` (and `labadmin`, whose
    reset predates the group only because the lab's clock was 7 h ahead when the domain was created). After a
    password reset both get AES-256 tickets from `kinit`.
  - **A Windows client joins the migrated domain**: a new `dockurr/windows` Windows 10 container (`DOMAIN=lab2008.test`,
    DNS at `mig2008`) joined during its install; `nltest /sc_verify:lab2008.test` → `NERR_Success` with
    `\\mig2008.lab2008.test`, and `nltest /dsgetdc` finds it as PDC, GC, KDC, writable. The ad-manager on `mig2008`
    moved the computer to `OU=Estacoes`, disabled and enabled it (audited); its DNS record registered.
  - Container notes: systemd needs `mount -o remount,rw /sys/fs/cgroup` before `/sbin/init` (private cgroup
    namespace, `SYS_ADMIN`, never `--privileged`: chrony runs with `-x` and must not set the host clock), and the
    Ubuntu `redis-server` unit needs `PrivateUsers=false` (no user namespaces in the container).
- **From scratch with the final tree** (same day, after every fix above; the `.deb` of this tree):
  - A new 2008 R2 DC with the right clock. dockur's answer file names no time zone, so WinPE reads the UTC RTC as
    Pacific time and the domain is born 7 h ahead; `<TimeZone>` in the answer file is too late for that. What
    works: the OEM `install.bat` syncs over NTP (`w32tm`, unlimited phase correction) before `dcpromo`.
  - `migrate preflight` against it: no blocker, `RC4-only accounts` → `Administrator` (its password was set
    before `dcpromo`). The runbook's fix — reset on the source (`net rpc password`) — and the preflight again:
    `ok`.
  - `dc-join` by the setup wizard, replication `[ALL GOOD]`, 7/7 roles, SYSVOL (2 GPOs, `sysvolcheck` ok), the
    Windows DC off and removed, `dbcheck` 0 errors, `MIG2008` the only DC. `kinit` of `Administrator`,
    `labadmin` and `plain`: AES-256 for all three; `preflight` on Samba: `RC4-only accounts` ok.
  - Clients joined to the migrated domain: **Windows 10, Windows 11 and Server 2022** (`dockurr/windows`,
    `DOMAIN=lab2008.test`, DNS at `mig2008`; `nltest /sc_verify` → `NERR_Success` with `mig2008`, `dsgetdc` → PDC,
    GC, KDC, writable) and **Ubuntu 26.04** (`ubuntu-client.sh join` in a systemd container: realmd/sssd into
    `OU=Estacoes`, DNS registered; `lab.ana` logs on over SSH with a password, gets a Kerberos ticket and a home
    folder, and reads the DC's share with Kerberos). The ad-manager on `mig2008` listed the five computers, moved the
    Windows ones to `OU=Estacoes`, disabled and enabled Windows 11 (audited).
  - Container notes for the Ubuntu client: `--cap-add DAC_READ_SEARCH` (sssd 2.10 reads the keytab with that
    capability), and Docker's own `/etc/resolv.conf` and `/etc/hostname` mounts (no `rm`, no `hostnamectl`).
- **The RC4-only check against the real keys** (each enabled user's `supplementalCredentials` in `sam.ldb`): on the
  Vagrant domains created by Windows 2022 and 2019 (2016 level, migrated to `samba-mig22`/`samba-mig19`) the first
  version flagged `Administrator` and `vagrant`, which do have AES keys (2016+ stores them for local accounts before
  `dcpromo`); on 2008 R2 they do not. Domains born on 2016+ (Key Admins created with the RODC group) are now `ok`;
  rechecked on the three migrated domains, in line with the keys.
- **The Vagrant labs again with the final tree** (`.deb` of this tree on every Samba VM):
  - `lab-clients.sh join/validate/roteiro`: Windows 11, Server 2022 (RSAT) and Ubuntu in `destino.lab` — secure
    channel, GPO, logon, Kerberos, share, RSAT on Samba, move to an OU with a GPO, SMB session, disable/enable (1787 →
    `NERR_Success`). `destino.lab` had been created again by a later lab, so the clients were members of a domain that
    no longer knew them: `ubuntu-client.sh join` now leaves it (old homes set aside, sssd cache emptied with
    `sssctl cache-remove`) and joins again; `client-join.ps1` stops with the recovery steps checked on win11 and
    ws2022m (leaving from WinRM breaks WinRM's local logons with `STATUS_NO_LOGON_SERVERS` until the workgroup is set
    again and the machine rebooted).
  - `lab-samba.sh`: the four syncs `ok`, access by name the same on the four shares, the SMB checks as expected, file
    hashes equal. Path B now points `samba-fs` (fixed DNS) at `samba-mig`: left on `samba-src`, once it was off every
    sync failed with "No logon servers" (the runbook says so now).
- **What the 2008 R2 lab fixed** (OpenSpec `windows-2008-migration-fixes`): a share without a security descriptor
  (Everyone: Full Control on Windows) was migrated with no access for `Domain Users`; a source user without a
  password was an `error` that made `migrate passwords` exit 2; `import ad --apply --dir X` lost the generated
  passwords when `X` did not exist.
- The **real** 2008 R2 DC was also read (read-only): the DRSUAPI hash equals the MD4 of the known password.

