# Install Samba 4 ready for ad-manager (Ubuntu 26.04)

> **Prefer the browser?** The setup wizard runs this same script for you, with automatic diagnostics, a review of the
> plan and a live log, without typing commands: [installation.md](installation.md).

The `scripts/samba-setup.py` script installs Samba 4 (distro packages, 4.23.6) and leaves **ad-manager**
running: the panel on the DC, talking LDAPS with Samba's own CA, and the agent on each file server.
It uses only the Python standard library (`python3` ships with Ubuntu Server); the `.deb` package also installs it in
`/opt/ad-manager/scripts/`.

```bash
sudo python3 scripts/samba-setup.py <role> [options] [--dry-run]
sudo python3 scripts/samba-setup.py --help
```

| Role | Purpose | Options |
|---|---|---|
| `new-dc` | new domain (new company, branch office, lab) | `--realm --domain --ip` · `--panel --deb` · `--forwarder` |
| `dc-join` | additional DC in an existing domain (migration of a Windows DC) | `--realm --domain --ip --src-dc` · `--panel --deb` |
| `member` | file server that is a domain member | `--realm --domain --dns-dc` · `--agent --deb` · `--share-root` |
| `panel` | only the ad-manager panel, on a DC that already exists | `--realm` · `--deb` · `--port` |
| `agent` | only the ad-manager agent, on a member that already exists | `--realm --dns-dc` · `--deb` · `--share-root` · `--new-token` |
| `packages` | only installs the Samba packages and stops the daemons | — |
| `restore-dc` | the DC from a backup of it, on a new machine ([backup.md](backup.md)) | `--realm --ip --backup` · `--panel [--files]` |
| `connect` | only the panel, managing an Active Directory that runs elsewhere | `--realm --url` · `--ca-file` or `--insecure` |
| `verify` | checks the installed role, the ports and the services | — |
| `diagnose` | what is installed and running, as JSON (read by the panel's setup wizard) | — |

The Portuguese names of earlier versions still work: roles `dc-novo`, `dc-restaurar`, `membro`, `painel`, `agente`,
`conectar`, `pacotes`, `verificar`, `diagnostico` and flags `--painel`, `--agente`, `--arquivos`, `--novo-token`.

- **Domain administrator password**: prompted in the terminal (no echo), or read from `AD_PASSWORD_FILE` (a `0600`
  file) or from the first line of stdin — never as an argument. Exception: `samba-tool domain provision` in
  `new-dc` only accepts the password as an argument, which is visible in `ps` during provisioning (new server,
  a few seconds).
- `--dry-run` shows each command without changing anything. The roles are idempotent where possible: `panel` and
  `agent` can be run again (the previous config goes to `config.toml.bak`).
- `--deb` points to the `ad-manager_X.Y.Z_amd64.deb` package (Releases page); without it, ad-manager must already
  be installed.

## Before you start

- Ubuntu Server 26.04 with a **static IP** and a short hostname set (`hostnamectl set-hostname dc01`).
- Synchronized clock (Kerberos tolerates 5 minutes).
- From the server to the existing DC: TCP/UDP 53, 88, 135, 389, 445, 464, 636, 3268 and RPC 49152-65535.
- **Check that the IP is also free in DNS** (`host <ip> <current-dc>` and the domain apex): orphan records of old
  DCs make clients look for a DC that does not exist. Use another IP or clean up the record first.

| Port | Who connects | Purpose |
|---|---|---|
| 8443/TCP on the DC | administrators' browser, desktop app | ad-manager panel (the desktop's only connection) |
| 636/TCP on the DC | panel, CLI, MCP | LDAPS |
| 88 and 464/TCP on the DC | panel | Kerberos: password change link (kpasswd) |
| 9443/TCP on the file server | the panel only (loopback when the agent runs beside the panel) | agent API (token) |
| 445/TCP on the file server | workstations | SMB shares |

## 1. New domain with the panel

```bash
sudo python3 scripts/samba-setup.py new-dc --realm example.local --domain EXAMPLE --ip 10.0.0.10 \
     --panel --deb ./ad-manager_X.Y.Z_amd64.deb
sudo python3 scripts/samba-setup.py verify
```

What happens:

1. Installs the Samba packages and provisions the domain with internal DNS and RFC 2307.
2. Disables `systemd-resolved` (the DC serves port 53), points `/etc/resolv.conf` to `127.0.0.1`, copies
   `krb5.conf` and enables `chrony` with NTP signing for the members. Waits for the `_ldap._tcp` SRV to respond.
3. **Panel**: installs the `.deb` and `redis-server`, copies the CA of the certificate Samba generates
   (`/var/lib/samba/private/tls/ca.pem`) to `/opt/ad-manager/tls/samba-ca.pem` and writes
   `/opt/ad-manager/config.toml`:

   ```toml
   [ad]
   url = "ldaps://dc01.example.local"      # <hostname>.<realm>: the CN of Samba's certificate
   domain = "example.local"
   ca_file = "/opt/ad-manager/tls/samba-ca.pem"
   [server]
   listen = "0.0.0.0:8443"                 # package's self-signed certificate in /opt/ad-manager/tls
   [redis]
   addr = "127.0.0.1:6379"                 # /asynqmon
   [samba]
   log_dir = "/var/log/samba"              # Logs → Samba screen
   ```

4. Enables `ad-manager.service` and checks ports 8443 and 636. Open `https://10.0.0.10:8443` and log in with a
   *Domain Admins* user (`Administrator` will do at first).

To replace the panel's certificate with a company one, see [deployment.md](deployment.md).

