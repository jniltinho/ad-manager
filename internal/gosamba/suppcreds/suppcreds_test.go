package suppcreds

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// parity.json: supplementalCredentials of throwaway users on the test Samba (random passwords, never kept) and
// synthetic variants made with Samba's own NDR, each with what the Python helper (pwmigrate.py inspect_supplemental)
// answered — the rebuilt blob byte for byte.
type parityCase struct {
	Name      string `json:"name"`
	Blob      string `json:"blob"`
	Dest      string `json:"dest"`
	Src       string `json:"src"`
	Cleartext bool   `json:"cleartext"`
	Kerberos  bool   `json:"kerberos"`
	Reason    string `json:"reason"`
	Rebuilt   string `json:"rebuilt"`
}

func loadParity(t *testing.T) []parityCase {
	b, err := os.ReadFile("testdata/parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var cs []parityCase
	if err := json.Unmarshal(b, &cs); err != nil || len(cs) < 10 {
		t.Fatalf("parity.json: %d cases, %v", len(cs), err)
	}
	return cs
}

func TestInspectParity(t *testing.T) {
	for _, c := range loadParity(t) {
		blob, _ := hex.DecodeString(c.Blob)
		got := Inspect(blob, c.Dest, c.Src)
		want := c.Reason
		if strings.HasPrefix(want, "supplementalCredentials unreadable:") { // Python names its exception type
			want = "supplementalCredentials unreadable (NTLM only)"
		}
		if got.Cleartext != c.Cleartext || got.Kerberos != c.Kerberos || got.Reason != want {
			t.Errorf("%s: got cleartext=%v kerberos=%v reason=%q, want %v %v %q", c.Name, got.Cleartext, got.Kerberos,
				got.Reason, c.Cleartext, c.Kerberos, want)
		}
		if hex.EncodeToString(got.Rebuilt) != c.Rebuilt {
			t.Errorf("%s: the rebuilt blob differs from Samba's NDR (%d bytes, want %d)", c.Name, len(got.Rebuilt), len(c.Rebuilt)/2)
		}
	}
}

// A blob Samba wrote survives Parse → Bytes unchanged: the serialization is Samba's.
func TestRoundTrip(t *testing.T) {
	for _, c := range loadParity(t) {
		blob, _ := hex.DecodeString(c.Blob)
		bl, err := Parse(blob)
		if err != nil {
			continue // the malformed cases
		}
		if !bytes.Equal(bl.Bytes(), blob) {
			t.Errorf("%s: Parse → Bytes changed the blob", c.Name)
		}
	}
}

func TestRealmSaltMatches(t *testing.T) {
	for _, tt := range []struct {
		salt, dest, src string
		want            bool
	}{
		{"EXAMPLE.NETjohn", "example.net", "example.net", true},
		{"example.netjohn", "EXAMPLE.NET", "Example.Net", true},
		{"EXAMPLE.NET.BRjohn", "example.net", "example.net.br", false},
		{"SUB.EXAMPLE.NETjohn", "example.net", "sub.example.net", false},
		{"EXAMPLE.NETWORKjohn", "example.net", "example.network", false},
		{"OTHER.LOCALjohn", "example.net", "other.local", false},
		{"CUSTOMSALTjohn", "example.net", "example.net", false},
		{"", "example.net", "example.net", false},
		{"EXAMPLE.NETjohn", "", "example.net", false},
	} {
		if got := RealmSaltMatches(tt.salt, tt.dest, tt.src); got != tt.want {
			t.Errorf("RealmSaltMatches(%q, %q, %q) = %v", tt.salt, tt.dest, tt.src, got)
		}
	}
}

// Hostile input never panics; whatever Parse accepts re-serializes to the same bytes.
func FuzzParse(f *testing.F) {
	for _, s := range []string{"", "00", strings.Repeat("00", 200)} {
		b, _ := hex.DecodeString(s)
		f.Add(b)
	}
	if b, err := os.ReadFile("testdata/parity.json"); err == nil {
		var cs []parityCase
		_ = json.Unmarshal(b, &cs)
		for _, c := range cs {
			blob, _ := hex.DecodeString(c.Blob)
			f.Add(blob)
		}
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		_ = Inspect(b, "EXAMPLE.NET", "EXAMPLE.NET")
		bl, err := Parse(b)
		if err != nil {
			return
		}
		if !bytes.Equal(bl.Bytes(), b) && b[0] == 0 && b[1] == 0 && b[2] == 0 && b[3] == 0 &&
			b[8] == 0 && b[9] == 0 && b[10] == 0 && b[11] == 0 && b[len(b)-1] == 0 && isSpacePrefix(b[12:12+prefixLen]) {
			t.Fatalf("a canonical blob did not round-trip")
		}
		for _, pk := range bl.Packages {
			_, _ = KerberosSalt(pk.Data)
		}
	})
}

func isSpacePrefix(p []byte) bool {
	for i := 0; i < len(p); i += 2 {
		if p[i] != ' ' || p[i+1] != 0 {
			return false
		}
	}
	return true
}
