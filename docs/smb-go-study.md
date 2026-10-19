# SMB libraries in Go — study for AD Manager

Question: can we use [hirochachacha/go-smb2](https://github.com/hirochachacha/go-smb2) or
[jfjallid/go-smb](https://github.com/jfjallid/go-smb) in the project, including to sync folders with another AD or
Samba4? State as of October 2026, read from the repositories and pkg.go.dev.

## What we already use

`go-msrpc` (remote DNS and sessions, `internal/winrpc`) brings in `github.com/oiweiwei/go-smb2.fork` — a fork of
`hirochachacha/go-smb2` with Kerberos (GSSAPI) — and the pruned, patched copy in `third_party/go-msrpc`. For
**files**, the agent works locally on the server (root, `os` + `security.NTACL` xattrs). `migrate shares` reads
the shares and the ACL of each share's **root** through `rpcclient`/`smbcacls` (`internal/winshares`), and reduces
each ACE to a level (read, write, full control) **by the name** of the source group/user — it does not translate
SIDs, and it **discards deny ACEs** and subfolder ACLs. There is no SMB file client in Go.

## hirochachacha/go-smb2

- SMB 2.0.2–3.1.1 client, TCP 445 and SMB over QUIC; **NTLMv2 and Kerberos** (password, keytab, ccache); signing
  can be required and AES-CCM/GCM encryption; `context.Context`; `io/fs` (`WalkDir`, `Glob`); DFS; `MkdirAll`,
  `RemoveAll`, symbolic links. **BSD-2-Clause** license, ~410 stars, ~990 commits, 12 open issues; requires
  **Go 1.27** (the project already uses 1.27.2).
- **No security descriptor API**: `Chmod` only touches the mode bits; `FileStat` does not carry the SD. Reading/writing
  the NTFS ACL would require `QUERY_INFO`/`SET_INFO` with `SecurityInformation` through `x/protocol` (marked
  experimental, with no stability guarantee).
- The latest published version is **v1.1.0 (2022)**, with advisory **GO-2026-5051** (out-of-bounds read and panic
  in `ReadDir`); the new code (Kerberos, QUIC, `io/fs`) is only on `master` — we would use a pinned pseudo-version
  checked with `govulncheck`.
- Stated limitations: cancellation is not always immediate (the KDC exchange and CREATE/TREE_CONNECT/LOCK wait for
  the server), concurrent writes to the same file without guaranteed ordering, `O_APPEND` for a single writer only,
  no rename across DFS shares, no DC discovery.

**Good for**: copying folder **content** between servers (Windows or Samba) with Kerberos and encryption, from
inside the agent, without mounting CIFS on the system — the "folder sync" case and the lab file copies.
**Does not solve permissions**: it neither reads nor writes SDs.

## jfjallid/go-smb

- SMB2/3 client with **RPC over SMB** (srvsvc, winreg, SAMR, LSAD, SCMR, DRSR, ICPR/WCCE, etc.), NTLM and Kerberos
  (modified gokrb5), SOCKS5. **MIT** license, ~80 stars, ~170 commits, tagged versions (v0.12.0, August
  2026), README still says "work in progress".
- **Files**: `ListDirectory`/`ListRecurseDirectory`, `OpenFile(Ext)`, `RetrieveFile`/`PutFile` (with context),
  `Mkdir(All)`, `DeleteFile`/`DeleteDir`, `ReadFile`/`WriteFile`.
- **Security descriptor**: reads it (`QueryInfoSecurity`, `QueryInfoSecurityRaw` → `msdtyp.SecurityDescriptor`);
  writing has no documented method (only the low-level `NewSetInfoReq`).
- Ships an **NTLM relay** package (forces SMB 2.1 without signing or encryption; the README asks for lab use
  only): part of the project is an offensive tool.

**Possible use, with care**: it is the only one of the two that **reads the full SD** of each file/folder, which
would let us replace `smbcacls` (external process, text) with a read in Go and also migrate subfolders and deny ACEs.
Against: the relay package in the same module (we would import only `smb`/`msdtyp`, never `relay`), an API with no
declared stability and RPC duplicated with `go-msrpc`.

## Recommendation

1. **`jfjallid/go-smb` only if we are going to read full SDs** (subfolders, denies): import only `smb`/`msdtyp`,
   pinned version, in an isolated child process, and write the translation source SID → name → target SID (which
   does not exist today). Until then, do not adopt it.
2. **Adopt `hirochachacha/go-smb2` for content only**, in its own phase (folder sync and lab copies), with: a
   pinned pseudo-version of `master`, `govulncheck` in CI, Kerberos required for the directory's `cifs/<server>`
   SPN (no NTLM, as in `internal/winrpc`), signing and encryption required, isolated execution in a child process
   like `ad-manager winrpc` (a panic or hang does not take the agent down), and hash (SHA-256) verification after
   the copy.
3. **ACLs through the current path** (`migrate shares` + agent), knowing the limit: level by name at the root, no
   denies and no subfolders. Copying full SDs requires reading (item 1), translating SIDs by name between domains
   and writing through the agent (`set_acl_sddl`), with tests against the Docker Samba and a lab Windows.
4. **Alternatives without a new library** remain valid where they fit: `robocopy /MIR` (content) on a source
   Windows and `rsync -a` between Samba servers. Careful: `/COPYALL` and `rsync -X` carry the SDs with the **source
   domain's SIDs**, which are not valid on the target — they only work when the domain is the same (DC migration).
   Sync through Go comes in when the agent needs to do the copy on its own, scheduled and audited (the
   `backup-samba` proposal and the labs).
