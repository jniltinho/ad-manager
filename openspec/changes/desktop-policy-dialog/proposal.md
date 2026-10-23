## Why

The web panel moved the password policy into a dialog opened from the **Password policy** button of the Users
toolbar (0.15.0). The desktop app still has it as its own side-menu page, so the two apps differ.

## What Changes

- Desktop: the side menu loses **Password policy**; the Users toolbar gets a **Password policy** button (beside
  Export CSV) that opens the policy in a dialog — the same fields, ranges, weak-policy warning and confirmation as
  before. Home's policy alert ("see ›") goes to Users and opens the dialog.
- The Users toolbar takes as few lines as the window allows (`toolbarRow`): wide, one line with the search, the
  filter and count, the actions and Password policy / Export CSV / Refresh; narrower, one line with those three
  behind a ⋯ menu; narrow, the actions on a second line with the extras still on the first; narrowest, the second
  line and ⋯. The search field gives way first (down to 220 px).
- Home builds its tiles before asking the panel for the totals (a quick answer recolored stripes not yet made).
- Screenshots of the desktop retaken (the side menu changed everywhere); `desktop-13-password-policy.png` is the
  dialog over Users.

## Capabilities

### Modified Capabilities

- `app-desktop`: the password policy is a dialog of the Users page.

## Impact

`desktop/page_policy.go` (`policyDialog`), `desktop/page_users.go`, `desktop/toolbar.go` (+ test), `desktop/main.go` (navigation),
`desktop/page_home.go`, the screenshot tests, `docs/desktop.md`, `docs/screenshots/desktop-*.png`, CHANGELOG. No API
change.
