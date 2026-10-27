## 1. Native navigation guard (Windows, macOS)

- [ ] 1.1 Open a Wails upstream issue/PR exposing a navigation-policy hook (WebView2 `NavigationStarting` and
      `NewWindowRequested`, WKWebView `decidePolicyForNavigationAction`), or ask the operator to approve a carried
      patch (vendored module, `replace`, the patch file and its upstream link).
- [ ] 1.2 `guard_windows.go` and `guard_darwin.go`: cancel every top-level navigation and new window off the app's exact
      origin on that engine (as `appURL` in `guard_linux.go`), installed before the app loads, fail closed;
      `guard_other.go` only for what is left.
- [ ] 1.3 Tests: the origin check as a unit test on every OS; the native smoke in `desktop-ci.yml` sets
      `location.href` and calls `window.open` to another origin and checks the window stays on the app (Windows,
      macOS, Linux).
- [ ] 1.4 Remove the `shortcut:` comment in `guard_other.go` and the known limit in `docs/desktop.md`.

## 2. Virtualized Users table

- [ ] 2.1 Virtualize the rows of `frontend/src/pages/Users.vue` (fixed row height from the theme's size tokens,
      overscan, `aria-rowcount`/`aria-rowindex`), keeping sort, filter, selection, the row actions and keyboard
      focus. Prefer an installed dependency or a small composable over a new library.
- [ ] 2.2 Unit test: with 5,000 users only the visible rows plus the overscan are rendered; scrolling to the end
      shows the last user; sort and filter still act on all rows.
- [ ] 2.3 Check every theme (row height, zebra rows, flat look) and retake the affected screenshots (2×, Classic
      blue, English, Example Company Ltd, `domain.local`).

## 3. Numbers and docs

- [ ] 3.1 Run `desktop/bench/run.sh` again; update `desktop/docs/benchmark.md` with each G4 budget pass/fail.
- [ ] 3.2 `docs/desktop.md` known limits, CHANGELOG.

## 4. Validate

- [ ] 4.1 `make check && make test`, `make desktop-check`, `make build`; the native smokes on the three OSes.
- [ ] 4.2 Codex review (`codex exec --sandbox read-only …`); `openspec validate desktop-native-guard-users-virtual --strict`.
