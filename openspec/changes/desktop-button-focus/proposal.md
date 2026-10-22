## Why

The web panel's buttons take the keyboard focus with the theme's focus ring in every theme, and the High contrast
theme promises a 3 px ring on a focused button. The desktop app's own button (`volButton`, used for every button of
the app) takes no keyboard focus at all: a keyboard user can tab through the fields but never reach a button, and can
only press one with the mouse.

## What Changes

- `volButton` (`desktop/button.go`) becomes focusable: Tab reaches it (a disabled button is skipped), it shows the
  theme's focus ring (the same ring the desktop draws around entries, selects and checks: `--focus-ring-w` in
  `--color-focus`), and Space or Enter press it. A click does not focus it (and drops a keyboard focus), as the
  web's `:focus-visible`. A control that takes the focus inside a scroll is scrolled into view with its ring (Fyne
  does not), and the user's Groups list leaves 3 px around its rows for the ring.
- Not covered: the sidebar items, the inner tabs and the password reveal button stay mouse-only (a later change).
- `docs/themes.md` and the `four-new-themes` change drop "no keyboard focus on buttons" from the accepted differences.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `app-desktop`: keyboard focus and the focus ring on buttons.

## Impact

`desktop/button.go`, `desktop/focus.go`, `desktop/page_users.go`, `desktop/focus_test.go`, `docs/themes.md`. No API, setting or web change. The look of an
unfocused button does not change in any theme.
