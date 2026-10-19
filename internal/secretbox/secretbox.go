// Package secretbox keeps small secrets (a sync source's password) encrypted at rest: AES-256-GCM with a random
// nonce, the key in a file of its own (0600, made on first use) next to the data it protects, and a context bound
// as additional data so a sealed value cannot be moved to another record.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Box seals and opens with one key.
type Box struct{ aead cipher.AEAD }

// Load reads the 32-byte key at path, creating it (0600, directory 0700) when missing.
func Load(path string) (*Box, error) {
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) { // made meanwhile by another process: use that one
			return Load(path)
		}
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(key); err != nil {
			f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%s: not a 32-byte key", path)
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCMWithRandomNonce(b)
	if err != nil {
		return nil, err
	}
	return &Box{aead: g}, nil
}

// Seal encrypts plain for context ctx.
func (b *Box) Seal(plain []byte, ctx string) []byte { return b.aead.Seal(nil, nil, plain, []byte(ctx)) }

// Open decrypts sealed for context ctx.
func (b *Box) Open(sealed []byte, ctx string) ([]byte, error) {
	out, err := b.aead.Open(nil, nil, sealed, []byte(ctx))
	if err != nil {
		return nil, errors.New("the stored secret does not open (another key or another record)")
	}
	return out, nil
}
