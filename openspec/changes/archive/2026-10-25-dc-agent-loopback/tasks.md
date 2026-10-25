## 1. Setup

- [x] 1.1 `samba-setup.py`: `DC_AGENT_CONF` listens on `127.0.0.1:9443`; `AGENT_CONF` unchanged.
- [x] 1.2 Test (`internal/agent/setup_test.go`): the DC agent's config listens on the loopback, a file server's on
      the network; an existing `[agent]` is not rewritten.

## 2. Docs

- [x] 2.1 `dc-file-server.md`; the `migrate passwords --agent` examples use `https://127.0.0.1:9443`; CHANGELOG.