## 2. Additional DC (migration)

See the step-by-step procedure and the gates in [migration-runbook.md](migration-runbook.md).

```bash
sudo python3 scripts/samba-setup.py dc-join --realm example.local --domain EXAMPLE --ip 10.0.0.11 \
     --src-dc 10.0.0.10 --dry-run          # checks ports, SRV and clock; changes nothing
sudo python3 scripts/samba-setup.py dc-join --realm example.local --domain EXAMPLE --ip 10.0.0.11 \
     --src-dc 10.0.0.10 [--panel --deb ./ad-manager_X.Y.Z_amd64.deb]
```

Joins as a DC at the 2008 R2 functional level, using the source DC as DNS during the join; afterwards it uses its
own DNS. With `--panel`, it does the same as steps 1.3–1.4 on this DC.

## 3. File server with the agent

```bash
sudo python3 scripts/samba-setup.py member --realm example.local --domain EXAMPLE --dns-dc 10.0.0.10 \
     --agent --deb ./ad-manager_X.Y.Z_amd64.deb
sudo python3 scripts/samba-setup.py verify      # wbinfo -t, net conf list, services
```

1. **Samba member**: winbind with `idmap rid`, `vfs objects = acl_xattr` (NTFS permissions editable from
   Explorer), `registry shares = yes` (the agent creates the shares with `net conf`), `net ads join` and
   `SeDiskOperatorPrivilege` for *Domain Admins*. The share root is `--share-root`
   (default `/srv/shares`).
2. **Agent**: installs the `.deb`, `redis-server` (local queue), `acl` and `python3-samba` (Windows permissions on
   folders: Full control, Modify, Read & execute — check with `ad-manager agent check`); writes `config.toml` with
   `[agent]` (`0.0.0.0:9443`, `share_roots`), generates the **token** with `ad-manager agent token` and enables
   `ad-manager-agent.service`.
3. The script shows the token **only once** (the server keeps only the hash). In the panel:
   **Shares → + New**, URL `https://fs01.example.local:9443`, paste the token and check "Accept self-signed
   certificate" while the certificate is the package's self-signed one. Lost the token?
   `samba-setup.py agent … --new-token`.

More about the agent in [agent.md](agent.md).

## Only the panel or only the agent

On a DC or member installed earlier (or by the old script), run only the ad-manager part:

```bash
sudo python3 scripts/samba-setup.py panel --realm example.local --deb ./ad-manager_X.Y.Z_amd64.deb
sudo python3 scripts/samba-setup.py agent --realm example.local --dns-dc 10.0.0.10 --deb ./ad-manager_X.Y.Z_amd64.deb
```

## Windows clients

Samba AD serves Windows workstations and servers like a Windows DC: domain join, Kerberos,
Netlogon, GPO (SYSVOL), NETLOGON, dynamic DNS and time synchronization.

| Windows | Works without changes | Note |
|---|---|---|
| 10, 11, Server 2016–2025 | yes | tested: Server 2022 in the lab |
| 7, 8.1, Server 2008 R2/2012 | yes | Netlogon with AES |
| Server 2008 (non-R2), XP, 2003 | **no** | no AES in Netlogon: per-machine exception; XP needs SMB1 |

Exceptions (only for the machines that need them, with a deadline to retire them):

```bash
sudo python3 scripts/samba-setup.py dc-join … --legacy-schannel 'OLD-DC$ PCXP01$' --xp-smb1
```

On the XP machines, force NTLMv2 via GPO (`LmCompatibilityLevel = 5`); Samba 4.23 refuses NTLMv1.

**DC with more than one network card**: use `--interfaces "lo eth0"`. Without it Samba publishes all the machine's
IPs in DNS and clients may try an unreachable IP — this is exactly what broke the first Windows join in the lab
(the IP of Vagrant's NAT card was the Windows VM's own IP).

### Join a workstation

1. Workstation DNS pointing **only** to the Samba DCs.
2. *Settings → System → About → Domain or workgroup* → `example.local`, or in PowerShell:
   `Add-Computer -DomainName example.local -Credential EXAMPLE\user -Restart`.
3. Verify: `Test-ComputerSecureChannel`, `nltest /sc_verify:example.local`, `gpresult /r`.

To manage GPOs use RSAT (*Group Policy Management*) on a member workstation; users and
groups through the `ad-manager` panel or RSAT.

### `samba-setup.py` test (2026-10-17)

In two Ubuntu 26.04 containers with systemd: `new-dc --panel` created `teste9.lan` and left the panel running on
`:8443`, with LDAPS validated by Samba's CA (certificate `CN=dc9.teste9.lan`, without `insecure`); login, user
creation and the **password change link** (kpasswd on the DC itself) worked. `member --agent` joined `fs9` to the
domain and started the agent (`/api/health` with the `fs-fs9` queue); registered in the panel with the generated
token, the agent created `\\fs9\Financeiro` for `TESTE9\Financeiro` and the user `TESTE9\ana` wrote to it over SMB.
`verify` passed on both.

### Test of the previous bash installer (2026-10-17)

In two Ubuntu 26.04 containers with systemd: `new-dc` created `teste.empresa.lan` (DNS responding, FSMO on the
DC); `member` joined the second one to the domain (`wbinfo -t` ok, AD groups resolved by winbind,
`SeDiskOperatorPrivilege` granted); `ad-manager agent` created `\\fs1\Financeiro` for `TESTEADC\Financeiro` and
the domain user `TESTEADC\ana` wrote to it over SMB.
