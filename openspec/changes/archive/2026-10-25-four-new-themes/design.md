## Context

A theme is only role values and recipe parameters under `:root[data-theme='id']` (theme-base contract in
`frontend/src/style.css`); the component CSS never changes per theme. The unit tests enforce the structure (only
known roles, typed values, no corners), the registry ↔ CSS link, completeness of dark themes and the contrast matrix
(`frontend/src/themes/matrix.json`: 4.5:1 text, 3:1 UI; 7:1 / 4.5:1 for the themes listed in `aaa`). The desktop
palettes are generated from the same CSS and registry (`desktop/internal/themegen` → `themes_gen.go`), so parity is
by construction: the desktop draws the same roles (header, sidebar from the tabs, card titles, table head, buttons,
dialogs, focus ring, login box and its bar, bevel, flat look) and its own contrast test (`TestFynePairsContrast`)
checks the drawn pairs at the theme's minimum (7:1 on AAA themes).

`matrix.json` is the contrast matrix, not a Matrix theme: the Matrix theme is a new `matrix.css`.

## Goals / Non-Goals

**Goals:** four distinctive, complete themes (every color, background and shadow role set, light and dark alike);
AA everywhere, AAA for High contrast; web and desktop identical within the documented accepted differences; no
change to any existing theme.

**Non-Goals:** new component CSS or theme-specific markup; embedded fonts (Matrix uses the system monospace stack,
which the desktop does not mirror — the accepted "system font × Fyne font" difference); animated effects (no digital
rain animation: the page pattern is static, and the flat look removes it).

## Decisions

1. **One file per theme** (`matrix.css`, `winamp.css`, `pink-panther.css`, `high-contrast.css`), imported in
   `style.css` like the others; every theme defines the full role set even when light (Pink Panther), so no Default
   value leaks into it. Stripe widths stay the recipes' own (header mark 2 px, login bar 4 px): only colors and the
   existing parameters change.
2. **Matrix** — page `#010502` with the rain: one short vertical phosphor stroke per 22 px cell
   (`--bg-page: radial-gradient(1px 4px at 50% 50%, var(--color-page-dots) 100%, transparent 100%)`, a single
   layer, because the login page writes `background: <color> var(--bg-page)`); surfaces `#07130b`/`#0a1a0f`; text
   `#a8f0b8`, strong `#d6ffe0` (the white-hot lead glyph), muted `#5fc777`; action `#3dff6c → #00e03a` and accent
   `#00ff41` with `#010502` text; glow as outer shadows in `rgba(0, 255, 65, …)` on the window, buttons, dialog,
   login box and, on top of a hard 2 px ring, focused fields; font `--font-sans` = a monospace stack
   (`'Cascadia Mono', 'JetBrains Mono', 'DejaVu Sans Mono', Consolas, 'Liberation Mono', 'Courier New', monospace`),
   12 px; card titles with a 2 px phosphor bar (a cursor).
   *Alternative rejected:* a moving rain — motion on a work screen, and no desktop equivalent.
3. **Winamp** — chrome `#3b3d52 → #23242f` (header, card and dialog titles), with tabs `#2b2c3a → #1d1e27`, table
   head `#45475e → #2e3040` and secondary buttons `#4a4c63 → #2b2c3a` in the same steel family; a 1 px
   light-top/dark-bottom bevel through `--bevel-raised` on what the recipes already bevel: buttons, cards, table head,
   dialogs and the login box (fields sunken through `--bevel-sunken`; the header has no bevel consumer and stays a
   gradient); faint horizontal brushing on the page (`--bg-page`, one layer); surfaces `#1b1c25`; silver text
   `#e2e3ea`; LCD green `#2cf02c` on black for fields (`--color-field: #000`) and tooltips, and as the text and icon
   of the current tab (a folder tab in the surface color with a 3 px amber bar on top) and the strong text; primary action amber `#ffbe45 → #f4a51c` with `#16110a` text; title-bar stripe gold `#d9a441`;
   selected row the playlist blue `#1f2fa8` with white text; the Home tiles' stripes in the equalizer's green, amber
   and red; Tahoma/Verdana 12 px; the pressed relief (`--shadow-btn-active: var(--bevel-pressed)`).
4. **Pink Panther** — page `#f7eef2` with soft rose dots, white surfaces, text `#2b1a23`, strong `#6b1f45`, muted
   `#6e5562`; header, footer, dialog title and login box in deep plum `#2e1828 → #3f2136` with the panther pink
   `#e98bb2` as their stripe, the card bar and ornaments; primary action deep rose `#b8407a → #a52d63` with white
   text (≥ 5.18:1 on either end) and `--btn-hover-filter: none` (the hover fill is `#b8407a`, still AA; the inherited
   `brightness(1.1)` would drop it to about 4.4:1); selection `#f6d3e2` with plum text. Inter 13 px. The saturated
   pink `#e98bb2` never fills an area with text; the blush page and the pale selection do, at ≥ 12:1.
