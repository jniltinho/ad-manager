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
| Text field | `.inp` 28 border box, px 8, 1 px border, 12 px | `TextEdit` 19 (captures), egui margin, 13 px |
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
| `pages/users.rs` | `Users.vue` | toolbar (search 256, select 176, count, right group), table without a card frame, row links; status badge **fixed 104 px** as the web (`Users.vue:228-253`), solid fills for locked, deactivated and system (`:301-307`) |
| User new | `UserFormDialog.vue` (new) | labels above fields, 2-column row-major grid, groups below; Create, Cancel |
| User edit | `UserFormDialog.vue` (edit) | the same grid, `.cardtabs` Account / Groups (n); Save, Cancel |
| Password / link | `components/PasswordDialog.vue` | Reset, Generate link, then Close once a link is shown |
| Move | `MoveUserDialog` | web fields, widths and labels |
| Policy | `PasswordPolicyDialog` | **label \| input \| unit** columns (`PasswordPolicyDialog.vue:64-67`), not labels above; "Recommended" as a warning callout; Save policy, Cancel |
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
  - the **depth recipe of every part** (`--bg-*` and `--shadow-*`):
    - gradients keep their direction (horizontal for Classic blue's header and the dialog title, vertical
      elsewhere), interpolation space (Oklab where the CSS says so), stops, and `none`;
    - shadows are the ordered list of layers, each inset or outer, with offset, blur, spread and color.

    The current exporters keep only part of this (`themegen.go` keeps one outer layer without spread and no blurred
    insets; `desktop-rust/scripts/gen-themes.py` exports no gradients or shadows), so both are extended. JSON carries it
    all;
  - the font stack (`--font-sans`, `--font-mono`) and base size;
  - the **flat override list** of `style.css:523-552`, as the parts it clears.
- **Reading them:** `adm_core::theme::Palette` reads all of this, and the UI has no copies of the numbers.

### D4 Controls as 28 px border boxes
(`ComboBox::height()` sets the popup's height, not the closed control's: that one comes from the scoped
`interact_size` and button padding.)
egui gives direct control over sizes:
- `Style::spacing.interact_size.y = control_h`, with `button_padding` and the `TextEdit` margin computed so the border
  box is 28.
- Our `ComboBox` frame draws the field border and the 10×6 arrow 8 px from the right.
- The button face is 28 and its shadow is painted outside it.
- A toolbar row lays its children out on one height and one text baseline (`ui.horizontal` with a fixed row height
  and vertically centered galleys).
- Tests check that caret, selection, the password reveal and popups keep working.

### D5 Depth
`style.rs` adds a painter for a part's recipe:
- **Gradients:** a mesh in the gradient's direction, with intermediate vertices at each stop. Colors are interpolated in
  the recipe's space (Oklab precomputed into extra stops), so the gradient matches the browser's.
- **Outer shadows:** each layer is drawn with `epaint::Shadow` (offset, blur, spread), in list order.
- **Inset shadows and bevels:** they are drawn as edge meshes inside the box. A blurred inset is a feathered band, so
  every CSS layer has a painted counterpart.

Header, tab strip, tabs, buttons, card heads, table heads, tiles, dialogs (head, body, foot), the footer and the
sign-in box use it.

**Flat look:** as `style.css:523-552`, the flat look clears exactly the parts it lists: page, header, tabs,
active-tab background, actions, danger, table and card heads, dialog head and foot, footer, button, card, field,
table-head, app, tabs and login-box shadows, pressed relief and footer line. It **keeps** the shadows that mark a state
(current tab, focus, invalid field) and the popups' and dialogs' shadows. Depthless themes (High Contrast) also clear
the decorative layers and keep the state marks.

### D6 Viewport, app box and zoom
- **Units and zoom:** egui's `Context::set_zoom_factor(z)` scales layout and text together, as the web's root zoom
  does: `pixels_per_point = native_pixels_per_point × z`, applied on the next pass.
  - The step is chosen from the **client** width before zoom: `W = current client width in egui points ×
    current z` (the window's content rect, never the monitor's size).
  - `z` is 1.6 when `W ≥ 3200`, 1.25 when `W ≥ 2400`, and otherwise 1.
  - It changes only when a step is crossed, either way, after the resize has settled.
- **OS scale:** a 3840 px screen at OS scale 2 is 1920 logical px, so `z` stays 1.
- **App box:** laid out after zoom in egui points, as `min(available_width − 32, 1840)` wide, centered, with 16-point
  margins (egui `Frame` inside the central panel). It is not multiplied by `z` again: egui's zoom does it. In window
  pixels that is `min(W − 32z, 1840z)`: 1248 wide at 1280, 2300 at 2560.
- **Tests:** the boundaries 2399/2400 and 3199/3200 in both directions, a fractional OS scale (1.5), and a move to a
  monitor with another scale.
- **Minimum:** the native minimum stays 800×600, which the login requires. The shell is laid out for ≥ 1024×640:
  toolbars use their overflow rule, tables scroll, and below that the main area scrolls.
- **Dialogs:** `min(460 or 760, 94% of W)`, horizontally centered, with the top at 10% of the height (existing rule).
  Header and footer stay fixed and the body scrolls. A wide dialog in an 800 px window is 752 px.

### D7 Typography: the browser's font resolution
The web names a font stack per theme and the browser takes the first font installed. The desktop resolves the same
stack the same way:
- **Lookup:** a lookup table maps each family in the stacks to its regular and bold file names in the OS's font
  folders (Windows `%WINDIR%\Fonts`, macOS `/System/Library/Fonts` and `/Library/Fonts`, Linux the fontconfig
  folders).
