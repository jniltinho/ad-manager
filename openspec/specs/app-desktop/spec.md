# app-desktop Specification

## Purpose
The desktop app (Go + Fyne) that managers run on their own machine: it talks to the panel API (users, groups,
logs) and to the file-server agents, with the same theme, layout and login as the web panel.

## Requirements

### Requirement: Desktop app theme
The desktop app SHALL use the theme and flat look saved in the panel Settings (`brand.theme`, `brand.flat`), with
colors generated from the same theme files as the web, and SHALL fall back to the Default theme when the API does
not respond or the theme is unknown.

#### Scenario: Theme chosen in the panel
- **WHEN** the administrator saves the `graphite` theme in Settings and an operator signs in to the desktop app
- **THEN** the desktop appears dark, with the Graphite background, text and primary button, and the Fyne dark
  variant

#### Scenario: Flat look
- **WHEN** `brand.flat` is true
- **THEN** desktop buttons, header and card titles appear without gradient and without shadow

#### Scenario: No API
- **WHEN** the desktop cannot read `/api/health` or `/api/me`
- **THEN** it uses the Default theme, as today

#### Scenario: Colors in sync with the web
- **WHEN** a web theme changes color and the generated desktop file is not regenerated
- **THEN** the desktop test fails pointing to the outdated theme

### Requirement: Desktop login screen
The desktop app login screen SHALL follow the web login in geometry and content (480 px box at 10% of the height,
company or AD Manager logo, "ACTIVE DIRECTORY" with "NETBIOS (domain)", "Sign in" button, error below the box, hint
with the user name formats), SHALL use the branding from `/api/health` even when it responds 503, and SHALL have
visible depth in every theme outside the flat look, with the box always in the same place.

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
shadows) outside the flat look and no depth in the flat look.

#### Scenario: Same roles
- **WHEN** the roles test runs for a part of the equivalence table
- **THEN** the desktop draws that part with the roles listed for it

#### Scenario: Drawn depth outside flat
- **WHEN** a theme is active, the look is not flat and the primary button, table header and card title are captured
- **THEN** the top pixel differs from the bottom one, except in Classic gray, where the bevel pixels exist

#### Scenario: Flat without depth
- **WHEN** the look is flat
- **THEN** those elements have a single color and there is no shadow pixel outside them

#### Scenario: Theme switch without residue
- **WHEN** the theme changes from Crystal to another
- **THEN** the look of the new theme matches its baseline

### Requirement: Desktop dialog position
Every desktop app dialog SHALL open with its top at 10% of the window height, horizontally centered, in every theme,
and SHALL return to that position when the window is resized.

#### Scenario: Dialog and resize
- **WHEN** a dialog is open and the window changes from 900 × 600 to 1200 × 800
- **THEN** the dialog top is at 80 px and it stays horizontally centered
