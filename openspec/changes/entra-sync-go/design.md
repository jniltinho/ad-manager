## Context

Sources read for this design (commits of 2026-10-27):

- `tranquilit/AzureADConnect_Samba4` `e7b6531`: `run_sync.py` (driver, state DB with peewee, delta, deletions,
  nested groups, hash sync, service loop), `libsync.py` (`SambaInfo`: LDB reads, source anchor,
  `ms-DS-ConsistencyGuid`, duplicate-mail warning, SSO account, service connection point; `AdConnect`: wrapper over
  AADInternals), `AdAccountReader.py` (DRSUAPI `GetNCChanges` level 8, `EXOP_REPL_SECRET`, `replicate_decrypt`),
  `azure.conf.exemple`, callback template.
- `tranquilit/AADInternals_python` `6ec70cb`: `AADInternals.py` (MSAL tokens, Graph paging, SOAP envelopes for
  `ProvisionAzureADSyncObjects` (Set and Delete), `ProvisionCredentials`,
  `ReadBackAzureADSyncObjects2`, `GetCompanyConfiguration`/`SetCompanyDirsyncFeatures`, AAD password hash,
  Seamless SSO registration), `_aad_certificate_manager.py` (app bootstrap through device code, PFX + passphrase
  file, state.json, rotation with a proof-of-possession JWT, cleanup).
- `tranquilit/python-wcfbin`: the [MC-NBFX] record codec and the [MC-NBFS] static dictionary.
- Samba `master`: `python/samba/drs_utils.py`, `source4/libnet/py_net.c` (`replicate_decrypt`), the `drsuapi`
  IDL.
- ad-manager: `internal/ad/conn.go` (LDAPS/NTLM bind), `internal/sambadc/pwmigrate.{go,py}` (the same DRSUAPI read
  through Python today), `internal/winrpc` and `third_party/go-msrpc` (pruned v1.6.4 with security patches),
  `internal/store` (SQLite), `go.mod` (Go 1.27, go-ldap, gokrb5, modernc sqlite).
- Target runtime: Ubuntu 26.04 (the base of `docker/samba`, `docker/allinone`, `docker/fileserver`), Samba from
  the distribution (`samba-ad-dc`). The ad-manager binary is static; `entra` needs no package from the distribution.

## Goals / Non-Goals

**Goals**

- Behavioral parity with AzureADConnect_Samba4 for: users, groups (nested, mail, proxyAddresses, description),
  devices (optional), source anchor rules, `ms-DS-ConsistencyGuid`, account expiry → disabled, manager, duplicate
  mail warning, NT hash sync, deletions (from Graph or from local state), delta, force, dry run, service loop,
  Seamless SSO, service connection point, base DNs and custom filters, `alternate_login_id_attr`, proxy and custom
  CA.
- Zero Python at run time; the same binary on a Samba DC, a member server or any host that reaches a DC.
- Drop-in adoption of an existing Python install: same `entra_aba_dir` files, importable state DB.
- The project's rules: secrets only through the environment or `0600` files, protections in Go next to the code that writes, audit
  per change, English sources with the pt-BR catalog.

**Non-Goals**

- A general Go port of `python3-samba`/`python3-ldb` (D1 explains why it is neither feasible nor needed).
- Replacing Python in `sambabridge` (smbd NT ACL store) or the write half of `pwmigrate` (ldb system session to set
  hashes) — no network protocol does what they do on Samba.
- Password writeback, user writeback, Cloud Sync, Pass-through authentication, federation.
- Python callbacks. A declarative replacement can be a later change if someone needs it.
- Panel UI, API endpoints and MCP tools for Entra sync (later change, after the CLI is proven on a tenant).

## Decisions

### D1. What "python-samba to Go" means here: a map of the used surface

