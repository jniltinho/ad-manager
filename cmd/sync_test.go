package cmd

import (
	"testing"
	"time"
)

func TestRunFinished(t *testing.T) {
	start := time.Date(2026, 10, 23, 8, 37, 14, 0, time.Local)
	for _, c := range []struct {
		r    syncRunRow
		want string
	}{
		{syncRunRow{Status: "ok", Started: start, Finished: start.Add(4*time.Minute + 38*time.Second)}, "08:41 · 4m38s"},
		{syncRunRow{Status: "cancelled", Started: start, Finished: start.Add(51 * time.Second)}, "08:38 · 51s"},
		{syncRunRow{Status: "ok", Started: start, Finished: start.Add(15*time.Hour + 30*time.Minute)}, "2026-10-24 00:07 · 15h30m"},
		{syncRunRow{Status: "running", Started: start}, "—"},
	} {
		if got := runFinished(c.r); got != c.want {
			t.Errorf("runFinished(%+v) = %q, want %q", c.r, got, c.want)
		}
	}
}
