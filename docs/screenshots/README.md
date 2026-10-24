# Screenshots

The web panel (1600 × 900) and the desktop app (1280 × 800), captured at 2× (3200 × 1800 and 2560 × 1600), on a
demo environment: the all-in-one image
([docker.md](../docker.md)) with the fictitious domain **`domain.local`**, seeded by `scripts/demo-seed.sh`
(18 users, 8 groups, 4 OUs, 8 computers, 6 shares, backups and a sync job). **Classic blue (XP)** theme, company
"Example Company Ltd". How to retake them is at the end of this page.

## Web panel

### Sign-in and overview

| Sign in | Home |
|---|---|
| ![Sign in](01-login.png) | ![Home](02-home.png) |

### Users

| List (clickable status, one column per action) | Reset password |
|---|---|
| ![Users](03-users.png) | ![Reset password](04-reset-password.png) |

| New user | Delete (type the login to confirm) |
|---|---|
| ![New user](05-new-user.png) | ![Confirm delete](06-confirm-delete.png) |

### Self-service password link

| Send a link (validity) | Link generated |
|---|---|
| ![Validity](21-password-link-validity.png) | ![Link generated](22-password-link-generated.png) |

| The link's page (the user picks the password) |  |
|---|---|
| ![Link page](25-password-link-page.png) |  |

### Groups, OUs and computers

| Groups | Group members |
|---|---|
| ![Groups](07-groups.png) | ![Members](08-group-members.png) |

| OUs | GPOs (links and backup) |
|---|---|
| ![OUs](09-ous.png) | ![GPOs](26-gpos.png) |

| Computers | Domain DNS |
|---|---|
| ![Computers](10-computers.png) | ![DNS](27-dns.png) |

| A computer's SMB sessions |  |
|---|---|
| ![Sessions](28-smb-sessions.png) |  |

### Shares

| Per file server (size and full control) | Edit (Windows permissions and history) |
|---|---|
| ![Shares](11-shares.png) | ![Edit](12-edit-share.png) |

### Backup and sync

| Backup (DC, file server and panel) | Sync (jobs and runs) |
|---|---|
| ![Backup](29-backup.png) | ![Sync](30-sync.png) |

| Wizard — source and connection test | Wizard — what to copy |
|---|---|
| ![Source](31-sync-wizard.png) | ![What to copy](32-sync-wizard-what-to-copy.png) |

| Run log (a long log scrolls inside the modal) |  |
|---|---|
| ![Run log](33-sync-log.png) |  |

### Policy, import and logs

| Password policy (Users → Password policy) | Import/export |
|---|---|
| ![Policy](13-password-policy.png) | ![Import/export](14-import-export.png) |

| Logs — audit | Logs — a server's Samba log |
|---|---|
| ![Audit](15-logs-audit.png) | ![Samba](16-logs-samba.png) |

| Logs — the panel's HTTP log |  |
|---|---|
| ![HTTP](23-logs-http.png) |  |

### Settings, language, API and queues

| Settings (options, theme, company, agent token, `config.toml`) | Portuguese UI |
|---|---|
| ![Settings](17-settings.png) | ![Português](18-portuguese-users.png) |

| Swagger (`/swagger`, switched on in Settings) | Queues (asynqmon, administrators only) |
|---|---|
| ![Swagger](24-swagger.png) | ![asynqmon](19-asynqmon.png) |

| QHD (2560 × 1440, automatic zoom) |  |
|---|---|
| ![QHD](20-qhd.png) |  |

### Browser-based setup

The wizard that prepares a new server (Samba, DC or file server) before a domain exists. Fictitious data (the
panel has no domain yet): `frontend/tests/visual/setup-shots.mjs`.

| 1. Access (code from the terminal) | 2. This server |
|---|---|
| ![Access](setup-1-access.png) | ![Server](setup-2-server.png) |

| 3. Role | 4. Details (with errors) |
|---|---|
| ![Role](setup-3-role.png) | ![Errors](setup-4-details-errors.png) |

