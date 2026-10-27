//go:build !smoke

package main

import (
	"net/http"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// smokeRoutes and smokeRun exist only in the smoke test build (-tags smoke, smoke.go).
func smokeRoutes(next http.Handler) http.Handler                    { return next }
func smokeRun(*application.App, *application.WebviewWindow, *Files) {}
