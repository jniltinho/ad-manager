## Why

A DC that also serves files (`samba-setup.py new-dc|dc-join|restore-dc --panel --files`, the wizard's "This DC also
serves files") runs its agent beside the panel, and the setup registers it in that panel as `https://127.0.0.1:9443`.
Yet the `[agent]` it writes listens on `0.0.0.0:9443`: the agent's API (its token is root on the server) is open to
the whole network although nothing outside the server needs it — the desktop app reaches the file servers through the
panel since `desktop-panel-only`. On the Criare DC it was found listening on every interface, with no firewall.

## What Changes

- `scripts/samba-setup.py`: the DC file-server `[agent]` (`DC_AGENT_CONF`) listens on `127.0.0.1:9443`. A separate
  file server (`agent` role) keeps `0.0.0.0:9443`, since its panel is on another machine. An existing `[agent]` is
  never rewritten (the setup adds the section only when there is none).
- Docs: `dc-file-server.md` says the setup does it; the CLI examples that reach the DC agent from that DC
  (`migrate passwords --agent`) use `https://127.0.0.1:9443`.

## Capabilities

### Modified Capabilities

- `dc-file-server`: the file-serving DC's agent listens on the loopback only.

## Impact

`scripts/samba-setup.py`, `internal/agent/setup_test.go`, `docs/dc-file-server.md`, `docs/cli.md`,
`docs/migration-runbook.md`, `docs/password-migration.md`, CHANGELOG. Existing installations keep their `listen`;
`docs/agent.md` and `install-guide.md` say how to change it. Out of scope: the all-in-one Docker image (its
entrypoint, its published 9443).