| 4. Details | 5. Review |
|---|---|
| ![Details](setup-5-details.png) | ![Review](setup-6-review.png) |

| 6. Installing | Done |
|---|---|
| ![Installing](setup-7-installing.png) | ![Done](setup-8-done.png) |

### Themes

![Themes](themes.jpg)

## Desktop app

| Sign in | Configure (first run) |
|---|---|
| ![Sign in](desktop-01-login.png) | ![Configure](desktop-02-configure.png) |

| Home | Users |
|---|---|
| ![Home](desktop-03-home.png) | ![Users](desktop-04-users.png) |

| New user | Groups and members |
|---|---|
| ![New user](desktop-05-new-user.png) | ![Groups](desktop-06-groups.png) |

| OUs | Computers |
|---|---|
| ![OUs](desktop-12-ous.png) | ![Computers](desktop-07-computers.png) |

| Shares | Password policy |
|---|---|
| ![Shares](desktop-08-shares.png) | ![Policy](desktop-13-password-policy.png) |

| Backup | Sync |
|---|---|
| ![Backup](desktop-14-backup.png) | ![Sync](desktop-15-sync.png) |

| Logs | Send a password link |
|---|---|
| ![Logs](desktop-09-logs.png) | ![Link](desktop-10-password-link.png) |

| Link generated |  |
|---|---|
| ![Link generated](desktop-11-link-generated.png) |  |

## Desktop app in Rust (egui)

The same screens of [`desktop-rust/`](../../desktop-rust/README.md), 1280 × 800 at 2× (2560 × 1600), Classic blue in
its flat look.

| Sign in | Configure |
|---|---|
| ![Sign in](desktop-rs-01-login.png) | ![Configure](desktop-rs-02-configure.png) |

| Home | Users |
|---|---|
| ![Home](desktop-rs-03-home.png) | ![Users](desktop-rs-04-users.png) |

| New user | Groups and members |
|---|---|
| ![New user](desktop-rs-05-new-user.png) | ![Groups](desktop-rs-06-groups.png) |

| OUs | Computers |
|---|---|
| ![OUs](desktop-rs-12-ous.png) | ![Computers](desktop-rs-07-computers.png) |

| Shares | Password policy |
|---|---|
| ![Shares](desktop-rs-08-shares.png) | ![Policy](desktop-rs-13-password-policy.png) |

| Backup | Sync |
|---|---|
| ![Backup](desktop-rs-14-backup.png) | ![Sync](desktop-rs-15-sync.png) |

| Logs | Send a password link |
|---|---|
| ![Logs](desktop-rs-09-logs.png) | ![Link](desktop-rs-10-password-link.png) |

| Link generated | DNS records |
|---|---|
| ![Link generated](desktop-rs-11-link-generated.png) | ![DNS](desktop-rs-16-dns.png) |

| SMB sessions of a computer | Sessions and open files of a file server |
|---|---|
| ![SMB sessions](desktop-rs-17-smb-sessions.png) | ![Share sessions](desktop-rs-18-share-sessions.png) |

| Samba log | HTTP log |
|---|---|
| ![Samba log](desktop-rs-19-logs-samba.png) | ![HTTP log](desktop-rs-20-logs-http.png) |

| Edit share | Restore a backup |
|---|---|
| ![Edit share](desktop-rs-21-edit-share.png) | ![Restore](desktop-rs-22-restore.png) |

| Sync wizard | Settings |
|---|---|
| ![Sync wizard](desktop-rs-23-sync-wizard.png) | ![Settings](desktop-rs-24-settings.png) |

| Backups of a file server | Import/export |
|---|---|
| ![Backup agent](desktop-rs-25-backup-agent.png) | ![Import/export](desktop-rs-26-import-export.png) |

The Rust app's are retaken with `make -C desktop-rust shots` (a mock panel, no domain needed).

## How to retake them

