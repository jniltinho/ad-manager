# Runbook — migrating a Windows DC to Samba

Each step has a **gate**: without the evidence, do not move on. Commands on Windows run over RDP:

```bash
xfreerdp /v:10.0.0.10 /u:administrator /d:EXAMPLE /dynamic-resolution
```

## Before you start

Example: domain `example.local` / `EXAMPLE`, single DC `old-dc` (Windows Server 2008 or newer) at
`10.0.0.10`, new Samba DC at `10.0.0.20`. Replace them with your own names and addresses.

- **New DC's IP**: check that it is also free in DNS (`host <ip> <old-dc>` and the domain apex): orphan records of
  old DCs make workstations look for a DC that does not exist.
- Old domains (Windows 2000 style) may have the `_msdcs` records **inside** the domain zone: after
  step 3 run `samba_dnsupdate --all-names` and check `dig +noedns @<new-dc> SRV _ldap._tcp.dc._msdcs.example.local`.
  Windows 2008 DNS answers `FORMERR` to queries with EDNS (`dig +noedns`).
- New DC with more than one network card: `INTERFACES=` in the installer (otherwise DNS publishes the wrong IP and
  workstation joins fail).
- Rehearse first in an isolated lab (`vagrant/labs`, [labs.md](labs.md)): the rehearsal does **not** cover
  the bidirectional join, FSMO or `dcpromo` — those only during the window, with the gates below.

## Steps

### 1. Baseline (before the window)

```bash
AD_URL=ldap://10.0.0.10 ad-manager migrate preflight
AD_URL=ldap://10.0.0.10 ad-manager migrate inventory -o before.json
AD_URL=ldap://10.0.0.10 ad-manager export -d export-before
```

On `old-dc`: `dcdiag /v > c:\dcdiag-before.txt`, `repadmin /showrepl`, Directory Service, DNS and FRS events
without critical errors. Inventory who does LDAP binds or uses DNS hardcoded to the old DC's IP (security log, firewalls and appliances).

**Gate:** preflight without a `blocker`; dcdiag without failures.

### 2. Back up `old-dc`

```
wbadmin start systemstatebackup -backuptarget:E:
wbadmin get versions
icacls D:\ /save C:\acl-shares.txt /t /c
```

Verify the **DSRM** password. **Gate:** backup listed in `wbadmin get versions`.

### 3. Samba join (Ubuntu 26.04 server on the LAN, static IP)

```bash
sudo python3 scripts/samba-setup.py dc-join --realm example.local --domain EXAMPLE --ip 10.0.0.20 \
     --src-dc 10.0.0.10 --legacy-schannel 'OLD-DC$' --dry-run     # the password is prompted in the terminal
sudo python3 scripts/samba-setup.py dc-join --realm example.local --domain EXAMPLE --ip 10.0.0.20 \
     --src-dc 10.0.0.10 --legacy-schannel 'OLD-DC$'
sudo samba_dnsupdate --all-names
```

`--legacy-schannel` writes the Netlogon exceptions **per machine** (Windows 2008 non-R2 and XP have no AES in
schannel); add the XP machines that still need it, with a deadline to retire them.

**Gate:** `samba-tool drs showrepl` and `repadmin /showrepl` with recent success on all partitions in
both directions; a test user created on each DC shows up on the other;

```bash
AD_URL=ldaps://10.0.0.20 ad-manager migrate inventory -o after.json
ad-manager migrate diff before.json after.json      # only DC accounts may differ
```

### 4. SYSVOL (GPO editing frozen from here until the cutover)

```bash
smbclient //10.0.0.10/SYSVOL -U 'EXAMPLE\administrator' -c 'prompt OFF; recurse ON; lcd /var/lib/samba/sysvol; mget *'
samba-tool ntacl sysvolreset && samba-tool ntacl sysvolcheck
```

Compare `GPT.INI` × `versionNumber` of each GPO; test every GPO and logon script with a regular user.

### 5. Time and DNS

`chrony` with `ntpsigndsocket /var/lib/samba/ntp_signd`. Tests aimed at the new DC:

```bash
dig @10.0.0.20 _ldap._tcp.example.local SRV
dig @10.0.0.20 _kerberos._tcp.example.local SRV
dig @10.0.0.20 _gc._tcp.example.local SRV
dig @10.0.0.20 _ldap._tcp.pdc._msdcs.example.local SRV
dig @10.0.0.20 www.google.com
```

### 6. Pilot

Two workstations (1 Windows 7, 1 XP) with DNS only on Samba and `old-dc` blocked for them in the firewall:
fresh logon (no cache), GPO applied, access to a share on `old-dc`.

### 7. FSMO

```bash
samba-tool fsmo transfer --role=all -U administrator   # includes domaindns and forestdns
samba-tool fsmo show
```

If Windows refuses the DNS partition roles (`domaindns`, `forestdns`: `UNWILLING_TO_PERFORM`, as in the
Windows 2022 lab), seize only those two with `samba-tool fsmo seize --role=domaindns --force` (and `forestdns`).
It is safe because the old owner leaves right after. The other five transfer normally.

Run for 1 day. **Gate:** `fsmo show` identical on both DCs.

### 8. Demoting `old-dc`

Before: if `old-dc` has **Certificate Services (AD CS)**, the CA must be migrated or removed, because Windows
does not demote a DC that hosts a CA. The DNS of **all** network cards on `old-dc` must point to Samba: the demotion
finds the other DC through DNS.

