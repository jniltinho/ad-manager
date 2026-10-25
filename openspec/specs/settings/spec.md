# settings Specification

## Purpose
Panel configuration from its own Settings screen, stored in the panel database, without editing `config.toml`.

## Requirements

### Requirement: Runtime settings
The panel SHALL let a domain administrator change, on the Settings screen, the default language (English, `en`, out
of the box), the session timeout (5 to 720 minutes), the days after which an account is considered inactive (7 to
3650), the generated password length (10 to 64) and the default OU for new users; the values live in the local
database, take effect without a restart and override `config.toml`.

#### Scenario: Session timeout
- **WHEN** the administrator saves a session timeout of 60
- **THEN** idle sessions expire after 60 minutes and the footer shows 60

#### Scenario: Value out of range
- **WHEN** someone sends a session timeout of 0 or a password length of 5
- **THEN** the API responds 400 and nothing is saved

### Requirement: Organization branding
The panel SHALL show the configured organization name, support link and logo on the login, in the header and in the
footer; without configuration, it uses the AD Manager branding. The logo MUST be PNG, SVG or JPEG up to 512 KiB,
served with the type verified by the server.

#### Scenario: Invalid logo
- **WHEN** the uploaded logo is an HTML file renamed to `.png` or larger than 512 KiB
- **THEN** the API rejects it and the previous logo is kept

#### Scenario: Support link
- **WHEN** the support link does not start with `https://` or `http://`
- **THEN** the API rejects the value

### Requirement: Audited and protected settings
Reading and writing settings SHALL require an administrator session; every write produces a `settings.update` (or
`settings.logo`) audit line without sensitive content. The name, link and logo are public (login).

#### Scenario: No session
- **WHEN** someone without a session calls `PUT /api/settings`
- **THEN** they get 401

### Requirement: Visual theme
The panel SHALL offer a Theme setting on the Settings screen with the themes of the server **theme registry**
(`default`, the default, `admanager` and the themes below). The theme is global: stored in the local database
with the other settings, it applies to all operators and to the login screen without a restart. The registry is
exposed in `brand.themes` (id, label, light/dark scheme) and the `theme` enum in `openapi.yaml` is checked against it
by a test.

#### Scenario: Choose the theme
- **WHEN** the administrator chooses a theme from the registry in Settings and saves
- **THEN** `PUT /api/settings` responds 200 with that `theme` and the panel switches its look immediately, without
  reloading, updating `data-theme` and `color-scheme`

#### Scenario: Theme persists and is global
- **WHEN** a theme is saved and another operator opens the login
- **THEN** the login HTML already arrives with that theme's `data-theme` and `GET /api/health` returns the same
  `brand.theme`

#### Scenario: Default without configuration
- **WHEN** nothing was saved or the client sends the settings without the `theme` field
- **THEN** the theme is `default`

#### Scenario: Invalid value on save
- **WHEN** someone sends a `theme` outside the registry in `PUT /api/settings`
- **THEN** the API responds 400 with the list of accepted themes and nothing is saved

#### Scenario: Invalid value already stored
- **WHEN** the database has a theme that is not in the registry
- **THEN** `GET /api/settings`, the public `brand` and the served HTML use `default`, and the frontend ignores values
  that are not in `brand.themes`

### Requirement: ADManager theme look
With the `admanager` theme, `<html>` SHALL get `data-theme="admanager"`, which switches the palette and shows the top
tabs with the active one white, the side list with a 3 px green bar on the active item, a dark table header
(`#556067`) and green section titles. With `default`, `<html>` gets `data-theme="default"` and the look is the one
from before themes (except for the contrast fixes approved in Phase B).

#### Scenario: Theme elements
- **WHEN** the `admanager` theme is active and the operator opens Shares
- **THEN** the active top tab has a white background, the selected server in the list has the 3 px green bar, the
  table header is `#556067` and the card titles are green

#### Scenario: Default theme intact
- **WHEN** the theme is `default`
- **THEN** `<html>` has `data-theme="default"` and the screens are pixel-identical to before Phase A

