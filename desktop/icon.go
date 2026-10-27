package main

import _ "embed"

// appIcon is the app's icon (256 px PNG, from packaging/icon.png): the window and taskbar icon on Windows (Wails'
// Options.Icon), and the themed icon of the window on Linux (icon_linux.go). macOS takes its own
// shaped icon from the .app (packaging/macos/icon.png, bundle.sh).
//
//go:embed packaging/icon-256.png
var appIcon []byte

// iconName is the icon's name in the icon theme and the program name (WM_CLASS, the Wayland app id), the same as
// the .desktop file the .deb and .rpm install.
const iconName = "ad-manager-desktop"
