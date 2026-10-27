# Toolchain of the Wails desktop (desktop-wails/): Go, GTK4 + WebKitGTK 6.0 (Linux builds, CGO) and Xvfb for the
# native smoke test. Ubuntu 24.04 is the oldest supported ABI (glibc 2.39). Windows builds need no CGO: same image.
FROM docker.io/library/ubuntu:24.04@sha256:24737cef17f4e22b1977df7d9a05e77c4e28b7da5b8911f8d8ccda136bf3573c

ARG GO_VERSION=1.27.3
ARG GO_SHA256=65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40

RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        ca-certificates curl build-essential pkg-config libgtk-4-dev libwebkitgtk-6.0-dev \
        xvfb xauth dbus dbus-x11 at-spi2-core fonts-dejavu-core imagemagick xdotool \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tgz \
    && echo "${GO_SHA256}  /tmp/go.tgz" | sha256sum --check --strict - \
    && tar -C /usr/local -xzf /tmp/go.tgz && rm /tmp/go.tgz

# nfpm: the .deb and .rpm (make packages)
ARG NFPM_VERSION=2.48.0
ARG NFPM_SHA256=6d7f9a28b2f397934403950fcbba83f598b08d02edbe79a0815b3b4897341750
RUN curl -fsSL "https://github.com/goreleaser/nfpm/releases/download/v${NFPM_VERSION}/nfpm_${NFPM_VERSION}_Linux_x86_64.tar.gz" -o /tmp/nfpm.tgz \
    && echo "${NFPM_SHA256}  /tmp/nfpm.tgz" | sha256sum --check --strict - \
    && tar -C /usr/local/bin -xzf /tmp/nfpm.tgz nfpm && rm /tmp/nfpm.tgz

ENV PATH=/usr/local/go/bin:$PATH