### Requirement: Aurora themes
The theme registry SHALL include `aurora` (light) and `aurora-dark` (dark), with buttons in the solid action color and
no welcome banner on Home (removed on request: Home is the same in every theme).

#### Scenario: Choose Aurora
- **WHEN** the administrator chooses "Aurora" and saves
- **THEN** `brand.theme` is `aurora`, the background is `#f9fafd` and the primary button is violet with white text

#### Scenario: Choose Aurora dark
- **WHEN** the administrator chooses "Aurora (dark)" and saves
- **THEN** the panel and the login turn dark (`#010105` / `#13121f`, except the customer logo plate) and the primary
  button is `#967aff` with `#010105` text

#### Scenario: Same Home in every theme
- **WHEN** any theme is active
- **THEN** Home has no welcome banner

### Requirement: Breeze themes
The theme registry SHALL include `breeze` (light) and `breeze-dark` (dark). In `breeze`, background `#eff0f1`,
surfaces `#ffffff`, text `#232629`, action `#1f6fa3` with white text and selection `#3daee9` with dark text. In
`breeze-dark`, `color-scheme: dark`, background `#2a2e32`, surfaces `#1b1e20`, text `#fcfcfc` and action `#3daee9`
with `#1b1e20` text.

#### Scenario: Choose Breeze
- **WHEN** the administrator chooses "Breeze" and saves
- **THEN** `brand.theme` is `breeze`, the selected table row has a `#3daee9` background and dark text, and the
  (two-tone) focus ring has ≥ 3:1 contrast, including over the selected row

#### Scenario: Choose Breeze dark
- **WHEN** the administrator chooses "Breeze (dark)" and saves
- **THEN** the panel and the login turn dark, with no light surface (except the customer logo plate), and the
  primary button is `#3daee9` with `#1b1e20` text

### Requirement: Graphite theme
The theme registry SHALL include `graphite` (dark scheme). With it, `<html>` gets `data-theme="graphite"` and
`color-scheme: dark`, with page background `#111217`, surfaces `#181b1f`, text `#ccccdc`, action `#3d71d9` with white
text and link `#6e9fff`.

#### Scenario: Choose Graphite
- **WHEN** the administrator chooses "Graphite (dark)" in Settings and saves
- **THEN** `PUT /api/settings` responds 200 with `theme: "graphite"` and the panel and the login turn dark, with no
  light surface on any screen of the base theme matrix (except the customer logo plate, per the base policy)

#### Scenario: Identifiable fields
- **WHEN** a form is opened in the Graphite theme
- **THEN** the border of fields, checkboxes and radios has ≥ 3:1 contrast with the field and with the surface

#### Scenario: Readable states
- **WHEN** success, error and warning badges and toasts appear in the Graphite theme
- **THEN** the success and error badges have white text on `#1a7f4b` and `#d10e5c`, the warning badge `#111217` text
  on `#ff9900`, and the toasts (cards) have the title in the state text color over the surface, all ≥ 4.5:1

### Requirement: Violet themes
The theme registry SHALL include `violet` (light) and `violet-dark` (dark). In `violet`, background `#f6f7fb`, white
surfaces, text `#151b26` and action `#894ffb` with white text. In `violet-dark`, `color-scheme: dark`, background
`#0e0d19`, surfaces `#171624`, text `#e7e7f0` and action `#a36eff` with `#0a0a13` text.

#### Scenario: Choose Violet
- **WHEN** the administrator chooses "Violet" and saves
- **THEN** `brand.theme` is `violet`, the active menu item has a `#f1efff` background and `#6f2ad9` text, and the
  primary button is violet with white text

#### Scenario: Choose Violet dark
- **WHEN** the administrator chooses "Violet (dark)" and saves
- **THEN** the panel and the login turn dark, with no light surface (except the customer logo plate), and the
  primary button has dark text on `#a36eff`

