//go:build smoke

package main

// The smoke test build (-tags smoke, never shipped): local /smoke/ routes to measure the webview bridge, dialogs
// answered from ADM_SMOKE_DIR, and a scripted run once the operator (xdotool in scripts/smoke-linux.sh) has signed
// in. It prints "SMOKE …" lines, then "SMOKE PASS" or "SMOKE FAIL <what>", which the script checks.

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

var (
	smokeMu      sync.Mutex
	smokeReports = map[string]string{}
)

func smokeRoutes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/smoke/echo": // the request body back: both directions of the bridge
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.Copy(w, r.Body)
		case "/smoke/report": // "<name> <value>"
			b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			name, value, _ := strings.Cut(string(b), " ")
			smokeMu.Lock()
			smokeReports[name] = value
			smokeMu.Unlock()
			fmt.Printf("SMOKE %s\n", b)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// bridgeJS posts bodies of growing size to /smoke/echo and reports whether each came back whole, and how fast.
const bridgeJS = `(async () => {
  const out = {}
  let ok = true
  for (const mb of [1, 16, 32, 64, 128]) {
    const body = new Uint8Array(mb << 20); body[0] = 7; body[body.length - 1] = 9
    const t = performance.now()
    try {
      const r = await fetch('/smoke/echo', { method: 'POST', body })
      const got = new Uint8Array(await r.arrayBuffer())
      const whole = got.length === body.length && got[0] === 7 && got[got.length - 1] === 9
      ok = ok && whole
      out[mb + 'MiB'] = { ok: whole, ms: Math.round(performance.now() - t) }
    } catch (e) { ok = false; out[mb + 'MiB'] = { error: String(e) } }
  }
  await fetch('/smoke/report', { method: 'POST', body: 'bridge ' + (ok ? 'ok ' : 'failed ') + JSON.stringify(out) })
})()`

func report(name string, wait time.Duration) string {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		smokeMu.Lock()
		v, ok := smokeReports[name]
		smokeMu.Unlock()
		if ok {
			return v
		}
		time.Sleep(200 * time.Millisecond)
	}
	return ""
}

func smokeRun(app *application.App, win *application.WebviewWindow, d *desktop, files *Files) {
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
		var failed []string
		check := func(ok bool, what string) {
			if !ok {
				failed = append(failed, what)
			}
		}
		time.Sleep(time.Duration(delay) * time.Second)
		p := d.current()
		check(p != nil && p.Generation() > 1, "signed in (a session after the first)")
		win.ExecJS(bridgeJS)
		check(strings.HasPrefix(report("bridge", 30*time.Second), "ok "), "bridge")
		if p != nil {
			ok, err := files.SaveFromPanel(p.Generation(), "/api/export/users", "users.csv")
			st, serr := os.Stat(filepath.Join(dir, "users.csv"))
			fmt.Printf("SMOKE files saved=%v err=%v\n", ok, err)
			check(ok && err == nil && serr == nil && st.Size() > 0 && st.Mode().Perm() == 0o600, "files")
		}
		win.ExecJS(`location.href = 'https://example.org/'`)
		time.Sleep(2 * time.Second)
		win.ExecJS(`fetch('/smoke/report', { method: 'POST', body: 'location ' + location.href })`)
		check(strings.HasPrefix(report("location", 10*time.Second), "wails://localhost/"), "navigation guard")
		win.ExecJS(`fetch('/smoke/report', { method: 'POST', body: 'binding ' + window.__admDesktopVersion })`)
		check(report("binding", 10*time.Second) == version, "binding")
		if len(failed) == 0 {
			fmt.Println("SMOKE PASS")
		} else {
			fmt.Println("SMOKE FAIL", strings.Join(failed, ", "))
		}
		app.Quit()
	}()
}
