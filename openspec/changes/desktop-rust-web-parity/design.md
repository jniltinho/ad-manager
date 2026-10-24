## Context

`desktop-rust/` draws with egui 0.36:
- `style.rs` turns a theme's color roles into egui visuals: every theme flat, square corners, `interact_size.y` 26,
  `button_padding` 10×4.
- `shell.rs` has a header, a 200 px left panel of nine pages, the page, and a footer.
- `widgets.rs` has the house widgets: button, card, tag, select, fields.
- The palettes come from `crates/core/assets/themes.json`, generated from the Fyne app's `themes_gen.go`.

The web is measured in `frontend/src/style.css`, `App.vue`, `components/Modal.vue` and the pages. All sizes are
logical px, in the Default theme:

| Part | Web | egui today |
|---|---|---|
| Text field | `.inp` 28 border box, px 8, 1 px border, 12 px | `TextEdit` ≈ 26, egui margin, 13 px |
| Select | `select.inp` 28, pl 8, pr 24, 1 px border, 10×6 arrow | `ComboBox` ≈ 26, egui arrow |
| Button | 28 border box, px 12, 12 px; ghost 400, others 600; shadow outside | 26, padding 10×4, bundled font |
| Toolbar | gap 8, mb 12, page widths (Users search 256, select 176) | item spacing 8×6 |
| Shell | header, top tabs (px 16 py 8, 12/600, 15 px icons), main p-5, footer | header, 200 px left panel, footer |
| Page tabs / card tabs | `.subtab` / `.cardtabs` per theme recipe | one egui style |
| Card head | px 12 py 6, `--card-head-size`, 4 px bar, bottom border | `widgets::card` own sizes |
| Table | th 8/6 12/600; td 8/4 12 px, ≈ 27 for single-line rows | `table.rs` own sizes |
| Badge | `.tag` 6/1, 10/600 uppercase | `widgets::tag` own sizes |
| Dialog | head 8/12 `--dialog-head-size`; body 12/16 scrolls; frame per theme; primary, then Cancel; 460/760, max 94vw | `modal.rs`, top at 10% |
| Depth | gradients, bevels, shadows outside the flat look | always flat |
| Type | Inter 400/600, base 13; theme families | egui bundled fonts |
| Viewport | app box `min(100% − 32, 1840)`, zoom 1.25 ≥ 2400, 1.6 ≥ 3200 | full window, no zoom |

Themes override this baseline in their recipes (`frontend/src/themes/*.css`):
- Default: card tabs in mixed case, no left bar, strip padding 6, gaps 2, a 3 px top mark on the active tab
  (`default.css:33-48`).
- Crystal Dark: a 1 px frame and a 15 px dialog title (`crystal.css:459-473`).
- High Contrast: no card-tab bar and a 1 px frame (`high-contrast.css:217-231`).
- Matrix: monospace at 12 (`matrix.css:197-198`).
- Classic: a Tahoma stack at 11 (`classics.css:194-195,433-434`).

## Goals / Non-Goals

**Goals**
- Each page and dialog has the structure, geometry, depth and typography of its web counterpart, in every theme and
  in the flat look, from 1024 to 3840 logical px.
- The numbers are checked against the web's measured values.
- Keyboard and screen-reader access.

**Non-Goals**
- Reimplementing Import/export or Settings, which open in the browser.
- The Fyne app.
- Mouse cursors: egui's `Response::on_hover_cursor` uses stay as they are.

## Decisions

### D1 The web's shell
`shell.rs` becomes header, top tab strip, main and footer, inside the app box (D6):
- **Header:** logo or company plate, "Active Directory" with the domain, the global search (Enter opens Users with the
  term in its filter), the user name, **my password** (a dialog posting `/api/me/password` with `{old, new}`, as
  `ChangeOwnPasswordDialog.vue`) and **sign out**.
- **Tabs:** in the web's order. Backup holds the page tabs Backup | Sync (`BackupTabs.vue`), and its top tab stays
  active on Sync.
- **Browser exception:** Import/export, Settings, API (`settings.swagger`) and Queues (the queue monitor) open the panel
  URL in the default browser (`ctx.open_url`), with an "opens in the browser" hint.
- **Footer:** the web's items in order.
- **Configure:** the desktop-only screen stays reachable from the header, as today.

