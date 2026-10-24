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

#### Scenario: Import/export and Settings in the app
- **WHEN** the operator activates Import/export or Settings
- **THEN** the desktop shows its own page with the web's cards and actions; no tab opens the browser

#### Scenario: Global search
- **WHEN** the operator types "carol" in the header search and presses Enter
- **THEN** the Users page opens with "carol" in its filter and every status shown, and "carol" stays in the header
  field

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
  the action columns), with no gap before the first row, and its row pitch, dividers and striping match the web
  fixture of that same table within 1 px, with numbers and sizes right-aligned where the web aligns them so

#### Scenario: Badges as the web
- **WHEN** the Users status badges (active, locked, deactivated, system) and the Home alert badges are drawn
- **THEN** each has the web's variant: bold uppercase 10 px text centered, 17 px tall; the Users status badge
  (active, locked, deactivated) 104 px wide, pale for active and solid for locked and deactivated; SYSTEM in its own
  column, as wide as its text; the alert badges as wide as their text — as `Users.vue:232-253,301-307`

#### Scenario: Dialog layout
- **WHEN** the new-user, password-policy or reset-password dialog is open in the Default theme
- **THEN** its title is bold and left-aligned with no notch in its bar, the footer is a full-width band, the primary
  action (Create, Save policy, Generate link) comes before Cancel, and the fields follow the web dialog: labels above
  fields with every field of a column at the same x in the new-user and reset dialogs, and label | input | unit columns
  in the password-policy dialog

#### Scenario: Header and footer
- **WHEN** the shell is shown in the Default theme at 1280×800
- **THEN** the header is 64 px tall with the logo about 114 px wide and a bold, vertically centered title, and the
  footer is 35 px tall with its 3 px top line and the web's items

### Requirement: Web features found missing
The Rust desktop SHALL offer the web features the audit found missing: Home's Domain controllers and Operating systems
panels, Groups' filter, type select, count, Export CSV and Scope column with members in a detail dialog, the Shares
File servers list with add, edit and remove, Tasks and Sessions, Logs' action filter, Export CSV and HTTP tab, Sync's
Last run, Quarantine and Size columns, OUs' GPOs, Computers' DNS, and the sign-in screen's language switch.

#### Scenario: Home panels
- **WHEN** Home is shown
- **THEN** it has the Domain controllers and the Operating systems panels, laid out as the web's

#### Scenario: Shares servers
- **WHEN** the Shares page is shown
- **THEN** its left column lists the panel's file servers with add, edit and remove, and a Tasks card, as the web's

#### Scenario: Logs HTTP tab
- **WHEN** the Logs page is shown
- **THEN** its page tabs include the panel's HTTP log, and the audit tab has the action filter and Export CSV

#### Scenario: Groups filter and export
- **WHEN** the operator types "fin" in the Groups filter, chooses Security in the type select and clicks Export CSV
- **THEN** the table and the "x of y" count show only matching security groups, the Scope column is shown, and the
  CSV the panel returns is offered for saving

#### Scenario: Sync columns
- **WHEN** the Sync page lists jobs and runs
- **THEN** jobs show Last run and Quarantine, runs show Size, and numbers are right-aligned

#### Scenario: GPOs, DNS and Sessions
- **WHEN** the operator opens GPOs on OUs, DNS on Computers, and Sessions on Shares
- **THEN** each opens the web's dialog (`GpoListDialog`, `DnsRecordsDialog`, `ShareSessionsDialog`) with its data

#### Scenario: Language switch on sign-in
- **WHEN** the operator chooses Português on the sign-in screen
- **THEN** the screen switches to Portuguese at once, as the web's switch

#### Scenario: Conditional tabs
- **WHEN** the panel's Swagger setting is off and its queue monitor is off
- **THEN** the API and Queues tabs are not shown, and they are when the settings are on

#### Scenario: CSV of the rows on screen
- **WHEN** the operator filters Users, Groups or Computers and clicks Export CSV
- **THEN** the desktop saves the rows on screen with the web's columns, `;` between quoted fields and a BOM

#### Scenario: Sync preview followed in the wizard
- **WHEN** the operator clicks Save and preview in the sync wizard
- **THEN** the wizard shows a fifth step that follows the preview every 2 s until it ends, with its plan, and offers
  Run now and Close (the job is saved)

#### Scenario: Suggestions while typing
- **WHEN** the operator types "bru" in Home's quick box (or a group's new member, a user's group, a share list)
- **THEN** a list under the field offers the matching values with their names; arrows move, Enter picks, Esc
  closes the list only

#### Scenario: Backup schedules edited in place
- **WHEN** the operator changes a schedule's cron in its row and clicks Save schedules
- **THEN** the desktop sends every schedule with `PUT /api/agents/{id}/backup-schedules`

### Requirement: Theme chosen on this computer
The Rust desktop SHALL let the operator choose, in Configure › Appearance, any of the panel's themes and the flat or
depth look for this computer only, applied at once and saved as `[appearance]` in desktop.toml; "Panel default"
SHALL follow the panel's Settings.

#### Scenario: Another theme on this computer
- **WHEN** the operator chooses Matrix (dark) in Configure › Appearance
- **THEN** the desktop redraws in Matrix at once and desktop.toml holds `[appearance] theme = "matrix"`, without a
  panel test

#### Scenario: Back to the panel's theme
- **WHEN** the operator chooses Panel default
- **THEN** the desktop draws with the theme saved in the panel's Settings and desktop.toml has no `[appearance]`

### Requirement: Native Settings and Import/export
The Rust desktop SHALL offer the web's Settings (general, users, company with the logo, the values from config.toml,
the agent token) and Import/export (exports, CSV and other-AD and folder imports previewed as a plan before writing,
with a log of the write) as pages of its own.

