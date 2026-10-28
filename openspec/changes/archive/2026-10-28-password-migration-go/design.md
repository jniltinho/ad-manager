## Context

`pwmigrate.py` (539 lines) does: pin the destination by domain SID; list the source's users over LDAP; classify each
against the local `sam.ldb` (`missing`, `builtin` by RID/SID and account type); per user, read the secrets with a
sealed `GetNCChanges`, decrypt the NT hash (RID-keyed DES) and `supplementalCredentials`, inspect the Kerberos package
and its salt, check idempotency (`same`), and write the material in one transaction with the password-hash bypass
control, falling back to the NT hash when Samba refuses the copied Kerberos blob.

## Decisions

### D1. Read with gosamba/drs (Kerberos only)

`drs.Reader` gains `Secrets(ctx, guid) (Secrets, error)`: the NT hash (`unicodePwd`, as `NTHash` today) and the
`supplementalCredentials` value decrypted from its encrypted payload (session key + salt, CRC-checked; no RID layer).
The bind stays Kerberos-only with the KDC at the source address. Its service principal is the DC's replication SPN,
`E3514235-4B06-11D1-AB04-00C04FC2DCD2/<NTDS Settings objectGUID>/<domain>`, which only a DC of the domain holds:
the rootDSE that names the DC is anonymous, so a host principal taken from it could be any machine whose key an
attacker controls; a ticket for the replication SPN is useless to anyone but a real DC (checked on Samba 4.23 and
Windows Server 2008 R2). The reply also carries the object's SID, `sAMAccountName` and `sAMAccountType`, returned
with the secrets.

A refusal (access denied, or Windows' `ERROR_DS_DRA_BAD_DN`) counts as missing replication rights only while no
secret has been read yet: the run stops with the "Replication rights required" error and nothing written. After a
read succeeded, a refusal is that user's `error` (an object deleted meanwhile is not a rights problem).

### D1b. The listing is a proposal; the sealed reply decides

Users, GUIDs and the source SID come over LDAP, which is not integrity-protected here. Before anything is written for
a user, its sealed DRS reply must name the same `sAMAccountName` (case-insensitive) and a SID of the source domain —
not of this one; otherwise the run stops ("does not match its LDAP listing"). A tampered listing can hide or add
candidates, never send one account's material to another.

LDAP binds without the password travelling: NTLM over plain LDAP (a 2008 DC), else Kerberos (GSSAPI) over LDAPS
with the TLS channel binding (`tls-server-end-point`, RFC 5929), which Samba ("ldap server require strong auth") and
a Windows DC with LDAP signing require. go-ldap's GSSAPI client sends no binding, so a small client of our own
does the bind: the binding in the authenticator, the AP-REP checked against the authenticator sent (RFC 4120
3.2.5), and RFC 4752's last step with a rotated wrap token undone and "no security layer" (1) selected. LDAP calls
end when the run is cancelled. The
certificate is not checked: integrity comes from D1b, and neither bind gives a man in the middle the password.

### D2. Parse supplementalCredentials in Go, rebuild it as Samba does

New `internal/gosamba/suppcreds`: `USER_PROPERTIES` (signature, length, packages), each package name UTF-16LE and
value hex-ASCII; `Primary:Kerberos-Newer-Keys` (`KERB_STORED_CREDENTIAL_NEW`, revision 4): default salt and key
types; `Primary:CLEARTEXT` presence. Strict bounds on every offset and length; fuzzed. As the Python helper did: `Primary:CLEARTEXT` →
`rejected`; no Kerberos package, an unreadable package or a salt that does not transfer → NTLM-only with a reason;
`Primary:Kerberos-Newer-Keys` preferred to `Primary:Kerberos` (revision 3); the keys transfer only when the source
realm equals the destination's (case-insensitive) and the salt starts with it on a label boundary (a custom salt or
`EXAMPLE.NET.BR` for `EXAMPLE.NET` does not). The copy keeps only the two Kerberos packages and the `Packages` index
rebuilt to name them (WDigest and anything else dropped), serialized byte for byte as Samba's NDR. Fixtures: real
blobs of throwaway users on the test Samba and variants made with Samba's own `ndr_pack`, each with the Python
helper's answer and rebuilt blob; never taken from a real domain.

### D3. Write with ldbmodify, secrets on stdin

The database is the one Samba's configuration names (`testparm -s --parameter-name="private dir"` + `sam.ldb`), used
for the pin, every read and every write; no configuration or no database there is an error, never a default path
that may hold another database. The DC-side code builds only on Linux (a stub elsewhere: the CLI and the panel
still build for Windows).

`ldbmodify -H <that sam.ldb> --controls=local_oid:1.3.6.1.4.1.7165.4.3.12:0` with the LDIF on stdin
(never a file, an argument or the environment), one invocation per user = one transaction; stdout/stderr never
returned (a sanitized reason only). LDIF: `replace: unicodePwd`, `replace: supplementalCredentials` with the rebuilt blob — or with no value for
NTLM-only (an empty replace, which also works when the attribute is absent), so no stale keys remain — and
`replace: pwdLastSet` with the current FILETIME. When Samba rejects the copied
blob, a second invocation writes the NT hash only → `ntlm_only` with the same reason text as today. `ldbsearch`
(same database) reads, for the plan and the classification, the users' metadata only (`sAMAccountName`,
`objectSid`, `sAMAccountType`, `pwdLastSet`); during the apply, a user's `unicodePwd` and `supplementalCredentials`
only for the `same` check, dropped as soon as compared. Malformed output is an error, never "no credentials".

A cancelled or timed-out run stops (the DRS reader's own timeout included: it closed its sockets); no fallback write
follows a write cut short; an unreadable current state is that user's `error`, never a write. Owned copies of the
material (the parsed packages, the decoded Kerberos package, the LDIF, ldbsearch's output) are cleared after use. Users already set stay set (the
apply is idempotent, so the run can be repeated).

### D4. Same guarantees

`same` needs a non-zero `pwdLastSet`, the same NT hash and either the rebuilt Kerberos blob or no Kerberos keys
stored (an NTLM-only user, including one whose keys Samba refused before: the outcome would not change).

Destination pinned by SID (required, must match this DC, source ≠ destination); `Administrator`, `Guest`, `krbtgt`,
trust and machine accounts never set (RID/SID and `userAccountControl`); no user created; plan reads no secret;
idempotent apply; the source never written; audit counts only; secrets zeroed after use.

### D5. Parity before removal

During the change both implementations exist: a lab script runs the Python and the Go plan/apply on twin
destinations from the same source and compares outcomes, `unicodePwd` and `supplementalCredentials` (hashes of the
values, never the values). The Python files are removed when parity holds on: Samba 4.23 → Samba, Windows Server 2008
R2 (dockur) → Samba, Windows Server 2022/2019 (dockur) → Samba.

## Risks

- `ldbmodify` option or output differences across Samba versions: version-gated tests on 4.22 (Debian 13) and 4.23.
- A process argument must never carry a secret: the LDIF only on stdin; a unit test asserts the argv.
- Reading another domain's hashes into a lab copies real secrets: production DCs are used only for the plan.

## Validation

Unit tests (parser, LDIF builder, classification), `scripts/test-dc.sh` (Samba → Samba in containers on Ubuntu 26.04 and
Debian 13), the dockur labs (2008 R2, 2022/2019 sources), parity with the Python implementation, `make check`,
`make test`, `make test-integration`, Codex review.
