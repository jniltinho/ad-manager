package sambabridge

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The Go reader of the Windows ACL (security.NTACL) answers a read only when Samba itself would return the stored
// descriptor as is: an xattr_NTACL version 4 with a SHA-256 of the POSIX ACL that still matches the folder (Samba's
// acl_xattr does the same check, vfs_acl_common.c validate_nt_acl_blob). Anything else (other versions, a POSIX ACL
// changed by setfacl, unusual ACEs, a malformed blob) is errNotMine, and the Python helper answers.

var errNotMine = errors.New("not readable in Go")

// ntACLXattr is the xattr Samba's acl_xattr keeps the descriptor in (unless acl_xattr:security_acl_name says otherwise).
const ntACLXattr = "security.NTACL"

const (
	maxNTACLBlob = 64 << 10 // the most an xattr holds on ext4/xfs
	maxACEs      = 2000     // the bound of Samba's NDR (security_acl.num_aces)

	sdDACLPresent   = 0x0004
	sdDACLAutoReq   = 0x0100
	sdDACLAutoInher = 0x0400
	sdDACLProtected = 0x1000

	aceInherited = 0x10

	maskFull     = 0x1F01FF
	maskModify   = 0x1301BF
	maskReadExec = 0x1200A9
)

type sid struct {
	auth uint64 // 48 bits
	subs []uint32
}

// String is the form of Samba's dom_sid_string_buf.
func (s sid) String() string {
	var b strings.Builder
	b.WriteString("S-1-")
	if s.auth >= 0xFFFFFFFF {
		fmt.Fprintf(&b, "0x%x", s.auth)
	} else {
		b.WriteString(strconv.FormatUint(s.auth, 10))
	}
	for _, v := range s.subs {
		b.WriteByte('-')
		b.WriteString(strconv.FormatUint(uint64(v), 10))
	}
	return b.String()
}

type ace struct {
	typ, flags uint8
	mask       uint32
	trustee    sid
}

type secDesc struct {
	typ          uint16
	owner, group *sid
	dacl         []ace
	hasDACL      bool
}

// reader walks a blob with bounds checks.
type reader struct {
	b   []byte
	err bool
}

func (r *reader) at(off, n int) []byte {
	if r.err || off < 0 || n < 0 || off > len(r.b)-n {
		r.err = true
		return make([]byte, n)
	}
	return r.b[off : off+n]
}
func (r *reader) u8(off int) uint8   { return r.at(off, 1)[0] }
func (r *reader) u16(off int) uint16 { return binary.LittleEndian.Uint16(r.at(off, 2)) }
func (r *reader) u32(off int) uint32 { return binary.LittleEndian.Uint32(r.at(off, 4)) }

func align4(n int) int { return (n + 3) &^ 3 }

// parseNTACL decodes an NDR xattr_NTACL version 4 (librpc/idl/xattr.idl) and returns its descriptor and the SHA-256
// of the POSIX ACL it was written with. The descriptor's offsets count from the start of the blob, as Samba's NDR
// writes them.
func parseNTACL(blob []byte) (*secDesc, []byte, error) {
	if len(blob) > maxNTACLBlob {
		return nil, nil, errNotMine
	}
	r := &reader{b: blob}
	// version, union level, unique pointer to security_descriptor_hash_v4
	if r.u16(0) != 4 || r.u16(2) != 4 || r.u32(4) == 0 {
		return nil, nil, errNotMine
	}
	// security_descriptor_hash_v4: sd pointer, hash_type, hash[64], description (UTF-8, NUL-terminated),
	// NTTIME (aligned to 4), sys_acl_hash[64]; then the descriptor
	if r.u32(8) == 0 || r.u16(12) != 1 { // XATTR_SD_HASH_TYPE_SHA256
		return nil, nil, errNotMine
	}
	off := 14 + 64
	end := off
	for end < len(blob) && end-off < 256 && blob[end] != 0 {
		end++
	}
	if end >= len(blob) || blob[end] != 0 {
		return nil, nil, errNotMine
	}
	for _, c := range blob[off:end] { // Samba converts it to the unix charset and may refuse other bytes
		if c < 0x20 || c > 0x7e {
			return nil, nil, errNotMine
		}
	}
	off = align4(end+1) + 8
	sysHash := r.at(off, 64)
	sdStart := align4(off + 64)
	if r.err {
		return nil, nil, errNotMine
	}
	for _, b := range sysHash[32:] { // a SHA-256 fills 32 of the 64 bytes
		if b != 0 {
			return nil, nil, errNotMine
		}
	}
	sd, err := parseSD(r, sdStart)
	if err != nil {
		return nil, nil, err
	}
	return sd, sysHash[:32], nil
}

