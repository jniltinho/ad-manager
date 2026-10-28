// Package samdb holds the value conversions and constants that python3-samba's SamDB and samba.dsdb give: binary
// SIDs and GUIDs as text, FILETIME values, and the userAccountControl and groupType flags.
package samdb

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// userAccountControl flags ([MS-ADTS] 2.2.16), as samba.dsdb.UF_*.
const (
	UFAccountDisable          = 0x00000002
	UFNormalAccount           = 0x00000200
	UFWorkstationTrustAccount = 0x00001000
	UFServerTrustAccount      = 0x00002000
	UFDontExpirePasswd        = 0x00010000
)

// GroupTypeSecurityEnabled marks a security group in groupType, as samba.dsdb.GTYPE_SECURITY_*.
const GroupTypeSecurityEnabled = -0x80000000

// SID formats a binary SID as S-1-5-21-…; it returns "" for a malformed value ([MS-DTYP] 2.4.2.2: revision 1,
// at most 15 sub-authorities).
func SID(b []byte) string {
	if len(b) < 8 || b[0] != 1 || b[1] > 15 || len(b) != 8+4*int(b[1]) {
		return ""
	}
	var auth uint64
	for _, x := range b[2:8] {
		auth = auth<<8 | uint64(x)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "S-%d-%d", b[0], auth)
	for i := 8; i < len(b); i += 4 {
		sb.WriteString("-" + strconv.FormatUint(uint64(binary.LittleEndian.Uint32(b[i:])), 10))
	}
	return sb.String()
}

// RID returns the last sub-authority of a binary SID, and false for a SID without sub-authorities.
func RID(b []byte) (uint32, bool) {
	if SID(b) == "" || b[1] == 0 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(b[len(b)-4:]), true
}

// GUID formats a binary objectGUID in the Windows mixed-endian layout; it returns "" unless b has 16 bytes.
func GUID(b []byte) string {
	if len(b) != 16 {
		return ""
	}
	return fmt.Sprintf("%08x-%04x-%04x-%x-%x", binary.LittleEndian.Uint32(b[0:4]),
		binary.LittleEndian.Uint16(b[4:6]), binary.LittleEndian.Uint16(b[6:8]), b[8:10], b[10:16])
}

// FileTime converts an AD FILETIME (100 ns ticks since 1601, as text) to a time. 0 and 0x7FFFFFFFFFFFFFFF
// (never) give the zero time and false.
func FileTime(s string) (time.Time, bool) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 || v == 0x7FFFFFFFFFFFFFFF {
		return time.Time{}, false
	}
	return time.Unix(v/1e7-11644473600, (v%1e7)*100).UTC(), true
}
