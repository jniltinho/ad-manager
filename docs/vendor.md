# Libraries vendored in the project (`third_party/`)

ad-manager uses Go modules normally, downloaded through `go.mod`. Only two libraries are **copied into the
repository**, under `third_party/`, wired in by a `replace` in the root `go.mod`:

```text
replace github.com/oiweiwei/go-msrpc => ./third_party/go-msrpc
replace github.com/rclone/rclone => ./third_party/rclone
```

Each one is a **pruned copy**: only the packages ad-manager imports, with the original license and a `PATCHED.md`
listing the fixes. The changed code is marked with `ad-manager patch`.

They live here, rather than as a regular dependency, for three reasons:

1. **Security patches** that upstream does not have yet. Without the copy, the build would go back to the unfixed version.
2. **Fewer external programs in production**: Windows RPC and sync run inside the binary, without
   `rclone`, `rpcclient` or `net rpc`. What still comes from the system is the server's own Samba (`net`
   gets the machine account's Kerberos ticket for sync) and `systemd-run`, which confines each run.
3. **Reproducible, auditable build**: the code that runs is in git, reviewed along with everything else. An upstream
   update only gets in when someone copies it, reapplies the patch and runs the tests.

| Library | Version | Size | License | Used in |
|---|---|---|---|---|
| go-msrpc | v1.6.4 | 4.5 MB | MIT (`third_party/go-msrpc/LICENSE`) | `internal/winrpc` |
| rclone | v1.75.2 | 2.5 MB | MIT (`third_party/rclone/COPYING`) | `internal/syncer` |

## go-msrpc — Windows RPC without external tools

**What it is**: a pure Go implementation of Microsoft DCE/RPC (MS-RPCE) and the Windows RPC interfaces, over
SMB2 (named pipes), with NTLM/Kerberos authentication.

**What problem it solves**: the parts of AD that are **written** or **queried through the DNS or SMB server**, not
through LDAP:

- **AD DNS (MS-DNSP)**: list zones and records and create or delete A, CNAME and PTR records, in the DNS window of
  the Computers screen. In the "From another AD" import (`cmd/import_ad.go`), the source is read over LDAP
  (`internal/ad/dns_ldap.go`); MS-DNSP writes the records and, with `--create-zones`, the zones to the target.
- **SMB sessions (MS-SRVS)**: who is connected to a server, from which machine, with how many open files and for
  how long (Computers → sessions).

Without the library, we would have to install and call `samba-tool dns`, `rpcclient` or `net rpc` and parse their
text output. That would require Samba packages wherever the panel runs and passwords on the command line.

**How ad-manager uses it** (`internal/winrpc`): Kerberos only, with a ticket for the server itself (`cifs/` and
`host/`). No NTLM response is sent, so a host reached through spoofed DNS gets nothing it can use. Every call goes
over signed SMB straight to the named pipe, without the endpoint mapper. Each RPC bind is also authenticated and
sealed with the ticket's keys. Each call runs in a child process (`ad-manager winrpc`, `internal/winrpc/isolate.go`)
that is killed on timeout: a hang or panic inside the library never takes the panel down.

**Patches** (`third_party/go-msrpc/PATCHED.md`): they close flaws that would let a host in the middle forge or
truncate a response on a sealed connection:

- a `fault` with status 0 accepted as success;
- PDUs outside signature verification (`bind_ack`, `alter_context_resp`) accepted as a response;
- a response with `PFC_OBJECT_UUID` shifting the verified range;
- an unauthenticated `auth_pad_length` cutting the stub (a missing `Return` became 0).

Tests in `make test` and CI: `dcerpc/patch_test.go` covers the response truncated by `auth_pad_length`, and
`TestMsrpcPatched` (`internal/winrpc`) checks that the patched copy is in use (the errors it introduces). The
other fixes are reviewed in the code (`ad-manager patch`), without a dedicated test.

