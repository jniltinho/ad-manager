// Package suppcreds reads and rebuilds the supplementalCredentials attribute ([MS-SAMR] 2.2.10.1 USER_PROPERTIES;
// Samba's drsblobs.supplementalCredentialsBlob) for the password migration: which packages it holds, the Kerberos
// salt, and a copy holding only the Kerberos packages, serialized byte for byte as Samba's NDR does (Samba's write
// path re-serializes the blob and refuses one that differs).
//
// It never logs or returns key material in an error; reasons name packages, never values.
package suppcreds

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// The Kerberos packages copied when the salt transfers, newest first; everything else is dropped.
const (
	KerberosNewer = "Primary:Kerberos-Newer-Keys"
	Kerberos      = "Primary:Kerberos"
	Cleartext     = "Primary:CLEARTEXT"
	packagesIndex = "Packages"
)

// Package is one USER_PROPERTY: its name, the reserved word Samba keeps (1, or 2 for "Packages") and its value as
// stored, hexadecimal ASCII.
type Package struct {
	Name     string
	Reserved uint16
	Data     []byte
}

// Blob is a parsed supplementalCredentials value.
type Blob struct {
	Packages []Package
}

const (
	headerLen = 12   // unknown1, the sub-blob's size, unknown2
	prefixLen = 0x60 // 0x30 UTF-16 characters
	signature = 0x50 // SUPPLEMENTAL_CREDENTIALS_SIGNATURE ('P')
)

var errMalformed = errors.New("malformed supplementalCredentials")

// Parse reads a supplementalCredentials value strictly: every length must fit and nothing may be left over.
func Parse(b []byte) (*Blob, error) {
	if len(b) < headerLen+prefixLen+4+1 {
		return nil, errMalformed
	}
	size := int(binary.LittleEndian.Uint32(b[4:8]))
	if size < prefixLen+4 || headerLen+size+1 != len(b) {
		return nil, errMalformed
	}
	sub := b[headerLen : headerLen+size]
	if binary.LittleEndian.Uint16(sub[prefixLen:]) != signature {
		return nil, errMalformed
	}
	n := int(binary.LittleEndian.Uint16(sub[prefixLen+2:]))
	p := sub[prefixLen+4:]
	out := &Blob{Packages: make([]Package, 0, n)}
	for range n {
		if len(p) < 6 {
			return nil, errMalformed
		}
		nl, dl, res := int(binary.LittleEndian.Uint16(p)), int(binary.LittleEndian.Uint16(p[2:])), binary.LittleEndian.Uint16(p[4:])
		p = p[6:]
		if nl%2 != 0 || len(p) < nl+dl {
			return nil, errMalformed
		}
		u := make([]uint16, nl/2)
		for i := range u {
			u[i] = binary.LittleEndian.Uint16(p[2*i:])
		}
		out.Packages = append(out.Packages, Package{Name: string(utf16.Decode(u)), Reserved: res, Data: bytes.Clone(p[nl : nl+dl])})
		p = p[nl+dl:]
	}
	if len(p) != 0 {
		return nil, errMalformed
	}
	return out, nil
}

// Bytes serializes the blob as Samba's ndr_pack does: zero unknowns, the sub-blob's size, a prefix of 0x30 UTF-16
// spaces and the 'P' signature.
func (bl *Blob) Bytes() []byte {
	var sub bytes.Buffer
	for range prefixLen / 2 {
		sub.Write([]byte{' ', 0})
	}
	w16 := func(v int) { _ = binary.Write(&sub, binary.LittleEndian, uint16(v)) }
	w16(signature)
	w16(len(bl.Packages))
	for _, pk := range bl.Packages {
		name := utf16.Encode([]rune(pk.Name))
		w16(2 * len(name))
		w16(len(pk.Data))
		w16(int(pk.Reserved))
		for _, c := range name {
			w16(int(c))
		}
		sub.Write(pk.Data)
	}
	out := make([]byte, headerLen, headerLen+sub.Len()+1)
	binary.LittleEndian.PutUint32(out[4:], uint32(sub.Len()))
	out = append(out, sub.Bytes()...)
	return append(out, 0)
}

// Zero overwrites the packages' values (the keys they hold).
func (bl *Blob) Zero() {
	for i := range bl.Packages {
		clear(bl.Packages[i].Data)
	}
}

// Has reports whether the blob holds a package with that name.
func (bl *Blob) Has(name string) bool {
	for _, pk := range bl.Packages {
		if pk.Name == name {
			return true
		}
	}
	return false
}

