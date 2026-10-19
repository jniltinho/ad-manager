package audit

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
)

// Event is one line of audit.log.
type Event struct {
	Time   string `json:"time"`
	Actor  string `json:"actor"`
	IP     string `json:"ip"`
	Action string `json:"action"`
	Target string `json:"target"`
	Result string `json:"result"`
}

// tailSize is how much of the end of a log file is read.
//
// ponytail: reads only the last 8 MiB of the file; rotate the logs (logrotate)
// or index them in SQLite if the screen needs a longer history.
const tailSize = 8 << 20

// Read returns up to n audit events containing q, newest first.
func Read(path string, n int, q string) ([]Event, error) {
	return readTail(path, n, q, func(e Event) bool { return e.Action != "" })
}

// readTail decodes the JSON lines at the end of path into T, newest first,
// keeping up to n lines that contain q (case-insensitive) and pass ok.
func readTail[T any](path string, n int, q string, ok func(T) bool) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start := max(fi.Size()-tailSize, 0)
	b, err := io.ReadAll(io.NewSectionReader(f, start, fi.Size()-start))
	if err != nil {
		return nil, err
	}
	if start > 0 { // the first line was cut in half
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	q = strings.ToLower(q)
	lines := bytes.Split(bytes.TrimSpace(b), []byte("\n"))
	out := []T{}
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		var e T
		if json.Unmarshal(lines[i], &e) != nil || !ok(e) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(string(lines[i])), q) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
