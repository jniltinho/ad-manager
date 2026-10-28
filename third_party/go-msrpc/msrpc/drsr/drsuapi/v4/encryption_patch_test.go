package drsuapi

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rc4"
	"encoding/binary"
	"hash/crc32"
	"testing"

	"github.com/oiweiwei/go-msrpc/ndr"
	"github.com/oiweiwei/go-msrpc/ssp/crypto"
)

// ad-manager patch (PATCHED.md): the payload's CRC32 is checked and short hashes are refused.

func encrypt(t *testing.T, key, salt, plain []byte) []byte {
	t.Helper()
	h := md5.New()
	h.Write(key)
	h.Write(salt)
	c, _ := rc4.NewCipher(h.Sum(nil))
	data := binary.LittleEndian.AppendUint32(nil, crc32.ChecksumIEEE(plain))
	data = append(data, plain...)
	c.XORKeyStream(data, data)
	b, err := ndr.Marshal(&EncryptedPayload{Salt: salt, CheckSum: binary.LittleEndian.Uint32(data[:4]), EncryptedData: data[4:]}, ndr.Opaque)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecryptPatched(t *testing.T) {
	ctx := context.Background()
	key, salt := bytes.Repeat([]byte{7}, 16), bytes.Repeat([]byte{9}, 16)
	nt := []byte("0123456789abcdef")
	b := encrypt(t, key, salt, nt)
	if got, err := DecryptDataWithKey(ctx, key, b); err != nil || !bytes.Equal(got, nt) {
		t.Fatalf("DecryptDataWithKey = %x, %v", got, err)
	}
	want, _ := crypto.DES_ECB_LM(1104, nt)
	if got, err := DecryptHashWithKey(ctx, key, 1104, b); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("DecryptHashWithKey = %x, %v", got, err)
	}
	if _, err := DecryptDataWithKey(ctx, bytes.Repeat([]byte{8}, 16), b); err == nil {
		t.Error("wrong session key accepted")
	}
	corrupt := bytes.Clone(b)
	corrupt[len(corrupt)-1] ^= 1
	if _, err := DecryptDataWithKey(ctx, key, corrupt); err == nil {
		t.Error("corrupt payload accepted")
	}
	short := encrypt(t, key, salt, nt[:7])
	if _, err := DecryptHashWithKey(ctx, key, 1104, short); err == nil {
		t.Error("7-byte hash accepted")
	}
	for i := 0; i < len(b); i++ { // every truncation fails, without a panic
		if _, err := DecryptHashWithKey(ctx, key, 1104, b[:i]); err == nil {
			t.Errorf("payload truncated to %d bytes accepted", i)
		}
	}
}
