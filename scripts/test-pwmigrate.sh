#!/usr/bin/env bash
# The password migration (new-domain cutover) in Go against real DCs: a source DC and a destination Samba DC of the
# same realm (a new domain, its own SID), the users imported on the destination with other passwords. It applies the
# migration with internal/sambadc (TestMigratePasswordsLive, run as root in the destination), then checks that the
# users sign in to the destination with their source passwords (NTLM, and Kerberos when the keys transferred), that a
# second run changes nothing, and that a user without rights is refused.
#
# The source is a throwaway Samba DC on an internal Docker network, or an external DC (a Windows lab DC) given by
#   SRC_HOST, SRC_REALM, SRC_DOMAIN, NET   its address, realm, NetBIOS name and the Docker network that reaches it
#   SRC_ADMIN_FILE                         a 0600 file with its Administrator password
#   SRC_USERS_ENV                          a 0600 file of name=password lines: the users to migrate and check; the
#                                          one named "plain" has no replication rights
# PARITY=1 also migrates a twin destination with the previous Python helper (pwmigrate.py of git REF, default HEAD)
# and compares, user by user, the outcomes and the stored unicodePwd and supplementalCredentials (as hashes).
#   IMAGE=ubuntu:26.04 PARITY=1 scripts/test-pwmigrate.sh
set -euo pipefail
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
base=${IMAGE:-ubuntu:26.04}
tag=$(echo "$base" | tr ':.' '--')
work=$(mktemp -d)
trap 'docker rm -f $(cat "$work/containers" 2>/dev/null) >/dev/null 2>&1 || true; docker network rm $(cat "$work/networks" 2>/dev/null) >/dev/null 2>&1 || true; rm -rf "$work"' EXIT
rnd() { echo "Pw-$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')-x9Q"; }
ADMIN_PASS=$(rnd) # the destinations' Administrator (and the throwaway source's)
export ADMIN_PASS
declare -A pw
if [ -n "${SRC_HOST:-}" ]; then
	realm=$SRC_REALM nb=$SRC_DOMAIN net=$NET srcip=$SRC_HOST
	src_admin=$(cat "$SRC_ADMIN_FILE")
	while IFS='=' read -r k v; do [ -n "$k" ] && pw[$k]=$v; done <"$SRC_USERS_ENV"
	users=$(printf '%s\n' "${!pw[@]}" | grep -vx plain | sort | tr '\n' ' ')
	dcs="dstgo"
else
	realm=PARITY.TEST nb=PARITY net="adm-pw-$tag" src_admin=$ADMIN_PASS
	users="ana bia caio"
	for u in $users plain; do pw[$u]=$(rnd); done
	dcs="src dstgo"
