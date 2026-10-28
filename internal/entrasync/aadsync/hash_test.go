package aadsync

import (
	"encoding/hex"
	"regexp"
	"testing"
)

// Vector computed with hashlib.pbkdf2_hmac the way AADInternals_python's create_aadhash does, with its commented
// fixed salt.
func TestPasswordHashVector(t *testing.T) {
	nt, _ := hex.DecodeString("8846F7EAEE8FB117AD06BDD830B7586C")
	salt, _ := hex.DecodeString("b477124de54c2030378f")
	got, err := passwordHash(nt, salt)
	want := "v1;PPH1_MD4,b477124de54c2030378f,1000,4c4243e74323445464bdecff0b09d777f8d98794b554b6ba518e9b51f6aace6c;"
	if err != nil || got != want {
		t.Fatalf("passwordHash = %q, %v; want %q", got, err, want)
	}
}

func TestPasswordHashRandomSalt(t *testing.T) {
	nt := make([]byte, 16)
	a, err := PasswordHash(nt)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := PasswordHash(nt)
	if a == b || !regexp.MustCompile(`^v1;PPH1_MD4,[0-9a-f]{20},1000,[0-9a-f]{64};$`).MatchString(a) {
		t.Fatalf("hashes %q and %q", a, b)
	}
	if _, err := PasswordHash(make([]byte, 15)); err == nil {
		t.Fatal("a 15-byte hash was accepted")
	}
}
