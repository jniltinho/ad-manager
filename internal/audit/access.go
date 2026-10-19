package audit

import (
	"io"
	"log/slog"
	"os"
	"sync"
)

// Request is one line of the HTTP access log (panel API calls). It never
// carries bodies, headers or secrets: the caller redacts tokens in the path.
type Request struct {
	Time   string  `json:"time"`
	IP     string  `json:"ip"`
	User   string  `json:"user"`
	Method string  `json:"method"`
	Path   string  `json:"path"`
	Status int     `json:"status"`
	Ms     float64 `json:"ms"`
	Bytes  int64   `json:"bytes"`
	Error  string  `json:"error,omitempty"`
}

// accessRotate is the size above which the access log is renamed to .1 when
// it is opened.
//
// ponytail: rotates only at startup and keeps one old file; use logrotate
// (copytruncate) if a busy panel outgrows it between restarts.
const accessRotate = 32 << 20

var (
	accessMu  sync.Mutex
	accessLog = slog.New(slog.DiscardHandler)
)

// OpenAccess sends the access log to path (append mode, 0600). An empty path
// disables it.
func OpenAccess(path string) (io.Closer, error) {
	if path == "" {
		return io.NopCloser(nil), nil
	}
	if fi, err := os.Stat(path); err == nil && fi.Size() > accessRotate {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	accessMu.Lock()
	accessLog = slog.New(slog.NewJSONHandler(f, nil))
	accessMu.Unlock()
	return f, nil
}

// Access records one request.
func Access(r Request) {
	accessMu.Lock()
	l := accessLog
	accessMu.Unlock()
	args := []any{"ip", r.IP, "user", r.User, "method", r.Method, "path", r.Path, "status", r.Status, "ms", r.Ms, "bytes", r.Bytes}
	if r.Error != "" {
		args = append(args, "error", r.Error)
	}
	l.Info("http", args...)
}

// ReadAccess returns up to n requests containing q, newest first.
func ReadAccess(path string, n int, q string) ([]Request, error) {
	return readTail(path, n, q, func(r Request) bool { return r.Method != "" })
}