fi
dom=$(echo "$realm" | tr '[:upper:]' '[:lower:]')
basedn=DC=${dom//./,DC=}
[ "${PARITY:-}" = 1 ] && dcs="$dcs dstpy"

(cd "$root" && CGO_ENABLED=0 go test -c -tags integration -o "$work/pw.test" ./internal/sambadc)
docker build -q --build-arg BASE="$base" -t "ad-manager-samba-test:$tag" "$root/docker/samba" >/dev/null
if [ -z "${SRC_HOST:-}" ]; then docker network create --internal "$net" >/dev/null && echo "$net" >>"$work/networks"; fi
for dc in $dcs; do
	docker run -d --name "adm-pw-$dc-$tag" --network "$net" --hostname "$dc" --domainname "$dom" -e REALM="$realm" \
		-e DOMAIN="$nb" -e ADMIN_PASS --cap-add SYS_ADMIN --tmpfs /run "ad-manager-samba-test:$tag" >/dev/null
	echo "adm-pw-$dc-$tag" >>"$work/containers"
done
for dc in $dcs; do
	i=0
	until [ "$(docker inspect -f '{{.State.Health.Status}}' "adm-pw-$dc-$tag")" = healthy ]; do
		i=$((i + 1)); [ $i -lt 150 ] || { docker logs "adm-pw-$dc-$tag" | tail -20; exit 1; }; sleep 2
	done
done
ip() { docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "adm-pw-$1-$tag"; }
x() { local dc=$1; shift; docker exec -i "adm-pw-$dc-$tag" "$@"; }
nopw() { printf 'dn: CN=nopw,CN=Users,%s\nobjectClass: user\nsAMAccountName: nopw\n' "$basedn" | x "$1" ldbadd -H /var/lib/samba/private/sam.ldb >/dev/null; }
if [ -z "${SRC_HOST:-}" ]; then
	# the test users' passwords are random and live only in these throwaway containers
	for u in $users plain; do x src samba-tool user create "$u" "${pw[$u]}" >/dev/null; done
	nopw src
	x src samba-tool user create onlysrc "$(rnd)" >/dev/null
	srcip=$(ip src)
fi
for dc in ${dcs#src }; do
	for u in $users plain; do x "$dc" samba-tool user create "$u" "$(rnd)" >/dev/null; done
	nopw "$dc"
done
sid() { x "$1" ldbsearch -H /var/lib/samba/private/sam.ldb -s base -b "$basedn" objectSid | sed -n 's/^objectSid: //p'; }
docker cp "$work/pw.test" "adm-pw-dstgo-$tag:/root/pw.test" >/dev/null
gorun() { # gorun APPLY [USER PASSWORD]: the Go migration in dstgo, its JSON result
	printf 'PW_SRC_HOST=%s\nPW_SRC_USER=%s\nPW_SRC_PASSWORD=%s\nPW_DEST_SID=%s\nPW_APPLY=%s\n' "$srcip" "${2:-$nb\\Administrator}" \
		"${3:-$src_admin}" "$(sid dstgo)" "$1" >"$work/env"
	local rc=0
	docker exec --env-file "$work/env" "adm-pw-dstgo-$tag" /root/pw.test -test.run TestMigratePasswordsLive -test.v \
		-test.count=1 >"$work/run.log" 2>&1 || rc=$?
	rm -f "$work/env"
	sed -n 's/.*RESULT //p' "$work/run.log"
	[ $rc = 0 ] || { grep -vE 'RESULT' "$work/run.log" | tail -5 >&2; return $rc; }
}
outcomes() { jq -c '[.users[]|{(.sam):.outcome}]|add'; }
echo "== $base: plan"; gorun 0 | outcomes
echo "== apply"; gorun 1 | tee "$work/go.json" | jq -r '.users[]|"\(.sam): \(.outcome)\(if .reason then " — "+.reason else "" end)"'
if [ -z "${SRC_HOST:-}" ]; then
	expect='{"ana":"set","bia":"set","caio":"set","nopw":"no_password","onlysrc":"missing","plain":"set"}'
	[ "$(outcomes <"$work/go.json")" = "$expect" ] || { echo "unexpected outcomes" >&2; exit 1; }
fi
for u in $users; do
	o=$(jq -r --arg u "$u" '.users[]|select(.sam==$u)|.outcome' "$work/go.json")
	[ "$o" = set ] || [ "$o" = ntlm_only ] || { echo "$u: $o" >&2; exit 1; }
done
echo "== the source passwords on the destination"
cat >"$work/check.sh" <<'CHK'
read -r P
u=$1 realm=$2 nb=$3 want=$4
PASSWD=$P smbclient //127.0.0.1/netlogon -U "$nb\\$u" -c ls >/dev/null 2>&1 && n=ok || n=FAIL
k=FAIL
# this DC's own KDC (the container's DNS is Docker's, which knows no realm)
printf '[libdefaults]\n default_realm = %s\n dns_lookup_kdc = false\n[realms]\n %s = {\n  kdc = 127.0.0.1\n }\n' "$realm" "$realm" >/tmp/krb5.conf
if printf '%s\n' "$P" | KRB5_CONFIG=/tmp/krb5.conf kinit "$u@$realm" >/tmp/kinit.log 2>&1; then
	k=$(KRB5_CONFIG=/tmp/krb5.conf klist -e | grep -m1 -A1 krbtgt | tail -1 | sed 's/.*tkt): //')
fi
echo "$u ($want): NTLM $n, Kerberos $k"
# NTLM always works with the source password; Kerberos when the keys transferred (set)
[ "$n" = ok ] && { [ "$want" != set ] || [ "$k" != FAIL ]; }
CHK
docker cp "$work/check.sh" "adm-pw-dstgo-$tag:/root/check.sh" >/dev/null
for u in $users; do
	printf '%s\n' "${pw[$u]}" | x dstgo bash /root/check.sh "$u" "$realm" "$nb" "$(jq -r --arg u "$u" '.users[]|select(.sam==$u)|.outcome' "$work/go.json")"
done
echo "== apply again"; again=$(gorun 1 | outcomes)
echo "$again"
want=$(outcomes <"$work/go.json" | sed 's/"set"/"same"/g; s/"ntlm_only"/"same"/g')
[ "$again" = "$want" ] || { echo "the second run changed something (want $want)" >&2; exit 1; }
echo "== an account without replication rights is refused"
if gorun 1 "$nb\\plain" "${pw[plain]}" >/dev/null 2>&1; then echo "accepted" >&2; exit 1; fi
echo "refused"
if [ "${PARITY:-}" = 1 ]; then
	echo "== parity with the Python helper (git ${REF:-HEAD})"
	git -C "$root" show "${REF:-HEAD}:internal/sambadc/pwmigrate.py" >"$work/pwmigrate.py"
	docker cp "$work/pwmigrate.py" "adm-pw-dstpy-$tag:/root/pwmigrate.py" >/dev/null
	(umask 077; printf '%s' "$src_admin" >"$work/src.pass") # to jq by file, never an argument
	jq -n --arg h "$srcip" --arg u "$nb\\Administrator" --rawfile p "$work/src.pass" --arg s "$(sid dstpy)" \
		'{op:"apply",source:{host:$h,user:$u,password:$p},dest_sid:$s}' |
		x dstpy bash -c 'python3 -I -s -B -c "$(cat /root/pwmigrate.py)"' | jq -c '.result' >"$work/py.json"
	echo "python: $(outcomes <"$work/py.json")"
	[ "$(outcomes <"$work/py.json")" = "$(outcomes <"$work/go.json")" ] || { echo "the Python outcomes differ" >&2; exit 1; }
	material() { # sha256 of each user's stored unicodePwd and supplementalCredentials
		for u in $users plain nopw; do
			# LDIF folds long values: unfold before keeping the two attributes, so the whole values are compared
			x "$1" ldbsearch -H /var/lib/samba/private/sam.ldb "(sAMAccountName=$u)" unicodePwd supplementalCredentials |
				awk '/^ / { l = l substr($0, 2); next } { if (l != "") print l; l = $0 } END { if (l != "") print l }' |
				{ grep -E '^(unicodePwd|supplementalCredentials)::' || true; } | sort | sha256sum | cut -c1-16 | sed "s/^/$u /"
		done
	}
	material dstgo >"$work/m-go"; material dstpy >"$work/m-py"
	if diff "$work/m-go" "$work/m-py"; then echo "stored material: the same for every user"; else echo "stored material differs" >&2; exit 1; fi
fi
x dstgo samba --version
echo "ok"