func parseSD(r *reader, start int) (*secDesc, error) {
	if r.u8(start) != 1 { // SECURITY_DESCRIPTOR_REVISION_1
		return nil, errNotMine
	}
	sd := &secDesc{typ: r.u16(start + 2)}
	ptr := func(p int) (int, bool) {
		v := r.u32(p)
		if v == 0 {
			return 0, false
		}
		// Samba's NDR aligns each part to 4 before reading it: an odd offset would read elsewhere there
		if int64(v) < int64(start)+20 || int64(v) >= int64(len(r.b)) || v%4 != 0 {
			r.err = true
		}
		return int(v), true
	}
	if o, ok := ptr(start + 4); ok {
		s := parseSID(r, o)
		sd.owner = &s
	}
	if o, ok := ptr(start + 8); ok {
		s := parseSID(r, o)
		sd.group = &s
	}
	if _, ok := ptr(start + 12); ok { // a SACL: Samba parses (and may refuse) it before dropping it; the helper reads those
		return nil, errNotMine
	}
	if o, ok := ptr(start + 16); ok {
		sd.hasDACL = true
		sd.dacl = parseACL(r, o)
		sd.typ |= sdDACLPresent // make_sec_desc, on Samba's way out of the blob
	}
	if r.err {
		return nil, errNotMine
	}
	return sd, nil
}

func parseSID(r *reader, off int) sid {
	n := int(r.u8(off + 1))
	if r.u8(off) != 1 || n > 15 {
		r.err = true
		return sid{}
	}
	a := r.at(off+2, 6)
	s := sid{auth: uint64(a[0])<<40 | uint64(a[1])<<32 | uint64(binary.BigEndian.Uint32(a[2:]))}
	for i := range n {
		s.subs = append(s.subs, r.u32(off+8+4*i))
	}
	return s
}

func sidLen(s sid) int { return 8 + 4*len(s.subs) }

func parseACL(r *reader, off int) []ace {
	// NDR security_acl: revision (uint16), size (uint16), num_aces (uint32)
	rev, size, n := r.u16(off), int(r.u16(off+2)), r.u32(off+4)
	if (rev != 2 && rev != 4) || n > maxACEs || size < 8 {
		r.err = true
		return nil
	}
	end := off + size
	aces := make([]ace, 0, n)
	p := off + 8
	for range n {
		typ, flags, asize := r.u8(p), r.u8(p+1), int(r.u16(p+2))
		if typ > 1 { // only ACCESS_ALLOWED and ACCESS_DENIED: object, callback and audit ACEs go to the helper
			r.err = true
			return nil
		}
		a := ace{typ: typ, flags: flags, mask: r.u32(p + 4), trustee: parseSID(r, p+8)}
		if r.err || asize < 8+sidLen(a.trustee) || asize%4 != 0 || p+asize > end {
			r.err = true
			return nil
		}
		aces = append(aces, a)
		p += asize
	}
	r.at(off, size) // the ACL lies inside the blob
	return aces
}

// posixEntry is one entry of a POSIX ACL, with Samba's tags (smb_acl.idl): USER 1, USER_OBJ 2, GROUP 3,
// GROUP_OBJ 4, OTHER 5, MASK 6.
type posixEntry struct {
	tag  uint16
	perm uint32
	id   uint32 // uid or gid for USER and GROUP
}

// parsePosixXattr decodes a system.posix_acl_* xattr (Linux: version 2, then tag/perm/id entries) into Samba's tags.
func parsePosixXattr(b []byte) ([]posixEntry, error) {
	if len(b) < 4 || (len(b)-4)%8 != 0 || binary.LittleEndian.Uint32(b) != 2 {
		return nil, errNotMine
	}
	var out []posixEntry
	for p := 4; p < len(b); p += 8 {
		e := posixEntry{perm: uint32(binary.LittleEndian.Uint16(b[p+2:]))}
		switch binary.LittleEndian.Uint16(b[p:]) {
		case 0x01:
			e.tag = 2
		case 0x02:
			e.tag, e.id = 1, binary.LittleEndian.Uint32(b[p+4:])
		case 0x04:
			e.tag = 4
		case 0x08:
			e.tag, e.id = 3, binary.LittleEndian.Uint32(b[p+4:])
		case 0x10:
			e.tag = 6
		case 0x20:
			e.tag = 5
		default:
			return nil, errNotMine
		}
		if e.perm > 7 {
			return nil, errNotMine
		}
		out = append(out, e)
	}
	return out, nil
}

