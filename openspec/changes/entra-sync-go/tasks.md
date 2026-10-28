## 1. Proposal

- [ ] 1.1 Proposal, design, tasks and the `entra-sync` spec; `openspec validate entra-sync-go --strict`.
- [x] 1.2 Codex review of the proposal (`codex exec --sandbox read-only`), round 1: 6 blockers (DRS extended op
      and inner errors, delete operation, rotation in dry run, deletion ownership, anchor encoding, unchecked hash
      CRC), 6 should-fix, 2 nits. All applied. Round 2: 12 of 14 fixed, 2 partial; 1 blocker (codec output
      amplification), 9 should-fix, 1 nit. All applied. Round 3: 6 of 11 fixed, 5 partial; 1 blocker (attribute
      list memory), 5 should-fix, 2 nits. All applied; AGENTS.md rule 3 amended
      to name `internal/entrasync` (maintainer's decision).
- [x] 1.3 Layout: `internal/gosamba/` and `internal/entrasync/` in the main module (first as separate modules,
      moved under `internal/` at the maintainer's request); `scripts/test-containers.sh` runs both in Ubuntu 26.04
      and Debian 13, offline.

## 2. Phase 0 — protocol spike (gate G1: read-only calls on a test tenant)

- [x] 2.1 `internal/entrasync/nbfx`: [MC-NBFX] reader and writer, [MC-NBFS] dictionary, caps, fuzz test (two fuzz
      findings fixed and kept as corpus); Codex round 2: output cap, lists, arrays, DateTime zone, strict
      UTF-8/UTF-16/XML chars, one root, whitespace kept — all fixed, fuzz invariant now a full round trip.
- [ ] 2.2 Interop check with python-wcfbin in an offline Ubuntu 26.04 container (`internal/entrasync/parity/wcfbin-interop.sh`):
      Go-encoded envelopes decoded by Python and Python-encoded ones decoded by Go give the same canonical XML.
- [ ] 2.3 `internal/entrasync/identity`: read `state.json`, PFX (go-pkcs12) and passphrase with the Python checks;
      MSAL Go client credentials with proxy and CA; `entra status`.
- [ ] 2.4 Read-only SOAP (`GetCompanyConfiguration`/sync configuration, `ReadBackAzureADSyncObjects2`) and Graph
      paging against a test tenant (`ENTRA_TEST_TENANT`). **G1**: both decode into typed structs.

## 3. Phase 1 — directory side (gate G2: parity on Docker Samba)

- [x] 3.1 Vendor `msrpc/drsr/drsuapi/v4` into `third_party/go-msrpc` (pruned copy rule, `PATCHED.md`) with the
      length/CRC patch to `DecryptData`/`DecryptHash`; wrong-key, corrupt and truncated tests; patch tests green.
- [x] 3.0 `internal/gosamba/samdb`: SID, RID, GUID, FILETIME, `UF_*`/`GTYPE_*`.
- [x] 3.2 `internal/gosamba/drs`: `Dial`/`NTHash`/`Close` (Kerberos only, sealed DRSUAPI, one bind, `EXOP_REPL_OBJ`
      by GUID, outer and inner result checks, same-object check), opnum allowlist test, missing-right error;
      `scripts/test-dc.sh` passes against Samba 4.23.6 (Ubuntu 26.04) and 4.22.11 (Debian 13): hash equals
      `samba-tool user getpassword`, passwordless user gives none, a user without rights is refused.
      Windows Server 2022 DC (`vagrant/labs`, `lab-windows.sh dc`): hash equals MD4 of the known password,
      passwordless user gives none, a user without rights is refused (`ERROR_DS_DRA_BAD_DN` there).
      Windows Server 2008 R2 DC (read only, no fixtures created): Administrator hash equals MD4 of its password;
      its LDAPS speaks only TLS 1.0/1.1, so the LDAP reads go over `ldap://` with NTLM, as for any legacy DC.
      Codex review of `drs`: no blocker; 6 should-fix (endpoint-mapper socket leak, timeout races, socket left open
      after a failed bind, mapper choosing the host, malformed `unicodePwd` read as "no password", script masking
      failures) and 2 nits — all fixed (pinned dialer that owns and closes every socket, typed `guard` over a
      channel, strict value counts, `-race` tests) and re-run green on Samba 4.23.6, 4.22.11, Windows Server 2022
      and 2008 R2.
- [x] 3.3 LDAPS reads through `internal/ad` instead of a second LDAP client: exported `Search` (paged) and
      `NamingContexts` (rootDSE); `TestSearchForSync` passes on Samba 4.23.6, 4.22.11 and Windows Server 2022. The
      sync's own filters (base DNs, custom filters) come with 3.4.
- [ ] 3.4 `internal/entrasync/sync` compute: anchors, consistency GUID, expiry, manager, members, devices, duplicate
      mail; config `[entra]` with secret checks; callback key refused.
- [ ] 3.5 Parity script (Python in member mode with every Microsoft call stubbed, offline container, vs
      `entrasync sync --dry-run --dump`) on the Docker Samba fixture with a passwordless user and an existing state.
      **G2**: equal sets except the named differences; hash read equals `samba-tool user getpassword` on Ubuntu
      26.04 and Debian 13 Samba, and the reference value on the Windows Server 2022 Vagrant lab.

## 4. Phase 2 — writes (gate G3: full run on a test tenant)

- [x] 4.0 `internal/entrasync/aadsync` AAD password hash (`crypto/rand` salt, vector test).
- [ ] 4.1 SOAP writes: `ProvisionAzureADSyncObjects` (`Set` and `Delete`, per-object result checked),
      `ProvisionCredentials` (AAD hash); escaping tests; AAD hash vector.
- [ ] 4.2 State DB (`azure_object`, `meta` with tenant/domain/anchor policy, HMAC key in `.state-key`),
      `entra import-state`.
- [ ] 4.3 Engine order of `run_sync`: deletions (state scope, Graph re-check, guard; tenant scope opt-in), users,
      devices + SCP, groups with nested resend, hashes with the shared wait; audit and JSON log; dry run writes
      nothing (rotation included).
- [ ] 4.4 `--service` loop (Python parity).
- [ ] 4.4a Clean stop on refusal (D11): `sync_service_refused`, deletions only on a healthy run, no automatic
      DirSync, `hard_match_blocked`; fake-service tests for each spec scenario.
- [ ] 4.5 **G3** on the test tenant: first full sync, a delta run sends nothing, a delete, a password change
      reaches Entra (sign-in works), dry run leaves the tenant unchanged.

## 5. Phase 3 — identity lifecycle and extras

- [ ] 5.1 `entra init`: bootstrap (device code, app + permissions, resumable `pending`), DirSync flag, password
      hash sync feature bit (read-modify-write), Seamless SSO enablement; `entra sync` only verifies them.
- [ ] 5.2 Rotation with proof JWT and cleanup of retired keys; tested against the test tenant with a short cert.
- [ ] 5.3 Seamless SSO: `AZUREADSSOACC` creation and 30-day password renewal (TTY only) over LDAPS.
- [ ] 5.4 Full run against the Windows Server 2022 DC lab (`vagrant/labs`) and a test tenant.

## 6. Docs and release

- [ ] 6.1 `docs/entra-sync.md`: install on Ubuntu 26.04, sync account rights, config mapping from `azure.conf`,
      migration and rollback, differences from the Python tool.
- [ ] 6.2 Server i18n entries for the new messages (`UPDATE_I18N=1 go test ./internal/i18n`), CHANGELOG.
- [ ] 6.3 `make check && make test && make test-integration`, `make build`; Codex review of the implementation.
