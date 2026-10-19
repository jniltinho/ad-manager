#!/usr/bin/env bash
set -euo pipefail
mkdir -p /opt/ad-manager/tls
openssl req -x509 -nodes -days 30 -newkey rsa:2048 -subj "/CN=fileserver" -addext "subjectAltName=DNS:fileserver,IP:127.0.0.1" \
  -keyout /opt/ad-manager/tls/tls.key -out /opt/ad-manager/tls/tls.crt 2>/dev/null
printf '%s' "${AGENT_TOKEN:?}" | sha256sum | cut -d' ' -f1 > /opt/ad-manager/agent.token
chmod 600 /opt/ad-manager/agent.token
(printf '%s\n%s\n' "${ANA_PASS:-Ana-123}" "${ANA_PASS:-Ana-123}") | smbpasswd -s -a ana >/dev/null
smbd --no-process-group -D
cat > /opt/ad-manager/config.toml <<CFG
[ad]
url = "ldaps://127.0.0.1"
domain = "test.local"
[redis]
addr = "redis:6379"
[agent]
listen = "0.0.0.0:9443"
cert_file = "/opt/ad-manager/tls/tls.crt"
key_file = "/opt/ad-manager/tls/tls.key"
token_file = "/opt/ad-manager/agent.token"
queue = "fs-test"
share_roots = ["/srv/shares"]
CFG
exec /opt/ad-manager/bin/ad-manager agent --config /opt/ad-manager/config.toml