**Updating**: copy the packages of the new version again, with the list from
`go list -deps ./... | grep go-msrpc`. Reapply the patch if upstream does not have it yet and run
`go test ./internal/winrpc`.

Analysis of the alternatives (`go-smb2`, `go-smb`) in `docs/smb-go-study.md`.

## rclone — the folder sync engine

**What it is**: rclone's sync engine (listing, comparison, copy, verification, `backup-dir`) and its SMB backend,
used as a **library**, without the `rclone` binary and without mounting.

**What problem it solves**: the **Sync** screen brings folders from an SMB share (Windows or Samba) to the file
server or the DC with files, in copy or mirror mode (`docs/sync.md`). rclone already solves, in a production-tested
way, what would take months to redo:

- comparison by size and date (the SMB backend does not expose hashes);
- resuming and parallelism;
- bandwidth limit;
- exclusions;
- content verification after the copy, when the job asks for it (`verify`);
- `backup-dir`, which is the basis of the quarantine.

The alternatives were worse:

- `rsync` does not speak SMB;
- mounting the share with `mount.cifs` requires privileges, leaves a mount point open and mixes up the
  permissions;
- calling the rclone binary would require installing it and handing the password to another process, through
  configuration or an environment variable.

**How ad-manager uses it** (`internal/syncer`):

- each preview or run happens in a child process (`ad-manager agent sync-worker`), confined in a transient
  systemd unit that can only write the target and the quarantine. Without systemd (a container) it only runs with
  `sync_unconfined = true`, without that confinement;
- the mirror only deletes what was in the plan, within the limits, and moves it to the quarantine;
- the password typed in the wizard is stored encrypted (AES-GCM) in the panel and the agent. A `password_file` (a
  root-owned 0600 file) is read as is. Kerberos uses the machine account.

**Patches** (`third_party/rclone/PATCHED.md`):

- every SMB connection **requires signing**, which also refuses guest or anonymous sessions;
- links and junctions **inside** the source are skipped in the listing and checked again before reading each folder
  or file (a window remains between the check and the open: go-smb2 has no "open without following"). A source whose
  path goes through a link is refused;
- an `Exists` method, which the mirror calls before deleting each file in the plan.

**Updating**:

1. temporarily remove the `replace` from `go.mod` and change the version (with the `replace` the build keeps using the local copy);
2. copy the packages from `go list -deps ./internal/syncer | grep rclone/rclone` again;
3. put the `replace` back and reapply the patch;
4. run `go test ./internal/syncer/...` (with `SYNC_TEST_HOST` against the Docker Samba).

## Outside `third_party/`

- `samba-4.23.6/` is the Samba source code, downloaded for reference and for the Python API study
  (`docs/samba-api.md`). It is not in git or in the build.
- Reading the Windows ACL in Go (`internal/sambabridge/ntacl.go`) uses neither go-msrpc nor any other library: the
  `xattr_NTACL` v4 and the POSIX ACL that Samba checks have a small, fixed format, parsed by hand with bounds checks
  and fuzzing (`golang.org/x/sys/unix` reads the xattrs). No Go library reads Samba's TDB/LDB or writes the ACL the
  way `acl_xattr` does; writing stays in the Python helper.
- The other dependencies (Echo, go-ldap, asynq, Fyne, Vue, Tailwind etc.) come from the regular registries (Go
  modules and npm), pinned by `go.sum` and `package-lock.json`.
- On the archived Fyne desktop (`archive/desktop-go/`, kept as code only), `github.com/fyne-io/oksvg` and `github.com/srwiley/rasterx` already came with Fyne (Fyne uses them
  to draw SVG) and are now used directly in `archive/desktop-go/client.go`: the company's SVG logo is read and drawn with the
  same parser before it goes to the login. An SVG that Fyne would not draw (invalid attribute, no size, nothing
  visible) falls back to the AD Manager logo instead of leaving the login without a logo.
