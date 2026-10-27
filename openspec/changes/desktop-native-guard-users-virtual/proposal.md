## Why

The Wails desktop became the desktop app in v0.27.0 (`desktop-switch`) with two gaps accepted in writing and
listed as known limits in the release notes (`openspec/changes/archive/2026-10-27-desktop-wails`, tasks 4.5 and
5.5a):

- **No native navigation guard on Windows and macOS.** Linux cancels any top-level navigation off the app origin in
  WebKitGTK (`desktop/guard_linux.go`). On Windows and macOS only the CSP and the SPA's link guard keep the window on
  the app (`desktop/guard_other.go`), because Wails v3.0.0 keeps the WebView2 controller and the WKWebView
  navigation delegate private. The requirement "Wails desktop stays on the app" asks for the native block on all
  three.
- **The Users table renders every row.** With 5,000 users the desktop's WebKit takes ~10 s and ~1.4 GB
  (`desktop/docs/benchmark.md`, gate G4 not met). Browsers pay the same cost on large domains.

## What Changes

- **Native navigation guard on Windows and macOS.** WebView2 `NavigationStarting` (cancel) and
  `NewWindowRequested` (handled, nothing opens), and the WKWebView navigation delegate's
  `decidePolicyForNavigationAction` (cancel off-origin top-level navigations and new windows). These come from a
  Wails upstream PR, or, until it lands, a patch carried in the repository (vendored module with a
  `replace`, the patch and its upstream link recorded). Like Linux, the guard is installed before the app loads and
  fails closed: if it cannot be installed, the window does not load the app.
- **Virtualized Users table in the web panel.** Only the rows in view (plus a small overscan) are in the DOM; sort,
  filter, selection, the row actions, keyboard focus and the screen-reader row count keep working. The desktop gets
  it for free because it runs the same page.
- **Benchmark again** (`desktop/bench/run.sh`) and update `desktop/docs/benchmark.md`.

## Capabilities

### Modified Capabilities

- `app-desktop-wails`: the navigation guard scenario is native on all three engines.
- `web-panel`: the Users table is virtualized.

## Impact

- `desktop/guard_*.go` and its tests; a carried Wails patch if upstream does not take it in time (needs the
  operator's go-ahead before it is vendored).
- `frontend/src/pages/Users.vue` (and the table component it uses), its unit test.
- `desktop/docs/benchmark.md`, `docs/desktop.md` known limits, CHANGELOG.
