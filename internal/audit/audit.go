// Package audit writes one JSON line per change made to the directory.
// It never receives passwords: only actor, origin, action, target and result.
package audit

import (
	"io"
	"log/slog"
	"os"
)

var log = slog.New(slog.NewJSONHandler(os.Stderr, nil))

// Open redirects the audit log to path (append mode). An empty path means stderr.
func Open(path string) (io.Closer, error) {
	if path == "" {
		return io.NopCloser(nil), nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	log = slog.New(slog.NewJSONHandler(f, nil))
	return f, nil
}

// SetWriter replaces the destination (used by tests).
func SetWriter(w io.Writer) { log = slog.New(slog.NewJSONHandler(w, nil)) }

// Log records the action; a nil err is logged as "ok".
func Log(actor, ip, action, target string, err error) {
	result := "ok"
	if err != nil {
		result = err.Error()
	}
	log.Info("audit", "actor", actor, "ip", ip, "action", action, "target", target, "result", result)
}
