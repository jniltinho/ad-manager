# AD Manager desktop in Rust (egui). Every recipe runs in Docker (scripts/cargo-docker.sh, the pinned rustXterm
# toolchain image): nothing here needs a host Rust toolchain.
#
#   make help          list targets
#   make check         what CI runs: fmt-check, clippy, test, deny
#   make dist          release archives for Linux and Windows into dist/

SHELL := /bin/sh
.DEFAULT_GOAL := help

CARGO   := scripts/cargo-docker.sh cargo
VERSION := $(shell sed -n 's/^version = "\(.*\)"$$/\1/p' Cargo.toml | head -n 1)
DIST    ?= dist
TARGET  := target/docker

DENY        := cargo-deny-0.20.2-x86_64-unknown-linux-musl
DENY_SHA256 := 9f12ed4c49936e09b48bf862b595cde2fe64fcbd9d74dfacac6131ca824c8d5f

.PHONY: help build release run test fmt fmt-check clippy deny check shots mock bench dist dist-linux dist-windows themes catalog

help: ## Show this help
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z_-]+:.*## / { printf "  %-12s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

build: ## Debug build of the workspace
	$(CARGO) build --workspace

release: ## Release build (target/docker/release/ad-manager-desktop-rs)
	$(CARGO) build --release --locked -p ad-manager-desktop-rs

test: ## Unit and UI acceptance tests (the Fyne sources are compared when present)
	ADM_REQUIRE_FYNE=1 $(CARGO) test --workspace --locked

fmt: ## Format the workspace
	$(CARGO) fmt --all

fmt-check: ## Check formatting
	$(CARGO) fmt --all --check

clippy: ## Lint, warnings are errors
	$(CARGO) clippy --workspace --all-targets --locked -- -D warnings

deny: ## cargo deny (licenses, advisories, sources) with the pinned, SHA-256 checked binary of CI
	@set -eu; \
	if [ ! -x target/tools/cargo-deny ]; then \
		mkdir -p target/tools; \
		curl --fail --show-error --silent --location --retry 3 \
			"https://github.com/EmbarkStudios/cargo-deny/releases/download/0.20.2/$(DENY).tar.gz" -o target/tools/deny.tgz; \
		echo "$(DENY_SHA256)  target/tools/deny.tgz" | sha256sum --check --strict -; \
		tar -xzf target/tools/deny.tgz -C target/tools --strip-components=1 "$(DENY)/cargo-deny"; \
		rm -f target/tools/deny.tgz; \
	fi
	scripts/cargo-docker.sh target/tools/cargo-deny check

check: fmt-check clippy test deny ## What CI runs

shots: ## The docs screenshots (2x, Classic blue, domain.local) into ../docs/screenshots
	DESKTOP_RUST_DOCKER_ARGS="--add-host panel.domain.local:127.0.0.1" ADM_SHOTS=/work/docs/screenshots ADM_SHOTS_HOST=panel.domain.local \
		$(CARGO) test -p adm-ui --test ui --locked shots -- --ignored

mock: ## A mock panel with 5,000 users on https://127.0.0.1:8443 (prints the URL and pin)
	DESKTOP_RUST_DOCKER_ARGS="-p 127.0.0.1:8443:8443" $(CARGO) run -p adm-core --example mock_panel --features testkit --locked -- --users 5000 --port 8443

bench: ## Benchmark against the Fyne app (bench/run.sh; needs both release binaries)
	bench/run.sh

themes: ## Regenerate assets/themes.json from desktop/themes_gen.go
	python3 scripts/gen-themes.py

catalog: ## Pull new pt-BR entries from desktop/i18n_pt_*.go
	python3 scripts/gen-catalog.py

dist: dist-linux dist-windows ## Release archives (Linux tar.gz, Windows zip) and SHA256SUMS into DIST

dist-linux: release
	mkdir -p $(DIST)/stage-linux
	cp $(TARGET)/release/ad-manager-desktop-rs README.md CHANGELOG.md $(DIST)/stage-linux/
	tar -C $(DIST)/stage-linux -czf $(DIST)/ad-manager-desktop-rs_$(VERSION)_linux_x86_64.tar.gz .
	cd $(DIST) && sha256sum *.tar.gz *.zip 2>/dev/null > SHA256SUMS || true

dist-windows:
	DESKTOP_RUST_TOOLCHAIN=windows $(CARGO) build --release --locked -p ad-manager-desktop-rs --target x86_64-pc-windows-gnu
	mkdir -p $(DIST)/stage-windows
	cp $(TARGET)/x86_64-pc-windows-gnu/release/ad-manager-desktop-rs.exe README.md CHANGELOG.md $(DIST)/stage-windows/
	cd $(DIST)/stage-windows && zip -q -r ../ad-manager-desktop-rs_$(VERSION)_windows_x86_64.zip .
	cd $(DIST) && sha256sum *.tar.gz *.zip 2>/dev/null > SHA256SUMS || true
