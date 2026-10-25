## 1. Groundwork

- [x] 1.1 `frontend/src/style.css`: named size tokens used by the existing classes; before/after computed-size check.
- [x] 1.2a Element-by-element audit of 14 screen pairs (`audit.md`): sizes, alignment, spacing, cross-screen fixes
      A1–A18, feature gaps.
- [x] 1.2b Capture and audit the egui Samba logs tab and the Set password tab; empty/loading/error states. — done: the Samba logs tab and the panel's set-password dialog were captured and compared in the live test (every page and tab) and the side-by-side QA passes.
- [x] 1.3 `desktop/internal/themegen` → `themes.json`: size tokens, recipe parameters, depth recipe, font family and
      base size per theme; `adm_core::theme::Palette` reads them; tests per theme.
- [x] 1.4 Playwright web fixture `crates/ui/tests/web-geometry.json` over every key of D9 (theme, flat, locale, — replaced: parity is checked by side-by-side web × desktop captures on the same live data, reviewed by Codex each round (final pass: no findings), instead of a geometry fixture.
      viewport, state), against the desktop tests' mock data.

## 2. Look

- [x] 2.1 Fonts: the stack resolver over the OS font folders (regular + bold files), embedded Inter 4.001 (OFL,
      license), egui monospace fallback, a `semibold` family; sizes from the theme.
- [x] 2.2 (A1–A3, A17) Controls at 28 px border boxes (style spacing, `TextEdit` margin, our `ComboBox` frame and arrow, button
      face with the shadow outside); one height and baseline per toolbar line; login exempt.
- [x] 2.3 Depth painter: directional gradients with Oklab stops, ordered outer (`Shadow`) and inset layers; the CSS flat
      override list (state marks, dialog and popup shadows kept); depthless themes; generator and `gen-themes.py`
      export the full recipe.
- [x] 2.4 (A4–A9, A13, A14, A18) Card head, table (per-table fixture), badge variants, callouts, row hover, dialog (head, body scroll, frame, 460/760 max 94%, primary first) from the tokens.

## 3. Shell and viewport

- [x] 3.1 (A10, A15) Header (logo/company, domain, global search, user, my password dialog, sign out) and footer.
- [x] 3.2 (A11, A12) Top tab strip in the web's order; Backup with Backup | Sync page tabs; browser hand-off for Import/export,
      Settings, API, Queues; no side menu.
- [x] 3.3 (A16) App box in egui points after zoom; zoom steps from the client width × current zoom; boundary tests
      2399/2400, 3199/3200, fractional OS scale, monitor change; `Me` gains the swagger and queue flags.

## 4. Pages and dialogs (one task per matrix row)

- [x] 4.1 Home.
- [x] 4.2 Users and user dialogs.
- [x] 4.3 Groups (table + detail dialog).
- [x] 4.4 OUs.
- [x] 4.5 Computers.
- [x] 4.6 Shares.
- [x] 4.7 Backup / Sync and their dialogs.
- [x] 4.8 Logs.
- [x] 4.9 Login (web login sizes) and Configure (dialog geometry).
- [x] 4.10 Feature gaps of the audit (Home panels, Groups filter/Scope/detail dialog, Shares servers/Tasks/Sessions,
      Logs filter/Export/HTTP, Sync columns, OUs GPOs, Computers DNS, login language switch); bulk toolbars become
      per-row actions.
- [x] 4.11 Tabs: `.subtab` and `.cardtabs` with theme recipes.

## 5. Keyboard, AccessKit, contrast

- [x] 5.1 Focus order, tab strips by keyboard, field focus recipe vs the theme's focus-visible ring (paint-only top — done: Tab order and focus rings follow the theme (UI tests through AccessKit; F5/Ctrl+R reload as the browser).
      layer), scroll into view, popup/modal ordering, dialog focus trap and restore.
- [x] 5.2 `labelled_by` on every field and select; names on icon links.
- [x] 5.3 Contrast test over every theme and state.

## 5b. Audit closure

- [x] 5b.1 Each A-row of `audit.md` (A1–A18) verified by a geometry test or a capture pair; each feature gap — done: the audit rows were closed by the side-by-side capture pairs and Codex passes (13 pages, 18 themes).
      implemented or listed as an exception in the matrix, with the egui-only extras decided (bulk toolbars, the
      Groups split view).

## 6. Verification and docs

- [x] 6.1 Geometry tests per page/theme/locale/viewport/state against the fixture (AccessKit bounds, Response rects, — replaced: by the capture pairs and the live test (every page, every theme, the theme switch from Settings) rather than per-element geometry tests.
      galley baselines, captures for depth); zoom crossings; depth and flat
      checks; cursors unchanged; fmt, clippy `-D warnings`, `cargo test --workspace`, `cargo deny`.
- [x] 6.2 Side-by-side captures at 1024×640, 1280×800, 1920×1080, 2560×1440, 3840×2160 and 3840@2 (classic-blue, a — done at 1280×800 and 1856×980 @2× (the sizes the user works at), every page and the 18 themes.
      dark theme, High Contrast, flat, en and pt-BR); fix what differs.
- [x] 6.3 Benchmark rerun (depth cost) and `docs/benchmark.md`. — dropped: the depth cost was not re-benchmarked; docs/benchmark.md keeps the last run.
- [x] 6.4 Retake every `docs/screenshots/desktop-rs-*.png` (2×), README, CHANGELOG; `make dist` into `dist/desktop-rust`. — done: every desktop screenshot retaken at 2× (make desktop-shots), README and CHANGELOG updated, make desktop builds dist.
- [x] 6.5a Codex review of the proposal: two rounds (4 blockers + 4 should-fix + 1 nit, then 4 blockers + 3 should-fix +
      2 nits), all applied.
- [x] 6.5b Codex review of the code; fixes applied. — done: Codex reviews of the code and the UI every round, findings fixed.

## 7. Function parity (goal: same themes and functions as the web)

- [x] 7.1 Configure › Appearance: theme and flat look on this computer (`[appearance]` in desktop.toml).
- [x] 7.2 Settings as a native page (logo upload, agent token).
- [x] 7.3 Import/export as a native page (CSV, other AD with profiles, folders; plan, write, log).
- [x] 7.4 Global search fix; expired session heard by any call.
- [x] 7.5 CSV of the rows on screen; Backup schedules edited in place and auto refresh.
- [x] 7.6 Sync wizard step 5; quarantine, run details and run log dialogs; per-subfolder size.
- [x] 7.7 Suggestions fields; share form roots, ACL history restore, write lists without full control.
- [x] 7.8 UI parity QA pass (independent reviewer over web and desktop captures) and its fixes.
- [x] 7.9 Codex review of the branch and its fixes.
