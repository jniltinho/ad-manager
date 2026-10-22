package ad

import (
	"testing"
	"time"
)

// TestFileTimeRoundTrip checks toFileTime against fileTime: the zero time produces no value, and a real time
// round-trips to the second (the import carries an approximate last-logon).
func TestFileTimeRoundTrip(t *testing.T) {
	if got := toFileTime(time.Time{}); got != "" {
		t.Errorf("zero time must produce no value, got %q", got)
	}
	want := time.Date(2026, 3, 1, 12, 30, 45, 0, time.UTC)
	ft := toFileTime(want)
	if ft == "" {
		t.Fatal("non-zero time produced no value")
	}
	got := fileTime(ft)
	if !got.Equal(want) {
		t.Errorf("round trip: got %v, want %v (filetime %s)", got, want, ft)
	}
}