// modeACL is the access ACL of a file without one, built from its mode as libacl's acl_from_mode does.
func modeACL(mode uint32) []posixEntry {
	return []posixEntry{{tag: 2, perm: mode >> 6 & 7}, {tag: 4, perm: mode >> 3 & 7}, {tag: 5, perm: mode & 7}}
}

// posixACLHash is the SHA-256 Samba keeps in xattr_NTACL v4: of the NDR smb_acl_wrapper (smb_acl.idl) with the
// access and default ACLs, owner, group and mode (smbd/posix_acls.c posix_sys_acl_blob_get_fd). A nil default ACL is
// a NULL pointer (files); a directory without one has an empty ACL.
func posixACLHash(access, def []posixEntry, uid, gid, mode uint32) []byte {
	var b []byte
	pad := func(n int) {
		for len(b)%n != 0 {
			b = append(b, 0)
		}
	}
	ref := uint32(0x20000) // NDR unique pointers: 0x20000, 0x20004, …
	ptr := func(acl []posixEntry) {
		if acl == nil {
			b = binary.LittleEndian.AppendUint32(b, 0)
			return
		}
		b = binary.LittleEndian.AppendUint32(b, ref)
		ref += 4
	}
	ptr(access)
	ptr(def)
	pad(8)
	b = binary.LittleEndian.AppendUint64(b, uint64(uid))
	b = binary.LittleEndian.AppendUint64(b, uint64(gid))
	b = binary.LittleEndian.AppendUint32(b, mode)
	for _, acl := range [][]posixEntry{access, def} {
		if acl == nil {
			continue
		}
		pad(4)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(acl))) // size_is(count)
		pad(8)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(acl))) // count
		b = binary.LittleEndian.AppendUint32(b, 0)                // next
		for _, e := range acl {
			pad(8)
			b = binary.LittleEndian.AppendUint16(b, e.tag) // a_type
			b = binary.LittleEndian.AppendUint16(b, e.tag) // union level
			if e.tag == 1 || e.tag == 3 {
				pad(8)
				b = binary.LittleEndian.AppendUint64(b, uint64(e.id))
			}
			pad(4)
			b = binary.LittleEndian.AppendUint32(b, e.perm)
		}
	}
	h := sha256.Sum256(b)
	return h[:]
}

// sddlAliases are the SIDs Samba's as_sddl() writes as two letters without a domain SID (libcli/security/sddl.c).
var sddlAliases = map[string]string{
	"S-1-1-0": "WD", "S-1-3-0": "CO", "S-1-3-1": "CG", "S-1-3-4": "OW",
	"S-1-5-2": "NU", "S-1-5-4": "IU", "S-1-5-6": "SU", "S-1-5-7": "AN", "S-1-5-9": "ED", "S-1-5-10": "PS",
	"S-1-5-11": "AU", "S-1-5-12": "RC", "S-1-5-18": "SY", "S-1-5-19": "LS", "S-1-5-20": "NS", "S-1-5-33": "WR",
	"S-1-5-32-544": "BA", "S-1-5-32-545": "BU", "S-1-5-32-546": "BG", "S-1-5-32-547": "PU", "S-1-5-32-548": "AO",
	"S-1-5-32-549": "SO", "S-1-5-32-550": "PO", "S-1-5-32-551": "BO", "S-1-5-32-552": "RE", "S-1-5-32-554": "RU",
	"S-1-5-32-555": "RD", "S-1-5-32-556": "NO", "S-1-5-32-558": "MU", "S-1-5-32-559": "LU", "S-1-5-32-568": "IS",
	"S-1-5-32-569": "CY", "S-1-5-32-573": "ER", "S-1-5-32-574": "CD", "S-1-5-32-575": "RA", "S-1-5-32-576": "ES",
	"S-1-5-32-577": "MS", "S-1-5-32-578": "HA", "S-1-5-32-579": "AA", "S-1-5-32-580": "RM",
	"S-1-5-84-0-0-0-0-0": "UD", "S-1-15-2-1": "AC",
	"S-1-16-4096": "LW", "S-1-16-8192": "ME", "S-1-16-8448": "MP", "S-1-16-12288": "HI", "S-1-16-16384": "SI",
	"S-1-18-1": "AS", "S-1-18-2": "SS",
}