- **Order:** the first family found, in stack order, becomes egui's proportional family, with a named `semibold`
  family from its bold file (egui has no weights). Headings, primary and secondary buttons, table heads and badges use
  `semibold`; body text and ghost buttons use regular.
- **Theme stacks:**

  | Themes | Stack (`--font-sans`) |
  |---|---|
  | Default and most themes | `'Inter', Tahoma, Verdana, Arial` |
  | Classic (2), Winamp | `Tahoma, Verdana, 'Segoe UI', Arial` |
  | AD Manager | `Verdana, 'Open Sans', Helvetica, Arial` |
  | Breeze | `'Noto Sans', 'Inter', …` |
  | Crystal | system UI (SF Pro on macOS), `'Helvetica Neue'`, `'Inter'`, Arial |
  | Matrix | `'Cascadia Mono', 'JetBrains Mono', 'DejaVu Sans Mono', Consolas, …` (monospace everywhere) |

- **Embedded:** Inter **4.001**, the web's own version, regular and semibold, OFL, with its license file. It is the
  font of the Inter stacks and the last resort of every sans stack. egui's bundled Hack (a DejaVu Sans Mono
  derivative) is the last resort of monospace stacks.
- **Exception:** where the stack's fonts are missing (Tahoma on Linux, for example), the browser falls back to the
  system's generic font and the desktop to Inter. The captures record the font used.
- **Sizes:** sizes and line height come from the theme's tokens.

### D8 Keyboard, AccessKit and contrast
- **Keyboard:** every interactive widget takes focus in visual order. Tab strips move with Left/Right and activate
  with Enter/Space.
- **Focus, as the web:**
  - Fields (`.inp`) use their own focus recipe (border color and focus shadow), not the global outline.
  - Buttons, tabs and links use the theme's `:focus-visible` ring (2 px by default, 3 px in Crystal Dark and High
    Contrast).
  - The ring is painted on the existing top-layer painter (`style.rs`). It is paint-only, so it never changes hit
    testing or cursors.
  - A focused control is scrolled into view. Popups and dialogs order above it.
  - Dialogs trap Tab and return focus to the control that opened them.
- **AccessKit:** fields and selects are `labelled_by` their visible labels, and icon-only links carry their action
  name.
- **Contrast:** a test computes contrast from `themes.json`, with opacity composited on the real background, for text
  and for non-text UI (borders, focus ring, icons). It covers the normal, hover, selected and focused states of every
  theme, and disabled controls are exempt. Text needs at least 4.5:1 (3:1 for large text), and 7:1 (4.5:1 large)
  where the web pair meets AAA. Non-text UI needs at least 3:1. No pair may be lower than the web's.

### D9 Verification
- **Web fixture:** a Playwright script writes the web's bounding boxes for each D2/D3 part, per theme, at 1280×800,
  to `desktop-rust/crates/ui/tests/web-geometry.json`.
- **Desktop geometry tests:** egui_kittest compares at ±1 px, through four kinds of evidence:
  - semantic widgets: AccessKit node bounds (`Node::rect()`, in egui points; the transformed bounding box is physical
    px);
  - containers: recorded `Response` rectangles;
  - text baselines: galley metrics;
  - depth: captures.

  The fixture is keyed by theme, locale, viewport and state, and records border box, painted ink and shadow extents
  separately.
  They run per page, theme and locale (en, pt-BR), with long Portuguese labels, long names and DNs, and empty, loading
  and error states.
- **Zoom tests:** step crossings both ways, with a dialog and a popup open.
- **Captures:** web and desktop side by side at 1024×640, 1280×800, 1920×1080, 2560×1440, 3840×2160 and 3840 at OS
  scale 2. They use classic-blue, a dark theme, High Contrast, the flat look, en and pt-BR, 2×, "Example Company Ltd"
  and `domain.local`. The pairs are reviewed by eye, and rasterizer differences are allowed but typeface differences
  are not.

### D10 Decisions on the audit's extras and gaps
- **Kept as desktop extras**, marked as exceptions in the matrix:
  - Configure (desktop-only);
  - the Refresh buttons;
  - Users' compact icon columns at 1280 px, with tooltips naming each action.
- **Replaced by the web's interaction:**
  - the bulk toolbars of Computers, Shares, Backup and Sync become per-row actions;
  - the Groups split view becomes the table plus a detail dialog.
- **Feature gaps implemented** (each with a scenario):
  - Home: Domain controllers and Operating systems;
  - Groups: filter, type and Scope;
  - Shares: the File servers list with add, edit and remove, Tasks, and Sessions;
  - Logs: the action filter, Export CSV and the HTTP tab;
  - Sync: Last run, Quarantine and Size;
  - Login: the language switch;
  - OUs: GPOs; Computers: DNS.
- **Visibility of API and Queues:** they follow the panel's `settings.swagger` and queue-monitor flags, which the
  core's `Me` model gains. The footer's session idle time comes from `settings.session_minutes`.

## Risks / Trade-offs

- [The top strip replaces the side menu] → the user asked for the web's layout. Order and icons follow the web.
- [The fonts add about 600 KB] → parity needs the web's typeface.
- [Drawing depth costs CPU per frame] → simple meshes, measured with the existing benchmark. The flat look stays
  available.
- [Zoom changes relayout] → only on a step crossing, after the resize settles.
- [Browser hand-off for Import/export and Settings] → the one listed exception, marked on the tab.

## Migration Plan

Visual and navigation only; `desktop.toml` unchanged; screenshots retaken in the same change.
