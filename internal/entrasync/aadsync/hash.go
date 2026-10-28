// Package aadsync talks to the Azure AD Sync service (provisioningservice.svc) as AADInternals_python does.
package aadsync

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// hashIterations is the PBKDF2 count Azure AD Connect uses.
const hashIterations = 1000

// PasswordHash returns the credential data ProvisionCredentials takes for an NT hash (16 bytes):
// v1;PPH1_MD4,<salt hex>,1000,<PBKDF2-SHA256 of the uppercase hex hash in UTF-16LE, hex>;
func PasswordHash(nt []byte) (string, error) {
	salt := make([]byte, 10)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	return passwordHash(nt, salt)
}

func passwordHash(nt, salt []byte) (string, error) {
	if len(nt) != 16 {
		return "", errors.New("aadsync: an NT hash has 16 bytes")
	}
	u := utf16.Encode([]rune(strings.ToUpper(hex.EncodeToString(nt))))
	in := make([]byte, 0, 2*len(u))
	for _, c := range u {
		in = append(in, byte(c), byte(c>>8))
	}
	key, err := pbkdf2.Key(sha256.New, string(in), salt, hashIterations, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("v1;PPH1_MD4,%x,%d,%x;", salt, hashIterations, key), nil
}