func sddlSID(s sid) string {
	str := s.String()
	if a, ok := sddlAliases[str]; ok {
		return a
	}
	return str
}

type flagName struct {
	name string
	flag uint32
}

var (
	sddlACEFlags = []flagName{{"OI", 0x01}, {"CI", 0x02}, {"NP", 0x04}, {"IO", 0x08}, {"ID", 0x10}, {"SA", 0x40}, {"FA", 0x80}}
	sddlRights   = []flagName{{"CC", 0x1}, {"DC", 0x2}, {"LC", 0x4}, {"SW", 0x8}, {"RP", 0x10}, {"WP", 0x20}, {"DT", 0x40},
		{"LO", 0x80}, {"CR", 0x100}, {"SD", 0x10000}, {"RC", 0x20000}, {"WD", 0x40000}, {"WO", 0x80000},
		{"GA", 0x10000000}, {"GX", 0x20000000}, {"GW", 0x40000000}, {"GR", 0x80000000}}
	sddlFileRights = []flagName{{"FA", 0x1F01FF}, {"FR", 0x120089}, {"FW", 0x120116}, {"FX", 0x1200A0}}
	sddlACLFlags   = []flagName{{"P", sdDACLProtected}, {"AR", sdDACLAutoReq}, {"AI", sdDACLAutoInher}}
)

// sddlFlags is sddl_flags_to_string: an exact match, else the names of the bits; ok is false when checkAll and bits
// remain without a name.
func sddlFlags(m []flagName, v uint32, checkAll bool) (string, bool) {
	for _, f := range m {
		if f.flag == v {
			return f.name, true
		}
	}
	var b strings.Builder
	for _, f := range m {
		if v&f.flag != 0 {
			b.WriteString(f.name)
			v &^= f.flag
		}
	}
	return b.String(), !checkAll || v == 0
}

// describe builds what the helper's describe() returns for the descriptor, the SDDL as Samba's as_sddl() writes it.
func describe(sd *secDesc) (*ACL, error) {
	if sd.owner == nil || sd.group == nil {
		return nil, errNotMine
	}
	a := &ACL{Owner: sd.owner.String(), Group: sd.group.String(), ACEs: []ACE{},
		Protected: sd.typ&sdDACLProtected != 0, NullDACL: !sd.hasDACL}
	var s strings.Builder
	s.WriteString("O:" + sddlSID(*sd.owner) + "G:" + sddlSID(*sd.group))
	if sd.typ&sdDACLPresent != 0 && sd.hasDACL {
		f, _ := sddlFlags(sddlACLFlags, uint32(sd.typ), false)
		s.WriteString("D:" + f)
	}
	for _, e := range sd.dacl {
		kind, typ := "allow", "A"
		if e.typ == 1 {
			kind, typ = "deny", "D"
		}
		access := map[uint32]string{maskFull: "full", maskModify: "rw", maskReadExec: "ro"}[e.mask]
		if access == "" {
			access = "custom"
		}
		a.ACEs = append(a.ACEs, ACE{SID: e.trustee.String(), Type: kind, Mask: e.mask, Flags: int(e.flags),
			Inherited: e.flags&aceInherited != 0, Access: access})
		if sd.typ&sdDACLPresent == 0 {
			continue
		}
		flags, ok := sddlFlags(sddlACEFlags, uint32(e.flags), true)
		if !ok {
			return nil, errNotMine // as_sddl() fails on it: let the helper say so
		}
		rights, ok := sddlFlags(sddlRights, e.mask, true)
		if !ok {
			rights = fmt.Sprintf("0x%x", e.mask)
			for _, f := range sddlFileRights {
				if f.flag == e.mask {
					rights = f.name
				}
			}
		}
		fmt.Fprintf(&s, "(%s;%s;%s;;;%s)", typ, flags, rights, sddlSID(e.trustee))
	}
	a.SDDL = s.String()
	return a, nil
}
