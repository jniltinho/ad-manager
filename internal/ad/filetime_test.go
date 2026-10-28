package ad

import (
	"fmt"
	"strings"
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

// TestRC4Check: no RC4-only user is ok; some are a warning listing at most ten names, sorted.
func TestRC4Check(t *testing.T) {
	if c := rc4Check(nil); c.Status != "ok" || c.Item != "RC4-only accounts" {
		t.Errorf("none: %+v", c)
	}
	c := rc4Check([]string{"labadmin", "Administrator"})
	if c.Status != StatusWarning || !strings.Contains(c.Detail, "2 enabled user(s) whose password predates") || !strings.Contains(c.Detail, "Administrator, labadmin;") {
		t.Errorf("two: %+v", c)
	}
	many := make([]string, 12)
	for i := range many {
		many[i] = fmt.Sprintf("u%02d", i)
	}
	if c := rc4Check(many); !strings.Contains(c.Detail, "12 enabled") || !strings.Contains(c.Detail, "u09, …") || strings.Contains(c.Detail, "u10") {
		t.Errorf("twelve: %+v", c)
	}
}

// TestBornOn2016: Key Admins created by the same dcpromo as the RODC group means a 2016+ forest root; created later
// (a PDC upgraded to 2016) or absent (2008-2012 R2) it does not.
func TestBornOn2016(t *testing.T) {
	rodc := time.Date(2026, 10, 28, 8, 40, 0, 0, time.UTC)
	for _, tt := range []struct {
		ka   time.Time
		want bool
	}{
		{rodc.Add(2 * time.Second), true},
		{rodc.Add(-time.Second), true},
		{rodc.Add(400 * 24 * time.Hour), false},
		{time.Time{}, false},
	} {
		if got := bornOn2016(rodc, tt.ka); got != tt.want {
			t.Errorf("bornOn2016(%v) = %v, want %v", tt.ka, got, tt.want)
		}
	}
}
