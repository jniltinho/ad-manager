## MODIFIED Requirements

### Requirement: Wails desktop stays on the app
The window SHALL stay on the embedded SPA's origin: a top-level navigation, form submission or new window to
another origin SHALL be blocked by the native webview on Windows, Linux and macOS. The SPA's HTML SHALL carry a
Content-Security-Policy that forbids frames, objects and foreign form targets. Only the panel's `/asynqmon` and
`/swagger` pages over `https://` and the brand's `https://` support URL SHALL open, in the system browser.

#### Scenario: Queue monitor
- **WHEN** the operator signs in to a panel with the queue monitor
- **THEN** Queues shows disabled in the tab bar, says "The queue monitor is not available in the desktop app" on
  hover, and a click opens nothing

#### Scenario: Programmatic navigation blocked
- **WHEN** script in the window sets `location.href = 'https://example.org'` or opens it with `window.open`
- **THEN** the window stays on the app and nothing opens

#### Scenario: Opener allowlist
- **WHEN** `Shell.OpenExternal` is called with `file:///etc/passwd`, `http://panel/asynqmon` or
  `https://other.example/asynqmon`
- **THEN** nothing opens and the call fails

#### Scenario: Policy without frames
- **WHEN** the SPA's HTML is served by a production build
- **THEN** its `Content-Security-Policy` has `frame-src 'none'`, `child-src 'none'`, `object-src 'none'` and
  `form-action 'self'`, and no `ws:` source
