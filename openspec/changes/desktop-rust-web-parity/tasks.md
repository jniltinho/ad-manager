## 1. Groundwork

- [x] 1.1 `frontend/src/style.css`: named size tokens used by the existing classes; before/after computed-size check.
- [x] 1.2a Element-by-element audit of 14 screen pairs (`audit.md`): sizes, alignment, spacing, cross-screen fixes
      A1–A18, feature gaps.
- [ ] 1.2b Capture and audit the egui Samba logs tab and the Set password tab; empty/loading/error states.
- [x] 1.3 `desktop/internal/themegen` → `themes.json`: size tokens, recipe parameters, depth recipe, font family and
      base size per theme; `adm_core::theme::Palette` reads them; tests per theme.
- [ ] 1.4 Playwright web fixture `crates/ui/tests/web-geometry.json` over every key of D9 (theme, flat, locale,
      viewport, state), against the desktop tests' mock data.

## 2. Look

- [ ] 2.1 Fonts: the stack resolver over the OS font folders (regular + bold files), embedded Inter 4.001 (OFL,
      license), egui monospace fallback, a `semibold` family; sizes from the theme.
- [ ] 2.2 (A1–A3, A17) Controls at 28 px border boxes (style spacing, `TextEdit` margin, our `ComboBox` frame and arrow, button
      face with the shadow outside); one height and baseline per toolbar line; login exempt.
- [ ] 2.3 Depth painter: directional gradients with Oklab stops, ordered outer (`Shadow`) and inset layers; the CSS flat
      override list (state marks, dialog and popup shadows kept); depthless themes; generator and `gen-themes.py`
      export the full recipe.
- [ ] 2.4 (A4–A9, A13, A14, A18) Card head, table (per-table fixture), badge variants, callouts, row hover, dialog (head, body scroll, frame, 460/760 max 94%, primary first) from the tokens.

## 3. Shell and viewport

- [ ] 3.1 (A10, A15) Header (logo/company, domain, global search, user, my password dialog, sign out) and footer.
- [ ] 3.2 (A11, A12) Top tab strip in the web's order; Backup with Backup | Sync page tabs; browser hand-off for Import/export,
      Settings, API, Queues; no side menu.
- [ ] 3.3 (A16) App box in egui points after zoom; zoom steps from the client width × current zoom; boundary tests
      2399/2400, 3199/3200, fractional OS scale, monitor change; `Me` gains the swagger and queue flags.

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
- [ ] 4.10 Feature gaps of the audit (Home panels, Groups filter/Scope/detail dialog, Shares servers/Tasks/Sessions,
      Logs filter/Export/HTTP, Sync columns, OUs GPOs, Computers DNS, login language switch); bulk toolbars become
      per-row actions.
- [ ] 4.11 Tabs: `.subtab` and `.cardtabs` with theme recipes.

## 5. Keyboard, AccessKit, contrast

- [ ] 5.1 Focus order, tab strips by keyboard, field focus recipe vs the theme's focus-visible ring (paint-only top
      layer), scroll into view, popup/modal ordering, dialog focus trap and restore.
- [ ] 5.2 `labelled_by` on every field and select; names on icon links.
- [ ] 5.3 Contrast test over every theme and state.

## 5b. Audit closure

- [ ] 5b.1 Each A-row of `audit.md` (A1–A18) verified by a geometry test or a capture pair; each feature gap
      implemented or listed as an exception in the matrix, with the egui-only extras decided (bulk toolbars, the
      Groups split view).

## 6. Verification and docs

- [ ] 6.1 Geometry tests per page/theme/locale/viewport/state against the fixture (AccessKit bounds, Response rects,
      galley baselines, captures for depth); zoom crossings; depth and flat
      checks; cursors unchanged; fmt, clippy `-D warnings`, `cargo test --workspace`, `cargo deny`.
- [ ] 6.2 Side-by-side captures at 1024×640, 1280×800, 1920×1080, 2560×1440, 3840×2160 and 3840@2 (classic-blue, a
      dark theme, High Contrast, flat, en and pt-BR); fix what differs.
- [ ] 6.3 Benchmark rerun (depth cost) and `docs/benchmark.md`.
- [ ] 6.4 Retake every `docs/screenshots/desktop-rs-*.png` (2×), README, CHANGELOG; `make dist` into `dist/desktop-rust`.
- [x] 6.5a Codex review of the proposal: two rounds (4 blockers + 4 should-fix + 1 nit, then 4 blockers + 3 should-fix +
      2 nits), all applied.
- [ ] 6.5b Codex review of the code; fixes applied.
