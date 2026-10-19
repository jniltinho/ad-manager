#!/usr/bin/env bash
# Provisions the domain on first start and runs samba in the foreground.
set -euo pipefail
: "${REALM:=EMPRESA.TEST}" "${DOMAIN:=EMPRESATEST}" "${ADMIN_PASS:?set ADMIN_PASS}"
if [[ ! -f /var/lib/samba/private/sam.ldb ]]; then
  samba-tool domain provision --realm="$REALM" --domain="$DOMAIN" --adminpass="$ADMIN_PASS" \
    --server-role=dc --dns-backend=SAMBA_INTERNAL --use-rfc2307 \
    --option="dns forwarder = 127.0.0.11"
  # lax policy like the production domain's (min. 3, no complexity)
  samba-tool domain passwordsettings set --complexity=off --min-pwd-length=3 --min-pwd-age=0 --max-pwd-age=0
fi
exec samba -i --debug-stdout -d 1