Everything runs locally, with passwords only in environment variables.

1. Start the all-in-one image with the fictitious domain (LDAPS published on `127.0.0.1` only, for the seed):

   ```bash
   read -rs -p 'Administrator password: ' AD_ADMIN_PASSWORD; export AD_ADMIN_PASSWORD; echo
   ```

   ```bash
   docker run -d --name adm-docs --hostname dc1 --cap-add SYS_ADMIN -p 127.0.0.1:18443:8443 -p 127.0.0.1:16636:636 -e AD_DOMAIN=domain.local -e AD_ADMIN_PASSWORD -e AD_PUBLIC_PORT=18443 jniltinho/ad-manager
   ```

2. Seed it (users, groups, OUs, computers and shares):

   ```bash
   AD_TEST_PASSWORD="$AD_ADMIN_PASSWORD" AD_DOMAIN=domain.local AD_URL=ldaps://127.0.0.1:16636 CONTAINER=adm-docs PANEL_URL=https://127.0.0.1:18443 scripts/demo-seed.sh
   ```

   In the panel's Settings: theme **Classic blue (XP)**, company "Example Company Ltd". Run a backup and create a sync job
   (inside the container the machine account gets no Kerberos ticket: use **User and password** with a test
   account).

3. Web panel (Playwright's Chrome):

   ```bash
   (cd frontend && DOCS_PASS="$AD_ADMIN_PASSWORD" DOCS_SYNC_PASS='<test account password>' node tests/visual/docs-shots.mjs ../docs/screenshots)
   ```

4. Setup (fictitious data, after `npm run build`):

   ```bash
   (cd frontend && node tests/visual/setup-shots.mjs /tmp/setup classic-blue)
   ```

   and copy `setup-classic-blue-N-*.png` as `setup-N-*.png`.

   The Sync page (`30-sync.png`) and its run-log modal (`33-sync-log.png`) are captured the same way, from fixtures
   (a running run and finished ones with their end and duration; a finished preview run with a very long dry-run log;
   no agent is called), after `npm run build`:

   ```bash
   (cd frontend && node tests/visual/sync-log-shot.mjs ../docs/screenshots classic-blue)
   ```

   and rename `sync-classic-blue-page.png` to `30-sync.png` and `sync-classic-blue-log.png` to `33-sync-log.png`
   (`docs-shots.mjs` also takes a `30-sync`, from the container's real runs: keep the fixture one).

5. Desktop app (signed in to the container's panel; the file servers through it):

   ```bash
   (cd desktop && DOCS_URL=https://127.0.0.1:18443 DOCS_PASS="$AD_ADMIN_PASSWORD" DOCS_VERSION=$(git describe --tags) DOCS_SHOTS_DIR=../docs/screenshots go test -tags docsshots -run TestDocsShots -count=1 .)
   ```

   Configure (`desktop-02-configure.png`, a self-signed panel's certificate to trust), Shares
   (`desktop-08-shares.png`) and Sync (`desktop-15-sync.png`) come from fixtures instead (a fake panel, no agent),
   after the run above, which also writes them:

   ```bash
   (cd desktop && DOCS_SHOTS_DIR=../docs/screenshots DOCS_VERSION=$(git describe --tags --abbrev=0) go test -tags docsshots -run TestDocsFixtureShots -count=1 .)
   ```

6. Desktop app in Rust: the real app driven by its UI tests against the in-process mock panel (fixtures of
   `domain.local`, Example Company Ltd, answering as `https://panel.domain.local:8443`), rendered with lavapipe in
   the toolchain image — no panel or agent needed:

   ```bash
   (cd desktop-rust && make shots)
   ```

7. Theme mosaics (`themes.jpg` and `docs/img/conformity/`): `BASELINE=1 npx playwright test` in `frontend/`,
   `SCREEN_SHOTS=../.run/desk-themes go test -run TestScreensInEveryTheme` in `desktop/`, then
   `python3 scripts/theme-mosaics.py`.
