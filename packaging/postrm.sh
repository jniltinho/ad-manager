#!/bin/sh
set -e
systemctl daemon-reload >/dev/null 2>&1 || true
# purge: remove config and certificates; data/ and logs/ are kept for auditing
if [ "$1" = "purge" ]; then
  rm -rf /opt/ad-manager/tls /opt/ad-manager/env /opt/ad-manager/agent.token /opt/ad-manager/import.pass
  rm -f /opt/ad-manager/config.toml
fi
