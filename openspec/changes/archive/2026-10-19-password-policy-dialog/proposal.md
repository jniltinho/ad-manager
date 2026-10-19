## Why

The password policy is one domain-wide form with a single Save, yet it takes a whole top-level tab next to the
screens operators use every day. It belongs with the accounts it governs, and the tab bar gets shorter. The docs
screenshots are also blurry on HiDPI screens and in the README (1×), so they are retaken at 2×.

## What Changes

- The **Password policy** tab is removed from the web panel; the same form opens as a dialog from a
  **Password policy** button on the Users toolbar (save still asks for confirmation and warns about the Windows
  `Default Domain Policy`).
- `/password-policy` and the legacy `/politica` redirect to `/users?policy=1`, which opens the dialog; Home's weak
  policy alert links there too.
- Docs screenshots (web panel, setup wizard, sync wizard and desktop app) are captured at a 2× device scale.
- Out of scope: the desktop app keeps its Password policy screen; the API, CLI and MCP are unchanged.

## Capabilities

### New Capabilities

### Modified Capabilities
- `web-panel`: the administration interface reaches the password policy through a dialog on the users screen
  instead of its own screen.

## Impact

- Frontend: `pages/PasswordPolicy.vue` becomes `components/dialogs/PasswordPolicyDialog.vue`; `App.vue` tabs,
  `main.js` routes and redirects, `pages/Users.vue`, `pages/Home.vue`.
- Visual tests (`tests/visual/themes.spec.mjs`, `docs-shots.mjs`, `setup-shots.mjs`, `sync-shots.mjs`), the desktop
  `docs_shots_test.go`, `docs/screenshots/` and the docs that point to the tab.
