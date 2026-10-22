## ADDED Requirements

### Requirement: Desktop parity of the Matrix, Winamp, Pink Panther and High contrast themes
The desktop app SHALL render `matrix`, `winamp`, `pink-panther` and `high-contrast` from the generated palettes
when the panel reports them in `brand.theme`, with the same roles as the web (header and its stripe, sidebar from the
tabs, card titles, table head, buttons, dialogs, focus ring, login box and its bar, Winamp's bevel) and the flat
look, and SHALL hold `high-contrast` to 7:1 in every text color it draws, like the Crystal themes.

#### Scenario: Theme chosen in the panel
- **WHEN** the administrator saves `winamp` in Settings and an operator signs in to the desktop app
- **THEN** the desktop appears dark with the steel header and its gold stripe, the amber primary button and the Fyne
  dark variant

#### Scenario: Light theme
- **WHEN** the panel reports `pink-panther`
- **THEN** the desktop uses the light variant with the plum header, the pink stripe and the deep rose primary button

#### Scenario: High contrast without depth
- **WHEN** the panel reports `high-contrast` with the flat look off
- **THEN** the desktop draws it as the flat look (no button gloss or shadow, no card shadow, no login highlight or
  elliptic shadow), and every other theme keeps its depth

#### Scenario: High contrast dialog frame
- **WHEN** a dialog opens in High contrast on the desktop
- **THEN** it is framed in the theme's dialog line (white), 2 px, since no shadow sets it apart; every other theme
  keeps its 1 px frame in the dialog title color

#### Scenario: High contrast drawn at AAA
- **WHEN** a desktop button, field, label or focused select is drawn in High contrast, at rest, hovered or pressed,
  normal or flat
- **THEN** its text has ≥ 7:1 over the drawn background and the focus ring is 3 px

#### Scenario: Bevel in Winamp
- **WHEN** the theme is `winamp` and the look is not flat
- **THEN** buttons and the login box are drawn with the theme's raised bevel; in the flat look, without it

#### Scenario: Accepted differences
- **WHEN** a new theme is shown on the web and on the desktop
- **THEN** the only differences are the documented ones: the page pattern, the colored glow, Winamp's green field
  text and its sunken field and pressed relief, High contrast's current inner tab (black with white text, white sides
  and a yellow top bar on the desktop; yellow with black text on the web), Fyne's text-selection highlight, the
  font, the footer stripe (the desktop footer has none in any theme) and the hover tone of one-color buttons

## MODIFIED Requirements

### Requirement: Desktop login screen
The desktop app login screen SHALL follow the web login in geometry and content (480 px box at 10% of the height,
company or AD Manager logo, "ACTIVE DIRECTORY" with "NETBIOS (domain)", "Sign in" button, error below the box, hint
with the user name formats), SHALL use the branding from `/api/health` even when it responds 503, and SHALL have
visible depth in every theme outside the flat look (except a depthless theme, High contrast, which is always drawn
flat), with the box always in the same place.

#### Scenario: Shadow in a light theme
- **WHEN** the theme is light and the look is not flat
- **THEN** the box has the theme shadow (`--shadow-login-box`) and the elliptical shadow below it

#### Scenario: Bevel in Classic gray
- **WHEN** the theme is `classic-gray` and the look is not flat
- **THEN** the box appears raised (light on top and left, dark on bottom and right), as on the web

#### Scenario: Highlighted box in a dark theme
- **WHEN** the theme is dark and the look is not flat
- **THEN** the box has a light top border and a frame with ≥ 1.5:1 contrast against the page

#### Scenario: Flat look
- **WHEN** `brand.flat` is true, in any theme
- **THEN** the box has only the frame, with no shadow, bevel, highlight, gradient or gloss

#### Scenario: Same position
- **WHEN** the theme changes
- **THEN** the login box keeps the same position and size

#### Scenario: Company logo
- **WHEN** the panel reports `brand.logo`
- **THEN** the login shows the company logo; if the image does not load or is an SVG that Fyne cannot draw, the AD
  Manager logo

#### Scenario: LDAP unavailable
- **WHEN** `/api/health` responds 503 with domain and branding
- **THEN** the login applies the theme and shows "NETBIOS (domain)"

#### Scenario: Long error in a small window
- **WHEN** the window is 800 × 600 and the login fails with a long message
- **THEN** the message appears below the box, wrapped to at most 4 lines (the last one with an ellipsis), without
  leaving the window

### Requirement: Visual parity with the web
Each part of the desktop app SHALL use the same color roles as the equivalent part of the web panel (with the
accepted differences listed in the design), and each theme SHALL have drawn depth (short gradients or bevel, and
shadows) outside the flat look and no depth in the flat look; a theme whose web look has no depth (High contrast:
one-color bands and no shadow on buttons, cards and the login box) is drawn flat in both looks.

#### Scenario: Same roles
- **WHEN** the roles test runs for a part of the equivalence table
- **THEN** the desktop draws that part with the roles listed for it

#### Scenario: Drawn depth outside flat
- **WHEN** a theme is active, the look is not flat and the primary button, table header and card title are captured
- **THEN** the top pixel differs from the bottom one, except in Classic gray, where the bevel pixels exist, and in a
  theme whose top and base colors are equal by design (High contrast)

#### Scenario: Flat without depth
- **WHEN** the look is flat
- **THEN** those elements have a single color and there is no shadow pixel outside them

#### Scenario: Theme switch without residue
- **WHEN** the theme changes from Crystal to another
- **THEN** the look of the new theme matches its baseline
