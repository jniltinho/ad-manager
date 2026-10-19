# logs-http Specification

## Purpose
Record the panel's HTTP requests and browse them on the Logs screen, without sensitive data.

## Requirements

### Requirement: Record API requests
The panel SHALL write one line per request to `/api/*` and `/asynqmon*` with time, IP, operator, method, path,
status, duration and size, without body or headers, and SHALL mask the token of password change links.

#### Scenario: Password change link
- **WHEN** someone accesses `/api/password-link/<token>`
- **THEN** the log records `/api/password-link/***` and the token appears nowhere in the file

#### Scenario: Interface files
- **WHEN** the browser downloads `/assets/index.js`
- **THEN** nothing is recorded

### Requirement: View the HTTP log
The signed-in administrator SHALL see the most recent requests first on the **HTTP — panel** tab, with filters by
text, by status class and "errors only" (status ≥ 400).

#### Scenario: No session
- **WHEN** `GET /api/http-log` arrives without a session
- **THEN** the API responds 401
