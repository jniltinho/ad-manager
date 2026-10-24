## ADDED Requirements

### Requirement: Web shell in the desktop
After sign-in, the Rust desktop SHALL show the web panel's shell: the header (logo or company, domain, global search,
user with my password and sign out), the top tab strip in the web's order and grouping, and the web's footer. It
SHALL NOT show a side menu.

#### Scenario: Navigation strip
- **WHEN** the operator signs in
- **THEN** the tabs read Home, Users, Groups, OUs, Computers, Shares, Import/export, Backup, Logs, Settings under the
  header, the current one with the theme's active-tab look, and no side menu is shown

#### Scenario: Sync inside Backup
- **WHEN** the operator opens Sync from the Backup page tabs
- **THEN** the Backup top tab stays active and the page tabs read Backup, Sync

#### Scenario: Browser exception
- **WHEN** the operator activates Import/export or Settings
- **THEN** the panel's page opens in the default browser and the desktop stays on its current page

#### Scenario: Global search
- **WHEN** the operator types "carol" in the header search and presses Enter
- **THEN** the Users page opens with "carol" in its filter

#### Scenario: My password
- **WHEN** the operator chooses my password and submits the current and the new password
- **THEN** the desktop posts them to `/api/me/password` and shows the panel's answer

### Requirement: Page and dialog parity
Each desktop page and dialog SHALL take the structure, field order and breakpoints of its web counterpart, as listed
in the change's parity matrix.

#### Scenario: Groups as the web
- **WHEN** the Groups page is shown
- **THEN** it is a full-width table with the web's columns, and a group's members open in a detail dialog

#### Scenario: User form as the web
- **WHEN** the user edit dialog is open
- **THEN** each label is above its field, the fields fill a two-column grid row by row, and the buttons read Save,
  Cancel (Create, Cancel when creating a user)

### Requirement: Web control geometry
The Rust desktop SHALL draw text fields, selects and buttons as 28 px border boxes with the web's paddings and text
sizes in the Default theme, except on the sign-in screen, which keeps the web login's own control sizes; every control
on one toolbar line SHALL share one height and one text baseline.

#### Scenario: Toolbar controls line up
- **WHEN** the Users page is shown at 1280×800 in the Default theme
- **THEN** the search field, the filter select and the toolbar buttons are 28 px high with the same top and baseline,
  matching the web fixture within 1 px (AccessKit bounds in egui_kittest)

#### Scenario: Select has a field border
- **WHEN** a select is drawn in any theme
- **THEN** it has the 1 px field border and the arrow of the web's select

#### Scenario: Sign-in controls
- **WHEN** the sign-in screen is shown in the Default theme
- **THEN** its inputs are 25 px and its button 26 px high, as the web login, within 1 px of the fixture

#### Scenario: Fields as tall as the buttons beside them
- **WHEN** the Home reset field and its Open button, the Groups member field and its Add button, and the copy field
  of a generated link with its copy button are drawn
- **THEN** each field and its button have the same height (28 px in the Default theme), where egui drew 19 and 26 px

#### Scenario: Entry behavior kept
- **WHEN** the operator clicks inside a 28 px entry, selects text, or reveals a password
- **THEN** the caret, the selection and the reveal behave as before

### Requirement: Web card, table, badge and dialog geometry
Card heads, table headers and rows, badges, dialogs, the header and the footer SHALL use the web's sizes, weights and
alignment from the theme's tokens and recipe (`audit.md` A4–A15).

#### Scenario: Table header and rows as the web
- **WHEN** the Users, Groups, Computers, Logs or Backup table is drawn in the Default theme
- **THEN** its header is one continuous band with bold labels and a label over every column (an "Actions" header over
  the action columns), with no gap before the first row, and the rows have a 29 px pitch with a full-width divider,
  the first row unstriped, numbers and sizes right-aligned

#### Scenario: Badges as the web
- **WHEN** a status or system badge is drawn
- **THEN** its text is bold uppercase at 10 px on the tone's pale fill, centered, 17 px tall, as wide as its text

#### Scenario: Dialog layout
- **WHEN** the new-user, password-policy or reset-password dialog is open in the Default theme
- **THEN** its title is bold and left-aligned with no notch in its bar, labels are above their fields, every field of
  a column starts at the same x, the footer is a full-width band, and the primary action (Create, Save policy,
  Generate link) comes before Cancel

#### Scenario: Header and footer
- **WHEN** the shell is shown in the Default theme at 1280×800
- **THEN** the header is 64 px tall with the logo about 114 px wide and a bold, vertically centered title, and the
  footer is 35 px tall with its 3 px top line and the web's items

### Requirement: Web typography
The Rust desktop SHALL draw its text in the active theme's font family and sizes as the web resolves them, with Inter
400/600 as the baseline, monospace text kept monospace, ghost buttons in regular weight, and the Classic themes' Tahoma
stack mapped to an embedded open fallback.

#### Scenario: Default typeface
- **WHEN** a page is drawn in the Default theme
- **THEN** body text uses Inter regular, headings and primary buttons Inter semibold, and ghost buttons Inter regular