### D2 Page and dialog parity matrix
Each row is one task and one acceptance check. **[audit.md](audit.md)** has the element-by-element measurements for 14
screen pairs, from the docs screenshots: each element's size, alignment and spacing in the web and in egui. It also
lists the fixes that repeat on every screen (A1–A18) and the feature gaps found, such as Home's Domain controllers and
Operating systems panels, Groups' filter and Scope, and Shares' File servers list. Every A-row and every gap is closed
by the task of its screen, or listed as an exception. Task 1.2 adds the two captures still missing (Samba logs, Set
password) and the empty, loading and error states.

| egui | Web counterpart | Structure to take |
|---|---|---|
| `pages/home.rs` | `Home.vue` | tiles 2 columns, 6 from `md`; panels 3 and 2 columns from `md` (`Home.vue:61,96,138`) |
| `pages/users.rs` | `Users.vue` | toolbar (search 256, select 176, count, right group), table without a card frame, row links |
| User new | `UserFormDialog.vue` (new) | labels above fields, 2-column row-major grid, groups below; Create, Cancel |
| User edit | `UserFormDialog.vue` (edit) | the same grid, `.cardtabs` Account / Groups (n); Save, Cancel |
| Password / link | `components/PasswordDialog.vue` | Reset, Generate link, then Close once a link is shown |
| Move / policy | `MoveUserDialog`, `PasswordPolicyDialog` | web fields, widths and labels |
| `pages/groups.rs` | `Groups.vue` + `GroupDetailDialog`, `NewGroupDialog` | full-width table (group, system, type, scope, members, description, actions); members in a detail dialog |
| `pages/ous.rs` | `OrgUnits.vue` + `NewOrgUnitDialog`, `GpoListDialog` | web layout and dialogs |
| `pages/computers.rs` | `Computers.vue` + `DnsRecordsDialog` | web table, toolbar, dialogs |
| `pages/shares.rs` | `Shares.vue` + `ShareFormDialog`, `ShareSessionsDialog`, `SmbSessionsDialog`, `FileServerDialog` | web layout with its own scrolling area |
| `pages/backup.rs`, `sync.rs`, `sync_wizard.rs`, `quarantine.rs` | `Backup.vue`, `Sync.vue`, `BackupTabs.vue` + restore, send, run, wizard, log, quarantine dialogs | page tabs Backup / Sync, web sections and tables |
| `pages/logs.rs` | `Logs.vue` | `.subtab` sources, web filters and table |
| `login.rs` | `Login.vue` | exempt from the 28 px rule: 25 px inputs, 26 px button, its paddings and typography (`Login.vue:263-295`) |
| `setup.rs` (Configure) | none | desktop-only; web dialog geometry |

### D3 Size tokens and recipes from one source
- **Web tokens:** `frontend/src/style.css` gains named variables for what lives in Tailwind utilities today:
  - `--control-h`, `--input-px`, `--select-pr` and `--button-px`;
  - `--toolbar-gap` and `--toolbar-mb`;
  - `--th-px`/`--th-py` and `--td-px`/`--td-py`;
  - `--tag-px`/`--tag-py` and `--tag-size`.

  The existing classes use them, so the web looks the same. Page widths stay in the pages and are listed in D2.
- **Generator:** `desktop/internal/themegen` adds to `themes.json`, per theme:
  - these tokens;
  - the recipe parameters: card/dialog head sizes and paddings, subtab line width, tab case and bars, frame widths,
    focus ring width;
  - the depth recipe: gradient stops, bevel and shadow layers, already parsed for the Fyne app;
  - the font family and base size.
- **Reading them:** `adm_core::theme::Palette` reads all of this, and the UI has no copies of the numbers.

### D4 Controls as 28 px border boxes
egui gives direct control over sizes:
- `Style::spacing.interact_size.y = control_h`, with `button_padding` and the `TextEdit` margin computed so the border
  box is 28.
- Our `ComboBox` frame draws the field border and the 10×6 arrow 8 px from the right.
- The button face is 28 and its shadow is painted outside it.
- A toolbar row lays its children out on one height and one text baseline (`ui.horizontal` with a fixed row height
  and vertically centered galleys).
- Tests check that caret, selection, the password reveal and popups keep working.

