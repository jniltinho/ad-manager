# AD Manager desktop (Go, Wails v3; openspec/changes/desktop-wails and desktop-switch). The SPA is the web panel's frontend/ in
# desktop mode (built with the host's npm, as the server's); Go builds run in Docker (scripts/go-docker.sh: Go,
# GTK4, WebKitGTK 6.0). Linux needs CGO; Windows does not.
#
#   make dist     Linux tar.gz and Windows zip (+ SHA256SUMS) into DIST
#   make check    gofmt, go vet, go test, the web build without the desktop

SHELL   := /bin/sh
.DEFAULT_GOAL := help

GO      := scripts/go-docker.sh
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
DIST    ?= dist
LDFLAGS := -s -w -X main.version=$(VERSION)
# the packages' version: the tag's number without its v (v0.27.0 → 0.27.0)
PKG_VERSION = $(patsubst v%,%,$(VERSION))
# DIST as the toolchain container sees it (the repository is mounted at /work); DIST must be inside the repository
DIST_IN_DOCKER = $(patsubst $(abspath ..)/%,/work/%,$(abspath $(DIST)))
NAME    := ad-manager-desktop

.PHONY: help frontend linux windows dist packages check test web-check shots

help: ## Show this help
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z_-]+:.*## / { printf "  %-10s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

frontend: ## The SPA in desktop mode → frontend/dist (embedded)
	cd ../frontend && npm ci --no-audit --no-fund && npm run build:desktop

linux: frontend ## bin/linux/ad-manager-desktop (production build, CGO)
	$(GO) go build -tags production -trimpath -ldflags "$(LDFLAGS)" -o bin/linux/$(NAME) .

windows: frontend ## bin/windows/ad-manager-desktop.exe (production build, no CGO)
	$(GO) env GOOS=windows CGO_ENABLED=0 go build -tags production -trimpath -ldflags "$(LDFLAGS) -H windowsgui" -o bin/windows/$(NAME).exe .

packages: linux ## .deb and .rpm of the Linux build into DIST (nfpm, in the toolchain image)
	mkdir -p $(DIST)
	$(GO) sh -c 'for p in deb rpm; do VERSION=$(PKG_VERSION) nfpm package --config packaging/linux/nfpm.yaml --packager $$p --target $(DIST_IN_DOCKER)/ || exit 1; done'

dist: linux windows packages ## Release archives, packages and SHA256SUMS into DIST
	mkdir -p $(DIST)
	tar -C bin/linux -czf $(DIST)/$(NAME)_$(PKG_VERSION)_linux_x86_64.tar.gz $(NAME) -C $(CURDIR) README.md
	rm -f $(DIST)/$(NAME)_$(PKG_VERSION)_windows_x86_64.zip
	cd bin/windows && python3 -m zipfile -c $(abspath $(DIST))/$(NAME)_$(PKG_VERSION)_windows_x86_64.zip $(NAME).exe ../../README.md
	cp bin/linux/$(NAME) $(DIST)/
	cd $(DIST) && sha256sum *.tar.gz *.zip *.deb *.rpm > SHA256SUMS

test: ## Go tests
	$(GO) go test ./...

check: web-check ## gofmt, go vet, go test, and the web build without the desktop
	$(GO) sh -c 'test -z "$$(gofmt -l $$(find . -name "*.go" -not -path "./.cache/*"))" && go vet ./... && go vet -tags smoke . && go test ./...'

web-check: ## The web build (vp build) has none of the desktop's code
	cd ../frontend && npm run build >/dev/null
	@if grep -rlE 'adm-desktop-wails-bundle|@wailsio|wails/runtime|Confiar neste certificado' ../web/dist; then echo "the web build has desktop code" >&2; exit 1; fi

shots: linux ## The docs screenshots (2x, Classic blue, a demo panel) into ../docs/screenshots (on the host: Xvfb, xdotool)
	$(GO) env CGO_ENABLED=0 go build -o .cache/demopanel ./tools/demopanel
	scripts/shots.sh bin/linux/$(NAME) .cache/demopanel ../docs/screenshots