#### Scenario: Cards with a stripe
- **WHEN** a Violet theme is active and the operator opens Home
- **THEN** the summary cards have a 2 px stripe on top with the theme's decorative colors, and square corners

### Requirement: Crystal themes
The theme registry SHALL include `crystal` (light) and `crystal-dark` (dark). In `crystal`, background `#ececec`,
white surfaces, 85% black text, action and selection `#0064e1` with white text. In `crystal-dark`,
`color-scheme: dark`, background `#323232`, surfaces `#1e1e1e`, 85% white text and action `#0060c8` with white text.
The font is the system font when available, with no new embedded font.

#### Scenario: Choose Crystal
- **WHEN** the administrator chooses "Crystal" and saves
- **THEN** `brand.theme` is `crystal`, the selected row is `#0064e1` with white text and the corners are square

#### Scenario: Choose Crystal dark
- **WHEN** the administrator chooses "Crystal (dark)" and saves
- **THEN** the panel and the login turn dark, with no light surface (except the customer logo plate), and the
  primary button is `#0060c8` with white text

### Requirement: Cone theme
The theme registry SHALL include `cone` (dark). With it, `color-scheme: dark`, background `#121212`, surfaces
`#1e1e1e`, text `#f2f2f2` and orange action `#ff8800` with `#121212` text.

#### Scenario: Choose Cone
- **WHEN** the administrator chooses "Cone (dark)" and saves
- **THEN** `brand.theme` is `cone`, the panel and the login turn dark with no light surface (except the customer logo
  plate), and the primary button is orange with dark text

#### Scenario: Orange only as an accent
- **WHEN** the `cone` theme is active
- **THEN** orange appears only on actions, links, the active item, focus and ornaments, never as the background of
  text areas

### Requirement: Classic themes
The theme registry SHALL include `classic-blue` and `classic-gray`. In `classic-blue`, the top bar has a blue
`#0054e3 → #1f6ee0` gradient with bold white text, the background is `#ece9d8` and the action is green `#2f7d2f` with
white text. In `classic-gray`, the background is `#7fbfbf` (lightened teal), windows and cards are `#c0c0c0` with a 3D
bevel (`--bevel-raised`), fields are sunken white (`--bevel-sunken`) and the top bar has a `#000080 → #1060a8`
gradient with white text.

#### Scenario: Choose Classic blue
- **WHEN** the administrator chooses "Classic blue (XP)" and saves
- **THEN** `brand.theme` is `classic-blue`, the top bar is blue with bold white text and the selected row is
  `#316ac5` with white text

#### Scenario: Choose Classic gray
- **WHEN** the administrator chooses "Classic gray (98)" and saves
- **THEN** buttons, table headers and cards appear raised, fields appear sunken, and all corners stay square

#### Scenario: Text over the 98 background
- **WHEN** the `classic-gray` theme is active and there is text directly over the background (titles outside cards,
  "Loading…")
- **THEN** that text is black (`color-on-page`), with ≥ 4.5:1 contrast, like the secondary text

#### Scenario: Bevel does not leak
- **WHEN** any theme other than `classic-gray` and `winamp` (which has its own 1 px steel bevel) is active
- **THEN** `--bevel-raised`, `--bevel-sunken` and `--bevel-pressed` are empty shadows (`0 0 #0000`) and the look of
  those themes does not change

### Requirement: Crystal AAA+
The `crystal` and `crystal-dark` themes SHALL use the documented macOS 14 colors (with the recorded adjustments),
SHALL have normal text ≥ 7:1 and large text ≥ 4.5:1 (WCAG 1.4.6) and component and state indicators ≥ 3:1 (WCAG
1.4.11), on the web and in the colors drawn by the desktop, and SHALL lay out the screen like a macOS app (unified
toolbar, tabs as a segmented control, dialogs that slide down from the top to 10% of the height, notices as banners),
with square corners.

#### Scenario: Text contrast
- **WHEN** the contrast test runs for `crystal` and `crystal-dark`
- **THEN** all normal text recipes exceed 7:1, large text recipes 4.5:1 and component recipes 3:1

