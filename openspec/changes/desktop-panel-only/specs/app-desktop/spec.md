## ADDED Requirements

### Requirement: Panel as the single point of access
The desktop app SHALL reach only the panel: file servers' shares, folder sizes, share tasks and Samba logs SHALL go
through the panel's `/api/agents/{id}/…` proxies with the operator's session, and the app SHALL NOT keep, ask for or
send an agent token.

#### Scenario: Shares through the panel
- **WHEN** the operator opens Shares on a desktop whose workstation cannot reach any file server's port 9443
- **THEN** the page lists the panel's registered file servers and their shares, sizes and share tasks work, and each
  task is in the panel's audit under the operator's name

#### Scenario: Legacy config with agent tokens
- **WHEN** the app starts with a `desktop.toml` that still has `[[agents]]` (or `[[agentes]]`) with tokens
- **THEN** it ignores them, rewrites the same file without the section (0600, the other settings kept) and says once
  that file-server tokens are no longer used

#### Scenario: Legacy config that cannot be rewritten
- **WHEN** the file with agent tokens cannot be replaced (a read-only folder)
- **THEN** the original stays intact, nothing is written elsewhere and the status line says the tokens are still in
  the file, with the reason

#### Scenario: Slow folder sizes
- **WHEN** measuring the shares' sizes takes longer than the panel's 60-second write timeout
- **THEN** the answer still reaches the desktop whole, and an agent answer too large to pass on is an error, never a
  cut JSON

#### Scenario: Session lost while a share task runs
- **WHEN** the panel session ends while the app polls a share task
- **THEN** the polling stops and the app asks to sign in again

#### Scenario: No file server registered
- **WHEN** the panel has no registered file server
- **THEN** Shares says there is none and points to the panel's Settings

### Requirement: Configure validates the sign-in
Configure SHALL ask for the panel URL, user, password and language; Test SHALL reach the panel and sign in with the
given account, showing the domain, the user and the panel version or the panel's error; Save SHALL be possible only
after a successful test of the current values, SHALL write `desktop.toml` (0600) without the password and SHALL open
the app signed in with that session.

#### Scenario: Wrong password
- **WHEN** the operator tests a correct URL with a wrong password
- **THEN** Test shows the panel's "invalid user or password" message and Save stays disabled

#### Scenario: Not a Domain Admin
- **WHEN** the account signs in but is not in Domain Admins
- **THEN** Test shows the panel's refusal and Save stays disabled

#### Scenario: Successful test and save
- **WHEN** Test succeeds and the operator saves
- **THEN** `desktop.toml` holds the URL, the user, the language and the certificate pin (if any) but no password,
  and the app opens the shell signed in, without asking for the password again

#### Scenario: Values changed after or during the test
- **WHEN** the operator edits the URL, the user or the password after a successful test, or while a test runs
- **THEN** Save is disabled until a new test of the current values succeeds, and an older test's result is discarded

#### Scenario: Language change from the signed-in app
- **WHEN** the operator opens Configure from the app, changes only the language and saves without typing a password
- **THEN** the language is saved and the session and the certificate settings are kept

#### Scenario: Redirect from the panel
- **WHEN** the panel answers a request with a redirect
- **THEN** the desktop does not follow it and reports it

### Requirement: Pinned panel certificate
A panel certificate that does not verify SHALL be trusted only by its SHA-256 fingerprint, accepted by the operator
in Configure and stored as `fingerprint` under `[panel]`; every connection SHALL accept exactly that leaf certificate.

#### Scenario: Self-signed panel
- **WHEN** Test reaches a panel whose certificate is self-signed
- **THEN** it reads the certificate without sending any credential, shows its fingerprint, subject, issuer and
  validity and asks whether to trust it; after the operator accepts, the test signs in over a connection that accepts
  only that certificate, and Save stores the pin

#### Scenario: Trusted certificate
- **WHEN** the system trusts the panel's certificate for its host name
- **THEN** nothing is pinned and the normal certificate checks apply

#### Scenario: Certificate changed
- **WHEN** the panel's certificate no longer matches the stored pin
- **THEN** the sign-in fails with a message that the panel's certificate changed and to check it in Configure, and
  nothing is sent to the server beyond the TLS handshake

#### Scenario: Legacy insecure config
- **WHEN** the config has `insecure = true` and no fingerprint
- **THEN** the app still connects, and the next Save replaces `insecure` with the pin of the certificate the
  operator accepted in Test
