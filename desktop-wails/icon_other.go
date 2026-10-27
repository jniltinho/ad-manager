//go:build !linux || gtk3

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// setWindowIcon: Windows takes the icon from Options.Icon, macOS from the .app.
func setWindowIcon(*application.WebviewWindow) {}
