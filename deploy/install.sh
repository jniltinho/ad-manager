#!/usr/bin/env bash
# Installs/updates the ad-manager panel as a systemd service (Ubuntu 26.04).
# Usage: sudo deploy/install.sh <binary> [ip-or-name-for-the-certificate]
# Everything lives in /opt/ad-manager; a self-signed certificate is generated in tls/ if missing.
set -euo pipefail
BIN="${1:?pass the binary}"
CN="${2:-$(hostname -f)}"
id ad-manager >/dev/null 2>&1 || useradd --system --no-create-home --shell /usr/sbin/nologin ad-manager
install -d -m 0755 /opt/ad-manager
install -d -m 0750 -o root -g ad-manager /opt/ad-manager/tls
install -d -m 0750 -o ad-manager -g ad-manager /opt/ad-manager/data /opt/ad-manager/logs
install -m 0755 "$BIN" /opt/ad-manager/ad-manager
ln -sf /opt/ad-manager/ad-manager /usr/bin/ad-manager
if [[ ! -f /opt/ad-manager/tls/tls.crt ]]; then
  san="DNS:$CN"; [[ "$CN" =~ ^[0-9.]+$ ]] && san="IP:$CN"
  openssl req -x509 -nodes -days 825 -newkey rsa:2048 -subj "/CN=$CN/O=AD Manager" -addext "subjectAltName=$san" \
    -keyout /opt/ad-manager/tls/tls.key -out /opt/ad-manager/tls/tls.crt 2>/dev/null
  chgrp ad-manager /opt/ad-manager/tls/tls.key && chmod 0640 /opt/ad-manager/tls/tls.key
fi
[[ -f /opt/ad-manager/config.toml ]] || { echo "create /opt/ad-manager/config.toml (see config.toml.example)" >&2; exit 1; }
install -m 0644 "$(dirname "$0")/ad-manager.service" /etc/systemd/system/ad-manager.service
# file server (agent): Windows (NTFS) permissions go through Samba's Python API
if command -v smbd >/dev/null && ! python3 -c 'import samba.samba3.smbd' 2>/dev/null; then
  if command -v apt-get >/dev/null; then apt-get install -y -qq python3-samba
  else echo "warning: install python3-samba (Windows permissions on folders)" >&2; fi
fi
systemctl daemon-reload
systemctl enable --now ad-manager
systemctl restart ad-manager
sleep 1
systemctl --no-pager --lines=3 status ad-manager
