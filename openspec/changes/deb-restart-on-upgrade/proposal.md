## Why

Upgrading the `.deb` (`dpkg -i` / `apt install ./ad-manager_NEW.deb`) replaced the binary but left the running panel
and agent on the old one until someone ran `systemctl try-restart` by hand: on the Criare DC the agent kept running
0.18.1 after 0.18.1+fastreinherit1 was installed, and the health check still reported the old version.

## What Changes

- `packaging/postinst.sh`: on an upgrade (`configure` with the previous version), restart `ad-manager` and
  `ad-manager-agent` only if they are running (`systemctl try-restart`) and say which ones were restarted. A fresh
  install still starts and enables nothing; an upgrade never starts a stopped service. A failed restart is reported
  on stderr and does not fail the package.
- `docs/install-guide.md`: the upgrade row no longer tells the operator to restart by hand.

## Capabilities

### New Capabilities

- `packaging`: what the `.deb` maintainer scripts do with the services.

## Impact

`packaging/postinst.sh`, `docs/install-guide.md`. A sync or ACL pass running in the agent during the upgrade is
interrupted by the restart (as with a manual restart); the folder-sync pending mark repairs its inheritance on the
next run.