### D5 Depth
`style.rs` adds a painter helper that draws a vertical gradient (an epaint mesh with per-vertex colors), a bevel (1 px
light/dark lines) and box shadows (`epaint::Shadow`) from the theme's recipe. Header, tab strip, active tab, buttons,
card heads, tiles, dialogs and the sign-in box use it. The flat look (the panel's setting) and depthless themes skip
it, which is today's drawing.

### D6 Viewport, app box and zoom
- **Units and zoom:** egui's `Context::set_zoom_factor(z)` scales layout and text together, as the web's root zoom
  does. `z` is 1.25 from 2400 and 1.6 from 3200 px of **logical client width before zoom** (screen width divided by
  the OS's pixels per point); otherwise `1`. It changes only when a step is crossed, either way, after the resize has
  settled.
- **OS scale:** a 3840 px screen at OS scale 2 is 1920 logical px, so `z` stays 1.
- **App box:** `min(W − 32z, 1840z)` wide, centered, with `16z` vertical margins (egui `Frame` inside the central
  panel). At 1280 it is 1248 wide; at 2560 it is 2300.
- **Minimum:** the native minimum stays 800×600, which the login requires. The shell is laid out for ≥ 1024×640:
  toolbars use their overflow rule, tables scroll, and below that the main area scrolls.
- **Dialogs:** `min(460 or 760, 94% of W)`, horizontally centered, with the top at 10% of the height (existing rule).
  Header and footer stay fixed and the body scrolls. A wide dialog in an 800 px window is 752 px.

### D7 Typography
- **Inter:** Inter 400 and 600 TTF, OFL, embedded through `egui::FontDefinitions`. They are the proportional family,
  plus a named `semibold` family, because egui has no weights: headings, primary buttons and table heads use
  `semibold`, and ghost buttons and body text use regular.
- **Theme families:** Matrix maps to egui's monospace. The Classic themes' Tahoma stack maps to an embedded open
  sans-serif with similar metrics, as the one documented exception, since Tahoma cannot be bundled.
- **Sizes:** sizes and line height come from the theme's tokens.

### D8 Keyboard, AccessKit and contrast
- **Keyboard:** every interactive widget takes focus in visual order. Tab strips move with Left/Right and activate
  with Enter/Space. The focus ring uses the theme's color and width (2 px by default, 3 px in Crystal Dark and High
  Contrast) and is painted on a top layer, so containers never clip it. Dialogs trap Tab and return focus to the
  control that opened them.
- **AccessKit:** fields and selects are `labelled_by` their visible labels, and icon-only links carry their action
  name.
- **Contrast:** a test computes contrast from `themes.json`, with opacity composited on the real background, for text
  and for non-text UI (borders, focus ring, icons). It covers the normal, hover, selected and focused states of every
  theme, and disabled controls are exempt. Text needs at least 4.5:1 (3:1 for large text), and 7:1 (4.5:1 large)
  where the web pair meets AAA. Non-text UI needs at least 3:1. No pair may be lower than the web's.

### D9 Verification
- **Web fixture:** a Playwright script writes the web's bounding boxes for each D2/D3 part, per theme, at 1280×800,
  to `desktop-rust/crates/ui/tests/web-geometry.json`.
- **Desktop geometry tests:** egui_kittest tests read the AccessKit node bounds of the same parts and compare at ±1 px.
  They run per page, theme and locale (en, pt-BR), with long Portuguese labels, long names and DNs, and empty, loading
  and error states.
- **Zoom tests:** step crossings both ways, with a dialog and a popup open.
- **Captures:** web and desktop side by side at 1024×640, 1280×800, 1920×1080, 2560×1440, 3840×2160 and 3840 at OS
  scale 2. They use classic-blue, a dark theme, High Contrast, the flat look, en and pt-BR, 2×, "Example Company Ltd"
  and `domain.local`. The pairs are reviewed by eye, and rasterizer differences are allowed but typeface differences
  are not.

## Risks / Trade-offs

- [The top strip replaces the side menu] → the user asked for the web's layout. Order and icons follow the web.
- [The fonts add about 600 KB] → parity needs the web's typeface.
- [Drawing depth costs CPU per frame] → simple meshes, measured with the existing benchmark. The flat look stays
  available.
- [Zoom changes relayout] → only on a step crossing, after the resize settles.
- [Browser hand-off for Import/export and Settings] → the one listed exception, marked on the tab.

## Migration Plan

Visual and navigation only; `desktop.toml` unchanged; screenshots retaken in the same change.
