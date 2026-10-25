.PHONY: desktop desktop-check desktop-test desktop-shots desktop-themes server deb all frontend build build-prod test test-integration check run dev docker-build docker-allinone docker-push samba-up samba-down clean

BINARY := ad-manager
DIST   := dist

VERSION   ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILDTIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG       := ad-manager/internal/buildinfo
LDFLAGS   := -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).BuildTime=$(BUILDTIME)

all: build

# Vue SPA (Vite+) → web/dist, embedded in the binary via go:embed.
frontend:
	cd frontend && npm ci && npm run build

# Everything in dist/ (not tracked by git): server binary and the desktop app (Rust).
build: server desktop

# Server binary (panel, CLI, MCP, agent).
server: frontend
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) .

# Production: stripped and compressed with UPX (if installed).
build-prod: frontend
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w $(LDFLAGS)" -o $(DIST)/$(BINARY) .
	-command -v upx >/dev/null && upx -q --best --lzma $(DIST)/$(BINARY)

test:
	go vet ./...
	go test ./... -count=1
	cd third_party/go-msrpc && go test ./dcerpc -run 'TestBodyComplete' -count=1  # the security patch (PATCHED.md)
	cd frontend && npm run test

# Requires the test Samba AD: make samba-up (Docker) or the lab VM.
test-integration:
	go test -tags=integration ./... -count=1

check:
	gofmt -l $$(find . -name '*.go' -not -path './third_party/*' -not -path './node_modules/*' -not -path './frontend/node_modules/*') | (! grep .)  # third_party: upstream code as is
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /dev/null .  # the CLI still builds for windows (from source)
	cd frontend && npm run check

# Local panel against the AD_URL from the environment (HTTP on loopback only).
run: server
	$(DIST)/$(BINARY) server --insecure-dev

# Frontend with hot reload (proxies /api → 127.0.0.1:8080).
dev:
	cd frontend && npm run dev

docker-build:
	docker build --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILDTIME=$(BUILDTIME) -t $(BINARY):$(VERSION) -t $(BINARY):latest .

# All-in-one demo image (docs/docker.md): Samba 4 AD domain + panel + agent. docker-push sends it to Docker Hub
# (docker login first); IMAGE=other/name to publish elsewhere.
IMAGE ?= jniltinho/ad-manager
docker-allinone:
	docker build -f docker/allinone/Dockerfile --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILDTIME=$(BUILDTIME) -t $(IMAGE):$(VERSION:v%=%) -t $(IMAGE):latest .

docker-push: docker-allinone
	docker push $(IMAGE):$(VERSION:v%=%)
	docker push $(IMAGE):latest

# Test Samba AD DC (Ubuntu 26.04, distro packages) in Docker, port 6636.
samba-up:
	docker compose -f docker/compose.yml up -d --build --wait

samba-down:
	docker compose -f docker/compose.yml down -v

# Desktop app (Rust/egui, desktop/): every recipe runs in Docker (its pinned toolchain image), nothing to install
# but Docker. The Fyne app in archive/desktop-go/ is archived (kept as code only: not built, tested or released).
#   make desktop          Linux and Windows archives and the Linux binary in dist/desktop/
#   make desktop-check    what its CI runs: format, clippy, tests and cargo-deny
#   make desktop-test     unit and UI acceptance tests only
#   make desktop-shots    its docs screenshots (docs/screenshots/desktop-rs-*.png)
#   make desktop-themes   themes.json again from the web's theme CSS
# the desktop's version (desktop/Cargo.toml: the server's number), not a VERSION a command line passes down
DESKTOP_VERSION := $(shell sed -n 's/^version = "\(.*\)"$$/\1/p' desktop/Cargo.toml | head -n 1)
desktop:
	$(MAKE) -C desktop dist DIST=$(CURDIR)/$(DIST)/desktop VERSION=$(DESKTOP_VERSION)
	cp desktop/target/docker/release/ad-manager-desktop $(DIST)/desktop/
	rm -rf $(DIST)/desktop/stage-*

desktop-check desktop-test desktop-shots desktop-themes:
	$(MAKE) -C desktop $(patsubst desktop-%,%,$@)

# .deb package (nfpm) in dist/: binary, systemd units, config 0640 and docs.
deb: build-prod
	VERSION=$(patsubst v%,%,$(VERSION)) nfpm pkg --packager deb --config packaging/nfpm.yaml --target $(DIST)/

clean:
	rm -rf $(DIST) web/dist/assets web/dist/index.html web/dist/favicon.svg
