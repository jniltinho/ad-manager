## Why

In the desktop app the queue monitor link opens `{panel}/asynqmon` in the system browser, which has no session to
the panel, so it never works there. The links at the right of the tab bar (API, Queues and Configure) also changed
their background on hover, which looked like a stray tab in themes with tab shapes.

## What Changes

- **Queues is disabled in the desktop app**: shown muted, not clickable, with "The queue monitor is not available in
  the desktop app" on hover. The web panel keeps the link.
- **The tab bar's links change only their text color on hover** (`--color-on-tab-aux-hover`); the hover background
  token `--color-tab-aux-hover` goes away. Every theme's hover color differs from its normal one and passes the
  contrast matrix on the tab bar.
- Configure sits at the right of the tab bar, after Queues (already in the `app-desktop-wails` spec).

## Capabilities

### Modified Capabilities

- `app-desktop-wails`: the queue monitor scenario.

## Impact

`frontend/src/App.vue`, the theme files, `frontend/src/themes/matrix.json`, the pt-BR desktop catalog,
`docs/desktop.md`, CHANGELOG and the desktop screenshots.
