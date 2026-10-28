package samdb

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func TestSID(t *testing.T) {
	b, _ := hex.DecodeString("010500000000000515000000a065cf7e784b9b5fe77c8770f4010000")
	if got := SID(b); got != "S-1-5-21-2127521184-1604012920-1887927527-500" {
		t.Fatalf("SID = %q", got)
	}
	if r, ok := RID(b); !ok || r != 500 {
		t.Fatalf("RID = %d, %v", r, ok)
	}
	builtin, _ := hex.DecodeString("01020000000000052000000021020000") // S-1-5-32-545
	if got := SID(builtin); got != "S-1-5-32-545" {
		t.Fatalf("SID = %q", got)
	}
	for _, bad := range []string{"", "0105000000000005", "01010000000000051500",
		"020500000000000515000000a065cf7e784b9b5fe77c8770f4010000", // revision 2
		"0110000000000005" + strings.Repeat("01000000", 16),        // 16 sub-authorities
	} {
		b, _ := hex.DecodeString(bad)
		if SID(b) != "" {
			t.Errorf("SID(%s) accepted", bad)
		}
		if _, ok := RID(b); ok {
			t.Errorf("RID(%s) accepted", bad)
		}
	}
}

func TestGUID(t *testing.T) {
	b, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	if got := GUID(b); got != "33221100-5544-7766-8899-aabbccddeeff" {
		t.Fatalf("GUID = %q", got)
	}
	if GUID(b[:15]) != "" {
		t.Fatal("15-byte GUID accepted")
	}
}

func TestFileTime(t *testing.T) {
	got, ok := FileTime("133000000000000000")
	if want := time.Date(2022, 6, 18, 4, 26, 40, 0, time.UTC); !ok || !got.Equal(want) {
		t.Fatalf("FileTime = %v, %v; want %v", got, ok, want)
	}
	for _, never := range []string{"0", "9223372036854775807", "-1", "x"} {
		if _, ok := FileTime(never); ok {
			t.Errorf("FileTime(%s) is a time", never)
		}
	}
}
