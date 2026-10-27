# AD Manager desktop on Wails v3 (preview, openspec/changes/desktop-wails). The SPA is the web panel's frontend/ in
# desktop mode (built with the host's npm, as the server's); Go builds run in Docker (scripts/go-docker.sh: Go,
# GTK4, WebKitGTK 6.0). Linux needs CGO; Windows does not.
#
#   make dist     Linux tar.gz and Windows zip (+ SHA256SUMS) into DIST
#   make check    gofmt, go vet, go test

SHELL   := /bin/sh
.DEFAULT_GOAL := help

GO      := scripts/go-docker.sh
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
DIST    ?= dist
LDFLAGS := -s -w -X main.version=$(VERSION)
NAME    := ad-manager-desktop-wails

.PHONY: help frontend linux windows dist check test

help: ## Show this help
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z_-]+:.*## / { printf "  %-10s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

frontend: ## The SPA in desktop mode → frontend/dist (embedded)
	cd ../frontend && npm ci --no-audit --no-fund && npm run build:desktop

linux: frontend ## bin/linux/ad-manager-desktop-wails (production build, CGO)
	$(GO) go build -tags production -trimpath -ldflags "$(LDFLAGS)" -o bin/linux/$(NAME) .

windows: frontend ## bin/windows/ad-manager-desktop-wails.exe (production build, no CGO)
	$(GO) env GOOS=windows CGO_ENABLED=0 go build -tags production -trimpath -ldflags "$(LDFLAGS) -H windowsgui" -o bin/windows/$(NAME).exe .

dist: linux windows ## Release archives and SHA256SUMS into DIST
	mkdir -p $(DIST)
	tar -C bin/linux -czf $(DIST)/$(NAME)_$(VERSION)_linux_x86_64.tar.gz $(NAME) -C $(CURDIR) README.md
	rm -f $(DIST)/$(NAME)_$(VERSION)_windows_x86_64.zip
	cd bin/windows && python3 -m zipfile -c $(abspath $(DIST))/$(NAME)_$(VERSION)_windows_x86_64.zip $(NAME).exe ../../README.md
	cp bin/linux/$(NAME) $(DIST)/
	cd $(DIST) && sha256sum *.tar.gz *.zip > SHA256SUMS

test: ## Go tests
	$(GO) go test ./...

check: ## gofmt, go vet, go test
	$(GO) sh -c 'test -z "$$(gofmt -l $$(find . -name "*.go" -not -path "./.cache/*"))" && go vet ./... && go test ./...'
