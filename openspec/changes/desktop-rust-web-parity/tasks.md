## 1. Groundwork

- [ ] 1.1 `frontend/src/style.css`: named size tokens used by the existing classes; before/after computed-size check.
- [x] 1.2a Element-by-element audit of 14 screen pairs (`audit.md`): sizes, alignment, spacing, cross-screen fixes
      A1–A18, feature gaps.
- [ ] 1.2b Capture and audit the egui Samba logs tab and the Set password tab; empty/loading/error states.
- [ ] 1.3 `desktop/internal/themegen` → `themes.json`: size tokens, recipe parameters, depth recipe, font family and
      base size per theme; `adm_core::theme::Palette` reads them; tests per theme.
- [ ] 1.4 Playwright web fixture `crates/ui/tests/web-geometry.json` (per theme, 1280×800).

## 2. Look

- [ ] 2.1 Fonts: Inter 400/600 and the Classic fallback (OFL, license files) through `FontDefinitions`; a `semibold`
      family; theme family and sizes.
- [ ] 2.2 Controls at 28 px border boxes (style spacing, `TextEdit` margin, our `ComboBox` frame and arrow, button
      face with the shadow outside); one height and baseline per toolbar line; login exempt.
- [ ] 2.3 Depth painter (gradient mesh, bevel, `Shadow`) from the recipe; off in the flat look and depthless themes.
- [ ] 2.4 Card head, table, badge, dialog (head, body scroll, frame, 460/760 max 94%, primary first) from the tokens.

## 3. Shell and viewport

- [ ] 3.1 Header (logo/company, domain, global search, user, my password dialog, sign out) and footer.
- [ ] 3.2 Top tab strip in the web's order; Backup with Backup | Sync page tabs; browser hand-off for Import/export,
      Settings, API, Queues; no side menu.
- [ ] 3.3 App box and zoom steps (`set_zoom_factor`, pre-zoom logical width, settle after resize).

## 4. Pages and dialogs (one task per matrix row)

- [ ] 4.1 Home.
- [ ] 4.2 Users and user dialogs.
- [ ] 4.3 Groups (table + detail dialog).
- [ ] 4.4 OUs.
- [ ] 4.5 Computers.
- [ ] 4.6 Shares.
- [ ] 4.7 Backup / Sync and their dialogs.
- [ ] 4.8 Logs.
- [ ] 4.9 Login (web login sizes) and Configure (dialog geometry).
- [ ] 4.10 Tabs: `.subtab` and `.cardtabs` with theme recipes.

## 5. Keyboard, AccessKit, contrast

- [ ] 5.1 Focus order, tab strips by keyboard, top-layer focus ring with the theme width, dialog focus trap and
      restore.
- [ ] 5.2 `labelled_by` on every field and select; names on icon links.
- [ ] 5.3 Contrast test over every theme and state.

## 5b. Audit closure

- [ ] 5b.1 Each A-row of `audit.md` (A1–A18) verified by a geometry test or a capture pair; each feature gap
      implemented or listed as an exception in the matrix, with the egui-only extras decided (bulk toolbars, the
      Groups split view).

## 6. Verification and docs

- [ ] 6.1 egui_kittest geometry tests per page/theme/locale against the fixture; zoom crossings; depth and flat
      checks; cursors unchanged; fmt, clippy `-D warnings`, `cargo test --workspace`, `cargo deny`.
- [ ] 6.2 Side-by-side captures at 1024×640, 1280×800, 1920×1080, 2560×1440, 3840×2160 and 3840@2 (classic-blue, a
      dark theme, High Contrast, flat, en and pt-BR); fix what differs.
- [ ] 6.3 Benchmark rerun (depth cost) and `docs/benchmark.md`.
- [ ] 6.4 Retake every `docs/screenshots/desktop-rs-*.png` (2×), README, CHANGELOG; `make dist` into `dist/desktop-rust`.
- [ ] 6.5 Codex review of the proposal and of the code; fixes applied.