#### Scenario: States drawn on the desktop
- **WHEN** a desktop button is drawn in Crystal at rest, hovered or pressed, normal or flat
- **THEN** the text has ≥ 7:1 over the drawn background

#### Scenario: Focus ring
- **WHEN** a control receives focus in Crystal
- **THEN** the ring is 3 px and ≥ 3:1 against the background it appears on

#### Scenario: Banner without blur
- **WHEN** the browser lacks `backdrop-filter` or the look is flat
- **THEN** the banner is opaque and the text keeps ≥ 7:1

#### Scenario: Segmented tabs
- **WHEN** the Crystal theme is active
- **THEN** the tabs appear as a segmented control (joined buttons, the current one highlighted), on the web and on
  the desktop

#### Scenario: Dialog at 10% from the top
- **WHEN** a dialog opens in Crystal
- **THEN** it slides down from the top and stops with its top at 10% of the window height, on the web and on the
  desktop

#### Scenario: Other themes intact
- **WHEN** any other theme is active
- **THEN** the new parameters stay at their defaults and that theme's look does not change

### Requirement: First paint in the theme
The server SHALL deliver the SPA document (including fallbacks such as `/password/:token`) already with the saved
theme's `data-theme`, `color-scheme` and background, with `Cache-Control: no-store`, without inline script.

#### Scenario: Dark theme without flash
- **WHEN** a dark theme is saved and the login is opened with a cold cache, a warm cache and with `/api/health`
  delayed or failing
- **THEN** none of the first captured frames has a light background

#### Scenario: Theme changes between navigations
- **WHEN** another operator changes the theme and the page is reloaded
- **THEN** the document arrives with the new theme (no cache serves the old document)

### Requirement: Square corners in every theme
No theme SHALL round corners: buttons, fields, cards, menus, modals, notices, badges and avatars have
`border-radius` 0.

#### Scenario: Measured in the browser
- **WHEN** any registered theme is active and the computed `border-radius` of the visible elements is measured
- **THEN** all of them are 0

#### Scenario: Test blocks rounding
- **WHEN** `style.css`, a theme file or a component `<style>` declares a radius other than 0 (including individual or
  logical properties), or the global rule disappears
- **THEN** the theme test fails

### Requirement: Theme contrast
Every registered theme SHALL meet the contrast matrix (`themes/matrix.json`): ≥ 4.5:1 for normal text over the
effective background of each recipe (including hover, zebra, selection, fills and composited transparencies), ≥ 3:1
for large text, control borders, focus ring and informative series. The disabled exemption applies only to
inoperable controls. Existing themes become subject to the requirement at the end of Phase B.

#### Scenario: Executable matrix
- **WHEN** the contrast test runs over the registry themes
- **THEN** no recipe falls below its threshold, or the test fails naming the theme, recipe, roles and ratio

#### Scenario: Incomplete theme
- **WHEN** a theme does not define a role required by its scheme
- **THEN** the test fails before computing contrast

### Requirement: Colors by role
Components SHALL use contract roles (color, background, shadow, parameter) instead of fixed colors, palette utilities
or mixed-role tokens. In Phase A the migration SHALL be invisible in Default and ADManager.

#### Scenario: Light themes without regression in Phase A
- **WHEN** the matrix screens and states are captured in Default and ADManager at the base SHA and after Phase A, in
  the fixed environment
- **THEN** the RGBA pixels are identical

#### Scenario: New fixed color is blocked
- **WHEN** someone adds a literal color, a palette utility or `text-white` outside the theme files
- **THEN** the lint fails with file, line and context

#### Scenario: Phase B differences approved
- **WHEN** Phase B fixes a failing pair in Default or ADManager
- **THEN** the corresponding visual diff is published and approved and the baseline is updated in a separate commit