// KerberosSalt returns the default salt of a Primary:Kerberos (revision 3) or Primary:Kerberos-Newer-Keys
// (revision 4) package value (hexadecimal ASCII), checking that every key it lists lies inside the value.
func KerberosSalt(data []byte) (string, error) {
	raw := make([]byte, hex.DecodedLen(len(data)))
	defer clear(raw)
	if _, err := hex.Decode(raw, data); err != nil {
		return "", errMalformed
	}
	u16 := func(off int) (int, bool) {
		if off+2 > len(raw) {
			return 0, false
		}
		return int(binary.LittleEndian.Uint16(raw[off:])), true
	}
	u32 := func(off int) (int, bool) {
		if off+4 > len(raw) {
			return 0, false
		}
		return int(binary.LittleEndian.Uint32(raw[off:])), true
	}
	rev, ok := u16(0)
	if !ok {
		return "", errMalformed
	}
	var keys, saltAt, keysAt, keyLen int
	switch rev {
	case 3: // KERB_STORED_CREDENTIAL: credentials and old credentials, KERB_KEY_DATA (20 bytes)
		a, _ := u16(4)
		b, _ := u16(6)
		keys, saltAt, keysAt, keyLen = a+b, 8, 16, 20
	case 4: // KERB_STORED_CREDENTIAL_NEW: four key lists, KERB_KEY_DATA_NEW (24 bytes)
		for i := range 4 {
			v, _ := u16(4 + 2*i)
			keys += v
		}
		saltAt, keysAt, keyLen = 12, 24, 24
	default:
		return "", fmt.Errorf("revision %d", rev)
	}
	inside := func(off, n int) bool { return off >= 0 && n >= 0 && off+n <= len(raw) }
	if !inside(keysAt, keys*keyLen) {
		return "", errMalformed
	}
	for i := range keys {
		e := keysAt + i*keyLen
		l, _ := u32(e + keyLen - 8)
		o, _ := u32(e + keyLen - 4)
		if !inside(o, l) {
			return "", errMalformed
		}
	}
	sl, _ := u16(saltAt)
	so, ok := u32(saltAt + 4)
	if !ok || sl%2 != 0 || !inside(so, sl) {
		return "", errMalformed
	}
	s := make([]uint16, sl/2)
	for i := range s {
		s[i] = binary.LittleEndian.Uint16(raw[so+2*i:])
	}
	return string(utf16.Decode(s)), nil
}

// RealmSaltMatches reports whether a principal's Kerberos keys transfer: the source realm must equal the destination
// realm exactly (the salt has no separator, so it cannot mark the realm's end: EXAMPLE.NETWORK vs EXAMPLE.NET), and
// the salt must be the default realm-prefixed one (a custom salt is not copied blindly).
func RealmSaltMatches(salt, destRealm, srcRealm string) bool {
	if salt == "" || destRealm == "" || srcRealm == "" || !strings.EqualFold(srcRealm, destRealm) {
		return false
	}
	s, r := strings.ToUpper(salt), strings.ToUpper(destRealm)
	return strings.HasPrefix(s, r) && !strings.HasPrefix(s, r+".")
}

// Inspection is what the password migration needs from a source's supplementalCredentials.
type Inspection struct {
	Cleartext bool   // a Primary:CLEARTEXT package: the user is rejected
	Kerberos  bool   // the Kerberos keys transfer: Rebuilt holds them
	Reason    string // why they do not (secret-free), empty when they do
	Rebuilt   []byte // only the Kerberos packages and their "Packages" index
}

// Inspect classifies a source's supplementalCredentials for destRealm, as the Python helper did: a layout it cannot
// use is never an error, only Kerberos=false with a reason, so the user falls back to the NT hash.
func Inspect(supp []byte, destRealm, srcRealm string) Inspection {
	if len(supp) == 0 {
		return Inspection{Reason: "no supplementalCredentials (NTLM only)"}
	}
	bl, err := Parse(supp)
	if err != nil {
		return Inspection{Reason: "supplementalCredentials unreadable (NTLM only)"}
	}
	defer bl.Zero()
	in := Inspection{Cleartext: bl.Has(Cleartext)}
	var names, kerb []string
	for _, pk := range bl.Packages {
		names = append(names, pk.Name)
		if pk.Name == KerberosNewer || pk.Name == Kerberos {
			kerb = append(kerb, pk.Name)
		}
	}
	if len(kerb) == 0 {
		have := strings.Join(names, ", ")
		if have == "" {
			have = "none"
		}
		in.Reason = "no Kerberos package (have: " + have + ") → NTLM only"
		return in
	}
	for _, name := range []string{KerberosNewer, Kerberos} {
		var pk *Package
		for i := range bl.Packages {
			if bl.Packages[i].Name == name {
				pk = &bl.Packages[i]
				break
			}
		}
		if pk == nil {
			continue
		}
		salt, err := KerberosSalt(pk.Data)
		if err != nil {
			in.Reason = name + " parse failed → NTLM only"
			return in
		}
		if salt == "" {
			in.Reason = name + " has no salt → NTLM only"
			return in
		}
		if !RealmSaltMatches(salt, destRealm, srcRealm) {
			in.Reason = name + " salt does not match realm " + destRealm + " → NTLM only"
			return in
		}
		// only the Kerberos packages, in their order, then the index naming just them (Samba checks
		// num_packages == names + 1); WDigest and anything else are dropped
		var keep []Package
		var index *Package
		for _, p := range bl.Packages {
			switch p.Name {
			case KerberosNewer, Kerberos:
				keep = append(keep, p)
			case packagesIndex:
				index = &p
			}
		}
		if index != nil {
			u := utf16.Encode([]rune(strings.Join(kerb, "\x00")))
			b := make([]byte, 2*len(u))
			for i, c := range u {
				binary.LittleEndian.PutUint16(b[2*i:], c)
			}
			index.Data = []byte(strings.ToUpper(hex.EncodeToString(b)))
			keep = append(keep, *index)
		}
		in.Kerberos, in.Rebuilt = true, (&Blob{Packages: keep}).Bytes()
		return in
	}
	in.Reason = "no usable Kerberos package → NTLM only"
	return in
}
