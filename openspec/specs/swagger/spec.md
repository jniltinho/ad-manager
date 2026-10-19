# swagger Specification

## Purpose
OpenAPI documentation of the API served at /swagger, enabled on the Settings screen and only for a signed-in administrator.

## Requirements

### Requirement: Optional Swagger
The panel SHALL serve the API documentation at `/swagger` only when enabled on the Settings screen (off by default)
and only for a signed-in administrator.

#### Scenario: Production
- **WHEN** Swagger is disabled in Settings
- **THEN** `/swagger/openapi.yaml` and `/swagger/swagger-ui-bundle.js` are not served (falls through to the SPA)

#### Scenario: No session
- **WHEN** Swagger is enabled and someone opens `/swagger` without being signed in
- **THEN** they go to the login (`/?next=/swagger`) and, after signing in, return to `/swagger`

#### Scenario: Staging
- **WHEN** Swagger is enabled and the administrator has signed in to the panel
- **THEN** `/swagger` shows the routes and the Try it out of `GET /api/summary` responds 200 with the session

### Requirement: Agent token from the panel
The administrator SHALL be able to generate an agent token on the Settings screen; the panel shows the token once and
the command that stores only its hash on the file server.

#### Scenario: Generate
- **WHEN** they click "Generate token"
- **THEN** they get the token and a command with its SHA-256; the agent with that hash accepts the token and rejects
  any other