### Requirement: Matrix theme
The theme registry SHALL include `matrix` (dark). With it, `color-scheme: dark`, the page is near-black `#010502`
with a static rain of short phosphor strokes, surfaces `#07130b`, text phosphor green `#a8f0b8`, the primary action
phosphor green (`#3dff6c → #00e03a`) with dark text, a monospace font and a green glow around the window, focused fields and dialogs. Normal
text reaches ≥ 4.5:1.

#### Scenario: Choose Matrix
- **WHEN** the administrator chooses "Matrix (dark)" and saves
- **THEN** `brand.theme` is `matrix`, the panel and the login turn dark with no light surface (except the customer
  logo plate), the text is in a monospace font and the primary button is phosphor green with dark text

#### Scenario: Pattern only outside the flat look
- **WHEN** the `matrix` theme is active and the flat look is on
- **THEN** the page has the standard dots instead of the rain pattern and the window no glow, and the colors stay the
  same

### Requirement: Winamp theme
The theme registry SHALL include `winamp` (dark): dark brushed-steel chrome (header and dialog titles
`#3b3d52 → #23242f`), a raised 1 px bevel, fields as the LCD readout (green `#2cf02c` on black), the primary action
amber `#f4a51c` with dark text, a gold `#d9a441` header stripe and the playlist blue `#1f2fa8` selected row with white
text. Normal text reaches ≥ 4.5:1.

#### Scenario: Bevel on the raised parts
- **WHEN** the `winamp` theme is active and the look is not flat
- **THEN** buttons, cards, the table head, dialogs and the login box have the raised bevel, and fields are sunken

#### Scenario: Choose Winamp
- **WHEN** the administrator chooses "Winamp (dark)" and saves
- **THEN** `brand.theme` is `winamp`, the panel turns dark, a text field shows green text on black and the primary
  button is amber with dark text

#### Scenario: Selected row
- **WHEN** the `winamp` theme is active and the operator selects a row in a list
- **THEN** the row is `#1f2fa8` with white text

#### Scenario: Bevel off in the flat look
- **WHEN** the `winamp` theme is active and the flat look is on
- **THEN** buttons, the table head and cards are drawn without the bevel, gradient or shadow

### Requirement: Pink Panther theme
The theme registry SHALL include `pink-panther` (light). With it, the page is blush `#f7eef2`, surfaces white, text
`#2b1a23`, the header, footer, dialog title and login box deep plum `#2e1828` with a panther pink `#e98bb2` stripe,
and the primary action deep rose `#a52d63` with white text. The saturated panther pink is an accent only: it never
fills an area that holds text. Normal text reaches ≥ 4.5:1, at rest and under the mouse.

#### Scenario: Choose Pink Panther
- **WHEN** the administrator chooses "Pink Panther" and saves
- **THEN** `brand.theme` is `pink-panther`, the header is plum with a pink stripe and the primary button is deep
  rose with white text

#### Scenario: Pink only as an accent
- **WHEN** the `pink-panther` theme is active
- **THEN** `#e98bb2` appears only on stripes, bars, the current-tab mark and ornaments, never as the background of
  text

### Requirement: High contrast theme
The theme registry SHALL include `high-contrast` (dark), an accessibility theme: black `#000000` surfaces, white
`#ffffff` text, a single yellow `#ffff00` accent, Matrix's structure (the 1 px window line, bars, tab marks) in that
palette, no gradients and no depth shadows. It SHALL meet WCAG AAA text
contrast (1.4.6) and 3:1 for components (1.4.11) on the web and on the desktop, with a solid 3 px focus ring.

#### Scenario: Choose High contrast
- **WHEN** the administrator chooses "High contrast (dark)" and saves
- **THEN** `brand.theme` is `high-contrast`, the panel and the login are black with white text, and the primary
  button, the links, the selected row (black text) and the current tab use yellow as the only accent

#### Scenario: Text contrast
- **WHEN** the contrast test runs for `high-contrast`
- **THEN** all normal text recipes reach 7:1, large text recipes 4.5:1 and component recipes 3:1

