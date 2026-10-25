## Why

The desktop's Users page acts on the selected row through a toolbar of eight buttons (Copy, Edit, Password, Unlock,
Disable/Enable, Move, Delete, beside New user): the operator first selects a row, then looks for the button above,
and the toolbar needs two lines on the default 1280 px window. The web panel acts on each row in place: the login opens
the edit form, the status toggles the account, an "unlock" link sits beside a locked account, and one column per
action (password, copy, move, delete) keeps them aligned. The two apps should work the same way.

## What Changes

- **Row actions in the table**, as the web Users page. An account is *editable* when it is not built-in
  (`protected`: RID < 1000 or a DC) and its login is not the operator's own (compared case-insensitively):
  - the **login** of every row is a link that opens the user's edit form;
  - the **status** tag of an editable account toggles it — enabling at once, disabling (also from a locked status)
    after today's confirmation ("Disable {name} ({login})? The user will no longer be able to log on."); a cancelled
    confirmation sends nothing. A non-editable account's status is plain;
  - the column beside the status shows **system** on a built-in account and **unlock** (a link) on a locked one,
    independently: a locked built-in account shows both (the unlock icon before the system tag);
  - one column per action — **password**, **copy**, **move**, **delete** — with **move** and **delete** only on
    editable accounts (a blank, inert cell otherwise, so the columns stay aligned). Each opens the dialog the toolbar
    button opens today (delete still asks to type the login; a wrong or cancelled one sends nothing).
  - Every link acts on the account of the row it was tapped on, resolved when it is tapped and kept through the
    confirmation and the request — never on the selected row. Tapping a link does not change the selection; tapping
    a plain cell still selects its row.
  - **Keyboard:** Enter on the selected row opens a menu of that row's actions (the same ones, the same rules), so
    no action needs the mouse. Hovering a link shows what it does in the status bar ("Click to disable", …).
  - **Widths:** on the default 1280 px window the action links show only their icons (the names stay in the
    keyboard menu and the hover hint) so every column fits without scrolling; when the table has room for them, the
    links show icon and name, as the web's.
- **A lean toolbar** on one line: search, filter and count, then **Password policy**, **Export CSV**, **New user** and
  refresh on the right, as the web's. On a narrow window the right group goes behind the ⋯ menu (`toolbarRow`),
  New user included.
- **The table** (`gridTable`) gains per-cell links: the page says, for each cell, whether it is a link (its icon,
  text, hover hint and action), so a status cell can be a link on one row and plain on another; a link cell is
  rebound on every update (cells are reused for other rows and columns while scrolling).
- **Protections are unchanged and stay on the server:** `internal/ad` refuses to disable or delete a built-in or the
  operator's own account and to move a DC. Moving a built-in account is allowed by the server (documented in
  `docs/architecture.md`); the desktop, as the web, just does not offer it.

## Capabilities

### Modified Capabilities

- `app-desktop`: the Users page acts on each row in place, as the web panel.

## Impact

`desktop/widgets.go` (link columns), `desktop/page_users.go` (row actions, lean toolbar), `desktop/page_home.go`
(the locked-accounts alert still goes to Users, where the unlock links are), tests, the pt-BR catalog (`password`,
`copy`, `move`, `delete`, `unlock`, `Click to disable/enable`, the row menu), `docs/desktop.md`, Users screenshots
(`desktop-04-users`, `05-new-user`, `10-password-link`, `11-link-generated`, `13-password-policy`), CHANGELOG. No API
change. Out of scope: the same treatment of Groups, Computers and OUs (their toolbars stay).
