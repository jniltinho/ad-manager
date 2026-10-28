## Why

Syncing an on-premises Samba 4 (or Windows) domain to Microsoft Entra ID today needs
[AzureADConnect_Samba4](https://github.com/tranquilit/AzureADConnect_Samba4) (Tranquil IT, ~1,100 lines of Python)
plus its submodules [AADInternals_python](https://github.com/tranquilit/AADInternals_python) (~1,650 lines) and
[python-wcfbin](https://github.com/tranquilit/python-wcfbin) (~4,600 lines), on top of nine Debian/Ubuntu Python
packages: `python3-samba python3-ldb python3-peewee python3-passlib python3-xmltodict python3-requests python3-msal
python3-cryptography python3-jwt`. On the Ubuntu 26.04 hosts ad-manager targets, that is a second runtime, a
second set of credentials and a tool the panel cannot see.

ad-manager already has, in Go, most of what that stack does: LDAP(S) to any DC (`internal/ad`, go-ldap), SQLite
(`modernc.org/sqlite`), Kerberos/NTLM (gokrb5, go-msrpc) and — in the go-msrpc module already in `go.mod` —
DRSUAPI `GetNCChanges` with the secret decryption (`drsuapi.DecryptHash`) that the Python tool takes from
`samba.net.replicate_decrypt`. Porting the tool to Go gives one static binary with no Python dependency, the
project's protections (server-side rules, audit, secrets only through the environment) and a path to the panel.

"Porting python-samba to Go" is **not** a general port: `python3-samba`/`python3-ldb` are bindings to Samba's C
libraries (ldb, tdb, smbd, NDR for hundreds of interfaces). This change ports only the parts the Entra sync uses,
mapped to Go packages ad-manager already has (design D1).

## What Changes

- **Two new package trees under `internal/`**:
  - **`internal/gosamba/`** — the Go replacement for the `python3-samba`/`python3-ldb` calls: `samdb` (SamDB over
    LDAPS, SID/GUID/FILETIME, account flags) and `drs` (sealed DRSUAPI single-object read with CRC-checked
    decryption). Nothing Entra-specific, reusable by the rest of the server.
  - **`internal/entrasync/`** — the AzureADConnect_Samba4 port on top of `internal/gosamba`: `nbfx` (binary XML,
    replaces python-wcfbin), `aadsync` (AD Sync SOAP + AAD hash), `identity` (the `entra_aba_dir` certificate
    identity), `graph` and `sync` (engine, state and safety rules).

  ad-manager wires them as the `ad-manager entra` command group:
  - `entra init` — interactive (device code) setup: the certificate application in the tenant (same on-disk format
    as the Python `entra_aba_dir`, so an existing install is **adopted, never recreated**), DirSync on, the
    password hash sync feature and Seamless SSO.
  - `entra sync [--force] [--dry-run|--no-dry-run] [--service] [--allow-deletes N]` — users, groups (nested
    members), optional devices, NT hash sync, guarded deletions, delta through a local state database.
  - `entra status` — identity, certificate expiry, last run and counts; displays no secret.
  - `entra import-state <last_send_azuread.db>` — adopts the Python tool's state database.
- **Read side without Python**: users, groups and computers over LDAPS; NT hashes over a sealed, read-only DRSUAPI
  `GetNCChanges` (`EXOP_REPL_OBJ`, the single-object read `pwmigrate` already uses) on go-msrpc, with a carried
  patch that verifies the decrypted CRC. Same path on a Samba DC, a member server and a Windows DC. No `sam.ldb`.
- **Microsoft side in Go**: MSAL Go for tokens (certificate client credentials; device code for admin steps,
  memory only), Microsoft Graph reads, the Azure AD Sync SOAP service
  (`adminwebservice.microsoftonline.com/provisioningservice.svc`) in `application/soap+msbin1`, the Seamless SSO
  registration calls.
- **Safer than the original** where the original can lose data or leak:
  - deletions only of objects this tool synced for this tenant and domain, re-checked in Graph, behind an
    accidental-delete guard (500 by default, and never when the AD read returned nothing); the Python's tenant-wide
    scope is opt-in;
  - dry run writes nothing anywhere, certificate rotation included;
  - every value in the SOAP envelopes is XML-escaped and sent verbatim (the Python concatenates strings and its
    binary XML writer can change values such as `True` or `"Sales  "`);
  - the password-hash salt comes from `crypto/rand` (the Python uses `random`); hash digests in the state DB are
    keyed with a secret kept outside it;
  - the AD sync account's password only through the environment or a `0600` file.
- **Not ported**: Python callbacks (`folder_callback_python`) — refused with a clear error instead of syncing a
  different set of objects; the deprecated `objectSID_str` anchor; `verify = False`.
- **Unchanged**: `internal/sambabridge/bridge.py` and the write half of `internal/sambadc/pwmigrate.py` keep using
  `python3-samba` (they need smbd's NT ACL store and ldb's system session, which have no network equivalent).
- **Tested on** Ubuntu 26.04 and Debian 13 containers (unit, Docker Samba integration, parity with the Python tool
  in an offline container) and a Windows Server 2022 DC from `vagrant/labs`.

## Capabilities

### New Capabilities

- `entra-sync`: one-way sync of users, groups, devices and password hashes from Active Directory (Samba 4 or
  Windows) to Microsoft Entra ID, in Go, with its identity management, state database and safety rules.

### Modified Capabilities

None.

## Impact

- New packages under `internal/gosamba/` and `internal/entrasync/` (above), `scripts/test-containers.sh`;
  `third_party/go-msrpc` gains `msrpc/drsr/drsuapi/v4`
  with a CRC-check patch; later `cmd/entra.go` and `config` `[entra]`.
- New Go dependencies in the root `go.mod`, added with the packages that use them: `github.com/AzureAD/microsoft-authentication-library-for-go`
  (official MSAL) and `software.sslmate.com/src/go-pkcs12` (reads and writes the PFX the Python tool created);
  `golang-jwt/jwt/v5` (proof JWT). Everything else is the standard library or already used by ad-manager.
- Server i18n catalog (new CLI messages), `docs/entra-sync.md`, CHANGELOG, `openspec/specs/entra-sync`.
- No API endpoint and no UI in this change (a panel page is a later change), so no Swagger or screenshot update.
- Risk carried over: the AD Sync SOAP service is undocumented, and since July 2026 Microsoft moves tenants from
  Entra Connect Sync to Cloud Sync one by one, with no retirement date (design D11). The original has the same
  exposure; this port stops cleanly when the service refuses (no deletion, state unchanged), never turns sync
  back on by itself, and reports the hard-match block on role holders. `internal/gosamba` does not depend on it.
