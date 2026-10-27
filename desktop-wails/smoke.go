//go:build smoke

package main

// The smoke test build (-tags smoke, never shipped): local /smoke/ routes to measure the webview bridge, dialogs
// answered from ADM_SMOKE_DIR, and a scripted run once the operator (xdotool in scripts/smoke-linux.sh) has signed
// in. It prints "SMOKE …" lines, then "SMOKE PASS" or "SMOKE FAIL <what>" (also into ADM_SMOKE_DIR/smoke.log), which
// the script and CI check. Without a configured panel (CI on Windows and macOS) it checks what needs none: the bridge
// and a binding. The navigation guard is checked where it exists (Linux).

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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
		if r.URL.Path == "/smoke/echo" {
			say("SMOKE echo %d bytes announced", r.ContentLength)
		} else if r.URL.Path != "/smoke/report" {
			say("SMOKE request %s %s", r.Method, r.URL.Path) // what the webview asked for (a diagnosis when JS stays silent)
		}
		switch r.URL.Path {
		case "/smoke/echo": // the request body back: both directions of the bridge
			b, err := io.ReadAll(r.Body)
			say("SMOKE echo read %d bytes err=%v", len(b), err)
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(b)
		case "/smoke/report": // "<name> <value>"
			b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			name, value, _ := strings.Cut(string(b), " ")
			smokeMu.Lock()
			smokeReports[name] = value
			smokeMu.Unlock()
			say("SMOKE %s", b)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// bridgeJS posts bodies of growing size to /smoke/echo and reports whether each came back whole, and how fast.
// Each size is reported as it ends ("bridge-<n>"), and a request taking more than 60 s is aborted.
const bridgeJS = `(async () => {
  const out = {}
  let ok = true
  for (const mb of [1, 16, 32, 64, 128]) {
    const body = new Uint8Array(mb << 20); body[0] = 7; body[body.length - 1] = 9
    const t = performance.now()
    const abort = new AbortController()
    const timer = setTimeout(() => abort.abort(), 60000)
    try {
      const r = await fetch('/smoke/echo', { method: 'POST', body, signal: abort.signal })
      const got = new Uint8Array(await r.arrayBuffer())
      const whole = got.length === body.length && got[0] === 7 && got[got.length - 1] === 9
      ok = ok && whole
      out[mb + 'MiB'] = { ok: whole, ms: Math.round(performance.now() - t), status: r.status, got: got.length }
    } catch (e) { ok = false; out[mb + 'MiB'] = { error: String(e), ms: Math.round(performance.now() - t) } }
    clearTimeout(timer)
    await fetch('/smoke/report', { method: 'POST', body: 'bridge-' + mb + ' ' + JSON.stringify(out[mb + 'MiB']) })
  }
  await fetch('/smoke/report', { method: 'POST', body: 'bridge ' + (ok ? 'ok ' : 'failed ') + JSON.stringify(out) })
})()`

// say prints a smoke line and appends it to ADM_SMOKE_DIR/smoke.log (a GUI build on Windows has no stdout).
func say(format string, a ...any) {
	line := fmt.Sprintf(format, a...)
	fmt.Println(line)
	if f, err := os.OpenFile(filepath.Join(os.Getenv("ADM_SMOKE_DIR"), "smoke.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		fmt.Fprintln(f, line)
		f.Close()
	}
}

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
		win.ExecJS(`fetch('/smoke/report', { method: 'POST', body: 'page ' + location.href + ' ' + document.readyState + ' ' + (document.title || '') })`)
		say("SMOKE page %s", report("page", 10*time.Second))
		p := d.current()
		if os.Getenv("ADM_SMOKE_PANEL") != "" {
			check(p != nil && p.Generation() > 1, "signed in (a session after the first)")
		}
		win.ExecJS(`fetch('/smoke/report', { method: 'POST', body: 'binding ' + window.__admDesktopVersion })`)
		check(report("binding", 10*time.Second) == version, "binding")
		win.ExecJS(bridgeJS)
		check(strings.HasPrefix(report("bridge", 360*time.Second), "ok "), "bridge")
		if p != nil && os.Getenv("ADM_SMOKE_PANEL") != "" {
			ok, err := files.SaveFromPanel(p.Generation(), "/api/export/users", "users.csv")
			st, serr := os.Stat(filepath.Join(dir, "users.csv"))
			say("SMOKE files saved=%v err=%v", ok, err)
			check(ok && err == nil && serr == nil && st.Size() > 0 && (runtime.GOOS == "windows" || st.Mode().Perm() == 0o600), "files")
		}
		if runtime.GOOS == "linux" {
			win.ExecJS(`location.href = 'https://example.org/'`)
			time.Sleep(2 * time.Second)
			win.ExecJS(`fetch('/smoke/report', { method: 'POST', body: 'location ' + location.href })`)
			check(strings.HasPrefix(report("location", 10*time.Second), "wails://localhost/"), "navigation guard")
		} else {
			say("SMOKE guard none on %s (task 4.5)", runtime.GOOS)
		}
		if len(failed) == 0 {
			say("SMOKE PASS")
		} else {
			say("SMOKE FAIL %s", strings.Join(failed, ", "))
		}
		app.Quit()
	}()
}
