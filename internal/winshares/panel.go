package winshares

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"ad-manager/internal/shares"
)

// hostRe is a server to read: a DNS name or an IPv4 address (never an option such as -c, nor a path).
var hostRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,62})(\.[A-Za-z0-9]([A-Za-z0-9-]{0,62}))*$`)

// ErrHost reports a server name that is not a DNS name or an IPv4 address.
var ErrHost = errors.New("invalid server name")

// ValidHost checks the name of the server to read.
func ValidHost(host string) error {
	if !hostRe.MatchString(host) {
		return ErrHost
	}
	return nil
}

// AuthFile writes the rpcclient/smbcacls credential file (-A) in dir with mode 0600 and returns its path; the
// caller removes it. user may be user@domain.example (the domain is then its NetBIOS-style first label) or user
// with domain given.
func AuthFile(dir, user, password, domain string) (string, error) {
	if strings.ContainsAny(user+password+domain, "\r\n") {
		return "", errors.New("credentials with a line break")
	}
	name, dom, ok := strings.Cut(user, "@")
	if ok && domain == "" {
		domain = strings.Split(dom, ".")[0]
	}
	f, err := os.CreateTemp(dir, "adc-auth-*")
	if err != nil {
		return "", err
	}
	_, err = fmt.Fprintf(f, "username=%s\npassword=%s\ndomain=%s\n", name, password, strings.ToUpper(domain))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

// CSVRows turns the plans into rows of the shares CSV (shares.CSVHeader), the format the panel's "Folders" import
// reads.
func CSVRows(plans []SharePlan) [][]string {
	rows := make([][]string, 0, len(plans))
	for _, p := range plans {
		rows = append(rows, shares.CSVRow(shares.Share{Name: p.Name, Path: p.Path, Comment: p.Comment, RW: p.RW, RO: p.RO,
			RWUsers: p.RWUsers, ROUsers: p.ROUsers, Full: p.Full, FullUsers: p.FullUser, Browse: !strings.HasSuffix(p.Name, "$")}))
	}
	return rows
}

// KeepDomain drops, from every share's permission and root ACL, the allowed entries of another domain or of a local
// account (only netbios\x and the well-known BUILTIN\, NT AUTHORITY\ and unprefixed names stay): Plan intersects
// the share and NTFS sides on short names, so OUTRO\X on one side must not stand in for netbios\X on the other.
// Deny entries stay (they make the share refused). It returns the dropped names per share (lower-case name).
func KeepDomain(list []Share, netbios string) map[string][]string {
	dropped := map[string][]string{}
	for i := range list {
		key := strings.ToLower(list[i].Name)
		keep := func(acl []ACE) []ACE {
			return slices.DeleteFunc(acl, func(a ACE) bool {
				dom, _, ok := strings.Cut(a.Principal, `\`)
				drop := ok && !a.Denied && !strings.EqualFold(dom, netbios) && !strings.EqualFold(dom, "BUILTIN") && !strings.EqualFold(dom, "NT AUTHORITY")
				if drop && !slices.Contains(dropped[key], a.Principal) {
					dropped[key] = append(dropped[key], a.Principal)
				}
				return drop
			})
		}
		list[i].ShareACL, list[i].NTFS = keep(list[i].ShareACL), keep(list[i].NTFS)
	}
	return dropped
}
