#!/bin/sh
# Run go (or any command) in the Wails desktop's toolchain image (docker/Dockerfile: Go, GTK4, WebKitGTK 6.0, Xvfb),
# built on first use. The repository root is mounted at /work; the work dir is desktop; caches stay in the tree.
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
root=$(cd "$here/.." && pwd)
image=adm-desktop-toolchain:$(sha256sum "$here/docker/Dockerfile" | cut -c1-12)
if ! docker image inspect "$image" >/dev/null 2>&1; then
    docker build -q -t "$image" "$here/docker" >/dev/null
fi
[ $# -eq 0 ] && set -- go build ./...
tty=""; [ -t 0 ] && tty="-t"
# DESKTOP_WAILS_DOCKER_ARGS adds docker run options (a published port, a read-only secrets mount).
# shellcheck disable=SC2086
exec docker run --rm -i $tty ${DESKTOP_WAILS_DOCKER_ARGS:-} --network "${DESKTOP_WAILS_DOCKER_NET:-bridge}" \
    --user "$(id -u):$(id -g)" -v "$root:/work" -w /work/desktop \
    -e HOME=/work/desktop/.cache/home -e GOPATH=/work/desktop/.cache/go \
    -e GOCACHE=/work/desktop/.cache/go-build -e GOFLAGS=-buildvcs=false \
    "$image" "$@"
