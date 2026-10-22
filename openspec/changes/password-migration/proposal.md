## Why

A migration by the "From another AD" import gives every user a new generated password with "change at next sign-in":
LDAP never exposes the password hashes (`unicodePwd`, `supplementalCredentials` are write-only), so the import
cannot carry them. Users then cannot sign in to AD-backed applications (Gitea, and any LDAP/Kerberos service) until
they change their password once. For a cutover that keeps a **new domain** (different SID) — not a `dc-join` — the
operator needs the real passwords preserved so nothing breaks on day one.

Password hashes can only be obtained by directory replication (DRSUAPI), the same channel a DC join uses (normal
Windows LDAP never returns `unicodePwd` or `supplementalCredentials`). The NT hash is `MD4` of the password with no
salt, so it is independent of the domain SID and portable to the new domain. The Kerberos keys are **not** derivable
from the NT hash: they are copied as stored, with each principal's own salt and key types, and only when the salt
convention transfers; where they do not, the user keeps NTLM and the Kerberos keys are reset on the next password
change. Setting a password by hash is done through the local Samba database with its password-module controls (the
chosen restricted path; SAMR can also set an NT hash) — never over LDAP.

## What Changes

- A new, opt-in **password-material migration**: the tool extracts the password hashes of the source domain over
  DRSUAPI (a "DCSync", with an account holding *Replicating Directory Changes (All)*) and sets them on the matching
  users of the ad-manager-managed domain, so each user keeps their production password. Never over LDAP, never as
  plaintext.
- It is **one more option of the "From another AD" import** — a `Passwords` checkbox next to Objects, GPOs, DNS and
  Shares — driven by the **source URL, domain, user and password the operator already types in that form**. The
  server hands that credential to the DC agent (the same way the import already passes the source password for
  objects, DNS and shares); there is no separate credential file in the panel flow. A `migrate passwords` CLI stays
  for automation.
- It runs through the **DC agent's dedicated write mode** (a default-off gate, like the GPO restore's pattern but
  its own), on the Samba DC that owns the database, and is audited (`password.migrate`) without any secret.
- Users are matched by `sAMAccountName`; a user missing in the target is listed, not created (run the object import
  first). "Change at next sign-in" is cleared for the users whose hash was set.
- Both NTLM and the Kerberos keys are migrated when each principal's stored salt and key types transfer to the new
  domain; otherwise only NTLM is set, reported per user, and the Kerberos keys are regenerated on the next password
  change. A source that stores reversible/cleartext passwords (`Primary:CLEARTEXT`) is rejected, not copied.
- The **"From another AD" import also carries the last-logon timestamp** of users and computers and the
  `operatingSystem` of computers, so the inactivity view ("no logon for 90+ days") and the OS column are meaningful
  right after the migration.
- UI: a trash icon with a confirmation popup to delete a saved import profile, and the import's log/progress modal is
  a rectangle.
- Out of scope: creating users (the object import does that), `dc-join` (which replicates everything already),
  reading or storing plaintext passwords, migrating `krbtgt`/trust/machine-account secrets, and any path over LDAP.

## Capabilities

### New Capabilities

### Modified Capabilities
- `ad-migration`: gains an explicit step that migrates the password hashes from the source domain into the new
  domain, so a new-domain cutover preserves the users' passwords.

## Impact

- Server: new operation in `internal/ad`/`internal/sambadc` and the DC agent (`internal/agent`), reusing the
  explicit-write-mode gate of the GPO restore; new API route `POST /api/import/ad/passwords` (plan/apply), CLI
  `migrate passwords`, MCP tool `ad_password_migrate` (write mode only), `internal/i18n` entries and
  `internal/server/openapi.yaml` (rule 8), docs (`docs/migration-runbook.md`, `docs/cli.md`).
- Security boundary: a high-trust operation (reads every secret of the source). It is confidential, audited without
  secrets, refused unless the agent is in the explicit write mode, and the source credential is handled like the
  others (environment/stdin/0600 file, never an argument).
