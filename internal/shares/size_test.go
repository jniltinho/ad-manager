package shares

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// du as Size runs it: complete, partial (a total after errors), failed, timed out; on the symlink-resolved path.
func TestSize(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	real := filepath.Join(dir, "Finance")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	var got []string
	run := func(out, errOut string, err error) SizeRunner {
		return func(ctx context.Context, name string, args ...string) (string, string, error) {
			got = append([]string{name}, args...)
			if err == context.DeadlineExceeded {
				<-ctx.Done()
				return "", "", errors.New("signal: killed")
			}
			return out, errOut, err
		}
	}
	exit1 := errors.New("exit status 1")
	for _, c := range []struct {
		name    string
		run     SizeRunner
		bytes   int64
		partial bool
		err     string
	}{
		{"complete", run("1536\t"+real+"\n", "", nil), 1536, false, ""},
		{"partial", run("1024\t"+real+"\n", "du: cannot read directory 'x': Permission denied\ndu: other\n", exit1), 1024, true, "du: cannot read directory 'x': Permission denied"},
		{"no total", run("", "du: cannot access 'x': No such file or directory\n", exit1), 0, false, "du: cannot access 'x': No such file or directory"},
		{"no output", run("", "", exit1), 0, false, "du: exit status 1"},
	} {
		m := &Manager{SizeRun: c.run}
		r, err := m.Size(context.Background(), link)
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		if r.Bytes != c.bytes || r.Partial != c.partial || msg != c.err || r.RealPath != real {
			t.Errorf("%s: %+v %q", c.name, r, msg)
		}
		if got[len(got)-1] != real || got[len(got)-2] != "--" || !strings.Contains(strings.Join(got, " "), "du -s -b -P") {
			t.Errorf("%s: ran %q", c.name, got)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1)
	defer cancel()
	m := &Manager{SizeRun: run("", "", context.DeadlineExceeded)}
	if _, err := m.Size(ctx, real); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("timeout: %v", err)
	}
	if _, err := m.Size(context.Background(), filepath.Join(dir, "gone")); err == nil {
		t.Error("a missing folder measured")
	}
}

// The real runner keeps at most 4 KiB of each stream, whatever du writes.
func TestExecSizeBounded(t *testing.T) {
	t.Parallel()
	out, errOut, err := ExecSize(context.Background(), "sh", "-c", "head -c 1000000 /dev/zero | tr '\\0' x; head -c 1000000 /dev/zero | tr '\\0' y >&2; exit 1")
	if err == nil || len(out) != sizeOutMax || len(errOut) != sizeOutMax {
		t.Fatalf("out %d, err %d, %v", len(out), len(errOut), err)
	}
}