DHCP with Samba as the primary DNS; **normal** `dcpromo` on `old-dc` (do not check "last DC", no
`/forceremoval`), `old-dc`'s DNS pointing to Samba. Announce the share outage during the
reboot. Clean up **only** DC objects (NTDS Settings, SRV/`_msdcs` records of old-dc); keep `OLD-DC$`, A/PTR and SPNs.
On the remaining Samba, run `samba-tool dbcheck --cross-ncs` after the demotion: the references to the DC that left
(`msDS-NC-Replica-Locations`, `fromServer`) are removed with `--fix --yes`. Rehearsed in the Samba lab
(`docs/labs.md`): 2 errors before, 0 after.

**Gate:** shares accessible with a regular user after the reboot.

If `dcpromo` refuses with "could not find another Active Directory Domain Controller to transfer the remaining data
in directory partition DC=DomainDnsZones" (1355) — it happened in the Windows 2022 lab: Windows does not accept Samba
as the destination for the DNS application partition, even though Samba has it. First of all, check replication
(`samba-tool drs showrepl`) and that the zones answer on Samba. Then, one of two ways out:

- **`old-dc` stays as a file server** (while the shares have not been migrated): on `old-dc`,
  `dcpromo /forceremoval` (without replicating), reboot; on Samba, clean up the metadata **of the DC only** (NTDS
  Settings, server object, SRV/`_msdcs`) with the same rule as the step above; then `old-dc` joins the domain again
  as a member, and the shares are checked with a regular user.
- **`old-dc` leaves for good** (shares already migrated): shut it down and run
  `samba-tool domain demote --remove-other-dead-server=old-dc` on Samba. This also deletes the `OLD-DC$` account, its
  DNS records and its SPNs, not just the replication, and so it is **not** suitable for an `old-dc` that is still in
  use. Afterwards, `samba-tool dbcheck --cross-ncs --fix --yes`.

## Alternative — new-domain cutover (no dc-join)

When a `dc-join` is not wanted (a dirty legacy directory, an unsupported schema, a clean break on a **new SID**
keeping the same realm), the new Samba domain is built fresh and the source is copied in with the panel's
**"From another AD"** import. The import brings objects over LDAP, which never exposes the password hashes
(`unicodePwd`/`supplementalCredentials` are write-only), so without the step below every user would get a generated
password with "change at next sign-in" — breaking LDAP/Kerberos apps (Gitea and the like) until each user changes it.

Order: **objects and DNS → passwords → applications tested with the real passwords.**

1. **Objects and DNS** — panel → Import/export → *From another AD* (or `ad-manager import ad --apply`). This also
   carries each object's last-logon and the computers' operating system, so the inactivity view and the OS column
   match the source right away.
2. **Passwords** — turn on the dedicated, default-off write mode on the DC agent (separate from the GPO restore):

   ```toml
   [agent]
   password_migration_user          = "administrator@example.local"   # a target-domain administrator
   password_migration_password_file = "/etc/ad-manager/pwmigrate.pass" # 0600, root only; never in the config
   ```

   Then preview and apply (the source replication account needs *Replicating Directory Changes* **and** *…All*; its
   password comes from `AD_SOURCE_PASSWORD_FILE` or the terminal, never an argument):

   ```bash
   ad-manager migrate passwords --from ldap://10.0.0.10 --user 'OLD\administrator' --agent https://dc:9443          # preview
   AD_SOURCE_PASSWORD_FILE=/root/src.pass ad-manager migrate passwords --from ldap://10.0.0.10 --user 'OLD\administrator' --agent https://dc:9443 --apply
   ```

   The agent reads the hashes over a **sealed, read-only DRSUAPI** (`GetNCChanges` with the per-object secret op; no
   `UpdateRefs`, no replica, the source is left unchanged) and sets them on this domain's matching users by
   `sAMAccountName`: the NT hash always, the Kerberos keys when their salt transfers (otherwise the user is
   `ntlm_only` and the keys rebuild on the next password change). Built-ins (RID 500/501/502), trust and machine
   accounts are never touched; a user missing here is `missing` (import the objects first), and a source that stores
   reversible passwords (`Primary:CLEARTEXT`) is `rejected`. Nothing is written to a file, a log, the API or the
   audit (`password.migrate` records counts only); re-running is idempotent (`same` when the full state matches).
3. **Applications** — sign in to the LDAP/Kerberos apps (Gitea, etc.) with a real production password to confirm
   the material carried over before handing the domain to users.

## Rollback

| When | How to go back |
|---|---|
| Before step 7 | `samba-tool domain demote` on Samba; clean up DNS |
| Between 7 and 8 | on `old-dc`: `ntdsutil` → `roles` → `connections` → `connect to server old-dc` → `transfer …` for the 5 roles; then demote Samba. *Seize* only as recovery, with Samba shut down and removed right after |
| After 8 | restore the step 2 backup on `old-dc` (DSRM, authoritative if needed) with Samba isolated; whatever changed after the backup is lost |

## After the cutover

- Harden the password policy (panel → Users → Password policy): minimum 8, complexity, lockout after 10.
- Second Samba DC and a daily `samba-tool domain backup offline` with a tested restore.
- Retire the Windows XP machines (if any) and remove the Netlogon exceptions.
- Shares on `old-dc`: `migrate shares --csv` on `old-dc` and import into a member file server, with the
  SMB access matrix verified (write, deny, read-only, hidden). If Samba is the DC and the file server on the same
  machine: `docs/dc-file-server.md` (import "From another AD" with the DC's agent as the destination, data
  through Sync).
