# Desktop app (Fyne) for macOS: Intel (amd64), Apple Silicon (arm64), a universal
# binary and an .app bundle. Run on a Mac with Go and the Xcode Command Line
# Tools (xcode-select --install):
#
#   cd desktop && make mac        # everything below
#   make mac-arm64                # Apple Silicon only
#   make mac-amd64                # Intel only
#
# Output (dist/ is not tracked by git):
#   dist/desktop/darwin-arm64/ad-manager-desktop
#   dist/desktop/darwin-amd64/ad-manager-desktop
#   dist/desktop/darwin-universal/ad-manager-desktop      (lipo: both architectures)
#   dist/desktop/darwin-universal/ad-manager-desktop.app  (double-click; ad-hoc signed)
#   dist/ad-manager-desktop_<version>_darwin_{arm64,amd64,universal}.zip
#
# From Linux it only works with Apple's SDK copied from a Mac
# (MACOS_SDK=/path/MacOSX.sdk, cross-compiled with zig) — Xcode's license does not
# allow getting the SDK anywhere else.

.PHONY: mac mac-arm64 mac-amd64 mac-universal mac-app mac-zip help

VERSION   ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
OUT       ?= $(abspath ../dist/desktop)
ZIPS      ?= $(abspath ../dist)
MIN_MACOS ?= 12.0
MACOS_SDK ?=
ZIG       ?= $(shell command -v zig 2>/dev/null || echo /usr/local/zig/zig)
BIN       := ad-manager-desktop
APP       := $(OUT)/darwin-universal/$(BIN).app
LDFLAGS   := -s -w -X main.version=$(VERSION)
OS        := $(shell uname -s)

# compiler per architecture: Apple clang on a Mac, zig + SDK elsewhere
ifeq ($(OS),Darwin)
CC_arm64 := clang -arch arm64 -mmacosx-version-min=$(MIN_MACOS)
CC_amd64 := clang -arch x86_64 -mmacosx-version-min=$(MIN_MACOS)
else ifneq ($(MACOS_SDK),)
ZIG_SDK  := --sysroot=$(MACOS_SDK) -I$(MACOS_SDK)/usr/include -F$(MACOS_SDK)/System/Library/Frameworks -L$(MACOS_SDK)/usr/lib
CC_arm64 := $(ZIG) cc -target aarch64-macos.$(MIN_MACOS) $(ZIG_SDK)
CC_amd64 := $(ZIG) cc -target x86_64-macos.$(MIN_MACOS) $(ZIG_SDK)
endif

help:
	@sed -n '1,19p' Makefile | sed 's/^# \{0,1\}//'

mac: mac-app mac-zip

define build_arch
	@test -n "$(CC_$(1))" || { echo "macOS: run on a Mac (Xcode Command Line Tools) or set MACOS_SDK=/path/MacOSX.sdk"; exit 1; }
	mkdir -p $(OUT)/darwin-$(1)
	CGO_ENABLED=1 GOOS=darwin GOARCH=$(1) CC="$(CC_$(1))" \
		CGO_CFLAGS="-mmacosx-version-min=$(MIN_MACOS)" CGO_LDFLAGS="-mmacosx-version-min=$(MIN_MACOS)" \
		go build -trimpath -ldflags "$(LDFLAGS)" -o $(OUT)/darwin-$(1)/$(BIN) .
endef

mac-arm64:
	$(call build_arch,arm64)

mac-amd64:
	$(call build_arch,amd64)

# one binary that runs on both Intel and Apple Silicon
mac-universal: mac-arm64 mac-amd64
	mkdir -p $(OUT)/darwin-universal
	lipo -create -output $(OUT)/darwin-universal/$(BIN) $(OUT)/darwin-arm64/$(BIN) $(OUT)/darwin-amd64/$(BIN)
	lipo -info $(OUT)/darwin-universal/$(BIN)

# .app bundle with icon (assets/icon.png → .icns), ad-hoc signed so Gatekeeper
# accepts it on the machine that built it (to distribute, sign with a Developer ID).
mac-app: mac-universal
	rm -rf "$(APP)" $(OUT)/icon.iconset
	mkdir -p "$(APP)/Contents/MacOS" "$(APP)/Contents/Resources" $(OUT)/icon.iconset
	cp $(OUT)/darwin-universal/$(BIN) "$(APP)/Contents/MacOS/$(BIN)"
	for s in 16 32 128 256 512; do \
		sips -z $$s $$s assets/icon.png --out $(OUT)/icon.iconset/icon_$${s}x$${s}.png >/dev/null; \
		sips -z $$((s*2)) $$((s*2)) assets/icon.png --out $(OUT)/icon.iconset/icon_$${s}x$${s}@2x.png >/dev/null; \
	done
	iconutil -c icns $(OUT)/icon.iconset -o "$(APP)/Contents/Resources/icon.icns"
	rm -rf $(OUT)/icon.iconset
	printf '%s\n' \
		'<?xml version="1.0" encoding="UTF-8"?>' \
		'<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">' \
		'<plist version="1.0"><dict>' \
		'<key>CFBundleName</key><string>AD Manager</string>' \
		'<key>CFBundleDisplayName</key><string>AD Manager</string>' \
		'<key>CFBundleIdentifier</key><string>io.github.jniltinho.admanager</string>' \
		'<key>CFBundleExecutable</key><string>$(BIN)</string>' \
		'<key>CFBundleIconFile</key><string>icon</string>' \
		'<key>CFBundlePackageType</key><string>APPL</string>' \
		'<key>CFBundleShortVersionString</key><string>$(patsubst v%,%,$(VERSION))</string>' \
		'<key>LSMinimumSystemVersion</key><string>$(MIN_MACOS)</string>' \
		'<key>NSHighResolutionCapable</key><true/>' \
		'</dict></plist>' > "$(APP)/Contents/Info.plist"
	codesign --force --deep --sign - "$(APP)"
	@echo "done: $(APP)"

# zips for the release (ditto keeps the bundle and its signature intact)
mac-zip: mac-app
	cd $(OUT)/darwin-arm64 && rm -f $(ZIPS)/$(BIN)_$(VERSION)_darwin_arm64.zip && zip -q $(ZIPS)/$(BIN)_$(VERSION)_darwin_arm64.zip $(BIN)
	cd $(OUT)/darwin-amd64 && rm -f $(ZIPS)/$(BIN)_$(VERSION)_darwin_amd64.zip && zip -q $(ZIPS)/$(BIN)_$(VERSION)_darwin_amd64.zip $(BIN)
	rm -f $(ZIPS)/$(BIN)_$(VERSION)_darwin_universal.zip
	ditto -c -k --keepParent "$(APP)" $(ZIPS)/$(BIN)_$(VERSION)_darwin_universal.zip
	@ls -1 $(ZIPS)/$(BIN)_$(VERSION)_darwin_*.zip
