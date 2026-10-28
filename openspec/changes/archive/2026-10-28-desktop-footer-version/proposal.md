## Why

The desktop app's footer showed `ad-manager <version>` from the panel's `/api/me`: a v0.27.1 desktop app talking to a
v0.27.0 panel showed v0.27.0, so an updated desktop app looked like it had not been updated.

## What Changes

- In the desktop app the footer shows the app's own version (Go's `main.version`, from the release tag), and the
  panel's beside it when they differ ("ad-manager v0.28.0 · panel v0.27.0"). The web panel is unchanged.

## Capabilities

### Modified Capabilities

- `app-desktop-wails`: the footer's version.

## Impact

`frontend/src/App.vue`, `api.js`, `util.js` (+ test), `desktop/index.js`, the pt-BR catalog. When the two versions are
the same (the screenshots' demo panel) the footer looks as before.
