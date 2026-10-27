//go:build smoke

package main

// The smoke test build (-tags smoke, never shipped): local /smoke/ routes to measure the webview bridge, dialogs
// answered from ADM_SMOKE_DIR, and a scripted run once the operator (xdotool in scripts/smoke-linux.sh) has signed
// in. Every result is a "SMOKE …" line on stdout.

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func smokeRoutes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/smoke/echo": // the request body back: both directions of the bridge
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.Copy(w, r.Body)
		case "/smoke/report":
			b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			fmt.Printf("SMOKE %s\n", b)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// bridgeJS posts bodies of growing size to /smoke/echo and reports whether each came back whole, and how fast.
const bridgeJS = `(async () => {
  const out = {}
  for (const mb of [1, 16, 32, 64, 128]) {
    const body = new Uint8Array(mb << 20); body[0] = 7; body[body.length - 1] = 9
    const t = performance.now()
    try {
      const r = await fetch('/smoke/echo', { method: 'POST', body })
      const got = new Uint8Array(await r.arrayBuffer())
      out[mb + 'MiB'] = { ok: got.length === body.length && got[0] === 7 && got[got.length - 1] === 9, ms: Math.round(performance.now() - t) }
    } catch (e) { out[mb + 'MiB'] = { error: String(e) } }
  }
  await fetch('/smoke/report', { method: 'POST', body: 'bridge ' + JSON.stringify(out) })
})()`

func smokeRun(app *application.App, win *application.WebviewWindow, files *Files) {
	dir := os.Getenv("ADM_SMOKE_DIR")
	if dir == "" {
		return
	}
	chooseSave = func(_ *application.App, name string) (string, error) { return filepath.Join(dir, name), nil }
	chooseOpen = func(*application.App) (string, error) { return os.Getenv("ADM_SMOKE_UPLOAD"), nil }
	delay, _ := strconv.Atoi(os.Getenv("ADM_SMOKE_DELAY"))
	if delay == 0 {
		delay = 12
	}
	go func() {
		time.Sleep(time.Duration(delay) * time.Second)
		win.ExecJS(bridgeJS)
		time.Sleep(15 * time.Second)
		ok, err := files.SaveFromPanel(files.panel.Generation(), "/api/export/users", "users.csv")
		st, _ := os.Stat(filepath.Join(dir, "users.csv"))
		var mode os.FileMode
		var size int64
		if st != nil {
			mode, size = st.Mode().Perm(), st.Size()
		}
		fmt.Printf("SMOKE files saved=%v err=%v size=%d mode=%o\n", ok, err, size, mode)
		win.ExecJS(`location.href = 'https://example.org/'`)
		time.Sleep(2 * time.Second)
		win.ExecJS(`window.open('https://example.org/'); setTimeout(() => fetch('/smoke/report', { method: 'POST', body: 'location ' + location.href + ' binding-version ' + window.__admDesktopVersion }), 500)`)
		time.Sleep(3 * time.Second)
		app.Quit()
	}()
}
