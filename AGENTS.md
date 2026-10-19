# AGENTS.md — instructions for agents

Go (Cobra + Echo v5 + go-ldap) with an embedded Vue 3 SPA (Vite+, Tailwind 4). Manages an Active Directory (Windows
Server or Samba 4) and supports migrating a Windows DC to Samba 4.23. Read `docs/architecture.md`.

## Rules

1. **Never write to a production AD** without an explicit request. Reading (preflight, inventory, export) is
   allowed. Test against the Docker Samba (`make samba-up`, `empresa.test`), the all-in-one image
   (`docs/docker.md`) or the isolated labs (`vagrant/labs`).
2. Secrets only through the environment (`.env` outside git). Never write passwords to versioned files, logs or audit.
3. Every protection rule lives in `internal/ad` (server), not only in the UI.
4. Behavior change → OpenSpec change (`openspec/`), specs with testable scenarios.
5. Release: `create-release` skill (`.claude/skills/create-release`, GitHub with `gh`). Community Go skills
   (`golang-*`), if installed, live in `.agents/skills/` (outside git).
6. **Everything in English**: code, identifiers, comments and doc comments (godoc: `// Package x …`, an exported
   identifier's comment starts with its name), API and SPA routes, docs, CLI output, server and agent messages (API
   errors, logs, audit), OpenSpec specs and commit messages.
7. Portuguese exists only as translation catalogs:
   - web panel: UI source strings are English through `t('…')` (`frontend/src/i18n.js`), with the pt-BR
     translation in `frontend/src/locales/pt-BR/*.js`;
   - server: the `internal/i18n` catalog (served at `/api/i18n` for the web panel and used by the desktop and the
     CLI) — a new server message gets an entry there and `UPDATE_I18N=1 go test ./internal/i18n`;
   - desktop: `desktop/i18n_pt_*.go`.
8. **Keep Swagger up to date**: every new or changed API endpoint (`/api/…`: route, method, parameters, body or
   response) updates `internal/server/openapi.yaml` in the same commit. `TestOpenAPICoversRoutes`
   (`internal/server/swagger_test.go`) fails if a route exists without documentation or vice versa; also check it at
   `/swagger` (enabled in the Settings screen).

## Validate

```bash
make check && make test
make samba-up && make test-integration
```

After every change run `make build`: it updates `dist/` (server and `dist/desktop/<platform>/ad-manager-desktop`
for Linux and Windows; macOS only on a Mac or with `MACOS_SDK`).

UI: run the panel against the test Samba (`docs/development.md`) and check it in the browser.
Also validate big changes with Codex (`codex exec --sandbox read-only …`).
