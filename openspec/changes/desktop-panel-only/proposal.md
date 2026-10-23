## Why

The desktop app talks to two kinds of servers. Most pages go to the **panel** (HTTPS :8443, the operator's AD account,
a session cookie, *Domain Admins* only, audited under the operator's name). The **Shares** page and the file servers'
**Samba logs** go **directly to each agent** (HTTPS :9443) with the agent's bearer token, which the operator types in
**Configure** and the app keeps in `desktop.toml`.

That token is root on the file server, and it ends up on every administrator's laptop. Calls made with it bypass the
operator's identity: the agent sees a token, not a person, and the panel's audit never sees them. Every file server's
:9443 must also be reachable from every administrator's workstation. Meanwhile the panel already proxies every one of
those calls (`/api/agents/{id}/shares`, `…/sizes`, `…/tasks`, `…/tasks/{tid}`, `…/samba/logs`, `…/samba/logs/{name}`),
with the session's checks and the audit of tasks; the desktop's Sync and Backup pages already use them.

Configure also asks for the panel URL and tests only `/api/health`: a wrong password, or an account that is not a
Domain Admin, shows up only later on the sign-in screen. A self-signed panel certificate is accepted with
`insecure = true`, which turns certificate verification off for that panel entirely.

## What Changes

- **The panel is the desktop's single point of access.** The Shares page and the file servers' Samba logs list the
  servers from the panel (`GET /api/agents`) and call the panel's proxies under `/api/agents/{id}/…` with the
  session, as the Sync page does. The desktop no longer talks to an agent, holds an agent token or needs any port
  but the panel's. The panel → agent leg (token kept on the panel server only) is unchanged.
- **No agent tokens in the desktop config.** `desktop.toml` loses `[[agents]]` (and the older `[[agentes]]`). A
  config that still has it is read without the section and rewritten in place at once without it (0600, temp file
  and rename, keeping the panel, user, language and certificate settings), so the tokens do not stay on disk; the app
  says so once in the status line. If the file cannot be rewritten, the original is left intact and the status line
  says the tokens are still in it, with the reason; nothing is written elsewhere.
- **The panel keeps slow and large answers whole.** The folder-size proxy (`…/sizes`, du can take up to 2 minutes)
  extends its own write deadline past the server's 60-second `WriteTimeout`, so the answer is not cut, and an agent
  answer larger than the 4 MiB the panel passes on is an error instead of a truncated JSON. (This fixes the web
  panel's Shares too.)
- **No redirects.** The desktop never follows an HTTP redirect from the panel (a 307/308 could replay the password
  or the session to another origin or over plain HTTP, where no certificate check runs).
- **Configure validates the whole sign-in and writes the config itself.** Fields: panel URL, user, password (never
  stored) and language. **Test** reaches `/api/health`, then signs in (`POST /api/login`, `GET /api/me`) and shows the
  domain, the user and the panel version, or the panel's own message (wrong password, not a Domain Admin, panel in
  setup mode, …). Each test is one attempt: an edit, or a newer test, discards an older result still on its way.
  **Save** is enabled after a successful test of the current URL, user and password; it writes `desktop.toml` (URL,
  user, language, certificate pin; 0600) and opens the app already signed in with that session (its own cookie jar;
  the password is not kept). Opened from the signed-in app with the same URL and user and no password typed, Save
  changes only the language and keeps the session and the certificate settings.
- **Self-signed certificates are pinned, not trusted blindly.** Test first reads the panel's certificate over a TLS
  connection that carries no credential or request. If the system trusts it for that host name, nothing is pinned.
  If not, Test shows its SHA-256 fingerprint (of the leaf DER), subject, issuer and validity for the operator to
  compare out of band, and trusts it only when the operator says so; then it signs in over a fresh connection that
  accepts only that certificate (`VerifyConnection`, so resumed sessions are checked too). Save stores
  `fingerprint = "sha256:…"` under `[panel]`; every connection then accepts that exact leaf certificate (host name and
  dates are not checked: the pin is the trust) and nothing else, so a renewed certificate must be accepted again in
  Configure; a different certificate fails before any request with a message pointing to Configure. A legacy
  `insecure = true` keeps working until a Save after a full test, which replaces it with the pin of the certificate
  the operator accepted; a language-only Save leaves it as it is.

## Capabilities

### Modified Capabilities

- `app-desktop`: desktop reaches only the panel; Configure tests the sign-in, pins a self-signed certificate and
  writes the config without secrets.

## Impact

`desktop/` (`config.go`, `client.go`, `setup.go`, `main.go`, `page_shares.go`, `page_logs.go`, their tests and the
pt-BR catalog), `internal/server/extras.go` (write deadline of the slow proxies, oversized agent answers) and a
proxy test, `docs/desktop.md`, `docs/install-guide.md` (desktop configuration), `docs/screenshots/`
(`desktop-02-configure.png`, `desktop-08-shares.png`, from fixtures). No route or contract change: `openapi.yaml`
unchanged. A share task still polls through the panel; a lost session (signed out, panel restarted) stops the
polling and asks to sign in again. Out of scope: removing the agents' own token (the panel still needs it); a panel
without the store answers `/api/agents` with an error, which Shares shows (it is not an empty list).
