## ADDED Requirements

### Requirement: Wails desktop SSH terminal
The desktop app SHALL offer the Terminal sub-tab as the web panel does, with the panel's terminal WebSocket opened by
the desktop's Go (the session's cookie, the panel's origin, the pinned certificate, no redirect) and its frames passed
to the page as events; the page SHALL open no WebSocket of its own. A terminal SHALL close when its session ends
(sign-in, sign-out, Configure), and Shift+Insert SHALL paste the system clipboard read by Go.

#### Scenario: Terminal in the desktop
- **WHEN** the operator signs in from the desktop app to a panel whose SSH terminal is on, opens Backup › Terminal and
  connects to `sambadc01`
- **THEN** the shell opens in the window and `echo $((6*7))` prints `42`, as in the browser

#### Scenario: Stale session
- **WHEN** the page asks to open a terminal with the generation of a session that has ended
- **THEN** the desktop refuses it as stale and dials nothing

#### Scenario: Sign-out
- **WHEN** the operator signs out with a terminal open
- **THEN** the desktop closes the terminal's WebSocket and the panel writes its `ssh.close`

#### Scenario: Pinned certificate
- **WHEN** the panel presents a certificate other than the one trusted in Configure
- **THEN** the terminal does not open and the page shows the certificate error

#### Scenario: Paste
- **WHEN** the operator presses Shift+Insert in the terminal with text in the system clipboard
- **THEN** the text is typed into the shell
