#!/bin/sh
# Run cargo (or any command) inside the pinned toolchain image of rustXterm (Rust, mesa, Xvfb), the same one CI uses.
# The repository root is mounted (tests read ../desktop for the catalog and theme checks); the work dir is desktop-rust.
# DESKTOP_RUST_TOOLCHAIN=windows uses the image with the x86_64-pc-windows-gnu cross toolchain.
set -eu
here=$(cd "$(dirname "$0")/.." && pwd)
root=$(cd "$here/.." && pwd)
image=rustxterm-toolchain:dev
digest=docker.io/jniltinho/rustxterm-toolchain:snapshot-20261001@sha256:6872535a844a568c5fe8bf05c52c7a0adfa2370fce8990be511ff056ffdb9c70
if [ "${DESKTOP_RUST_TOOLCHAIN:-}" = windows ]; then
    image=rustxterm-windows-toolchain:dev
    digest=docker.io/jniltinho/rustxterm-windows-toolchain:snapshot-20261001@sha256:7b9892dc2cc56819f4ee1002a2eb7381b31cebcff9166716b02e9cbd2ebbb5e0
fi
if ! docker image inspect "$image" >/dev/null 2>&1; then
    docker pull -q "$digest" >/dev/null
    docker tag "$digest" "$image"
fi
[ $# -eq 0 ] && set -- cargo build
tty=""; [ -t 0 ] && tty="-t"
# DESKTOP_RUST_DOCKER_ARGS adds docker run options (the screenshots map panel.domain.local to 127.0.0.1).
# shellcheck disable=SC2086
exec docker run --rm -i $tty ${DESKTOP_RUST_DOCKER_ARGS:-} --network "${DESKTOP_RUST_DOCKER_NET:-bridge}" --user "$(id -u):$(id -g)" \
    -v "$root:/work" -w /work/desktop-rust \
    -e HOME=/work/desktop-rust/target/home -e CARGO_HOME=/work/desktop-rust/target/cargo-home \
    -e CARGO_TARGET_DIR="${CARGO_TARGET_DIR:-target/docker}" -e CARGO_INCREMENTAL="${CARGO_INCREMENTAL:-0}" \
    -e RUST_BACKTRACE=1 -e RUST_LOG -e ADM_REQUIRE_FYNE -e ADM_SHOTS -e ADM_SHOTS_HOST \
    "$image" "$@"
