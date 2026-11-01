## 1. Server

- [x] 1.1 Settings: `ssh_terminal` switch (off by default) in the settings struct, API, `openapi.yaml`; `auth.ssh`
      in the session info; turning it off cancels every terminal.
- [x] 1.2 Store: an `ssh_host_keys (agent, key, rev)` table (new table: existing databases get it from the schema);
      trust update conditional on the revision; cleared (and audited) when an agent's URL host changes.
- [x] 1.3 `internal/server/ssh.go`: `GET /api/agents/:id/ssh` (session, switch, Origin = Host, atomic slot of 4
      per lower-case user, first message ≤ 64 KiB within 30 s, `cols`/`rows` 1–1000), `x/crypto/ssh` client with a
      20 s deadline over the whole start (password, keyboard-interactive, key + passphrase), PTY, resize, pings,
      TOFU callback (Ed25519 preferred, the pinned type once pinned), fixed error codes, a codec that keeps the frame
      type.
- [x] 1.4 Terminal registry per session (one mutex with the switch state, so admission and switch-off are atomic): logout, deletion, per-terminal expiry timer and switch-off cancel; input
      refreshes the idle time only on a live session; shutdown cancels and waits for `ssh.close`.
- [x] 1.5 `DELETE /api/agents/:id/ssh/hostkey`; audit `ssh.open`, `ssh.close`, `ssh.hostkey.trust`,
      `ssh.hostkey.reset`.
- [x] 1.6 `internal/i18n` entries for the new messages; `UPDATE_I18N=1 go test ./internal/i18n`.
- [x] 1.7 Tests with an in-process SSH server: every scenario of the `ssh-terminal` spec (see the design's Testing
      list).

## 2. Web panel

- [x] 2.1 `@xterm/xterm` and `@xterm/addon-fit`; `pages/Terminal.vue` (Servers list as in Backup without "This
      panel", user, password or key file, fingerprint dialog, Connect/Disconnect, Clear SSH host key, fit on
      resize); route `/terminal` in `main.js`.
- [x] 2.2 `BackupTabs.vue`: Terminal sub-tab when `auth.ssh`; `App.vue`: Backup top tab active on `/terminal`.
- [x] 2.3 Settings screen switch with the bypass warning.
- [x] 2.4 pt-BR strings (`locales/pt-BR/*.js`, `desktop.js`).
- [ ] 2.5 Playwright test (`echo $((6*7))` → `42`); checked by hand in
      the browser against the test Samba's file server for now (the Go tests cover `42` end to end).

## 3. Docs and validation

- [x] 3.1 `docs/` (turning it on, the bypass warning, direct-host agent URL, Nginx/Caddy `Upgrade`, `Host` and idle
      timeout, host key reset), CHANGELOG.
- [x] 3.2 `make check && make test && make build`; `make desktop-check`; `make samba-up && make test-integration`;
      check the sub-tab in the browser against the test Samba with an `sshd`.
- [x] 3.3 Screenshots: `34-terminal` (with the sub-tabs), `35-terminal-hostkey` and `17-settings` retaken (2×, Classic blue, English, Example
      Company Ltd, `domain.local`); `29-backup` and the desktop shots are unchanged (the sub-tab shows only with the
      switch on, off by default).
- [x] 3.4 Review the implementation with Codex (`codex exec --sandbox read-only …`).

## 4. Desktop app

- [x] 4.1 `panel.Panel.Terminal`: the panel's terminal WebSocket in Go (session cookie, panel origin, pinned TLS on
      HTTP/1.1, no redirect, stale generation refused, closed with its session), with tests.
- [x] 4.2 `Terminals` service (`Open`, `Control`, `Input`, `Close`, `Clipboard`) and `adm:terminal` events.
- [x] 4.3 `platform.terminal` in `frontend/src/desktop/index.js` (the WebSocket's shape) and Shift+Insert through
      `platform.readClipboard`; the sub-tab enabled in the desktop.
- [x] 4.4 `make desktop-check`; the terminal checked in the real app (Xvfb) against an HTTPS test panel: connect,
      `42`, 256 colors, Shift+Insert, sign-out closes it; `docs/desktop.md`, `docs/ssh-terminal.md`.
- [ ] 4.5 A desktop screenshot of the terminal (`make desktop-shots`' demo panel has no SSH terminal yet).

