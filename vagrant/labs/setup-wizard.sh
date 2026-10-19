#!/usr/bin/env bash
# Runs this machine's setup wizard through its API, as the browser does: sign-in with the one-time setup token,
# preview of the plan, the run, then the log until it ends. For the labs (the panel must be in setup mode: the .deb
# installed, no configuration yet).
#   sudo setup-wizard.sh '<plan as JSON>'   — the domain administrator's password on stdin when the role needs one
set -euo pipefail
plan=$1
pw=""
read -r pw || true
P=https://127.0.0.1:8443/api
J=$(mktemp)
trap 'rm -f "$J"' EXIT
H=(-sk -b "$J" -c "$J" -H 'Content-Type: application/json' -H 'Origin: https://127.0.0.1:8443')
for _ in $(seq 60); do # the panel opens its port and writes the token a few seconds after it starts
  [ -s /opt/ad-manager/data/setup-token ] && curl -sk -o /dev/null https://127.0.0.1:8443/api/health && break
  sleep 2
done
tok=$(cat /opt/ad-manager/data/setup-token)
code=$(jq -n --arg t "$tok" '{token:$t}' | curl "${H[@]}" -o /dev/null -w '%{http_code}' -X POST -d @- "$P/setup/login")
[ "${code:0:1}" = 2 ] || { echo "setup-wizard: sign-in refused ($code; is the panel in setup mode?)" >&2; exit 1; }
echo "plan: $(curl "${H[@]}" -X POST -d "$plan" "$P/setup/plan" | jq -c '{args: (.args // .commands // .), error}' | cut -c1-400)"
PW=$pw jq '. + {password: env.PW}' <<<"$plan" | curl "${H[@]}" -X POST -d @- "$P/setup/run" | jq -c .
from=0
while :; do
  st=$(curl "${H[@]}" "$P/setup/status?from=$from" || true)
  state=$(jq -r '.state // empty' <<<"$st" 2>/dev/null || true)
  if [ -z "$state" ]; then # the panel restarts configured at the end: the outcome is the last line of the setup log
    sleep 3
    [ "$(curl -sk "$P/health" | jq -r '.setup // false')" = true ] && continue
    last=$(tail -1 "$(ls -t /var/log/ad-manager/setup-*.log | head -1)")
    echo "$last"
    [ "$last" = '{"step": "ok"}' ] && state=ok || state=error
    st='{}'
    break
  fi
  jq -r '.lines[]' <<<"$st"
  from=$(jq -r '.next' <<<"$st")
  [ "$state" = running ] || [ "$state" = backup ] || break
  sleep 3
done
echo "setup-wizard: $state $(jq -r '.error // ""' <<<"$st")"
[ "$state" = ok ]
