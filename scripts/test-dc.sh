#!/bin/sh
# Integration tests of what replaces python3-samba for the Entra sync — internal/gosamba/drs (DRSUAPI hash read)
# and internal/ad's sync reads (LDAPS paged search, naming contexts) — against a throwaway Samba AD DC built from
# docker/samba on each base image (Ubuntu 26.04 and Debian 13). The DC and the test run on an internal Docker
# network (no route out): the endpoint mapper's dynamic port is reachable there, which a host port map is not.
# The test binary runs in a container of the same base image, so it is also checked on that distribution.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
pass=${AD_TEST_PASSWORD:-Teste-ad-manager-2026} # the throwaway DC's, as docker/compose.yml
ppw=Plain-$(od -An -N6 -tx1 /dev/urandom | tr -d ' \n')-x9Q # the unprivileged fixture user's, new on each run
work=$(mktemp -d)
trap 'docker rm -f $(cat "$work/containers" 2>/dev/null) >/dev/null 2>&1 || true; docker network rm $(cat "$work/networks" 2>/dev/null) >/dev/null 2>&1 || true; rm -rf "$work"' EXIT

(cd "$root" && CGO_ENABLED=0 go test -c -tags integration -o "$work/drs.test" ./internal/gosamba/drs &&
	CGO_ENABLED=0 go test -c -tags integration -o "$work/ad.test" ./internal/ad)

for base in ${IMAGES:-ubuntu:26.04 debian:13}; do
	tag=$(echo "$base" | tr ':.' '--')
	echo "== $base"
	docker build -q --build-arg BASE="$base" -t "ad-manager-samba-test:$tag" "$root/docker/samba" >/dev/null
	net="adm-drs-$tag"
	docker network create --internal "$net" >/dev/null && echo "$net" >>"$work/networks"
	dc="adm-drs-dc-$tag"
	docker run -d --name "$dc" --network "$net" --hostname dc1 --domainname empresa.test -e ADMIN_PASS="$pass" \
		--cap-add SYS_ADMIN --tmpfs /run "ad-manager-samba-test:$tag" >/dev/null && echo "$dc" >>"$work/containers"
	i=0
	until [ "$(docker inspect -f '{{.State.Health.Status}}' "$dc")" = healthy ]; do
		i=$((i + 1)); [ $i -lt 120 ] || { docker logs "$dc" | tail -30; exit 1; }; sleep 2
	done
	ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$dc")
	x() { docker exec "$dc" "$@"; }
	x samba-tool user create plain "$ppw" >/dev/null
	printf 'dn: CN=nopw,CN=Users,DC=empresa,DC=test\nobjectClass: user\nsAMAccountName: nopw\n' |
		docker exec -i "$dc" ldbadd -H /var/lib/samba/private/sam.ldb >/dev/null
	guid() { x ldbsearch -H /var/lib/samba/private/sam.ldb "(sAMAccountName=$1)" objectGUID | sed -n 's/^objectGUID: //p'; }
	nt=$(x samba-tool user getpassword Administrator --attributes=unicodePwd | sed -n 's/^unicodePwd:: //p')
	admin=$(guid Administrator) nopw=$(guid nopw)
	[ -n "$nt" ] && [ -n "$admin" ] && [ -n "$nopw" ] || { echo "fixtures missing on $base" >&2; exit 1; }
	docker run --rm --network "$net" -v "$work/drs.test:/drs.test:ro" \
		-e DRS_TEST_ADDR="$ip" -e DRS_TEST_HOST=dc1.empresa.test -e DRS_TEST_REALM=EMPRESA.TEST \
		-e DRS_TEST_PASSWORD="$pass" -e DRS_TEST_ADMIN_GUID="$admin" -e DRS_TEST_ADMIN_NT="$nt" \
		-e DRS_TEST_NOPW_GUID="$nopw" -e DRS_TEST_PLAIN_USER=plain -e DRS_TEST_PLAIN_PASSWORD="$ppw" \
		"$base" /drs.test -test.v -test.count=1 -test.run 'TestReadNTHash|TestWithoutReplicationRights|TestWrongPassword'
	docker run --rm --network "$net" -v "$work/ad.test:/ad.test:ro" \
		-e AD_TEST_URL="ldaps://$ip:636" -e AD_TEST_DOMAIN=empresa.test -e AD_TEST_PASSWORD="$pass" \
		"$base" /ad.test -test.v -test.count=1 -test.run 'TestSearchForSync'
	x samba --version
done
