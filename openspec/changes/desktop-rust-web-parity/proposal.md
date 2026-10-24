## Why

The Rust desktop (`desktop-rust/`, egui) takes the web panel's colors, but not its shell, geometry, typography or
depth. Side by side, the two are not the same product:

- The main navigation is a 200 px side menu (`crates/ui/src/shell.rs`), while the web has a header, a top tab strip
  and a footer.
- Controls are 26 px high with egui's paddings, while the web's are 28 px border boxes.
- Every theme is drawn flat (`crates/ui/src/style.rs`), while the web draws its gradients, bevels and shadows outside
  the flat look.
- The text is egui's bundled font, while the web uses Inter, or the theme's own family.
- Groups, the user forms and Backup/Sync have other layouts than their web pages.
- Nothing scales the layout up on QHD and 4K as the web's zoom steps do.

The user wants the desktop to look **exactly like the web**, at AAA quality, from below Full HD up to 4K, with the
mouse cursors unchanged. egui is now the desktop being refined. The same request for the Fyne app
(`desktop-web-visual-parity`) is set aside for this one.

## What Changes

- **The web's shell.**
  - Header: logo or company plate, "Active Directory" with the domain, global search (Enter opens Users filtered by
    the term), and the user name with **my password** (`POST /api/me/password`, as the web's dialog) and **sign out**.
  - Top tab strip in the web's order: Home, Users, Groups, OUs, Computers, Shares, Import/export, Backup, Logs,
    Settings. Sync is a page tab inside Backup. API and Queues are right-aligned when enabled.
  - The web's footer.
  - Import/export, Settings, API and Queues open the panel's page in the default browser. That is the one listed
    exception.
- **Page and dialog parity matrix** (design D2), backed by **[audit.md](audit.md)**. That audit measures every
  element on 14 screen pairs (size, alignment, spacing) and lists the fixes that repeat on every screen:
  - fields 19 px instead of 28;
  - fragmented, non-bold table headers;
  - rows of 32 px instead of 29;
  - solid, fixed-width badges;
  - centered dialog titles with Cancel first;
  - a 72 px header with a large logo;
  - no footer line.

  It also lists the feature gaps: Home's Domain controllers and Operating systems panels, Groups' filter and Scope,
  Shares' server list, Logs' filters and HTTP tab, and Sync's columns. Every page and dialog takes the structure,
  field order, labels and breakpoints of its web counterpart, with explicit exceptions.
- **Web geometry from shared tokens.** Controls are 28 px border boxes (the sign-in screen keeps the web login's 25/26
  px), with one height and one baseline per toolbar line. Card heads, tables, badges and dialogs follow the web:
  dialogs are 460 or 760 px, at most 94% of the window, with the primary action first. The Default theme is the
  baseline, and each theme's recipe takes precedence.
- **Depth as the web.** Outside the flat look, the theme's gradients, bevels and shadows are drawn with egui meshes
  and shadows: header, tabs, buttons, card heads, tiles, dialogs. The flat look, and depthless themes such as High
  Contrast, stay flat.
- **Typography as the browser resolves it.** Each theme's font stack is looked up in the system and user font folders,
  by family name and CSS weight (600 when it exists): Tahoma or Verdana for Classic and Winamp, Noto Sans for Breeze,
  the system font for Crystal, a monospace stack for Matrix. Inter 4.001, the web's own file and licensed OFL, is
  embedded for the Inter stacks and as the last resort.
- **Viewport and zoom.**
  - The app box is `min(W − 32z, 1840z)` with `16z` margins.
  - egui's `set_zoom_factor` applies the web's steps: 1.25 from 2400 and 1.6 from 3200 logical px.
  - The OS scale stays separate.
  - The native minimum stays 800×600, and the shell is verified from 1024×640.
- **Accessibility.**
  - Every control is reachable by keyboard (Tab order, Enter/Space, arrows in tab strips), with the theme's focus
    ring visible and unclipped. Dialogs keep focus inside and restore it when they close.
  - AccessKit names come from visible labels (`labelled_by`), so screen readers work.
  - Contrast follows the web theme: text at least 4.5:1, or 7:1 where the web meets AAA; non-text UI at least 3:1.
    Disabled controls are exempt.
- **Mouse cursors unchanged.**

## Capabilities

### Modified Capabilities

- `app-desktop-rust`: visual parity with the web: shell, page layouts, geometry, depth, typography, viewport/zoom
  and keyboard access.
- `web-panel`: named CSS size tokens that both apps read, with no visual change on the web.

## Impact

- **Desktop (`desktop-rust/`):**
  - `crates/ui/src/{shell,style,widgets,modal,table,login,setup}.rs` and every page and dialog;
  - `crates/core/src/theme.rs`, with the size tokens and recipe parameters in `assets/themes.json`;
  - new embedded fonts in `crates/core/assets/fonts/`, each with its license.
- **Generator:** `desktop/internal/themegen` exports the tokens and recipes into `themes.json`.
- **Web:** `frontend/src/style.css` gains named tokens, with no visual change.
- **Tests and docs:**
  - a web geometry fixture measured with Playwright;
  - egui_kittest geometry, keyboard and contrast tests;
  - every `docs/screenshots/desktop-rs-*.png`, `desktop-rust/README.md` and `desktop-rust/CHANGELOG.md`.
- **API:** unchanged. My password uses the existing endpoint.
- **Out of scope:** the Fyne app (`desktop/`), except for the generator.
