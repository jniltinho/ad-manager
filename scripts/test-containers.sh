#!/bin/sh
# Runs the tests of internal/gosamba and internal/entrasync (the Go replacement for python3-samba and the
# AzureADConnect_Samba4 port) in Ubuntu 26.04 and Debian 13 containers, with the host's Go toolchain and module
# cache mounted read-only and no network: they must build and pass with nothing installed from the distribution
# (no python3-samba, no Samba libraries).
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
goroot=$(go env GOROOT)
modcache=$(go env GOMODCACHE)
for image in ${IMAGES:-ubuntu:26.04 debian:13}; do
	echo "== $image"
	docker run --rm --network none -u "$(id -u):$(id -g)" \
		-v "$goroot:/usr/local/go:ro" -v "$modcache:/gomodcache:ro" -v "$root:/src:ro" -w /src \
		-e HOME=/tmp -e GOCACHE=/tmp/gocache -e GOMODCACHE=/gomodcache -e GOPROXY=off -e GOFLAGS=-mod=readonly \
		-e GOTOOLCHAIN=local -e CGO_ENABLED=0 \
		"$image" sh -c 'head -1 /etc/os-release && /usr/local/go/bin/go vet ./internal/gosamba/... ./internal/entrasync/... &&
			/usr/local/go/bin/go test -count=1 ./internal/gosamba/... ./internal/entrasync/...'
done
