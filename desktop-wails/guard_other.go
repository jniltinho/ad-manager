//go:build !linux || gtk3

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// guardNavigation has no native hook on Windows and macOS in Wails v3.0.0: the CSP and the SPA's link guard keep the
// window on the app there.
// shortcut: no native navigation cancel on Windows/macOS, upgrade with the carried Wails patch (design D5, gate G2).
func guardNavigation(*application.WebviewWindow) error { return nil }

// guardedStart loads the app at once: there is no native guard to wait for.
func guardedStart() (start, html string) { return "/", "" }
