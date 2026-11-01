## Context

The panel (Echo v5) proxies the agents of registered servers (`/api/agents/:id/...`, `internal/server/extras.go`)
with a per-agent token. Operators are members of the configured administrative group (`ad.admin_group`, Domain
Admins by default), checked at login; sessions are an in-memory map with a `SameSite=Strict` cookie and lazy idle
expiry; the operator's AD password lives only in process memory. Backup and Sync are two pages sharing the Backup
top tab through `components/BackupTabs.vue`. The desktop app (Wails) runs the same SPA and forwards API calls
through a Go asset middleware that strips `Upgrade`/`Origin`.

## Decisions

### SSH client in the panel

`golang.org/x/crypto/ssh` dials `net.JoinHostPort(agentURL.Hostname(), "22")` (IPv6-safe). One 20 s deadline covers
TCP connect, handshake, authentication, PTY and shell start (`ClientConfig.Timeout` alone bounds only the TCP
connect). Auth methods: password, keyboard-interactive answered with the same password, or a PEM/OpenSSH private key
with optional passphrase. The credential fields are cleared from the request struct after the handshake (references
released; Go and JS memory wiping is not promised). No agent code changes.

### WebSocket transport

`GET /api/agents/:id/ssh` upgrades with `golang.org/x/net/websocket` (already indirect) wrapped as an Echo handler.
Before the upgrade, in order: valid session; switch on; agent exists; `Origin` present, parseable, not `null`, and
its `host[:port]` equal to the request's `Host` (the reverse proxy must pass `Host` through; documented); a terminal
slot reserved atomically for the operator. The library's own Origin check (URL syntax only) is replaced by this one.

Frames are read with a custom `websocket.Codec` whose `Unmarshal` keeps the frame's `payloadType` (the stock
`websocket.Message` drops it), with `MaxPayloadBytes = 64 KiB`. Only text frames are control messages; a binary
frame is terminal input even if its bytes are valid JSON:

- client → server, first message only, text JSON within 30 s:
  `{"user","password"?,"key"?,"passphrase"?,"cols","rows","trust"?}`; `trust` is the fingerprint the operator
  confirmed. Malformed JSON, missing user, or `cols`/`rows` outside 1–1000 → error and close.
- server → client control, text JSON: `{"type":"hostkey","fingerprint","key_type"}` (unknown key: connection closed, the SPA
  asks and reconnects with `trust`), `{"type":"error","code"}`, `{"type":"ready"}`.
- after `ready`: binary frames are terminal bytes both ways; text JSON `{"type":"resize","cols","rows"}` changes
  the PTY window; the server pings every 30 s (keeps reverse proxies from dropping an idle terminal).

### Fixed error codes

Errors map to a closed set: `auth_failed`, `hostkey_changed`, `unreachable`, `timeout`, `limit`, `closed`,
`internal`. The SPA translates the code; audit and logs get the same code. The SSH library's error text (which can
carry server-controlled disconnect text) is never written anywhere.

### Host key pinning (TOFU)

An `ssh_host_keys (agent, key, rev)` table: a new table, so the schema's `CREATE TABLE IF NOT EXISTS` also adds it
to existing databases (no `ALTER TABLE`); deleting an agent deletes its row. `rev` is read with the key before the
handshake and bumped by every reset and URL host change. `HostKeyCallback`:

- empty and no `trust`, or `trust` ≠ the key's fingerprint → fail with the fingerprint and key type;
- empty and `trust` matches → `INSERT … ON CONFLICT(agent) DO UPDATE … WHERE key = '' AND rev = ?`; zero rows
  (another connection trusted first, or a reset came meanwhile) → fail closed and re-read (the same key saved by the
  other connection goes on); `ssh.hostkey.trust`;
- saved and equal → go on; saved and different → `hostkey_changed`, nothing saved, no credential sent;
- any database error → fail closed.

Host key algorithms: with nothing pinned Ed25519 is preferred (then ECDSA, then RSA), the key operators check with
`ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`; once pinned only the pinned key's type is asked for, so a key of
another type the server gains later is not reported as changed.

