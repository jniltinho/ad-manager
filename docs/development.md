# Development

## Requirements

Go 1.27+, Node 24, Docker (test Samba), Vagrant + VirtualBox (lab). The frontend uses **Vite+** as the project's local
CLI (`vite-plus` in `devDependencies`; `vp dev/build/check/test` via `npm run …`), with no need for a global `vp`.

## Cycle

```bash
make build            # npm ci + vp build → web/dist; dist/ad-manager and dist/desktop/<platform>/ad-manager-desktop (dist/ is outside git)
make test             # go vet + go test + vitest
make check            # gofmt + vp check (oxfmt + oxlint)
make samba-up         # Samba AD 4.23 (Ubuntu 26.04, distro packages) at ldaps://127.0.0.1:6636
make test-integration # tests against the test Samba (skipped if it is down)
scripts/demo-seed.sh  # fictitious data for demos and screenshots
```

Panel with hot reload:

```bash
AD_URL=ldaps://127.0.0.1:6636 AD_DOMAIN=empresa.test AD_INSECURE_SKIP_VERIFY=true AD_LISTEN=127.0.0.1:8080 \
  dist/ad-manager server --insecure-dev --config /dev/null
make dev              # http://localhost:5173 with a proxy for /api
```

Demo login: `diego.rocha` / `Demo-0004#ok` (only on the `empresa.test` test domain).

## API documentation (Swagger)

Turn it on in **Settings → General → API documentation (Swagger)** (stored in SQLite, no restart; there is no key in
`config.toml`). The panel then serves Swagger UI at `/swagger`, with an **API** link in the menu, and the
specification `internal/server/openapi.yaml` (OpenAPI 3, written by hand: update it when creating or changing a
route — `TestOpenAPICoversRoutes` fails if an `/api` route is left out). Like the queues (`/asynqmon`), it only opens
for a signed-in administrator: without a session, it goes to the sign-in page and comes back. **Try it out** uses the
panel session. **Keep it off in production** (the default). Swagger UI comes from the `swagger-ui-dist` npm package
(only the bundle and the CSS, copied at build time to `web/dist/swagger`), without a CDN, because of the panel's CSP.

## Tests

| Type | Where | What |
|---|---|---|
| Unit (ad) | `internal/ad/ad_test.go` | SID/RID, unicodePwd, FILETIME, policy, diff, CSV, password generator, mandatory TLS |
| HTTP | `internal/server/server_test.go` | login, 403 for non-admin, 429, session/expiry, error mapping, audit without password, mandatory HTTPS |
| Integration | `internal/ad/integration_test.go` (`-tags=integration`) | user lifecycle, own password change, protected objects, groups, non-empty OU, policy, admin by SID |
| Frontend | `frontend/src/util.test.js` | password generator |
| Themes | `frontend/tests/unit/themes.test.mjs` | color lint, theme structure, square corners, registry ↔ CSS, contrast matrix (`src/themes/matrix.json`) |
| Visual | `frontend/tests/visual` (Playwright) | pixel-by-pixel regression of the themes; see below |
| Live theme | `frontend/tests/live` (Playwright) | first paint already in the theme, `no-store` document, switch without reload, measured corners |

Visual regression (needs the development panel above and the demo login only to record the API):

```bash
cd frontend
RECORD=1 npx playwright test                                  # records the API in tests/visual/har/<test>.har (versioned; cookies replaced by "redacted")
DIST=<checkout of the base commit>/web/dist BASELINE=1 npx playwright test   # baseline of the base commit (.run/visual/baseline)
npm run build && npx playwright test                          # compares the current build: 0 pixels of difference
LIVE_URL=http://127.0.0.1:8080 npx playwright test -c playwright.live.config.mjs  # live test (current panel)
```

Outside CI (it is slow on the runner): `scripts/visual-ci.sh <base-commit>` runs in the `mcr.microsoft.com/playwright`
container of the `package-lock.json` version, records the baseline with the base commit's build and compares the
current build in the same Chromium (`PW_CHANNEL=chromium`), with no versioned images:

```bash
docker run --rm --user $(id -u):$(id -g) -e HOME=/tmp -v "$PWD":"$PWD" -w "$PWD" --ipc=host mcr.microsoft.com/playwright:v1.56.1-noble bash -c 'git config --global --add safe.directory "*"; scripts/visual-ci.sh HEAD'
```

This replaces `.run/visual/baseline` with the Chromium images; to go back to the local Chrome, re-record with
`BASELINE=1 npx playwright test`.

## Conventions

- Specs and decisions in `openspec/` (OpenSpec, spec-driven schema). A behavior change → a new change.
- Go/Vue skills in `.agents/skills` (outside git), as in the other projects.
- Theme: components only use **roles** (`text-fg`, `bg-surface`, `border-line`, `bg-(image:--bg-action)`,
  `shadow-(--shadow-card)`…) from the contract in `frontend/src/style.css`; a fixed color or palette token
  (`--color-cn-*`) in a component breaks `npm run test`. A theme (`frontend/src/themes/*.css`) only redefines roles and
  parameters under `:root[data-theme='id']`, and is added to the `internal/server/themes.go` registry. No
  `border-radius`. Details in `openspec/specs/settings/spec.md` and `frontend/src/style.css`.
- New theme: copy a similar file from `frontend/src/themes/` (light or dark) and change the values — a dark theme must
  redefine **all** colors, backgrounds and shadows; import it in `style.css`; add it to the registry (`themes.go`,
  English label and pt-BR translation in `locales/pt-BR/common.js`) and to the `theme` `enum` in `openapi.yaml`.
  `npm run test` covers structure, types, contrast matrix and lint; record its reference screens with
  `THEMES=<id> BASELINE=1 npx playwright test` and check the images in `.run/visual/baseline/`.
- Languages: everything is in English — code, comments, docs, CLI output, server and agent messages, specs. pt-BR
  exists only as translation catalogs: UI texts use `t()` with the pt-BR in `frontend/src/locales/pt-BR/`; server and
  agent messages have their pt-BR in the `internal/i18n` catalog (web via `/api/i18n`, desktop and CLI through the
  package); the desktop app's own texts in `desktop/i18n_pt_*.go`. After touching the catalog:
  `UPDATE_I18N=1 go test ./internal/i18n` (updates the JSON used by the frontend tests).
- No secrets in versioned files: `.env`, `inventory/`, `base/`, `.run/` are in `.gitignore`.

## Release

GitHub Actions. `.github/workflows/ci.yml` runs on every push to `main` (and on pull requests): Go, frontend and
desktop app tests on Ubuntu, and the desktop app build on macOS. The tag `vX.Y.Z` → `.github/workflows/release.yml`
runs CI again and, if it passes, builds the SPA, linux/windows amd64 binaries with UPX, the `.deb`, the desktop app for
Linux, Windows and macOS (arm64, amd64 and universal `.app`) and publishes the release with `gh` (notes from the
version's section in `CHANGELOG.md`, `checksums.txt`). Always: push to `main`, green CI, then the tag. The version is
the tag (injected via ldflags into `internal/buildinfo`). Step by step in the `create-release` skill
(`.claude/skills/create-release`).
