#!/usr/bin/env bash
# ad-manager all-in-one (docs/docker.md). The first start provisions the Samba AD domain; every start writes the
# certificates (once), the panel and agent configuration and the agent's registration (once), then supervisord runs
# samba, redis, the panel and the agent.
#
#   AD_DOMAIN          DNS name of the domain (default example.local)
#   AD_NETBIOS         short name (default: the first label of AD_DOMAIN, upper case)
#   AD_ADMIN_PASSWORD  the Administrator's password; empty: a strong one is generated and shown once in the log
#   AD_PUBLIC_HOST     the name or IP the browser uses (default localhost): in the certificate and the URL shown
#   AD_PUBLIC_PORT     the host port mapped to 8443 (default 8443): only for the URL shown
#   AD_DNS_FORWARDER   where the DC asks names outside the domain (default: Docker's DNS, else 1.1.1.1)
set -euo pipefail
OPT=/opt/ad-manager
SAM=/var/lib/samba/private/sam.ldb
MARK=/var/lib/samba/.ad-manager-provisioned # written once the first start finished the domain
CONFIG=$OPT/config.toml
say() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31mERROR:\033[0m %s\n' "$*" >&2; exit 1; }

REALM=${AD_DOMAIN:-example.local}
REALM=${REALM,,}
[[ $REALM =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$ ]] ||
  die "AD_DOMAIN=$REALM: use a dotted DNS name, e.g. example.local"
NETBIOS=${AD_NETBIOS:-${REALM%%.*}}
NETBIOS=${NETBIOS^^}
NETBIOS=${NETBIOS:0:15}
[[ $NETBIOS =~ ^[A-Z0-9][A-Z0-9-]*$ ]] || die "AD_NETBIOS=$NETBIOS: up to 15 letters, digits or hyphens"
HOST=$(hostname -s)
HOST=${HOST,,}
# a domain already here keeps its identity: the variables only count on the first start
if [[ -f $MARK ]]; then
  param() { testparm -s --parameter-name="$1" 2>/dev/null | head -1; }
  have_realm=$(param realm) have_netbios=$(param workgroup) have_host=$(param "netbios name")
  [[ ${have_realm,,} != "$REALM" && -n ${AD_DOMAIN:-} ]] && say "AD_DOMAIN=$REALM ignored: this volume's domain is ${have_realm,,}"
  REALM=${have_realm,,} NETBIOS=${have_netbios^^} HOST=${have_host,,}
fi
[[ ${#HOST} -le 15 && ${HOST^^} != "$NETBIOS" ]] ||
  die "the container name ($HOST) must have up to 15 characters and differ from $NETBIOS: use --hostname dc1"
FQDN=$HOST.$REALM
IP=$(hostname -I | awk '{print $1}')
PUBLIC=${AD_PUBLIC_HOST:-localhost}
PORT=${AD_PUBLIC_PORT:-8443}
FORWARDER=${AD_DNS_FORWARDER:-$(awk '/^nameserver/ {print $2; exit}' /etc/resolv.conf)}
FORWARDER=${FORWARDER:-1.1.1.1}

# the DC's name resolves to its address (the panel reaches LDAPS by name)
grep -q " $FQDN" /etc/hosts || echo "$IP $FQDN $HOST" >>/etc/hosts

# strong: 8+ characters with three of lowercase, uppercase, digits and symbols (Samba's default policy)
strong() {
  local p=$1 n=0
  [[ $p =~ [a-z] ]] && n=$((n + 1))
  [[ $p =~ [A-Z] ]] && n=$((n + 1))
  [[ $p =~ [0-9] ]] && n=$((n + 1))
  [[ $p =~ [^a-zA-Z0-9] ]] && n=$((n + 1))
  [[ ${#p} -ge 8 && $n -ge 3 ]]
}
generate() { printf '%s-%s' "$(openssl rand -base64 18 | tr -dc 'A-Za-z0-9' | head -c 16)" "Aa1"; }

generated=""
if [[ ! -f $MARK ]]; then
  if [[ -e $SAM || -f /etc/samba/smb.conf ]]; then
    say "incomplete previous provisioning: starting the domain over"
    find /var/lib/samba -mindepth 1 -delete
    rm -f /etc/samba/smb.conf
  fi
  password=${AD_ADMIN_PASSWORD:-}
  if [[ -z $password ]]; then
    password=$(generate)
    generated=$password
  fi
  strong "$password" || die "AD_ADMIN_PASSWORD: at least 8 characters, mixing three of: lowercase, uppercase, digits and symbols"
  say "provisioning $REALM ($NETBIOS) with the DC $FQDN ($IP)"
  rm -f /etc/samba/smb.conf
  # samba-tool provision takes the password only as an argument: a random one goes there and is replaced right
  # after through Samba's API, so the chosen password is never on a command line
  tmp=$(generate)
  samba-tool domain provision --realm="${REALM^^}" --domain="$NETBIOS" --server-role=dc --dns-backend=SAMBA_INTERNAL \
    --use-rfc2307 --host-ip="$IP" --adminpass="$tmp" --option="dns forwarder = $FORWARDER" >/tmp/provision.log 2>&1 ||
    { cat /tmp/provision.log; die "provisioning failed"; }
  rm -f /tmp/provision.log
  ADMIN_PW=$password python3 - <<'PY'
import os
from samba.auth import system_session
from samba.param import LoadParm
from samba.samdb import SamDB
lp = LoadParm()
lp.load_default()
db = SamDB(url=lp.samdb_url(), session_info=system_session(), lp=lp)
db.setpassword("(sAMAccountName=Administrator)", os.environ["ADMIN_PW"], force_change_at_next_login=False,
               username="Administrator")
PY
  unset password ADMIN_PW
  if [[ -n $generated ]]; then # shown now: a later step that fails must not leave the password unknown
    printf '\n  Generated password for Administrator: %s\n  (shown only on this first start: keep it)\n\n' "$generated" >&2
  fi
  # the shares: Domain Admins and SYSTEM in control, authenticated users only see the root (each share sets its own)
  install -d -m 0755 /srv/shares
  samba-tool ntacl set "O:DAG:DAD:P(A;OICI;FA;;;DA)(A;OICI;FA;;;SY)(A;;0x1200a9;;;AU)" /srv/shares ||
    say "warning: no Windows ACL on /srv/shares (the container needs --cap-add SYS_ADMIN)"
  touch "$MARK"
fi
cp /var/lib/samba/private/krb5.conf /etc/krb5.conf

# ── certificates: an own CA and one certificate for Samba's LDAPS, the panel and the agent (made once, kept) ──
TLS=$OPT/tls
tls_ok() { # the CA, the key and the certificate: present, chained, matching and for this DC's name
  [[ -s $TLS/ca.pem && -s $TLS/ca.key && -s $TLS/dc.key && -s $TLS/dc.crt ]] &&
    openssl verify -CAfile "$TLS/ca.pem" "$TLS/dc.crt" >/dev/null 2>&1 &&
    openssl x509 -noout -checkhost "$FQDN" -in "$TLS/dc.crt" 2>/dev/null | grep -q 'does match' &&
    [[ $(openssl x509 -noout -pubkey -in "$TLS/dc.crt" | sha256sum) == $(openssl pkey -pubout -in "$TLS/dc.key" | sha256sum) ]]
}
if ! tls_ok; then
  say "self-signed certificates (own CA in $TLS/ca.pem)"
  san="DNS:$FQDN,DNS:$HOST,DNS:localhost,IP:127.0.0.1,IP:$IP"
  if [[ $PUBLIC =~ ^[0-9.]+$ ]]; then san="$san,IP:$PUBLIC"; elif [[ $PUBLIC != localhost ]]; then san="$san,DNS:$PUBLIC"; fi
  new=$(mktemp -d)
  openssl req -x509 -nodes -newkey rsa:3072 -days 3650 -subj "/CN=AD Manager demo CA ($REALM)" \
    -keyout "$new/ca.key" -out "$new/ca.pem" 2>/dev/null
  openssl req -nodes -newkey rsa:2048 -subj "/CN=$FQDN" -keyout "$new/dc.key" -out "$new/dc.csr" 2>/dev/null
  openssl x509 -req -in "$new/dc.csr" -CA "$new/ca.pem" -CAkey "$new/ca.key" -CAcreateserial -days 825 \
    -extfile <(printf 'subjectAltName=%s\nextendedKeyUsage=serverAuth\n' "$san") -out "$new/dc.crt" 2>/dev/null
  chmod 0600 "$new/ca.key" "$new/dc.key"
  mv -f "$new/ca.key" "$new/dc.key" "$new/dc.crt" "$TLS/" && mv -f "$new/ca.pem" "$TLS/" # the CA last: it marks a set
  rm -rf "$new"
  tls_ok || die "the generated certificates do not match"
fi
# Samba wants its key root-only; the panel reads its copy through its group
chmod 0600 "$TLS/ca.key" "$TLS/dc.key"
install -m 0640 -o root -g ad-manager "$TLS/dc.key" "$TLS/tls.key"
install -m 0644 "$TLS/dc.crt" "$TLS/tls.crt"
chmod 0644 "$TLS/ca.pem"

# Samba: the certificate above for LDAPS, and the registry shares the agent manages
smbconf() { # smbconf "name = value": set in [global]
  local name=${1%% =*}
  sed -i "/^\s*$name\s*=/d" /etc/samba/smb.conf
  sed -i "/^\[global\]/a\\	$1" /etc/samba/smb.conf
}
smbconf "tls enabled = yes"
smbconf "tls keyfile = $TLS/dc.key"
smbconf "tls certfile = $TLS/dc.crt"
smbconf "tls cafile = $TLS/ca.pem"
smbconf "registry shares = yes"

# ── panel and agent (this DC also serves files: docs/dc-file-server.md) ──
cat >"$CONFIG" <<CFG
# generated by docker/allinone/entrypoint.sh on every start (docs/docker.md)
[ad]
url = "ldaps://$FQDN"
domain = "$REALM"
ca_file = "$TLS/ca.pem"

[server]
listen = "0.0.0.0:8443"
cert_file = "$TLS/tls.crt"
key_file = "$TLS/tls.key"
session_minutes = 30
audit_log = "$OPT/logs/audit.log"

[db]
path = "$OPT/data/ad-manager.db"

[redis]
addr = "127.0.0.1:6379"

[agent]
listen = "0.0.0.0:9443"
cert_file = "$TLS/tls.crt"
key_file = "$TLS/tls.key"
token_file = "$OPT/data/agent.token"
queue = "fs-$HOST"
share_roots = ["/srv/shares"]
dc_file_server = true
backup_dir = "$OPT/data/backups"   # backups, sync jobs and their credentials: in the data volume
sync_unconfined = true   # no systemd in the container to confine each sync run

[samba]
log_dir = "/var/log/samba"
CFG
chown root:ad-manager "$CONFIG"
chmod 0640 "$CONFIG"
chown -R ad-manager:ad-manager "$OPT/data" "$OPT/logs"

if [[ ! -f $OPT/data/agent.token ]]; then
  # the token goes from one command to the other, never shown nor written in clear
  ad-manager agent token --config "$CONFIG" 2>/dev/null |
    runuser -u ad-manager -- ad-manager agent register-local --config "$CONFIG" --name "$HOST (DC)" >/dev/null ||
    { rm -f "$OPT/data/agent.token"; die "registering the agent in the panel failed"; }
  chmod 0600 "$OPT/data/agent.token"
  say "this DC's agent registered in the panel as $HOST (DC)"
fi

url="https://$PUBLIC:$PORT"
[[ $PORT == 443 ]] && url="https://$PUBLIC"
{
  echo
  echo "  ┌─ AD Manager ──────────────────────────────────────────────────────────"
  echo "  │ Panel:    $url"
  echo "  │ Domain:   $REALM ($NETBIOS) · DC $FQDN"
  echo "  │ User:     Administrator   (or $NETBIOS\\Administrator)"
  if [[ -n $generated ]]; then
    echo "  │ Password: $generated"
    echo "  │           generated now and shown only this once: keep it (change it in the panel under 'my password')"
  else
    echo "  │ Password: the one in AD_ADMIN_PASSWORD (or the one you changed in the panel)"
  fi
  echo "  │ CA:       docker cp <container>:$TLS/ca.pem .   (import it in the browser to remove the warning)"
  echo "  └───────────────────────────────────────────────────────────────────────"
  echo
} >&2
# the image's variables end here: the binary reads AD_* as its own settings (AD_DOMAIN would override the domain of
# the volume), and the password stays out of the environment of samba, the panel and the agent
unset generated AD_DOMAIN AD_NETBIOS AD_ADMIN_PASSWORD AD_PUBLIC_HOST AD_PUBLIC_PORT AD_DNS_FORWARDER
exec /usr/bin/supervisord -n -c /etc/supervisor/supervisord.conf
