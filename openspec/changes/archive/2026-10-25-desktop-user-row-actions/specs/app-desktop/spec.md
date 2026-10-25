## ADDED Requirements

### Requirement: Users row actions
The desktop's Users table SHALL let the operator act on each row in place, as the web panel, on the account of the
row that was tapped: the login opens the edit form; an editable account's status toggles it; a locked account shows
an unlock link; one column per action offers password, copy, move and delete, move and delete only for editable
accounts. An account is editable when it is not built-in and its login is not the operator's own (case-insensitive).

#### Scenario: Edit from the login
- **WHEN** the operator clicks a user's login
- **THEN** that user's edit form opens, and the selected row does not change

#### Scenario: Selected row is not the target
- **WHEN** row A is selected and the operator clicks delete on row B and types B's login
- **THEN** account B is deleted and A is untouched

#### Scenario: Disable from the status
- **WHEN** the operator clicks the "active" (or "locked") status of an editable account and accepts the confirmation
- **THEN** the account is disabled and the list reloads; when the confirmation is cancelled, nothing is sent

#### Scenario: Enable from the status
- **WHEN** the operator clicks the "deactivated" status of an editable account
- **THEN** it is enabled at once, without a confirmation

#### Scenario: Built-in or own account
- **WHEN** the row is a built-in account (Administrator, Guest, krbtgt) or the operator's own
- **THEN** its status is plain text, its move and delete cells are blank and inert, and its login, password and copy
  links work

#### Scenario: Locked account
- **WHEN** an account is locked
- **THEN** an unlock link stands beside its status and clicking it unlocks the account; a locked built-in account
  shows the unlock link and its system tag

#### Scenario: Delete
- **WHEN** the operator clicks delete on a row
- **THEN** the confirmation asks to type that row's login, and a wrong or cancelled one sends nothing

#### Scenario: Panel refuses
- **WHEN** the panel refuses an action with the session still valid
- **THEN** its message is shown and the list is left as it was (an expired session still returns to sign-in)

#### Scenario: List changes during a confirmation
- **WHEN** the operator opens B's disable confirmation, then the list reloads or another row is selected, and accepts
- **THEN** account B is the one disabled

#### Scenario: Keyboard
- **WHEN** the operator selects a row with the keyboard and presses Enter
- **THEN** a menu lists that row's actions, with the same availability, and choosing one runs it

#### Scenario: Default window
- **WHEN** the Users page is shown on a 1280 px window, in English or Portuguese
- **THEN** every column, the action links included (as icons), is visible without scrolling sideways, and hovering
  a link names its action in the status bar

### Requirement: Lean Users toolbar
The Users toolbar SHALL hold only the search, the filter and count, Password policy, Export CSV, New user and refresh,
on one line on the default window, the right group (New user included) behind ⋯ when the window is too narrow.

#### Scenario: Toolbar on the default window
- **WHEN** the Users page is shown on a 1280 px window
- **THEN** the toolbar is one line and no action needs a row to be selected first

#### Scenario: Home's locked alert
- **WHEN** the operator follows Home's locked-accounts alert
- **THEN** the Users page opens, where each locked account has its unlock link

### Requirement: Group count on the edit dialog
The desktop's user edit dialog SHALL name its Groups tab with the number of groups the user is in, as the web panel
(`Groups (n)`), and keep it current as groups are added or removed.

#### Scenario: Count follows the memberships
- **WHEN** the operator opens a user who is in 2 groups and removes one on the Groups tab
- **THEN** the tab reads `Groups (2)` when the dialog opens and `Groups (1)` after the removal
