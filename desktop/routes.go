package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// csp is the policy of the SPA's documents: no frames, objects or foreign form targets.
const csp = "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; font-src 'self' data:; " +
	"form-action 'self'; frame-src 'none'; child-src 'none'; object-src 'none'; base-uri 'none'; connect-src 'self'"

// policy is csp, with the Vite dev server's hot-reload socket in a development build that uses one.
func policy() string {
	if u, err := url.Parse(devServer()); err == nil && u.Host != "" {
		return csp + " ws://" + u.Host
	}
	return csp
}

// browserRuntimeObject is Wails' runtime Browser object (OpenURL): the SPA opens pages only through Shell's allowlist.
const browserRuntimeObject = 9

// isBrowserObject reads a query's object as Wails does (strconv.Atoi: "09" and "+9" are 9 too).
func isBrowserObject(s string) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n == browserRuntimeObject
}

// routes is the asset server's middleware: Wails' runtime (without its Browser object), Configure's test, the panel's
// API through the proxy, and the embedded SPA with its CSP and the history fallback of its router.
func (d *desktop) routes(next http.Handler) http.Handler {
	pol := policy()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/wails/runtime":
			if allowRuntime(w, r) {
				next.ServeHTTP(w, r)
			}
		case strings.HasPrefix(p, "/wails/"):
			next.ServeHTTP(w, r)
		case p == "/desktop/test":
			d.serveTest(w, r)
		case p == "/api" || strings.HasPrefix(p, "/api/"):
			if pp := d.current(); pp != nil {
				pp.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "the panel is not configured: open Configure"})
		default:
			w.Header().Set("Content-Security-Policy", pol)
			// a router path (/users) reloads as the app; a missing asset stays a 404
			if devServer() == "" && (r.Method == http.MethodGet || r.Method == http.MethodHead) && path.Ext(p) == "" && p != "/" {
				r = r.Clone(r.Context())
				r.URL.Path, r.URL.RawPath = "/", ""
			}
			next.ServeHTTP(w, r)
		}
	})
}

// allowRuntime refuses the runtime calls the SPA never makes: its Browser object (it would open any URL), and
// chunked calls (only bodies over a few MiB are chunked, and they could hide the object).
func allowRuntime(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("x-wails-chunk-id") != "" || isBrowserObject(r.URL.Query().Get("object")) {
		http.Error(w, "refused", http.StatusForbidden)
		return false
	}
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "refused", http.StatusRequestEntityTooLarge)
		return false
	}
	var call struct {
		Object *int `json:"object"`
	}
	if len(b) > 0 && (json.Unmarshal(b, &call) != nil || call.Object != nil && *call.Object == browserRuntimeObject) {
		http.Error(w, "refused", http.StatusForbidden)
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(b))
	return true
}