#### Scenario: Focus ring
- **WHEN** a field, select, checkbox or button receives focus in High contrast, on the web or on the desktop
- **THEN** a solid 3 px yellow ring surrounds it, ≥ 3:1 against black

#### Scenario: Focus on the current item
- **WHEN** the current item of a side list receives focus in High contrast
- **THEN** the item is black with yellow text and a yellow bar, and the yellow ring stays visible around it

#### Scenario: Current tab
- **WHEN** a top, inner or card tab is current in High contrast
- **THEN** it has the content's color (black) with yellow text and icon, white sides and a 2 px yellow bar on top,
  like the folder tabs of the other themes, not a yellow block

#### Scenario: Select arrow kept
- **WHEN** a select is shown in High contrast
- **THEN** it has a white arrow (the icon is not removed with the gradients)

#### Scenario: Flat look changes nothing
- **WHEN** the flat look is turned on in High contrast
- **THEN** the screen looks the same but for the thin lines the flat look removes in every theme (the window line,
  the table-head and tab-strip lines, the card hover outline): the theme has no gradient or depth to remove, on the
  web and on the desktop

### Requirement: Folder tabs in the new themes
In `winamp`, `pink-panther` and `high-contrast` the current top, inner and card tab SHALL have the color of the
content surface below it, the theme's accent on its text and icon and a 2–3 px accent bar on top, with thin
separators around the other tabs, as in the existing folder-tab themes (Cone, Matrix).

#### Scenario: Current tab connects to the content
- **WHEN** a page is open in one of these themes
- **THEN** its top tab has `--color-tab-on` equal to `--color-surface` and a 2–3 px accent bar on top
  (`--shadow-tab-on`), and the other tabs have thin separators (`--shadow-tab`)

### Requirement: New themes leave the others intact
Adding `matrix`, `winamp`, `pink-panther` and `high-contrast` SHALL NOT change any role or parameter of the other
themes, on the web or in the desktop palettes.

#### Scenario: Existing themes unchanged
- **WHEN** the visual suite compares the 14 existing themes (`THEMES` set to their ids) with the base commit's build
- **THEN** their screens are pixel-identical, except for the list frame moved to the scrolling wrapper (the
  `list-frame` change, one pixel on the list screens of every theme)

#### Scenario: Desktop palettes unchanged
- **WHEN** `themes_gen.go` is regenerated
- **THEN** the entries of the 14 existing themes keep every value they had (they only gain the `dialog-line` role)
  and none of them is depthless

### Requirement: Classic blue Luna depth at AAA
The `classic-blue` theme SHALL draw the Windows XP Luna look (vertical title-bar gradient in the header and dialog
heads, taskbar footer, green primary button, XP push buttons, task-pane card heads, listview column headers, orange
selected-tab top) through color, gradient and shadow values only, with the same layout as the other themes, and SHALL
hold normal text to 7:1, large text to 4.5:1 and components to 3:1, on the web and in the colors the desktop draws.
The flat look SHALL remove its gradients and depth shadows like any theme.

#### Scenario: Text contrast
- **WHEN** the contrast test runs for `classic-blue`
- **THEN** all normal text recipes reach 7:1, large text recipes 4.5:1 and component recipes 3:1

#### Scenario: Highlights away from text
- **WHEN** the header, a dialog head or the footer is drawn in Classic blue
- **THEN** the light Luna band is only at the top edge and the text sits over colors with ≥ 7:1 against white

#### Scenario: Same structure
- **WHEN** a page is drawn in Classic blue before and after this change
- **THEN** every element keeps its position and size; only colors, gradients and shadows differ

### Requirement: Flat look keeps the page dots
The flat look SHALL keep the page's standard dot pattern (in the theme's `color-page-dots`) in every theme, on the web
and on the desktop, while it removes gradients and depth shadows.

#### Scenario: Dots in the flat look
- **WHEN** the flat look is turned on in Classic blue
- **THEN** the page still shows the dot pattern, and the header, buttons and cards lose their gradients and shadows