#### Scenario: Panel theme saved from the desktop
- **WHEN** the operator chooses Breeze (dark) in Settings and saves
- **THEN** the desktop sends the settings with `PUT /api/settings`, keeps the other values as they were, and redraws
  with the panel's new theme unless Appearance overrides it

#### Scenario: Import from another AD
- **WHEN** the operator fills the source AD, clicks Connect and preview, then Write to the domain and confirms
- **THEN** the desktop posts to `/api/import/ad/plan`, shows the plan, then posts to `/api/import/ad/apply` and
  shows the log of the write

### Requirement: Expired session from any call
When any call of the session answers that it ended, the Rust desktop SHALL go back to the sign-in screen, also when
the page that made the call showed or ignored its error.

#### Scenario: A background call hears the end
- **WHEN** a call made without error handling answers 401
- **THEN** the sign-in screen opens

### Requirement: Web typography
The Rust desktop SHALL resolve the active theme's font stack as a browser does: the first family of the stack installed
in the system or user font folders, matched by family name and CSS weight (400, and 600 or the next heavier face); the embedded Inter 4.001 for the Inter stacks and as the last resort of
sans stacks, egui's monospace as the last resort of monospace stacks; ghost buttons in regular weight.

#### Scenario: Default typeface
- **WHEN** a page is drawn in the Default theme
- **THEN** body text uses Inter regular, headings and primary buttons Inter semibold, and ghost buttons Inter regular

#### Scenario: Theme typeface
- **WHEN** a page is drawn in a Classic theme on Windows, and in Matrix on any OS
- **THEN** the Classic theme uses Tahoma (from the system's fonts) at base 11, and Matrix the first installed family
  of its monospace stack at base 12

#### Scenario: Missing font
- **WHEN** a Classic theme runs on a Linux system without Tahoma, Verdana or Segoe UI
- **THEN** the text uses the next family of the stack that is installed, or Inter, and nothing fails

#### Scenario: Semibold weight
- **WHEN** Breeze runs where Noto Sans Regular and SemiBold are installed
- **THEN** semibold text uses Noto Sans SemiBold (600), not Bold

#### Scenario: Broken font file
- **WHEN** the stack's first family is an unreadable, invalid or wrongly indexed file
- **THEN** it is skipped for the next family, and the app starts

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
The Rust desktop SHALL draw each part's gradients, bevels and shadows from the theme's recipe as the web does; the flat
look SHALL clear exactly the layers that `style.css`'s flat override clears, and keep the state marks, the dialog and
popup shadows and each theme's structural lines (High Contrast's lines included).

#### Scenario: Depth outside flat
- **WHEN** the Default theme is active, the look is not flat, and the primary button, the header and a card head are
  captured
- **THEN** each has a top pixel different from its bottom pixel, as the web's gradient, and the dialog has a shadow

#### Scenario: Flat look clears decoration only
- **WHEN** the flat look is chosen
- **THEN** the parts `style.css` lists in its flat override (header, tabs, buttons, card and table heads, dialog head
  and foot, footer, app box, login box, fields) are single-colored without shadows, while the dialog and popup shadows,
  the current tab's mark, the focus ring and an invalid field's mark are still drawn

#### Scenario: Depthless theme
- **WHEN** High Contrast is active
- **THEN** no decorative gradient is drawn, while its structural lines (drawn as shadows in its CSS) and the state
  marks are

#### Scenario: Shadow layer order
- **WHEN** a Winamp app box (an outline layer listed before a blurred shadow) is drawn
- **THEN** the first listed layer is on top, as in the browser's capture

#### Scenario: Gradient direction and interpolation
- **WHEN** the Classic blue header and the Default dialog title are drawn outside the flat look
- **THEN** their gradients run horizontally, with colors matching the web's (Oklab where the CSS says so) within 2 per
  channel at the start, middle and end

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

### Requirement: Dialog actions reach their own dialog
A footer click SHALL act only on the dialog it was drawn in, and a reply SHALL reach only the dialog and the session
that asked for it.

#### Scenario: Confirmation over a form
- **WHEN** Restore of an earlier permission is confirmed over the open share form
- **THEN** an `acl:restore` task is queued and no `share:create` task

#### Scenario: Late reply after sign-out
- **WHEN** a Settings save or an import answers after the operator signed out and another signed in
- **THEN** the reply changes nothing in the new session

#### Scenario: Run now after a preview
- **WHEN** Run now is clicked on the wizard's preview step after the job id field was edited
- **THEN** the run is asked for the previewed job

### Requirement: Secrets on this computer
Files the app downloads SHALL be readable by their owner only, and passwords and tokens SHALL NOT outlive their use in
plain buffers or in the toolkit's text state.

#### Scenario: Private downloads
- **WHEN** a backup or a CSV export is saved on Unix, also over an existing file
- **THEN** the file's mode is 0600

#### Scenario: Secret request bodies
- **WHEN** the sync wizard, the own-password change or a file server registration sends a password or token
- **THEN** the JSON body is built straight into a buffer wiped on drop, with the secret as its last member

#### Scenario: Secret fields forgotten at sign-out
- **WHEN** the operator signs out after typing the source AD's password or generating an agent token
- **THEN** egui keeps no text state (and no undo history) for those fields

### Requirement: Footer panes and focused fields as the web's
The footer SHALL draw the theme's status-bar panes and line when the theme defines them, and a focused text field
SHALL show the theme's field-focus border and ring.

#### Scenario: Classic gray footer
- **WHEN** the Classic gray theme is on
- **THEN** each footer item sits in a sunken pane 3 px from the next, under a white line

#### Scenario: Focused field
- **WHEN** the new-user dialog opens with the login field focused
- **THEN** the field shows a 1 px border in the field-focus color with its 2 px ring outside
