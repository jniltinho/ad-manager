package ad

import (
	"encoding/base64"
	"errors"
	"testing"
)

// Values read from the Docker Samba 4.23 (ldbsearch of fixture records added with samba-tool dns add).
func TestDecodeDNSRecord(t *testing.T) {
	for _, tt := range []struct {
		name, b64, typ, data string
		ttl                  uint32
	}{
		{"A", "BAABAAXwAAC2CAAAAAADhAAAAAAAAAAACgECAw==", "A", "10.1.2.3", 900},
		{"CNAME", "FAAFAAXwAAC3CAAAAAADhAAAAAAAAAAAEgMDZGMxB2VtcHJlc2EEdGVzdAA=", "CNAME", "dc1.empresa.test.", 900},
		{"PTR", "GgAMAAXwAAAEAAAAAAADhAAAAAAAAAAAGAMJZml4dHVyZS1hB2VtcHJlc2EEdGVzdAA=", "PTR", "fixture-a.empresa.test.", 900},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b, _ := base64.StdEncoding.DecodeString(tt.b64)
			typ, data, ttl, static, err := DecodeDNSRecord(b)
			if err != nil || typ != tt.typ || data != tt.data || ttl != tt.ttl || !static {
				t.Fatalf("%s %q ttl %d static %v err %v", typ, data, ttl, static, err)
			}
		})
	}
	a, _ := base64.StdEncoding.DecodeString("BAABAAXwAAC2CAAAAAADhAAAAAAAAAAACgECAw==")
	dyn := append([]byte{}, a...)
	dyn[20] = 1 // a timestamp: dynamic
	if _, _, _, static, _ := DecodeDNSRecord(dyn); static {
		t.Error("a record with a timestamp is dynamic")
	}
	cname, _ := base64.StdEncoding.DecodeString("FAAFAAXwAAC3CAAAAAADhAAAAAAAAAAAEgMDZGMxB2VtcHJlc2EEdGVzdAA=")
	for name, b := range map[string][]byte{
		"short":             a[:20],
		"data past the end": append(append([]byte{}, a[:24]...), 1),
		"A with 3 bytes":    func() []byte { x := append([]byte{}, a...); x[0] = 3; return x[:27] }(),
		"wrong version":     func() []byte { x := append([]byte{}, a...); x[4] = 4; return x }(),
		"label past name":   func() []byte { x := append([]byte{}, cname...); x[26] = 60; return x }(),
		"wrong label count": func() []byte { x := append([]byte{}, cname...); x[25] = 2; return x }(),
		"name length past":  func() []byte { x := append([]byte{}, cname...); x[24] = 200; return x }(),
	} {
		if _, _, _, _, err := DecodeDNSRecord(b); !errors.Is(err, ErrDNSBlob) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func FuzzDecodeDNSRecord(f *testing.F) {
	for _, s := range []string{"BAABAAXwAAC2CAAAAAADhAAAAAAAAAAACgECAw==", "FAAFAAXwAAC3CAAAAAADhAAAAAAAAAAAEgMDZGMxB2VtcHJlc2EEdGVzdAA="} {
		b, _ := base64.StdEncoding.DecodeString(s)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) { _, _, _, _, _ = DecodeDNSRecord(b) })
}