5. **High contrast** — `#000000` surfaces, text `#ffffff` (muted `#e6e6e6`, 16.8:1), lines `#bfbfbf`, fields black
   with a white frame, single accent yellow `#ffff00` (19.6:1) only where something acts, is current or has the
   focus: primary action with black text, links, selected row with black text, the header mark, focus, and the
   current tab. Its structure is Matrix's (the user's reference): the same 1 px line around the window (light gray
   `#bfbfbf`, no glow; cards, dialogs and the login box are framed by their own border, so the flat look only removes the
   thin lines it removes in every theme), the 2 px yellow bar before card titles, the 2 px tile
   stripe, the same tab geometry with a 2 px yellow bar on top of the current top, inner and card tab (black with
   yellow text and icon, white sides; the others framed in gray), the same dialog and toast parameters, yellow chart
   bars and strong tags; Inter stays (legibility is this theme's job). The `aaaOnly` recipes `subtab-on/subtab` and
   `cardtab-on/cardtab` are `segmentedOnly` in `matrix.json` (a segmented control, Crystal, marks the selected tab by
   its fill alone; a folder tab marks it with the bar). The current side-list item (`nav-on`) is black with yellow
   text and a 3 px yellow bar, so the yellow focus ring still reads on it
   (`focus/nav-on`). The login focus is solid yellow, 3 px (`login-focus/login-box`). States: ok `#5cff5c`, error
   text `#ffa3a3` (≥ 7:1 on the `#262626` row hover too), error fill `#ff8a8a`, warning `#ffc04d`, all with black
   text on fills. Pink Panther's customer-logo plate is a light `#fdf0f6` with padding, since its header and login box
   are dark plum. Every decorative `--bg-*` is `none` and the top/base pairs are equal; the select arrow
   (`--bg-select-arrow`) stays, in white, since it is an icon. Shadows are zero except the hard state outlines
   (`--shadow-field-focus: 0 0 0 3px #ffff00`, invalid), the tab frames and the 1 px window line; `--focus-ring-w: 3px`. Buttons have
   one color, so the hover is `--btn-hover-filter: brightness(0.85)` (yellow `#d9d900`, black text 14:1) and the
   secondary button's yellow hover line. Listed in `matrix.json` `aaa`: the web test and `themegen` (`MinText` 7)
   hold it to 7:1 on both sides. Inter 13 px.
6. **Desktop**: `go generate` in `desktop/` regenerates `themes_gen.go`; `applyLook` resolves the four ids from
   `brand.theme`. Because the desktop draws some depth of its own (button gloss and shadow, card shadow, the login's
   highlight and elliptic shadow) independently of the CSS shadows, the generator marks a theme **depthless** when
   `--shadow-btn`, `--shadow-card` and `--shadow-login-box` have no visible layer (only High contrast), and
   `applyLook` always draws a depthless theme in the flat look (a 1 px line with no blur and no offset, Matrix's
   frames, is not depth); for the same reason its dialogs are framed in
   `dialog-line` at 2 px (`modal.go`; `dialog-line` is added to the generated roles, the other themes keep their 1 px
   frame in `dialog-head`). Since the current inner tab has the content's color on the web too, the desktop draws it
   the same way (black, yellow text, white sides and the yellow top bar). `TestVolumeDrawn` accepts one-color bands where the
   palette's top and base are equal. `theme_test.go` checks the four ids and schemes, High contrast's `minText` 7,
   3 px ring and depthlessness (and that no other theme is depthless), Winamp's bevel; `TestFocusRing` adds High
   contrast and Matrix.
7. **Accepted web × desktop differences** (added to `docs/themes.md`): the page pattern (Matrix rain, Winamp
   brushing, Pink Panther dots: the desktop page is one color, like Default's dots today), the colored glow (the
   desktop casts its own neutral shadow), the field text color (Fyne draws entry text in `fg`, so Winamp's LCD green
   shows on the web only; the field stays black), Winamp's sunken field and pressed relief (the desktop draws the
   raised bevel only), Fyne's translucent text-selection highlight, the monospace font (system font × Fyne font,
   already accepted), and two gaps the desktop has in every theme: no footer stripe and its own hover tone for
   one-color buttons. (Keyboard focus on desktop buttons, missing in every theme, is the `desktop-button-focus`
   change.)
8. **Labels**: `Matrix (dark)`, `Winamp (dark)`, `Pink Panther`, `High contrast (dark)`; pt-BR `Matrix (escuro)`,
   `Winamp (escuro)`, `Pantera Cor-de-Rosa`, `Alto contraste (escuro)` in the SPA catalog (the labels are rendered
   by the SPA through `t()`; the server and desktop never show them, so `internal/i18n` needs no entry). The stale
   pt-BR keys of four existing labels (`Brisa (dark)`… while the registry says `Breeze (dark)`) are fixed in passing,
   keeping the names as the catalog comment says (`Breeze (escuro)`): this changes only the text of the Settings
   theme list in pt-BR.

## Risks / Trade-offs

- [Disabled buttons are drawn at 50% opacity by the shared recipe: their text falls under 4.5:1 / 7:1] → inactive
  controls are exempt from WCAG 1.4.3/1.4.6; unchanged, as in every theme.

- [Monospace widens text: tables and tabs may truncate sooner in Matrix] → 12 px base and the existing ellipsis
  handling; checked on the Home, Users and Settings screens.
- [Green glow on a dark theme can smear focus visibility] → focus stays a solid 2 px outline (`--color-focus`); the
  glow is only an extra layer.
- [High contrast without depth can make cards blend with the page] → every card, field and dialog has a visible
  frame (`--color-line` ≥ 3:1 against black).
- [More themes mean bigger mosaics] → the mosaic script lays out 18 themes (two per row on the web, web × desktop in
  three images of six).
- [Regression of the existing themes] → the visual suite compares the 14 existing themes (`THEMES=` their ids) with
  the base commit's build; the new themes are left out of that comparison since the base has no CSS for them.
