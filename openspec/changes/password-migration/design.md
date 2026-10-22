## Context

- The import reads over LDAP, which never returns `unicodePwd` or `supplementalCredentials`. So password material
  must come from DRSUAPI replication (`DRSUAPI_REPLICA_FLAG`/`GetNCChanges` with secrets), the same protocol
  `samba-tool domain join` uses. It needs *Replicating Directory Changes* **and** *Replicating Directory Changes All*
  on the source (Domain Admins has both).
- The target is a **different domain** (new SID): a DRS replica of the source into it is impossible (replication is
  within one domain). The material is instead extracted as a standalone DCSync and re-applied per user.
- The NT hash is `MD4(UTF-16LE(password))` — unsalted, SID-independent, so it transfers to any domain. The Kerberos
  keys are **not** derivable from it: they are copied as stored, each with its own salt and key types, and only when
  the salt convention transfers; a realm or salt mismatch leaves the user NTLM-only until the next password change.
- Setting a password by hash is not possible over LDAP (`unicodePwd` wants the quoted plaintext there); it needs the
  local Samba database with its password-module controls, or SAMR. This design uses the local database write.
- The DC agent already has an **explicit write mode** on a DC (GPO restore, `gpo_restore_user`): its *validation
  pattern* fits here (`internal/agent/agent.go` `gpoRestoreReady`), but this needs its **own** default-off gate, not
  the GPO-restore permission.
- The "From another AD" inventory already reads `lastLogonTimestamp` and the computers' `operatingSystem`; the
  import does not yet write them to the target. This change also carries them, so the inactivity view and the OS
  column are correct right after the migration.

## Goals / Non-Goals

**Goals:** preserve the users' production passwords (NTLM, and Kerberos when the realm matches) in a new-domain
cutover; no plaintext anywhere; explicit, audited, confidential.

**Non-Goals:** creating or deleting users; `dc-join`; migrating `krbtgt`, trust, or machine-account secrets; any
path over LDAP; storing the extracted material.

## Decisions

1. **Extraction = read-only DCSync, not DRS into the DB.** The agent performs `GetNCChanges` against the source for
   the requested accounts over **sealed RPC**, decrypts in memory, and keeps only `{sAMAccountName -> NT hash,
   kerberos package}`. It MUST NOT set `DRS_ADD_REF`, call `UpdateRefs`, or run any join/provision step — nothing is
   written to the source, including `repsTo`. Implemented with Samba's `samba.drs_utils`/`drsuapi` Python bindings
   (already on the DC), no vendored `impacket`. The material is never written to disk, the API, logs or audit, and
   secret-bearing helper output is never passed through in an error (not a plain `CombinedOutput`).
2. **Setting = local DB write by the agent, with Samba's password controls.** For each matching user the agent sets
   `unicodePwd` by NT hash and, when the keys transfer, the Kerberos keys, through the Samba Python bindings on the
   local `sam.ldb` using the password-module controls that permit a hash write (an ordinary LDAP modify rejects it),
   one atomic transaction per user. `pwdLastSet` is set non-zero (no "must change"). This is the chosen restricted
   path, not the only one (SAMR can also set an NT hash).
3. **Kerberos keys are copied, never derived.** Each principal's stored key set, salt and key types are copied as a
   whole only when the salt convention transfers to the new domain; where it does not, only NTLM is set and the user
   is reported `ntlm-only` (the keys regenerate on the next password change — they cannot be rebuilt from the NT
   hash). Only the allowlisted Kerberos packages are rebuilt; a source with `Primary:CLEARTEXT` (reversible
   passwords) is **rejected**, never copied.
4. **Match and protect by SID/RID, never by name.** Users are matched by `sAMAccountName`; a source user missing in
   the target is reported as skipped (run the object import first), never created. Protected accounts
   (`Administrator` RID 500, `Guest` 501, `krbtgt` 502, trust and machine accounts) are identified by RID and
   account type **on both sides** — a renamed or localized Administrator is still skipped.
5. **Dedicated, default-off gate — not the GPO account.** The operation reuses the GPO restore's *validation
   pattern* (a configured write mode, a 0600 credential file, a `/api/health` flag) but a **separate**
   `password_migration` gate, off by default; the GPO-restore permission does not authorize it. It requires a
   target-domain administrator's authorization and an explicit `apply`, both enforced **before** any extraction.
6. **Pin the destination, reject source = destination.** Because the two domains share the realm name, the agent is
   pinned by the destination domain's SID/GUID and its local database identity is verified; a request whose source
   SID equals the destination SID, or whose destination does not match the pinned identity, is refused before
   anything is read.
7. **Plan then apply, full-state idempotency.** `plan` does not read secret material: it reports per user would-set,
   missing or built-in, using authenticated DRS connectivity and non-secret metadata only. The secret-dependent
   outcomes (`ntlm-only`, cleartext-rejected, `same`) are determined during the authorized `apply`, which reads the
   secrets, sets the material in a per-user transaction and rolls back on a partial failure. A user is `same` only
   when the NT hash, the Kerberos keys and `pwdLastSet` all already match — never on the NT hash alone.
8. **Authenticated DRS connectivity checked before apply.** The preflight's port-135 probe does not prove the
   negotiated DRS endpoint; before `apply`, the destination agent tests an authenticated DRS bind to the source.
9. **Credentials.** The source replication account is read like the import's source password
   (`AD_SOURCE_PASSWORD_FILE` 0600 or stdin), never an argument; it needs *Replicating Directory Changes* **and**
   *…All*. The extracted secrets live only in the agent's memory for the call.

## Risks / Trade-offs

- [DCSync can read every secret of the source domain] → the operation requests only the named users' NT and Kerberos
  material and discards the rest; `krbtgt`, trusts and machine accounts are never set on the target. High-trust:
  explicit, confidential, audited, dedicated-write-mode only, target-admin authorized.
- [Kerberos keys do not derive from the NT hash] → where a principal's salt/key types do not transfer, the user is
  `ntlm-only` and the Kerberos keys regenerate on the next password change; a realm or salt mismatch is reported.
- [`supplementalCredentials` may hold reversible cleartext] → a source with `Primary:CLEARTEXT` is rejected; only
  allowlisted Kerberos packages are rebuilt, so no cleartext is copied.
- [A read that writes the source] → `DRS_ADD_REF`/`UpdateRefs`/join are forbidden and a test proves the source's
  directory metadata is unchanged after plan and apply.
- [Replicating a 2008 R2 source over the VPN] → DRS uses 135 + the dynamic range; the destination agent tests an
  authenticated DRS bind before apply (port 135 alone does not prove it), and the guard allows those ports.
- [A partial apply] → per-user atomic with rollback; re-running completes the rest, idempotent on full state.

## Migration Plan

This is an optional step **after** the object import and **before** handing the domain to users, only on the
new-domain cutover (not needed with `dc-join`, which already replicates the hashes). Order in the runbook: objects
and DNS import → password migration → applications tested with real passwords.

## Open Questions

- Does the source use the default Kerberos salt for all principals (checked on a sample during `plan`)?
- Should the machine accounts' passwords be migrated so joined computers keep their secure channel without a rejoin,
  or is a rejoin acceptable (current Non-Goal)?
