package sambabridge

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

type corpusItem struct {
	Blob       string          `json:"blob"`
	SysACLHash string          `json:"sys_acl_hash"`
	ACL        json.RawMessage `json:"acl"`
}

func loadCorpus(t testing.TB) []corpusItem {
	t.Helper()
	b, err := os.ReadFile("testdata/ntacl_corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var c []corpusItem
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// TestNTACLCorpus: blobs packed by Samba's NDR (testdata/gen_ntacl_corpus.py) read in Go give what the helper's
// describe() gives, SDDL included.
func TestNTACLCorpus(t *testing.T) {
	for i, it := range loadCorpus(t) {
		blob, _ := hex.DecodeString(it.Blob)
		sd, sysHash, err := parseNTACL(blob)
		if err != nil {
			t.Fatalf("#%d: %v", i, err)
		}
		if hex.EncodeToString(sysHash) != it.SysACLHash {
			t.Fatalf("#%d: sys_acl_hash %x", i, sysHash)
		}
		got, err := describe(sd)
		if err != nil {
			t.Fatalf("#%d: describe: %v", i, err)
		}
		var want, have any
		_ = json.Unmarshal(it.ACL, &want)
		b, _ := json.Marshal(got)
		_ = json.Unmarshal(b, &have)
		if !jsonEqual(want, have) {
			t.Fatalf("#%d:\n got %s\nwant %s", i, b, it.ACL)
		}
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func TestNTACLRejects(t *testing.T) {
	blob, _ := hex.DecodeString(loadCorpus(t)[0].Blob)
	for _, tt := range []struct {
		name string
		edit func([]byte) []byte
	}{
		{name: "version 3", edit: func(b []byte) []byte { b[0] = 3; return b }},
		{name: "hash type", edit: func(b []byte) []byte { b[12] = 2; return b }},
		{name: "truncated", edit: func(b []byte) []byte { return b[:len(b)-1] }},
		{name: "empty", edit: func([]byte) []byte { return nil }},
		{name: "owner outside", edit: func(b []byte) []byte { b[len(b)-1] = 0xff; return b[:170] }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := parseNTACL(tt.edit(bytes.Clone(blob))); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

func FuzzParseNTACL(f *testing.F) {
	for _, it := range loadCorpus(f)[:20] {
		b, _ := hex.DecodeString(it.Blob)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if sd, _, err := parseNTACL(b); err == nil {
			_, _ = describe(sd)
		}
	})
}

func TestPosixXattr(t *testing.T) {
	// user::rwx user:1000:r-x group::r-x mask::r-x other::---
	x, _ := hex.DecodeString("02000000" + "01000700ffffffff" + "02000500e8030000" + "04000500ffffffff" + "10000500ffffffff" + "20000000ffffffff")
	got, err := parsePosixXattr(x)
	want := []posixEntry{{tag: 2, perm: 7}, {tag: 1, perm: 5, id: 1000}, {tag: 4, perm: 5}, {tag: 6, perm: 5}, {tag: 5}}
	if err != nil || len(got) != len(want) {
		t.Fatalf("%v %v", got, err)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d: %+v, want %+v", i, got[i], want[i])
		}
	}
	if _, err := parsePosixXattr(x[:10]); err == nil {
		t.Fatal("truncated accepted")
	}
}

// TestPosixACLHash: a folder of the lab DC (Samba 4.23, ACL written by set_nt_acl, named groups in the POSIX ACLs):
// the hash of its POSIX ACL as Go encodes it is the one Samba stored; any change to the ACL breaks it.
func TestPosixACLHash(t *testing.T) {
	b, err := os.ReadFile("testdata/ntacl_lab.json")
	if err != nil {
		t.Fatal(err)
	}
	var lab struct {
		NTACL, Access, Default string
		UID, GID, Mode         uint32
	}
	if err := json.Unmarshal(b, &lab); err != nil {
		t.Fatal(err)
	}
	dec := func(s string) []byte { b, _ := hex.DecodeString(s); return b }
	_, sysHash, err := parseNTACL(dec(lab.NTACL))
	if err != nil {
		t.Fatal(err)
	}
	access, err1 := parsePosixXattr(dec(lab.Access))
	def, err2 := parsePosixXattr(dec(lab.Default))
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if !bytes.Equal(posixACLHash(access, def, lab.UID, lab.GID, lab.Mode), sysHash) {
		t.Fatal("hash differs from Samba's")
	}
	access[1].perm ^= 1
	if bytes.Equal(posixACLHash(access, def, lab.UID, lab.GID, lab.Mode), sysHash) {
		t.Fatal("a changed ACL kept the hash")
	}
	if bytes.Equal(posixACLHash(access, nil, lab.UID, lab.GID, lab.Mode), posixACLHash(access, []posixEntry{}, lab.UID, lab.GID, lab.Mode)) {
		t.Fatal("NULL and empty default ACLs must differ")
	}
}

func TestGoReadable(t *testing.T) {
	dc := Caps{NTACL: true, ACLXattr: true, VFSObjects: []string{"dfs_samba4", "acl_xattr"}}
	for _, tt := range []struct {
		name string
		edit func(*Caps)
		want bool
	}{
		{name: "DC default", edit: func(*Caps) {}, want: true},
		{name: "xattr_tdb", edit: func(c *Caps) { c.VFSObjects = append(c.VFSObjects, "xattr_tdb") }},
		{name: "own xattr name", edit: func(c *Caps) { n := "security.NTACL"; c.NTACLXattr = &n }},
		{name: "xattr name set empty", edit: func(c *Caps) { n := ""; c.NTACLXattr = &n }},
		{name: "no acl_xattr", edit: func(c *Caps) { c.ACLXattr = false }},
		{name: "unknown stack", edit: func(c *Caps) { c.VFSObjects = nil }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := dc
			c.VFSObjects = append([]string(nil), dc.VFSObjects...)
			tt.edit(&c)
			if c.GoReadable() != tt.want {
				t.Fatalf("GoReadable = %v", !tt.want)
			}
		})
	}
}

// sdStartOf finds the descriptor in a v4 blob the way parseNTACL does.
func sdStartOf(b []byte) int {
	end := 78
	for b[end] != 0 {
		end++
	}
	return align4(align4(end+1) + 8 + 64)
}

func TestNTACLRejectsLayout(t *testing.T) {
	blob, _ := hex.DecodeString(loadCorpus(t)[0].Blob) // with a DACL
	sd := sdStartOf(blob)
	for _, tt := range []struct {
		name string
		edit func([]byte)
	}{
		{name: "SACL present", edit: func(b []byte) { copy(b[sd+12:sd+16], b[sd+16:sd+20]) }},
		{name: "odd owner offset", edit: func(b []byte) { b[sd+4]++ }},
		{name: "bad UTF-8 description", edit: func(b []byte) { b[78] = 0xff }},
		{name: "non-ASCII description", edit: func(b []byte) { b[78], b[79] = 0xc3, 0xa9 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := parseNTACL(blob); err != nil {
				t.Fatal("the base blob must parse:", err)
			}
			b := bytes.Clone(blob)
			tt.edit(b)
			if _, _, err := parseNTACL(b); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}
