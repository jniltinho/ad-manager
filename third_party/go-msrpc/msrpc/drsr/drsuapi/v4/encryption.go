package drsuapi

import (
	"context"
	"crypto/md5"
	"crypto/rc4"
	"encoding/binary"
	"fmt"
	"hash/crc32"

	"github.com/oiweiwei/go-msrpc/ndr"
	"github.com/oiweiwei/go-msrpc/ssp/crypto"
	"github.com/oiweiwei/go-msrpc/ssp/gssapi"
)

// DecryptData decrypts the data using the session key from the context.
func DecryptData(ctx context.Context, b []byte) ([]byte, error) {
	key, ok := gssapi.GetAttribute(ctx, gssapi.AttributeSessionKey)
	if !ok {
		return nil, fmt.Errorf("unable to get session key")
	}
	bkey, _ := key.([]byte)
	return DecryptDataWithKey(ctx, bkey, b)
}

// DecryptDataWithKey decrypts the data using the provided key.
func DecryptDataWithKey(ctx context.Context, key []byte, b []byte) ([]byte, error) {

	if len(key) == 0 {
		return nil, fmt.Errorf("unable to get session key")
	}

	payload := EncryptedPayload{}

	if err := ndr.Unmarshal(b, &payload, ndr.Opaque); err != nil {
		return nil, fmt.Errorf("unmarshal encrypted payload: %w", err)
	}

	// ad-manager patch: the checksum is encrypted together with the data ([MS-DRSR] 4.1.10.6.22); decrypt both and
	// refuse a payload whose CRC32 does not match, so a wrong session key or a corrupt reply never yields a value.
	if len(payload.Salt) != 16 {
		return nil, fmt.Errorf("encrypted payload: salt of %d bytes", len(payload.Salt))
	}

	h := md5.New()
	h.Write(key)
	h.Write(payload.Salt)
	key = h.Sum(nil)

	data := binary.LittleEndian.AppendUint32(make([]byte, 0, len(payload.EncryptedData)+4), payload.CheckSum)
	data = append(data, payload.EncryptedData...)

	cipher, err := rc4.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("init cipher: %w", err)
	}
	cipher.XORKeyStream(data, data)

	if crc32.ChecksumIEEE(data[4:]) != binary.LittleEndian.Uint32(data[:4]) {
		return nil, fmt.Errorf("encrypted payload: checksum mismatch (wrong session key or corrupt data)")
	}

	return data[4:], nil
}

// DecryptHash decrypts the hash using the session key from context and rid.
func DecryptHash(ctx context.Context, rid uint32, b []byte) ([]byte, error) {
	b, err := DecryptData(ctx, b)
	if err != nil {
		return nil, err
	}
	if len(b) != 16 { // ad-manager patch: DES_ECB_LM slices 16 bytes and panics on less
		return nil, fmt.Errorf("encrypted hash: %d bytes, want 16", len(b))
	}
	return crypto.DES_ECB_LM(rid, b)
}

// DecryptHashWithKey decrypts the hash using the provided key and rid.
func DecryptHashWithKey(ctx context.Context, key []byte, rid uint32, b []byte) ([]byte, error) {
	b, err := DecryptDataWithKey(ctx, key, b)
	if err != nil {
		return nil, err
	}
	if len(b) != 16 { // ad-manager patch: DES_ECB_LM slices 16 bytes and panics on less
		return nil, fmt.Errorf("encrypted hash: %d bytes, want 16", len(b))
	}
	return crypto.DES_ECB_LM(rid, b)
}
