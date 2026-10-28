# AD Manager desktop (Go, Wails v3; openspec/changes/desktop-wails and desktop-switch). The SPA is the web panel's frontend/ in
# desktop mode (built with the host's npm, as the server's); Go builds run in Docker (scripts/go-docker.sh: Go,
# GTK4, WebKitGTK 6.0). Linux needs CGO; Windows does not.
#
#   make dist     Linux AppImage and Windows zip (+ SHA256SUMS) into DIST
#   make check    gofmt, go vet, go test, the web build without the desktop

SHELL   := /bin/sh
.DEFAULT_GOAL := help

GO      := scripts/go-docker.sh
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
DIST    ?= dist
LDFLAGS := -s -w -X main.version=$(VERSION)
# the files' version: the tag's number without its v (v0.27.0 → 0.27.0)
PKG_VERSION = $(patsubst v%,%,$(VERSION))
# the exe's version resource: numbers only (v0.27.0-3-gabc → 0.27.0)
WIN_VERSION = $(firstword $(subst -, ,$(PKG_VERSION)))
# DIST as the toolchain container sees it (the repository is mounted at /work); DIST must be inside the repository
DIST_IN_DOCKER = $(patsubst $(abspath ..)/%,/work/%,$(abspath $(DIST)))
NAME    := ad-manager-desktop

.PHONY: help frontend linux windows dist appimage check test web-check shots

help: ## Show this help
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z_-]+:.*## / { printf "  %-10s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

frontend: ## The SPA in desktop mode → frontend/dist (embedded)
	cd ../frontend && npm ci --no-audit --no-fund && npm run build:desktop

linux: frontend ## bin/linux/ad-manager-desktop (production build, CGO)
	$(GO) go build -tags production -trimpath -ldflags "$(LDFLAGS)" -o bin/linux/$(NAME) .

windows: frontend ## bin/windows/ad-manager-desktop.exe (production build, no CGO; icon, manifest and version)
	$(GO) go run github.com/tc-hib/go-winres@v0.3.3 simply --arch amd64 --out rsrc --manifest gui \
		--icon packaging/icon-256.png --product-name "AD Manager" --file-description "AD Manager desktop" \
		--original-filename $(NAME).exe --product-version $(WIN_VERSION) --file-version $(WIN_VERSION)
	$(GO) env GOOS=windows CGO_ENABLED=0 go build -tags production -trimpath -ldflags "$(LDFLAGS) -H windowsgui" -o bin/windows/$(NAME).exe .

# shortcut: Wails' generator fetches linuxdeploy and its GTK plugin ("continuous" builds) at each run; pin them if
# one of their releases breaks the AppImage.
appimage: linux ## The AppImage of the Linux build into DIST (GTK4 and WebKitGTK 6.0 inside: any Linux desktop)
	mkdir -p $(DIST) .cache/appimage-in .cache/appimage-out
	cp packaging/icon-256.png .cache/appimage-in/$(NAME).png
	$(GO) sh -c 'GOFLAGS= go run github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0 generate appimage -binary bin/linux/$(NAME) \
		-icon .cache/appimage-in/$(NAME).png -desktopfile packaging/linux/$(NAME).desktop -outputdir .cache/appimage-out \
		-builddir .cache/appimage'
	mv .cache/appimage-out/$(NAME)-x86_64.AppImage $(DIST)/$(NAME)_$(PKG_VERSION)_linux_x86_64.AppImage

dist: linux windows appimage ## Release AppImage, zip and SHA256SUMS into DIST
	mkdir -p $(DIST)
	rm -f $(DIST)/$(NAME)_$(PKG_VERSION)_windows_x86_64.zip
	cd bin/windows && python3 -m zipfile -c $(abspath $(DIST))/$(NAME)_$(PKG_VERSION)_windows_x86_64.zip $(NAME).exe ../../README.md
	cp bin/linux/$(NAME) $(DIST)/
	cd $(DIST) && sha256sum *.AppImage *.zip > SHA256SUMS

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