| Python (package) | Used for | Go replacement |
|---|---|---|
| `samba.samdb.SamDB`, `ldb` (`python3-samba`, `python3-ldb`) | searches, `modify_ldif`, `newcomputer`, `setpassword`, `get_config_basedn`, `schema_format_value` (SID) | reads: `internal/ad` (`Dial`, `Search` paged, `NamingContexts` from the rootDSE — already LDAPS with CA, StartTLS, NTLM without TLS); value formatting: `internal/gosamba/samdb` (SID/GUID/FILETIME); the three writes: `internal/ad` methods added with task 4.3 |
| `samba.param.LoadParm`, `Credentials.guess` | realm from `smb.conf`, machine/system credentials | `[entra]` config (realm from the rootDSE `defaultNamingContext`); explicit sync account |
| `samba.auth.system_session` on local `sam.ldb` | reading `unicodePwd` on the DC itself | not ported: DRSUAPI to the DC over the network (D3), same on DC and member |
| `samba.drs_utils.drsuapi_connect`, `drsuapi.DsGetNCChanges`, `samba.net.replicate_decrypt` | NT hash per user | `internal/gosamba/drs` on go-msrpc `msrpc/drsr/drsuapi/v4` `GetNCChanges` (`EXOP_REPL_OBJ`, D3) + a patched `DecryptHash` that checks length and CRC32 before the RID DES layer |
| `samba.dsdb.UF_ACCOUNTDISABLE` | constant | constants in `internal/gosamba/samdb` |
| `python3-peewee` | state table `azureobject` | `modernc.org/sqlite` + `database/sql` (as `internal/store`) |
| `python3-passlib` (`nthash`) | NT hash from a clear password (only the unused `password=` path) | not needed: sync sends hashes; tests use a fixed vector |
| `hashlib.pbkdf2_hmac` | AAD hash `v1;PPH1_MD4,…` | `crypto/pbkdf2` (stdlib) |
| `python3-xmltodict` | parsing SOAP and SSO replies | `encoding/xml` into typed structs |
| `python3-requests` | HTTP with proxy and custom CA | `net/http` with `http.ProxyURL` and a `tls.Config` pool from `internal/tlsutil` |
| `python3-msal` | client credentials with certificate, device code, in-memory cache | MSAL Go (`confidential.NewCredFromCert`, `public.AcquireTokenByDeviceCode`), in-memory cache only |
| `python3-cryptography` | RSA 2048 key, self-signed cert, PFX read/write, SHA-1 thumbprint | `crypto/rsa`, `crypto/x509`, `crypto/sha1`; PFX through `go-pkcs12` (PBES2/AES-256, the format Python writes) |
| `python3-jwt` | proof-of-possession JWT for `addKey`/`removeKey` | `golang-jwt/jwt/v5` (RS256, `x5t` header) |
| `python-wcfbin` | `application/soap+msbin1` encode/decode | `internal/entrasync/nbfx` (D4) |

### D2. Two package trees under `internal/`: `internal/gosamba/` and `internal/entrasync/`

The port lives in the main module, in two package trees with one job each:

- **`internal/gosamba/`** — Go replacements for the `python3-samba`/`python3-ldb` calls, over network protocols.
  Nothing Entra-specific; usable by the rest of the server (for example the read half of `pwmigrate`, a follow-up
  change).
- **`internal/entrasync/`** — the AzureADConnect_Samba4 port: Microsoft protocols, the sync engine and its safety
  rules. Uses `internal/gosamba`.

```
internal/gosamba/
  samdb/                 SID/GUID/FILETIME, UF_*/GTYPE_* (done); the LDAPS side of SamDB is internal/ad
                         (Dial, Search, NamingContexts — done), not a second LDAP client
  drs/                   sealed DRSUAPI bind, single-object GetNCChanges (EXOP_REPL_OBJ), CRC-checked decryption
                         (drs_utils + net.replicate_decrypt)
internal/entrasync/
  nbfx/                  [MC-NBFX] reader/writer + [MC-NBFS] dictionary (python-wcfbin) (done)
  aadsync/               AD Sync SOAP client: envelopes, ProvisionAzureADSyncObjects (Set/Delete),
                         ProvisionCredentials, ReadBackAzureADSyncObjects2, company configuration; AAD hash (hash done)
  identity/              entra_aba_dir: state.json, PFX, passphrase, lock, bootstrap, rotation (_aad_certificate_manager)
  graph/                 Microsoft Graph paging (users, groups), DirSync flag, SSO registration
  sync/                  SambaInfo + run_sync: object computation, the three AD writes (consistency GUID, service
                         connection point, AZUREADSSOACC) through gosamba, state DB, safety rules
scripts/test-containers.sh       tests of both trees in ubuntu:26.04 and debian:13, offline
```

