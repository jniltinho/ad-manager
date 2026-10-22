## Why

The 14 themes cover corporate, desktop-OS and dashboard looks, but nothing playful for operators who want some
personality, and no theme whose only job is maximum legibility. Operators with low vision (or a bright room, a
projector, a worn LCD) need a WCAG AAA high-contrast look; the Crystal themes reach AAA but keep soft grays and
blues by design.

## What Changes

- Four new themes in the theme registry (`internal/server/themes.go`), each a complete set of role tokens in its own
  file under `frontend/src/themes/`, square corners, and the flat look like every theme:
  - **Matrix** (`matrix`, dark): green phosphor on a black-green terminal; monospace type; a character-cell dot grid
    with faint scanlines on the page; a subtle green glow on the window, focus and dialogs.
  - **Winamp** (`winamp`, dark): the Winamp 2 skin — brushed dark-steel chrome with a raised bevel, the LCD readout
    (green on black) in fields and the current tab, amber actions, gold title-bar stripe, and the playlist's blue
    selected row.
  - **Pink Panther** (`pink-panther`, light): blush page, white cards, plum-ink text, the deep plum title bar of the
    1963 film titles with the panther pink as a stripe; deep rose actions. Pink only as an accent.
  - **High contrast** (`high-contrast`, dark): pure black and white with a single yellow accent (actions, links,
    selection, focus), no gradients, no shadows, no muted grays, 3 px focus ring, held to WCAG AAA (7:1 text).
- The desktop app picks up the four from the same files (generated `desktop/themes_gen.go`), with the High contrast
  theme held to 7:1 in every color the desktop draws, like Crystal.
- Settings → Theme lists them (the labels are translated to pt-BR in the SPA catalog), the `theme` enum in
  `openapi.yaml` gains the four ids, and the docs (`docs/themes.md`, overview, README counts) and the theme mosaics
  show 18 themes.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `settings`: four new themes in the registry, each with its look and contrast requirements.
- `app-desktop`: the desktop renders the four themes from the panel's choice, High contrast at AAA.

## Impact

- `frontend/src/themes/{matrix,winamp,pink-panther,high-contrast}.css` (new), `frontend/src/style.css` (imports),
  `frontend/src/themes/matrix.json` (the contrast matrix: `high-contrast` in `aaa`), `frontend/src/locales/pt-BR`.
- `internal/server/themes.go`, `internal/server/openapi.yaml` (theme enum), `desktop/themes_gen.go` (regenerated),
  `desktop/theme_test.go`.
- Docs and screenshots: `docs/themes.md`, `docs/overview.md`, `README.md`, `docs/screenshots/themes.jpg`,
  `docs/img/conformity/web-desktop-*.jpg`, `scripts/theme-mosaics.py`.
- No API shape, setting or stored data changes: a stored theme id keeps working; an unknown id still falls back to
  Default.
