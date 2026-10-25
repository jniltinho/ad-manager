## Why

The Classic blue (XP) theme read almost flat: a horizontal blue header, plain buttons and headers, so it looked little
like Windows XP. It was also held only to AA contrast.

## What Changes

- Only the color, gradient and shadow values of `:root[data-theme='classic-blue']` in
  `frontend/src/themes/classics.css` change; no layout, size, spacing or radius changes.
- Luna title bar and dialog head: vertical gradients with a thin light top band. Taskbar-like footer. Start-green
  primary buttons with a glossy top. XP push buttons (white → #d6d0c5, border #003c74). Explorer task-pane card heads
  (white → light blue, left to right). Listview column headers with the three bottom lines. Tabs bordered #91a7b4 with
  the orange/yellow top of a selected XP tab. Light-blue side-list selection with an orange bar.
- The bright highlights sit only in thin bands with no text, and the text roles get darker values, so `classic-blue`
  joins the AAA themes in `matrix.json` (normal text 7:1, large 4.5:1, components 3:1).
- The flat look keeps the page's standard dots in every theme (it cleared them before); a theme's own pattern
  (Matrix's rain, Winamp's steel lines) gives way to the dots in the flat look, on the web and on the desktop.
- `make desktop-themes` regenerates `desktop/crates/core/assets/themes.json`, so the desktop app draws the same.

## Impact

- `frontend/src/themes/classics.css`, `frontend/src/themes/matrix.json`, `desktop/crates/core/assets/themes.json`,
  a desktop test value, `docs/themes.md`, the docs screenshots (all in Classic blue).
