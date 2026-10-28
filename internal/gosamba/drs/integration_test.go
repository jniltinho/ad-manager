//go:build integration

package drs

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/oiweiwei/go-msrpc/ssp/crypto"

	"ad-manager/internal/winrpc"
)

// The DRSUAPI reader against a real DC. scripts/test-dc.sh starts a Samba DC (Ubuntu 26.04 or Debian 13) on an
// internal Docker network, creates the fixtures and runs this test in a container on that network with:
//
//	DRS_TEST_ADDR, DRS_TEST_HOST, DRS_TEST_REALM, DRS_TEST_PASSWORD   the DC and its Administrator
//	DRS_TEST_ADMIN_GUID, DRS_TEST_ADMIN_NT    Administrator's objectGUID and NT hash (base64, from samba-tool);
//	                                          without DRS_TEST_ADMIN_NT (a Windows DC) it is the MD4 of the password
//	DRS_TEST_NOPW_GUID                        a user created without a password
//	DRS_TEST_PLAIN_USER, DRS_TEST_PLAIN_PASSWORD  a user without replication rights
func env(t *testing.T) (winrpc.Target, winrpc.Cred) {
	addr := os.Getenv("DRS_TEST_ADDR")
	if addr == "" {
		t.Skip("no test DC (scripts/test-dc.sh)")
	}
	realm := os.Getenv("DRS_TEST_REALM")
	return winrpc.Target{Addr: addr, Host: os.Getenv("DRS_TEST_HOST")},
		winrpc.Cred{Realm: realm, KDC: addr + ":88", User: "Administrator", Password: os.Getenv("DRS_TEST_PASSWORD")}
}

// guidBytes turns the text form of an objectGUID into its Windows mixed-endian bytes.
func guidBytes(t *testing.T, s string) []byte {
	b, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil || len(b) != 16 {
		t.Fatalf("GUID %q", s)
	}
	return []byte{b[3], b[2], b[1], b[0], b[5], b[4], b[7], b[6], b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15]}
}

func utf16le(s string) []byte {
	var b []byte
	for _, c := range utf16.Encode([]rune(s)) {
		b = append(b, byte(c), byte(c>>8))
	}
	return b
}

func TestReadNTHash(t *testing.T) {
	target, cred := env(t)
	ctx := context.Background()
	r, err := Dial(ctx, target, cred)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer r.Close(ctx)

	want, err := base64.StdEncoding.DecodeString(os.Getenv("DRS_TEST_ADMIN_NT"))
	if os.Getenv("DRS_TEST_ADMIN_NT") == "" { // a Windows DC: no samba-tool, the hash of the known password
		want, err = crypto.MD4(utf16le(cred.Password))
	}
	if err != nil || len(want) != 16 {
		t.Fatalf("expected hash: %d bytes, %v", len(want), err)
	}
	// hashes are compared, never printed: the test may run against a real domain (read only)
	got, err := r.NTHash(ctx, guidBytes(t, os.Getenv("DRS_TEST_ADMIN_GUID")))
	if err != nil || len(want) != 16 || !bytes.Equal(got, want) {
		t.Fatalf("Administrator: %d bytes, equal %v, %v", len(got), bytes.Equal(got, want), err)
	}
	if g := os.Getenv("DRS_TEST_NOPW_GUID"); g != "" { // the same binding serves the next object
		if nt, err := r.NTHash(ctx, guidBytes(t, g)); err != nil || nt != nil {
			t.Fatalf("user without a password: %d bytes, %v", len(nt), err)
		}
	}
	missing := bytes.Repeat([]byte{0xAB}, 16)
	if nt, err := r.NTHash(ctx, missing); err == nil {
		t.Fatalf("unknown GUID gave %d bytes", len(nt))
	}
}

func TestWithoutReplicationRights(t *testing.T) {
	target, cred := env(t)
	if os.Getenv("DRS_TEST_PLAIN_USER") == "" {
		t.Skip("no unprivileged test user")
	}
	cred.User, cred.Password = os.Getenv("DRS_TEST_PLAIN_USER"), os.Getenv("DRS_TEST_PLAIN_PASSWORD")
	ctx := context.Background()
	r, err := Dial(ctx, target, cred)
	if err != nil {
		return // refused at the bind: fine
	}
	defer r.Close(ctx)
	if nt, err := r.NTHash(ctx, guidBytes(t, os.Getenv("DRS_TEST_ADMIN_GUID"))); err == nil {
		t.Fatalf("a user without replication rights read a hash (%d bytes)", len(nt))
	} else {
		t.Logf("refused as expected: %v", err)
	}
}

func TestWrongPassword(t *testing.T) {
	target, cred := env(t)
	cred.Password += "x"
	if _, err := Dial(context.Background(), target, cred); err == nil {
		t.Fatal("bind with a wrong password succeeded")
	}
}
