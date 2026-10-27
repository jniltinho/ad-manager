// Command ad-manager-desktop-wails is the AD Manager desktop app on Wails v3: the web panel's own SPA (frontend/,
// built with `vp build --mode desktop`) in the system's webview, and a Go backend that is its only way to the panel
// (internal/panel).
package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"ad-manager/desktop-wails/internal/panel"
)

//go:embed all:frontend/dist
var assets embed.FS

// version is set by the build (-ldflags "-X main.version=…").
var version = "dev"

// csp is the policy of the SPA's documents: no frames, objects or foreign form targets; devCSP adds the Vite dev
// server's hot-reload socket (development builds with FRONTEND_DEVSERVER_URL only).
const csp = "default-src 'self'; connect-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; " +
	"font-src 'self' data:; form-action 'self'; frame-src 'none'; child-src 'none'; object-src 'none'; base-uri 'none'"

// Shell is the service the SPA calls for what a webview cannot do on its own.
type Shell struct {
	app   *application.App
	panel *panel.Panel
}

// PanelURL is the panel's base URL, for links built for use outside the app (the single-use password link).
func (s *Shell) PanelURL() string { return s.panel.URL() }

// OpenExternal opens path on the panel in the system browser: only the queue monitor and the API docs.
func (s *Shell) OpenExternal(path string) error {
	if path != "/asynqmon" && path != "/swagger" {
		return errors.New("only the panel's /asynqmon and /swagger pages open outside the app")
	}
	return s.app.Browser.OpenURL(s.panel.URL() + path)
}

// Session is the panel session's generation, which the SPA sends on every request (X-ADM-Session).
func (s *Shell) Session() uint64 { return s.panel.Generation() }

// UploadLimit is the most the webview's bridge carries in one request body (bigger files go through Files).
func (s *Shell) UploadLimit() int64 { return panel.BridgeCap - 1<<20 }

// Version is the desktop app's version.
func (s *Shell) Version() string { return version }

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ad-manager-desktop-wails:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fl := flag.NewFlagSet("ad-manager-desktop-wails", flag.ContinueOnError)
	panelURL := fl.String("panel", os.Getenv("ADM_PANEL_URL"), "panel URL (https://server:port)")
	pin := fl.String("fingerprint", os.Getenv("ADM_PANEL_FINGERPRINT"), "pinned panel certificate (sha256:<hex>)")
	showVersion := fl.Bool("version", false, "print the version and exit")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	p, err := panel.New(*panelURL, *pin, false)
	if err != nil {
		return err
	}
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return err
	}
	shell, files := &Shell{panel: p}, &Files{panel: p}
	app := application.New(application.Options{
		Name:        "AD Manager",
		Description: "AD Manager desktop",
		Services:    []application.Service{application.NewService(shell), application.NewService(files)},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(dist),
			Middleware: application.ChainMiddleware(withCSP, smokeRoutes, p.Middleware),
		},
		Linux: application.LinuxOptions{ProgramName: "ad-manager-desktop"},
		Mac:   application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	})
	shell.app, files.app = app, app
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "AD Manager", Width: 1280, Height: 800, MinWidth: 800, MinHeight: 600, URL: "/",
	})
	var guard sync.Once
	win.OnWindowEvent(events.Common.WindowShow, func(*application.WindowEvent) {
		guard.Do(func() {
			if err := guardNavigation(win); err != nil {
				app.Logger.Error(err.Error())
			}
		})
	})
	smokeRun(app, win, files)
	return app.Run()
}

// withCSP sets the SPA's Content-Security-Policy on everything that is not the panel's API.
func withCSP(next http.Handler) http.Handler {
	policy := csp
	if devServer() != "" {
		if u, err := url.Parse(devServer()); err == nil {
			policy = csp + "; connect-src 'self' ws://" + u.Host
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", policy)
		next.ServeHTTP(w, r)
	})
}