#### Scenario: Theme typeface
- **WHEN** a page is drawn in Matrix or in a Classic theme
- **THEN** it uses that theme's family and base size (monospace 12 for Matrix, the Classic fallback family at 11)

### Requirement: Theme recipes for geometry
The Default theme's numbers SHALL be the baseline, and a theme's own recipe (case, bars, frames, title sizes, line
widths) SHALL take precedence, as on the web.

#### Scenario: Crystal Dark dialog tabs
- **WHEN** the user edit dialog is open in Crystal Dark
- **THEN** its tabs are mixed case without a left bar, the frame is 1 px and the title 15 px, as the web's Crystal
  Dark

#### Scenario: Default dialog tabs
- **WHEN** the user edit dialog is open in the Default theme
- **THEN** its tabs are mixed case without icons and without a left bar, 2 px apart in a strip with 6 px padding, the
  active one with a 3 px top mark, and the strip runs to the dialog edges, as the web's Default

#### Scenario: Page tabs
- **WHEN** the Logs page is shown in any theme
- **THEN** its tabs follow that theme's `.subtab` recipe, including the strip line width

### Requirement: Web viewport and zoom
The Rust desktop SHALL place its shell in an app box of `min(W − 32z, 1840z)` logical px (W the client width, z the zoom), SHALL scale all its
geometry 1.25× from 2400 and 1.6× from 3200 logical px of client width, and SHALL lay out every page from 1024×640,
keeping the 800×600 native minimum.

#### Scenario: 1280 window
- **WHEN** the client area is 1280×800 at OS scale 1
- **THEN** the app box is 1248 px wide and controls are 28 px high

#### Scenario: QHD window
- **WHEN** the client area is 2560 px wide at OS scale 1
- **THEN** the zoom is 1.25, the app box is 1840 × 1.25 = 2300 px wide at most, and controls are 35 px high

#### Scenario: 4K at OS scale 2
- **WHEN** a 3840 px wide screen runs at OS scale 2
- **THEN** the client area is 1920 logical px and no extra zoom is applied

#### Scenario: Crossing a step
- **WHEN** a window with an open dialog is resized from 2300 to 2500 px and back
- **THEN** the zoom changes to 1.25 and back to 1 once each, and the dialog stays centered and inside the window

#### Scenario: Wide dialog in the smallest window
- **WHEN** the user edit dialog opens in an 800×600 window
- **THEN** it is 752 px wide (94% of the window), centered, its top at 60 px

#### Scenario: Smallest shell
- **WHEN** the client area is 1024×640
- **THEN** every page shows its toolbar and table, and every dialog's header and footer stay visible while its body
  scrolls

### Requirement: Keyboard access and contrast
The navigation and page tabs SHALL be reachable and operable by keyboard with a visible, unclipped focus ring, dialogs
SHALL keep and restore focus, and every theme's text and non-text UI colors SHALL meet the web theme's contrast, at
least WCAG 2.2 AA (4.5:1 text, 3:1 large text and non-text UI), and AAA for text where the web pair does.

#### Scenario: Tabs by keyboard
- **WHEN** the operator presses Tab until the tab strip and then Right and Enter
- **THEN** focus moves through the strip in visual order with a visible ring, and Enter opens the focused page

#### Scenario: Dialog focus
- **WHEN** a dialog opened from a button closes
- **THEN** Tab never left the dialog while it was open and focus returns to that button

#### Scenario: Contrast matrix
- **WHEN** the contrast test runs over every theme and state
- **THEN** no text pair is below 4.5:1 (3:1 large) or below the web's ratio, text pairs the web keeps at AAA stay at
  7:1 (4.5:1 large), non-text UI is at least 3:1, and disabled controls are not measured

### Requirement: Web depth
Outside the flat look, the Rust desktop SHALL draw the theme's gradients, bevels and shadows on the header, tab strip,
buttons, card heads, tiles, dialogs and sign-in box as the web does; in the flat look and in depthless themes it SHALL
draw none.

#### Scenario: Depth outside flat
- **WHEN** the Default theme is active, the look is not flat, and the primary button, the header and a card head are
  captured
- **THEN** each has a top pixel different from its bottom pixel, as the web's gradient, and the dialog has a shadow

#### Scenario: Flat look
- **WHEN** the flat look is chosen, or High Contrast is active
- **THEN** those parts are single-colored and there is no shadow pixel outside them

### Requirement: Screen-reader names
Every field, select and icon-only control SHALL carry an AccessKit name taken from its visible label or its action.

#### Scenario: Names
- **WHEN** the AccessKit tree of the Users page and the user edit dialog is read
- **THEN** every text input and combo box has a name equal to its visible label, and every icon link names its action

### Requirement: Mouse cursors unchanged
The visual parity work SHALL NOT change which mouse cursor any control shows.

#### Scenario: Cursors
- **WHEN** the pointer is over a button, a navigation tab, a page tab, a table link or a text field
- **THEN** it shows the same cursor as before this change
