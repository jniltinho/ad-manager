## ADDED Requirements

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
- **THEN** the page has no rain pattern and the window no glow, and the colors stay the same

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
`#ffffff` text, a single yellow `#ffff00` accent, no gradients and no depth shadows. It SHALL meet WCAG AAA text
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

#### Scenario: Select arrow kept
- **WHEN** a select is shown in High contrast
- **THEN** it has a white arrow (the icon is not removed with the gradients)

#### Scenario: Flat look changes nothing
- **WHEN** the flat look is turned on in High contrast
- **THEN** the screen looks the same (the theme has no gradient or depth to remove), on the web and on the desktop

### Requirement: New themes leave the others intact
Adding `matrix`, `winamp`, `pink-panther` and `high-contrast` SHALL NOT change any role or parameter of the other
themes, on the web or in the desktop palettes.

#### Scenario: Existing themes unchanged
- **WHEN** the visual suite compares the 14 existing themes (`THEMES` set to their ids) with the base commit's build
- **THEN** their screens are pixel-identical

#### Scenario: Desktop palettes unchanged
- **WHEN** `themes_gen.go` is regenerated
- **THEN** the entries of the 14 existing themes keep every value they had (they only gain the `dialog-line` role)
  and none of them is depthless

## MODIFIED Requirements

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
