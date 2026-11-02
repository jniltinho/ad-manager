## 1. Server and agent

- [x] 1.1 `internal/config`: the panel allowlist and `SetKeys(text, keys)` — a line editor that keeps comments and
      other keys, adding a missing key under its table; tests.
- [x] 1.2 `internal/ad`: `CheckAdmin(cfg, login, password)` — connect, bind and administrators group.
- [x] 1.3 `internal/agent`: `panel:config` validation (panel unit, not supervisord, allowlist, config hash), handler
      (hash again, edit, parse with `config.Load`, `.bak`, marker, atomic write, restart, panel health poll,
      rollback, `config.apply` audit), marker recovery at start, no retries; tests with a fake restarter.
- [x] 1.4 `internal/server/settings_config.go`: `GET /api/settings/config` (values, sources, env locks, local
      agent, last task), `PUT /api/settings/config` (current admin, lockout check, config hash, task to the local
      agent, audit); the generic task proxy refuses `panel:config`.
- [x] 1.5 `openapi.yaml`, `internal/i18n` (`UPDATE_I18N=1 go test ./internal/i18n`), Go tests for the scenarios.

## 2. Web panel

- [x] 2.1 `Settings.vue` in sub-tabs (`?tab=`), General/Company/Agents from the current cards.
- [x] 2.2 Directory tab: config form, env-locked fields, last result, "Apply and restart" with a confirmation
      that sessions end, read-only state without a local agent.
- [x] 2.3 pt-BR strings.

## 3. Docs and validation

- [x] 3.1 `docs/deployment.md`, `docs/agent.md` (the task, rollback, recovery by `config.toml.bak`), CHANGELOG.
- [x] 3.2 `make check && make test && make build`; check in the browser against the test Samba.
- [x] 3.3 Screenshots: `17-settings` and `36-settings-directory` (2×, Classic blue, English, Example Company Ltd,
      `domain.local`).
- [x] 3.4 Review with Codex.