`DELETE /api/agents/:id/ssh/hostkey` clears it (`ssh.hostkey.reset`). Saving an agent with another URL host clears
it right after the save, audited as `ssh.hostkey.reset` with target `server (url changed)`. Cross-origin `DELETE`
needs a CORS preflight the panel never allows, so no extra CSRF token.

### Session binding

A terminal registry keyed by session id holds each terminal's cancel function. Logout, session deletion, and an
expiry timer per terminal (it does not rely on lazy expiry) cancel them. An input frame refreshes the session's
idle time only after checking, under the session lock, that the session still exists and is not expired (never
revives it); output, pings and resize do not refresh it. Logout racing with an upgrade or a handshake: the slot is
registered before the SSH dial and the handler re-checks the session before sending `ready`.

Admission is atomic with switch-off: one registry mutex guards the switch state and the slots, a slot is taken
only while the switch is on, and switching off sets the state and cancels every slot under that mutex. Before
`ready` the handler re-checks, under the same mutex, that its slot was not cancelled and the session is live.

Limit: 4 terminals per operator across all their sessions, keyed by the lower-case user; slots include handshakes
in progress and are released on every exit path. 

### Shutdown

`http.Server.Shutdown` does not close hijacked connections, so the server's shutdown path cancels the registry and
waits (up to 5 s) for the terminals' `ssh.close` lines before returning.

### Audit

`ssh.open` after the handshake (target `server:user`, the error code on failure), `ssh.close` with the duration
(`server:user 12m3s`). Nothing from the stream, and no credential, is logged. The terminal is an escape hatch
outside `internal/ad`: commands run there skip the panel's protection rules and its per-operation audit; the
Settings switch and `docs/` say so, and the host's own `auth.log` and shell history are the record.

### Front end

`components/BackupTabs.vue` gains `['/terminal', 'Terminal', SquareTerminal]` when `auth.ssh` is true; `App.vue`
keeps the Backup top tab active on `/terminal`. `pages/Terminal.vue`: the Servers list on the left (agents only, as
in Backup), and on the right user, password or key file (read with `FileReader`, never stored), Connect/Disconnect,
"Clear SSH host key" for the selected server, the fingerprint confirmation dialog, and the xterm.js view with
`@xterm/addon-fit`. The terminal is `xterm-256color` with the classic xterm palette (black background, the 16 ANSI colors, xterm.js
deriving the 256-color cube), the same in every theme: the colors live in `src/themes/xterm.js`, the one place
the color lint allows literals; a monospace font; the frame follows the theme. In the desktop
app the sub-tab is disabled and the page, reached directly at `/terminal`, shows the message and opens no WebSocket.

## Risks

- The panel becomes a path to SSH on every registered server: off by default, administrative group only, Origin
  check, audit, TOFU. A compromised panel already holds Domain Admin passwords; what it gains is SSH credentials and
  terminal contents typed through it.
- Registration does not prove the URL is an agent: any administrator can register a host and SSH to it. Accepted:
  the same administrator can already point the panel at any HTTPS host.
- WebSocket through a reverse proxy needs `Upgrade` and `Host` passed and an idle timeout above 30 s: documented for
  Nginx and Caddy.

## Testing

Go tests with an in-process `x/crypto/ssh` server: password, keyboard-interactive and key auth (with passphrase);
wrong password; TOFU first use, wrong `trust`, concurrent trust, reset/edit racing a trust (including trust → reset
→ stale trust); switch-off racing an admission; binary input that is valid resize JSON; key change refused with
no auth attempt; Origin missing/`null`/other host refused; switch off → 404 and open terminals closed; 5th
terminal → 429 and slot release on every exit; silent client; oversized, malformed and bad-size first messages;
SSH server that stalls after TCP connect; idle expiry with no HTTP traffic; output-only traffic does not keep the
session; logout closes the terminal; shutdown writes `ssh.close`; a server whose disconnect message echoes the
password, and no credential (password, key, passphrase) in audit, logs or database; existing database upgraded and
reopened twice. A Playwright test types `echo $((6*7))` and sees `42`, and checks `/terminal` in desktop mode opens
no WebSocket.
