## 1. Panel as the single point of access

- [x] 1.1 `page_shares.go`: servers from `GET /api/agents`; shares, sizes (`?refresh=1`, 150 s), tasks and task
      polling through `/api/agents/{id}/…` (`api.via`) with the panel session; a lost session stops the polling.
- [x] 1.2 `page_logs.go`: each registered file server's Samba logs through `/api/agents/{id}/samba/logs[/{name}]`
      (the agent beside the panel is the "this server" tab already).
- [x] 1.3 `config.go`: drop `[[agents]]`/`[[agentes]]`; a legacy config is rewritten in place without them (0600),
      the status line says so (or why it could not).
- [x] 1.4 Panel: the slow proxies extend their write deadline past `WriteTimeout`; an oversized agent answer is an
      error (`internal/server/extras.go`, `proxy_test.go`).

## 2. Configure

- [x] 2.1 `setup.go`: URL, user, password (memory only), language; Test = certificate probe → health → login +
      `/api/me`, one attempt at a time; Save after a successful test of the current values (or language-only from
      the signed-in app); writes the config and opens the shell with the tested session.
- [x] 2.2 `client.go`: certificate pin (`VerifyConnection` on the leaf SHA-256), `probeCert`, no redirects; clear
      errors for a changed certificate and a redirect; legacy `insecure` until a Save after a full test.
- [x] 2.3 pt-BR for the new strings; obsolete agent strings removed.

## 3. Tests, docs, screenshots

- [x] 3.1 Unit tests: certificate pin (match, other pin, redirect), Configure's test (untrusted, wrong password, not
      a Domain Admin, signed in), Shares/Logs through the panel, Configure Save gating, the saved file and the logout
      of an abandoned test session, legacy config rewrite (also through a link); panel proxy (slow sizes past the
      write timeout, oversized answer).
- [x] 3.2 `docs/desktop.md`, `docs/install-guide.md`; CHANGELOG.
- [x] 3.3 `desktop-02-configure.png` and `desktop-08-shares.png` from fixtures (2×, classic-blue, English,
      domain.local, Example Company Ltd).
- [x] 3.4 Codex review of the proposal and of the code (fixes applied: a replaced session's task poll does not
      send the new one to sign-in; abandoned test sessions are logged out, the password cleared on Cancel; a linked
      config is rewritten at its target; the next sign-in reloads the panel's brand; screen updates of background
      work (`onUI`) and the tests' reads (`onScreen`) are ordered by one mutex — race-free under `-race`; pt-BR for
      the oversized-answer message).
