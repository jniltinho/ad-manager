// Command ad-manager-desktop is the AD Manager desktop app on Wails v3: the web panel's own SPA (frontend/,
// built with `vp build --mode desktop`) in the system's webview, and a Go backend that is its only way to the panel
// (internal/panel), configured by desktop.toml (internal/config).
package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"ad-manager/desktop/internal/config"
)

//go:embed all:frontend/dist
var assets embed.FS

// version is set by the build (-ldflags "-X main.version=…").
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ad-manager-desktop:", err)
		os.Exit(1)
	}
}

// instanceID is the single-instance id of one desktop.toml: a second start with the same file focuses the running
// window, while another file (a lab panel, a test) is an app of its own.
func instanceID(cfgPath string) string {
	if abs, err := filepath.Abs(cfgPath); err == nil {
		cfgPath = abs
	}
	// a link and its target are one file (saving follows the link); a file not written yet, by its folder's target
	if real, err := filepath.EvalSymlinks(cfgPath); err == nil {
		cfgPath = real
	} else if dir, err := filepath.EvalSymlinks(filepath.Dir(cfgPath)); err == nil {
		cfgPath = filepath.Join(dir, filepath.Base(cfgPath))
	}
	sum := sha256.Sum256([]byte(cfgPath))
	// a D-Bus name element: letters first, no dashes
	return "io.github.jniltinho.admanager.desktop.c" + hex.EncodeToString(sum[:8])
}

func run(args []string) error {
	fl := flag.NewFlagSet("ad-manager-desktop", flag.ContinueOnError)
	cfgPath := fl.String("config", "", "desktop.toml to use (default: AD_MANAGER_DESKTOP, .config/ beside the executable, the user's config folder)")
	safeGPU := fl.Bool("safe-gpu", false, "start the webview with GPU acceleration off (Linux, Windows)")
	showVersion := fl.Bool("version", false, "print the version and exit")
	if err := fl.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Println(version)
		return nil
	}
	d := newDesktop(config.Resolve(*cfgPath))
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return err
	}
	shell, files, configure := &Shell{d: d}, &Files{d: d}, &Configure{d: d}
	opts := application.Options{
		Name:        "AD Manager",
		Description: "AD Manager desktop",
		LogLevel:    slog.LevelInfo, // never Debug: Wails logs binding arguments there
		Services: []application.Service{application.NewService(shell), application.NewService(files),
			application.NewService(configure)},
		Assets: application.AssetOptions{
			Handler:        application.AssetFileServerFS(dist),
			Middleware:     application.ChainMiddleware(smokeRoutes, d.routes),
			DisableLogging: true, // request paths may carry tokens (a single-use password link)
		},
		Linux: application.LinuxOptions{ProgramName: iconName},
		Mac:   application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: true},
	}
	if runtime.GOOS != "darwin" {
		opts.Icon = appIcon // macOS: the .app's own icon (macOS shape); Wails would replace it in the Dock
	}
	winOpts := application.WebviewWindowOptions{Title: "AD Manager", Width: 1280, Height: 800, MinWidth: 800, MinHeight: 600,
		// the panel needs none of these; without a policy WebView2 grants them all without asking
		Permissions: map[application.PermissionType]application.Permission{
			application.PermissionMicrophone: application.PermissionDeny, application.PermissionCamera: application.PermissionDeny,
			application.PermissionGeolocation: application.PermissionDeny, application.PermissionNotifications: application.PermissionDeny,
			application.PermissionClipboardRead: application.PermissionDeny,
		}}
	saved := d.loadWindow(&winOpts)
	if *safeGPU || d.safeGPU() {
		safeGraphics(&opts, &winOpts)
	}
	var win *application.WebviewWindow
	opts.SingleInstance = &application.SingleInstanceOptions{
		UniqueID: instanceID(d.file.Path),
		OnSecondInstanceLaunch: func(application.SecondInstanceData) {
			if win != nil {
				win.Restore()
				win.Focus()
			}
		},
	}
	last := saved
	if last == nil {
		last = &windowState{Width: winOpts.Width, Height: winOpts.Height}
	}
	// once: closing the window, or quitting the app (Cmd+Q, the Dock), which closes no window first
	var saveOnce sync.Once
	save := func() { saveOnce.Do(func() { d.saveWindow(win, last) }) }
	opts.ShouldQuit = func() bool {
		if win != nil {
			save()
		}
		return true
	}
	app := application.New(opts)
	d.app = app
	// the app's own pages load only once the navigation guard is on (a blank page first where it needs the webview)
	winOpts.URL, winOpts.HTML = guardedStart()
	win = app.Window.NewWithOptions(winOpts)
	var once sync.Once
	win.OnWindowEvent(events.Common.WindowShow, func(*application.WindowEvent) {
		once.Do(func() {
			setWindowIcon(win)
			if err := guardNavigation(win); err != nil {
				app.Logger.Error(err.Error())
				app.Dialog.Error().SetTitle("AD Manager").SetMessage("The app cannot start safely: " + err.Error()).Show()
				app.Quit()
				return
			}
			if winOpts.URL == "" {
				win.SetURL("/")
			}
			d.placeWindow(win, saved)
		})
	})
	win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) { save() })
	smokeRun(app, win, d, files)
	return app.Run()
}
