#!/bin/sh
set -e
if [ "$1" = "remove" ]; then
  systemctl stop ad-manager.service ad-manager-agent.service >/dev/null 2>&1 || true
  systemctl disable ad-manager.service ad-manager-agent.service >/dev/null 2>&1 || true
fi
