package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// windowState is where the operator left the window, kept in a private desktop-state/ folder beside desktop.toml.
type windowState struct {
	X, Y, Width, Height int
	Maximised           bool `json:",omitempty"`
}

// stateDir is the private folder of this app's own state, beside desktop.toml: 0700, refused when it is a link or
// open to others.
func (d *desktop) stateDir() (string, error) {
	dir := filepath.Join(filepath.Dir(d.file.Path), "desktop-state")
	st, err := os.Lstat(dir)
	switch {
	case os.IsNotExist(err):
		return dir, os.MkdirAll(dir, 0o700)
	case err != nil:
		return "", err
	case st.Mode()&os.ModeSymlink != 0 || !st.IsDir():
		return "", fmt.Errorf("%s is not a private folder of this app", dir)
	case runtime.GOOS != "windows" && st.Mode().Perm()&0o077 != 0:
		return "", fmt.Errorf("%s must be private and has mode %#o: run chmod 700", dir, st.Mode().Perm())
	}
	return dir, nil
}

// loadWindow sizes the window as it was left and returns that state (nil the first time).
func (d *desktop) loadWindow(o *application.WebviewWindowOptions) *windowState {
	dir, err := d.stateDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "window state:", err)
		return nil
	}
	b, err := os.ReadFile(filepath.Join(dir, "window.json"))
	var s windowState
	if err != nil || json.Unmarshal(b, &s) != nil || s.Width < o.MinWidth || s.Height < o.MinHeight {
		return nil
	}
	o.Width, o.Height = s.Width, s.Height
	if s.Maximised {
		o.StartState = application.WindowStateMaximised
	}
	return &s
}

// placeWindow puts the window back where and as large as it was (Bounds and SetBounds measure the same way on every
// platform; the creation size does not on macOS), where that is still on a screen, then fits a window larger than its
// screen's work area (the default size on a small screen), centred, so its title bar stays reachable.
func (d *desktop) placeWindow(w *application.WebviewWindow, s *windowState) {
	if s != nil && s.Maximised {
		if !w.IsMaximised() {
			w.Maximise() // macOS: Wails zooms before it centres the window, which undoes the zoom
		}
		return
	}
	if s != nil {
		w.SetSize(s.Width, s.Height)
		for _, sc := range d.app.Screen.GetAll() {
			a := sc.WorkArea
			if s.X >= a.X && s.Y >= a.Y && s.X+64 <= a.X+a.Width && s.Y+64 <= a.Y+a.Height {
				w.SetBounds(application.Rect{X: s.X, Y: s.Y, Width: s.Width, Height: s.Height})
				break
			}
		}
	}
	if sc, err := w.GetScreen(); err == nil && sc != nil {
		a := sc.WorkArea
		if wd, ht := w.Size(); wd > a.Width || ht > a.Height {
			w.SetSize(min(wd, a.Width), min(ht, a.Height))
			w.Center()
		}
	}
}

// saveWindow keeps the window's place and size for the next start (a private file, through a temporary one). A
// maximised window keeps last, the normal size it started with, and starts maximised again.
func (d *desktop) saveWindow(w *application.WebviewWindow, last *windowState) {
	s := windowState{Maximised: w.IsMaximised()}
	switch {
	case w.IsMinimised() || w.IsFullscreen():
		return
	case s.Maximised:
		s.X, s.Y, s.Width, s.Height = last.X, last.Y, last.Width, last.Height
	default:
		b := w.Bounds()
		s.X, s.Y, s.Width, s.Height = b.X, b.Y, b.Width, b.Height
	}
	dir, err := d.stateDir()
	if err != nil {
		return
	}
	data, _ := json.Marshal(s)
	tmp, err := os.CreateTemp(dir, ".window-*.json")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err == nil && tmp.Close() == nil {
		_ = os.Rename(tmp.Name(), filepath.Join(dir, "window.json"))
	}
}
