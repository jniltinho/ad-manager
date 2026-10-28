# Shared by the lab orchestrators (lab-samba.sh, lab-windows.sh); sourced, run from vagrant/labs with LAB=1.
# Passwords come from .run/labs/secrets.env (outside git) and reach the VMs on stdin or in the environment, never in
# a process's arguments.
[ "${LAB:-}" = 1 ] || { echo "set LAB=1 (isolated lab)" >&2; exit 1; }
set -a
# shellcheck disable=SC1091
. "${LAB_SECRETS:-../../.run/labs/secrets.env}"
set +a

vm() { vagrant ssh "$1" -c "$2" 2> >(grep -v '^Connection to .* closed' >&2); } # secrets come on stdin
log() { printf '\n\033[1;34m== %s\033[0m\n' "$*"; }

# api_prelude: shell code for the VM that signs in to its panel as Administrator (password on the first stdin line)
# and defines api METHOD PATH [BODY | -]; "-" reads the body from stdin. Passwords go in JSON built from the
# environment (jq env.X) and on stdin, never in a process's arguments.
# shellcheck disable=SC2016
api_prelude='read -r PW; export PW; P=https://127.0.0.1:8443/api; J=$(mktemp); trap "rm -f $J" EXIT
H=(-sk -b $J -c $J -H "Content-Type: application/json" -H "Origin: https://127.0.0.1:8443")
jq -n "{user:\"Administrator\",password:env.PW}" | curl "${H[@]}" -X POST --data-binary @- $P/login >/dev/null
api() { if [ "${3:-}" = - ]; then curl "${H[@]}" -X "$1" --data-binary @- "$P$2"; else curl "${H[@]}" -X "$1" ${3:+-d "$3"} "$P$2"; fi; }'

# install MACHINE...: as docs/installation.md — the newest .deb of dist/, then the panel (in setup mode) and the agent
install() {
  local deb m
  deb=$(cd ../../dist && ls -t ad-manager_*_amd64.deb | head -1)
  for m in "$@"; do
    vm "$m" "command -v jq >/dev/null || sudo apt-get install -y -qq jq >/dev/null; sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --reinstall /vagrant/dist/$deb >/dev/null; sudo systemctl enable --now ad-manager ad-manager-agent >/dev/null 2>&1; echo \"$m: \$(dpkg-query -W -f='\${Version}' ad-manager)\""
  done
}

# wizard MACHINE PASSWORD PLAN: the setup wizard through its API (a machine already set up is skipped); the log in $OUT
wizard() {
  log "setup $1"
  if vm "$1" 'sudo test -s /opt/ad-manager/config.toml && ! sudo grep -q "^setup *= *true" /opt/ad-manager/config.toml'; then
    echo "$1 already set up"
    return
  fi
  # a wizard run again (the lab): a spent setup token is renewed as docs/installation.md says
  vm "$1" 'if sudo test -e /opt/ad-manager/data/setup-token.used; then sudo rm -f /opt/ad-manager/data/setup-token.used; sudo systemctl restart ad-manager; fi'
  printf '%s\n' "$2" | vm "$1" "sudo bash /vagrant/vagrant/labs/setup-wizard.sh '$3'" | tee "$OUT/setup-$1.log" | grep -E '^plan:|setup-wizard:|ERRO' || true
  grep -q 'setup-wizard: ok' "$OUT/setup-$1.log"
}

# register PANEL-MACHINE PASSWORD FS-MACHINE NAME ADDRESS: the file server's agent in its domain's panel (the token
# the setup wrote once, root only)
register() {
  local tok
  tok=$(vm "$3" 'sudo cat /opt/ad-manager/data/agent-token-once' | tr -d '\r\n')
  printf '%s\n%s\n' "$2" "$tok" | vm "$1" "$api_prelude
read -r TOK; export TOK; api GET /agents | jq -e '.[]|select(.name==\"$4\")' >/dev/null && { echo '$4 already registered'; exit 0; }
jq -n '{name:\"$4\",url:\"https://$5:9443\",token:env.TOK,insecure:true}' | api POST /agents - | jq -c '{id,name,error}'"
}

# check_syncs LOG: every "sync SHARE: {...}" line of a path A sync log ended "ok" (a failed sync once went unnoticed:
# the validation only printed the different hashes)
check_syncs() {
  awk '/^sync / { n++; if ($0 !~ /"status":"ok"/) { print "SYNC FAILED: " $0; bad=1 } } END { if (!n) { print "no sync ran"; bad=1 }; exit bad }' "$1" >&2
}

# check_report REPORT: a path A validation fails on what it would otherwise only print — different file hashes, an
# SMB result other than the expected one, a share whose access differs, attribute differences, a failed kinit
check_report() {
  awk '/: source [0-9a-f]+, target/ { split($0, a, "source "); split(a[2], b, ", target "); if (b[1] != b[2]) { print "FILES DIFFER: " $0; bad=1 } }
       /expected ok\): / && !/expected ok\): ok$/ { print "UNEXPECTED: " $0; bad=1 }
       /expected ACCESS_DENIED\): / && !/NT_STATUS_ACCESS_DENIED$/ { print "UNEXPECTED: " $0; bad=1 }
       /\| no \|$/ || /DIFFERENCES/ || /: FAILED/ { print "FAILED: " $0; bad=1 }
       END { exit bad }' "$1" >&2
}

# up_retry MACHINE...: vagrant up each machine; one whose guest kernel hangs on its first boot (RCU stalls before SSH,
# seen with VirtualBox 7.2 and the Ubuntu 26.04 kernel) is power-cycled once with its provisioners run again
up_retry() {
  local m
  for m in "$@"; do
    vagrant up "$m" || { echo "$m: the first boot hung, power-cycling it" >&2; vagrant reload --provision "$m"; }
  done
}