Being in the main module, they follow its toolchain, `make check`/`make test` and `go.mod`; the new dependencies
(MSAL Go, go-pkcs12) join the root `go.mod` when the packages that need them land, and `go build` links them only
into binaries that import those packages.

ad-manager wires it as `ad-manager entra …` (`cmd/entra.go`). The packages talk to AD with a dedicated sync
account (not the operator's), an exception to "`internal/ad` is the only layer that talks to AD". `gosamba` exposes
mechanisms, `entrasync` decides policy, and the protection rules for the writes live in `internal/entrasync/sync`
next to the calls, server-side; AGENTS.md rule 3 names this exception (amended at the maintainer's decision).
`sync/` depends on two narrow interfaces (`Directory`, `Cloud`) so the engine is tested with fakes.

### D3. Hashes over DRSUAPI everywhere

The Python reads `unicodePwd` straight from local `sam.ldb` when it runs on the DC and switches to DRSUAPI only
with `user_ad`. The Go port always uses DRSUAPI, because Go cannot open `sam.ldb` (ldb + tdb/lmdb, and Samba's
`encrypted_secrets` module) and LDAP never returns `unicodePwd`.

- Bind: `ncacn_ip_tcp` through the endpoint mapper to the DC in `ad_url`'s host, **Kerberos only** (never NTLM,
  `winrpc.Kerberos`), DCE style and **sealed** (privacy level), with the go-msrpc patches carried in
  `third_party/go-msrpc`, which gains the `msrpc/drsr/drsuapi/v4` package (pruned copy rule in `PATCHED.md`).
- Sockets: one dialer pinned to the DC — the endpoint mapper's answer picks the port, never the host — that keeps
  every socket it opens (the mapper's too) and closes them all on `Close`, on any failure and on a timeout. Results
  of a call cross a channel only; a late result after a timeout is dropped.
- Call: `DRSBind` → `DRSGetNCChanges` level 8 with **`EXOP_REPL_OBJ`** for one object named by GUID,
  `replica_flags = 0`, `max_object_count = 1` — the single-object DCSync that `internal/sambadc/pwmigrate.py` already
  uses and that works on Samba and Windows. (The Python's `EXOP_REPL_SECRET` is the RODC secret path; Windows
  documents it for RODCs only.) One bind per run.
- Result checks: the outer return, then the inner `ExtendedReturn` (`EXOP_ERR_SUCCESS`) and `DRSError`, then the
  returned object's GUID equals the requested one. The generated client checks only the outer return.
- Decryption: a carried patch (`ad-manager patch` in `PATCHED.md`) makes `DecryptData` refuse a payload shorter
  than its 16-byte salt + 4-byte CRC, verify the CRC32 of the decrypted data ([MS-DRSR] 4.1.10.6.22), and
  `DecryptHash` require exactly 16 bytes before the RID DES layer. Tests: wrong session key, corrupt byte,
  truncated payload — all errors, never a wrong hash, never a panic.
- Read-only: the code path has no write opnum; a test asserts only `DRSBind`, `DRSGetNCChanges`, `DRSUnbind`.
- Rights: *Replicating Directory Changes* and *Replicating Directory Changes All* on the domain NC plus read on the
  synced OUs. Docs give the `samba-tool dsacl set` and Windows commands. Password from `ENTRA_AD_PASSWORD` or
  `ad_password_file` (`0600`, checked like `.pfx-passphrase`).
- User without an NT hash: the Python skips the whole user on a DC (local mode) and sends 32 `X` characters as a
  hash in member mode. The port sends the user and skips only its hash, with a `hash_missing` log line — a
  documented difference, covered by a passwordless fixture.
- Gate: the hash read equals `samba-tool user getpassword --attributes=unicodePwd` on Samba 4.23 (Ubuntu 26.04
  and Debian 13 containers) and the DSInternals/`secretsdump` value on a Windows Server 2022 DC (Vagrant lab)
  before any cloud write.

### D4. .NET Binary XML codec (`internal/entrasync/nbfx`)

- Goal is **semantic interoperability, not byte equality** with python-wcfbin. Its writer guesses a typed record
  from the text (`0`/`1`, `true`/`false` in any case, integers, any base64-looking string, QNames, UUIDs) and strips
  surrounding whitespace, which changes values (`True` → `true`, a non-canonical base64 string re-encoded,
  `"Sales  "` → `"Sales"`). The Go writer emits every text as a UTF-8 `Chars8/16/32Text` record (the `+1` variant
  when it closes the element) and names and namespaces from the [MC-NBFS] static dictionary when present.
- Reader: every [MC-NBFX] record, arrays and lists in attribute values included; dynamic dictionary strings
  belong to the binary *session* encoding (`application/soap+msbinsession1`) and are refused. Output is one
  well-formed XML document for `xml.Unmarshal`. Names are checked as ASCII NCNames so input cannot inject markup;
  invalid UTF-8, unpaired UTF-16 surrogates and characters outside XML's `Char` are errors, not silently replaced;
  comments are dropped; a local-kind DateTime gets the host's offset, kind 3 is refused.
- Writer input must be well-formed with one root (matching end tags); every text is kept, whitespace included —
  envelopes are built without indentation instead.
- Limits: 16 MiB input, 64 MiB output (checked before every write, so dictionary strings and arrays cannot
  amplify), depth 256, every length checked against the remaining input before use. Fuzz invariant: whatever
  decodes re-encodes and decodes to the same document (done, 86 million runs).
- Vectors: python-wcfbin's doctest vectors and the [MC-NBFX] layouts; round trips of the five request envelopes.
  Optional interop: `internal/entrasync/parity/wcfbin-interop.sh` runs python-wcfbin in an offline Ubuntu 26.04 container
  and compares canonical XML of both decodings, except python-wcfbin's own reader defects: TimeSpan read as
  milliseconds/10 (36,000,000 ticks shown as `1:00:00` instead of `PT3.6S`), DateTime without its time zone and
  truncated to microseconds, typed text it alters on write. Gate G1 (the real service accepting the Go bytes) is
  the proof.

### D5. Identity and tokens: adopt, never recreate

- `entra_aba_dir` keeps the Python layout: `state.json`, `current.pfx` (symlink/copy of the active cert under
  `certs/`), `.pfx-passphrase` (`0600`) or `ENTRA_SYNC_PFX_PASSWORD` (≥ 16 chars), `.lock` (`flock`). Directory
  must be `0700`, files `0600`, no symlink for the passphrase — same checks as the Python.
- Fail closed exactly where the Python does: other tenant in `state.json`; `certs/*.pfx` without `state.json`;
  `ready` without `current.pfx`; expired certificate. None of them creates an application.
- `entra init` runs the bootstrap (device code with the Graph PowerShell public client, `Application.ReadWrite.All`,
  `AppRoleAssignment.ReadWrite.All`, `Directory.Read.All`), creates the app with `ADSynchronization.ReadWrite.All`
  (+ `User.Read.All`, `Group.Read.All`), waits for a token as the Python does, and is resumable from `pending`.
- Rotation (≤ 30 days left, new cert 90 days) and cleanup (retired keys after 14 days) run at the start of a
  **non-dry** `entra sync`, through `addKey`/`removeKey` with the proof JWT; a failure is a warning while the
  current cert is valid, as in the Python. In dry run they are reported (`would rotate`) and nothing is written to
  Graph or to `entra_aba_dir` (the Python rotates even in dry run; not ported).
- Delegated steps move to `entra init`: DirSync on the organization (Graph `PATCH /organization`), the
  `PasswordHashSync` feature bit (read-modify-write that keeps the other bits; skipped when
  `hash_synchronization = false`) and Seamless SSO enablement. Delegated tokens are in memory only and need a TTY.
- `entra sync` only verifies the prerequisites (company configuration: DirSync enabled, PHS bit when hash sync is
  on) and fails with "run `ad-manager entra init`" when one is missing. (The Python enables them on the first run
  with an empty state, aborting when there is no TTY for DirSync.) The SSO account password renewal stays in sync
  but only with a TTY; under `--service` it logs an error and continues, as in the Python.
- Because the format is the same, the operator can go back to the Python tool with the same directory.

### D6. State database

- SQLite file `db_path` (default `/var/lib/ad-manager/entra-sync.db`, `0600`), table `azure_object`
  (`source_anchor`, `object_type`, `last_data` JSON, `last_data_at`, `last_hash_digest`, `last_hash_at`), primary
  key `(source_anchor, object_type)` (the Python keys on the anchor alone and its updates are not scoped by type).
- A `meta` table binds the DB to its tenant id, AD domain SID and the full anchor policy (`source_anchor_attr`,
  `use_ms_ds_consistency_guid_if_exist`); a run with any of them different refuses to start.
- Change detection: canonical JSON (sorted keys) of the computed object compared to `last_data`, the Python's
  `json.loads(last) != data` semantics.
- Hash digest: HMAC-SHA-256 of the uppercase hex NT hash with a 32-byte key kept **outside the DB** in
  `entra_aba_dir/.state-key` (`0600`), so the DB alone gives nothing to compare offline. (The Python stores
  `sha1(pickle(hash))`.)
- `entra import-state` copies `sourceanchor`, `object_type`, `last_data_send`, dates (read-only open of the source);
  it records the tenant/domain/policy from the current config after confirming them with the operator. Hash
  digests cannot be converted, so every hash is resent once on the first Go run.

### D7. Engine order (parity with `run_sync`)

1. Identity (D5); prerequisites verified; nothing is written in dry run.
2. Seamless SSO password renewal when enabled (account missing or password older than 30 days, TTY only).
3. Read AD and compute objects (users, devices if enabled, groups; members mapped to anchors; anchors per D8).
4. Deletions when `do_delete` (D8 decides what may be deleted): `ProvisionAzureADSyncObjects` with
   `SyncOperation = Delete` and only the `SourceAnchor` property; the state row is removed only after a successful
   per-object result (`ResultCode`, `ResultErrorDescription` nil). (The Python ignores the result.)
5. Users, then devices (+ service connection point when `create_service_connection_point`), then groups
   (`ProvisionAzureADSyncObjects`, `SyncOperation = Set`); a group whose members include a group created in this
   run is resent after 30 s; a group with a failed member is not recorded so it is retried next run.
6. Hashes for users whose digest changed or `--force`: `ProvisionCredentials` with the AAD hash. On a `Result`
   error: one shared 30 s wait per run, skipped when the nested-group wait already happened or when no user was
   sent in this run, then one retry — the Python's `already_wait` logic.
7. Every send/delete/error is an audit line (`entra.user.send`, `entra.group.delete`, `entra.hash.send`, …) and a
   JSON log line like the Python's (`type`, `timestamp`, `data`) to `log_file` — hashes never logged.

### D8. Safety rules (in `internal/entrasync/sync` and `internal/gosamba/drs`, not only in docs)

- **Ownership**: by default (`delete_scope = "state"`) a deletion candidate must be in this tool's state for this
  tenant and domain (objects it synced, or imported from the Python DB) and absent from the AD read. Before each
  user/group deletion, Graph is checked: the object must still have that immutable id and
  `onPremisesSyncEnabled = true`; a user that became cloud-only is kept and its state row dropped.
  Graph has no `onPremisesImmutableId` on groups, so groups use a typed join in **both** scopes: one
  `ReadBackAzureADSyncObjects2` call per run maps each group entry (`SyncObjectType` `Group`, `CloudAnchor`
  `Group_<id>`) to its `SourceAnchor`; a candidate is deleted only when exactly one group entry has its anchor and
  Graph shows that group id with `onPremisesSyncEnabled = true`. A missing, duplicate or other-type entry skips the
  deletion (`group_delete_skipped`). With `use_get_syncobjects = false` there is no join, so groups are never
  deleted. Devices are deleted from state only (the Python's cloud device enumeration is unreachable code).
  `delete_scope = "tenant"` (opt-in) is the Python's behavior: every sync-enabled user with an immutable id in the
  tenant and every joined group.
  A user found cloud-only is not deleted and its state row is dropped — the one state change without a
  deletion, and not in dry run.
- **Accidental-delete guard**: when the run would delete more than `max_deletes` (default 500, `0` disables), or
  when the AD read returned zero users while state or cloud has some, no deletion is made, the run logs
  `delete_guard` and exits non-zero; `--allow-deletes N` overrides for one run.
- **Source anchor**, exactly the Python's `return_source_anchor`: raw value of `source_anchor_attr` (default
  `objectGUID`), replaced by `ms-DS-ConsistencyGuid` when set and `use_ms_ds_consistency_guid_if_exist`; the anchor
  string is the raw bytes when they are valid UTF-8, else their base64. Built-in (`S-1-5-32-*`) and RID < 1000
  objects never get an anchor (never synced). With `write_ms_ds_consistency_guid_if_empty`, an empty
  `ms-DS-ConsistencyGuid` receives the **selected raw anchor** (not dry run). The anchor policy is locked in the
  state DB (D6).
- **XML**: every value goes through `xml.EscapeText`/the encoder; a `displayName` of `A & <B>` reaches Microsoft
  intact.
- **AAD hash**: `v1;PPH1_MD4,<salt hex>,1000,<pbkdf2-sha256(utf16le(upper(hex(nt))), salt, 1000, 32) hex>;`
  with a 10-byte salt from `crypto/rand`.
- **Dry run** (default true): no write to AD or Microsoft, no state change; reads and token acquisition happen.
- **Writes to AD** (`ms-DS-ConsistencyGuid`, service connection point, `AZUREADSSOACC`) need an LDAPS (or
  StartTLS) connection, use the sync account, and are audited.
- **TLS**: `verify = false` is refused; a custom CA file is allowed (`ca_file`), as the project does for LDAPS.

### D11. Microsoft's move from Connect Sync to Cloud Sync

What Microsoft published ([What's new in Entra](https://learn.microsoft.com/en-us/entra/fundamentals/whats-new),
updated 2026-10-08):

- *Upcoming change — Migrate from Microsoft Entra Connect Sync to Microsoft Entra Cloud Sync* (April 2026, plan
  for change): no retirement date; from July 2026 tenants are told one by one (Message Center, Connect Health,
  e-mail) when their transition window opens, simple tenants first. After a tenant's transition, Cloud Sync
  replaces the identity sync of Entra Connect; hybrid authentication features stay.
- *Enhanced admin authorization for Connect Sync configuration changes* (May 2026): enabling or disabling sync
  features needs an interactive admin sign-in; the cloud is the source of truth for feature state.
- Hard-match block (from 2026-06-01): Entra refuses to hard-match an AD user to a cloud user that holds an Entra
  role.

This port speaks the service Connect Sync uses, so a transitioned tenant will most likely stop accepting it, in a
way Microsoft does not describe. The rules below make that a clean stop, never a data loss:

- **Fail closed on refusal**: when the service refuses a whole call — the prerequisite check, the read-back or a
  send answered with a SOAP fault or HTTP 401/403 with a valid token, or DirSync reported off — the run stops before any further send or
  deletion, logs `sync_service_refused` with Microsoft's message, leaves the state unchanged and exits non-zero;
  `--service` retries at its interval. (The Python logs and continues object by object.)
- **Deletions only on a healthy run**: the deletion phase starts only after this run's prerequisite check,
  read-back and Graph calls succeeded, and the first refused deletion stops the remaining ones.
- **Never turn sync back on by itself**: DirSync and the feature bits are changed only by `entra init`, from a TTY
  (matching the May 2026 change), so a tenant converted to cloud-only or moved to Cloud Sync is never flipped back
  by a scheduled run.
- **Per-object failures are not refusals**: a failure *result* for one object inside a successful call is logged
  for that object and the run goes on, as in the Python.
- **Hard-match block**: a refused user (an existing cloud user with an Entra role) is logged as
  `hard_match_blocked` with the documented way out (remove the role, sync, restore it), not recorded as sent, and
  the run goes on with the other objects.
- `internal/gosamba` does not depend on any of this: the DRSUAPI and SamDB-over-LDAPS replacements stay useful to
  the server whatever happens to the Entra side.

### D9. Configuration

`config.toml` `[entra]` mirrors `azure.conf` names in snake_case (`azure_ad_name`, `tenant_id`, `entra_aba_dir`,
`proxy`, `ca_file`, `do_delete`, `max_deletes`, `sync_device`, `create_service_connection_point`,
`source_anchor_attr`, `write_ms_ds_consistency_guid_if_empty`, `use_ms_ds_consistency_guid_if_exist`,
`hash_synchronization`, `dry_run`, `delete_scope`, `db_path`, `log_file`, `warning_duplicate_mail_value`,
`calculate_deletions_based_on_last_sync`, `use_get_syncobjects`, `alternate_login_id_attr`, `basedn`,
`basedn_user|group|computer` (lists instead of `|`), `custom_filter_user|group|computer`, `enable_single_sign_on`,
`sync_interval`, `ad_url`, `ad_user`, `ad_password_file`). `docs/entra-sync.md` has the full mapping table.
Removed keys: `password_ad` (environment only), `verify=False`, `folder_callback_python` (refused), `url` pointing
at a `.ldb` file, `pathsmbconf`.

### D10. Validation strategy

- Unit: NBFX golden + fuzz; AAD hash vector (the Python's commented fixed salt `b4 77 12 4d e5 4c 20 30 37 8f`);
  anchor rules; account expiry; duplicate mail; delete guard; XML escaping; state import; PFX read of a file
  written by the Python manager.
- Engine: fakes for `Directory`/`Cloud` reproduce `run_sync` scenarios (nested groups wait, failed member, Result
  retry, `--force`, from-state deletions).
- **Parity**: against the Docker Samba (`make samba-up`, `empresa.test`) with a fixture of users, groups (nested),
  computers, a passwordless user and an existing state DB, run the Python tool in member mode with **every
  Microsoft call and identity initialization stubbed** (an `AADInternals` stand-in that records calls) and
  `entrasync sync --dry-run --dump`; the two JSON sets must be equal except the named differences: missing hash
  (D3), whitespace/typed-text values (D4), deletion scope (D8). Script under `internal/entrasync/parity/`: a Docker network
  created with `--internal` (no route out) holds the Samba DC container and the two Ubuntu 26.04 runner
  containers, so they reach the AD and nothing else.
- **Containers and labs**: unit tests of both trees run in `ubuntu:26.04` and `debian:13` containers with the
  host's Go toolchain and module cache mounted and no network (`scripts/test-containers.sh`, done); integration runs the reader against the Docker Samba (Ubuntu 26.04, Samba from the
  distribution) and a Debian 13 Samba container; the Windows path runs against a Windows Server 2022 DC from
  `vagrant/labs`.
- Microsoft: a test tenant only (`ENTRA_TEST_TENANT`, never production, rule 1), manual and documented; gate G1
  is read-only calls, G3 writes.

## Risks / Trade-offs

- **Undocumented service on a timeline** — `provisioningservice.svc` and its client headers (`ClientVersion 8.0`,
  build `2.6.84.0`) can change at any time, and tenants are being moved to Cloud Sync one by one since July 2026
  (D11). Same exposure as the Python; D11 turns a refusal into a clean stop, version strings are constants in one
  file. The useful life of `internal/entrasync` depends on each tenant's transition date.
- **Codec correctness** — a wrong NBFX record is a silent server fault. Mitigated by the spec vectors, round-trip
  fuzzing, the python-wcfbin interop check and gate G1 before any write.
- **DRSUAPI** — `EXOP_REPL_OBJ` single-object reads are what `pwmigrate` does on Samba today; Windows Server 2022
  is checked in the Vagrant lab before any cloud write (D3 gate).
- **Two new dependencies** — MSAL Go (official Microsoft, handles the cert assertion and device flow correctly)
  and go-pkcs12 (needed to read the PFX the Python wrote; without it every existing install would have to
  recreate its application). Both pinned.
- **Everything resends hashes once** after adopting a Python install (D6). Acceptable.

## Migration Plan

1. Install ad-manager with the `entra` command on the host that ran the Python tool; stop the Python timer.
2. Fill `[entra]` from `azure.conf` (table in docs); put the AD password in the environment or a `0600` file.
3. `ad-manager entra import-state /root/last_send_azuread.db`.
4. `ad-manager entra status` (adopts `entra_aba_dir`, prints the app id and cert expiry, no network write).
5. `ad-manager entra sync --dry-run` and compare with the Python's last log; then `--no-dry-run`.
6. Rollback: stop `entra`, restart the Python tool with the same `entra_aba_dir`; its state DB is unchanged by
   the import (read-only copy).

## Open Questions

- When `delete_scope = "state"` proves itself on test tenants, should the opt-in tenant scope be dropped?
- Is a later panel page worth it before Microsoft's Cloud Sync plans are clear?
- Plan B for tenants moved to Cloud Sync, to evaluate before investing past gate G3: the Cloud Sync provisioning
  agent (Windows only, its own undocumented protocol to Azure) or API-driven inbound provisioning (Graph
  `bulkUpload`, documented, but it creates cloud-only users and syncs no password hash). Neither replaces this tool
  today.
