#!/bin/sh
# Safe install: creates the system user and directories with minimal permissions.
# Does NOT enable or start services — the operator decides the machine's role.
set -e
if ! getent passwd ad-manager >/dev/null; then
  adduser --system --group --home /opt/ad-manager/data --no-create-home --shell /usr/sbin/nologin ad-manager >/dev/null
fi
install -d -m 0750 -o ad-manager -g ad-manager /opt/ad-manager/data /opt/ad-manager/logs
install -d -m 0750 -o root -g ad-manager /opt/ad-manager/tls
chown root:ad-manager /opt/ad-manager/config.toml
chmod 0640 /opt/ad-manager/config.toml
if [ ! -f /opt/ad-manager/tls/tls.crt ]; then
  openssl req -x509 -nodes -days 825 -newkey rsa:2048 -subj "/CN=$(hostname -f)/O=AD Manager" \
    -addext "subjectAltName=DNS:$(hostname -f),DNS:$(hostname -s)" \
    -keyout /opt/ad-manager/tls/tls.key -out /opt/ad-manager/tls/tls.crt >/dev/null 2>&1 || true
  [ -f /opt/ad-manager/tls/tls.key ] && chgrp ad-manager /opt/ad-manager/tls/tls.key && chmod 0640 /opt/ad-manager/tls/tls.key
fi
systemctl daemon-reload >/dev/null 2>&1 || true
if grep -q '^setup = true' /opt/ad-manager/config.toml 2>/dev/null; then
  cat <<'MSG'
ad-manager installed (fresh install). To configure it from the browser:
  sudo systemctl enable --now ad-manager ad-manager-agent
  open https://<this server's IP>:8443 and enter the token from:  sudo cat /opt/ad-manager/data/setup-token
The wizard installs and configures Samba (new domain, additional DC, file server) or just connects the panel to
an existing AD; if Samba is already here, it is backed up first. Guide: /opt/ad-manager/docs/installation.md
MSG
  exit 0
fi
cat <<'MSG'
ad-manager installed. No service was started.
  panel:  edit /opt/ad-manager/config.toml and run  systemctl enable --now ad-manager
  agent:  ad-manager agent token  &&  systemctl enable --now ad-manager-agent
          (python3-samba for Windows permissions; check with  ad-manager agent check)
Everything in /opt/ad-manager (config.toml, tls/, data/, logs/, docs/).
systemd units: /usr/lib/systemd/system/ad-manager.service and ad-manager-agent.service
Guide: /opt/ad-manager/docs/install-guide.md
MSG
