## Why

The Vue frontend for asynqmon (the queue monitor ad-manager embeds) added two themes, Grafana and RabbitMQ, built
on ad-manager's theme contract. The panel and the desktop app should offer the same themes, so the monitor and the
panel can match.

## What Changes

- Two themes join the registry (`internal/server/themes.go`), the web CSS (`frontend/src/themes/grafana.css`,
  `rabbitmq.css`) and the desktop's generated `themes.json`: **Grafana (dark)**, the Grafana 11 dashboard as it is
  (flat panels, no gradients or shadows), and **RabbitMQ** (light), the RabbitMQ management UI look.
- Both follow the panel's standard pattern: folder tabs with a 3 px accent cap, a header gradient band, page dots,
  gradient buttons and drop shadows; RabbitMQ's page is a step darker than its header.
- A 1 px outline in the theme's `--color-line` marks the app box in every theme, kept in the flat look (web: an
  `outline` on the app box; desktop: a stroke around it).
- The queue monitor moves from `github.com/hibiken/asynqmon` to `github.com/jniltinho/asynqmon` (Vue frontend with
  the panel's themes); its `Options.Theme` gets the panel's theme and flat look, so `/asynqmon` opens in them.
- Both pass the contrast matrix (AA); the pt-BR catalogs label Grafana (dark); the docs list 20 themes.

## Impact

- `internal/server/themes.go`, `frontend/src/style.css`, the two theme files, `desktop/crates/core/assets/themes.json`,
  a desktop test count, the pt-BR catalogs, docs and the theme mosaics.
