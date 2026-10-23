package main

import (
	"testing"
	"time"
)

func TestShortDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "0s", 38 * time.Second: "38s", 4*time.Minute + 38*time.Second: "4m38s",
		time.Hour + 2*time.Minute + 9*time.Second: "1h02m", -time.Second: "0s",
	} {
		if got := shortDuration(d); got != want {
			t.Errorf("shortDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestSyncFinished(t *testing.T) {
	defer func(l string) { lang = l }(lang)
	lang = "en"
	start := time.Date(2026, 10, 23, 8, 37, 14, 0, time.Local)
	if got := syncFinished(syncRun{Status: "ok", Started: start, Finished: start.Add(4*time.Minute + 38*time.Second)}); got != "08:41 AM · 4m38s" {
		t.Errorf("same day: %q", got)
	}
	late := time.Date(2026, 10, 22, 23, 58, 0, 0, time.Local)
	if got := syncFinished(syncRun{Status: "ok", Started: late, Finished: late.Add(10 * time.Minute)}); got != "10/23/2026 12:08 AM · 10m00s" {
		t.Errorf("next day: %q", got)
	}
	// the Finished column (220) holds the longest cell, the date and the duration, without the table's ellipsis
	width := 220.0
	if n, limit := len([]rune("10/23/2026 12:08 AM · 10h00m")), int(width/7.2)-2; n > limit {
		t.Errorf("a cross-day cell of %d characters is cut at %d", n, limit)
	}
	if got := syncFinished(syncRun{Status: "running", Started: start}); got != "—" {
		t.Errorf("running: %q", got)
	}
	lang = "pt-BR"
	if got := syncFinished(syncRun{Status: "error", Started: start, Finished: start.Add(51 * time.Second)}); got != "08:38 · 51s" {
		t.Errorf("pt-BR: %q", got)
	}
}
