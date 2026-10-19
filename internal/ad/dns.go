package ad

import (
	"errors"
	"net/netip"
	"regexp"
	"slices"
	"strings"
)

// dnsLabelName is a host name: labels of letters, digits and - (no _: the SRV locator names are not edited).
var dnsLabelName = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,62})(\.[A-Za-z0-9]([A-Za-z0-9-]{0,62}))*$`)

// reverseHost is the name of a PTR in its reverse zone: the host part of the address (decimal octets, or the hex
// nibbles of IPv6).
var reverseHost = regexp.MustCompile(`^[0-9a-f]{1,3}(\.[0-9a-f]{1,3})*$`)

// Errors of CheckDNSRecord.
var (
	ErrDNSType      = errors.New("only A, CNAME and PTR records can be changed")
	ErrDNSZone      = errors.New("this zone is managed by the domain controllers")
	ErrDNSName      = errors.New("invalid record name")
	ErrDNSData      = errors.New("invalid record data")
	ErrDNSProtected = errors.New("this record belongs to the domain controllers")
)

// CheckDNSRecord is the protection rule of a DNS write (add or delete): only A and CNAME in a forward zone and PTR
// in a reverse one, never the zone itself (@: SOA, NS and the DCs' addresses), the _msdcs zone, a name with a _
// label (SRV locator), DomainDnsZones/ForestDnsZones, a name of a DC (dcs: their dNSHostName) nor a PTR that points
// to one. name is relative to zone (a full name inside the zone is made relative, as the DNS server does); data is
// an IPv4 address (A) or a host name (CNAME, PTR). It returns the name and the data in the form sent to the server
// (the checked name; host names in the data end with a dot).
func CheckDNSRecord(zone, name, typ, data string, dcs []string) (string, string, error) {
	zone = strings.ToLower(strings.TrimSuffix(zone, "."))
	reverse := strings.HasSuffix(zone, ".in-addr.arpa") || strings.HasSuffix(zone, ".ip6.arpa")
	switch {
	case typ != "A" && typ != "CNAME" && typ != "PTR":
		return "", "", ErrDNSType
	case zone == "" || strings.HasPrefix(zone, "_msdcs.") || (typ == "PTR") != reverse:
		return "", "", ErrDNSZone
	}
	// the server strips the zone from a name that ends with it (and "zone" itself is the apex): check, and send, the
	// relative name; a name that still names the zone after that is refused (no second guess at the server's parsing)
	n := strings.ToLower(name)
	if n == zone || strings.HasSuffix(n, "."+zone) {
		n = strings.TrimSuffix(strings.TrimSuffix(n, zone), ".")
	}
	if n == "" || n == zone || strings.HasSuffix(n, "."+zone) || strings.HasSuffix(n, ".") {
		return "", "", ErrDNSName
	}
	// Samba also strips a partial suffix (dc1.net in example.net is dc1 to it; Windows reads dc1.net.example.net):
	// a name whose last label is the zone's would mean different nodes, so it is refused
	if l := strings.LastIndex(n, "."); l >= 0 && n[l+1:] == zone[strings.LastIndex(zone, ".")+1:] {
		return "", "", ErrDNSName
	}
	if reverse { // the name of a PTR is the host part of the address: digits, or hex nibbles for IPv6
		if !reverseHost.MatchString(n) {
			return "", "", ErrDNSName
		}
	} else if !dnsLabelName.MatchString(n) {
		return "", "", ErrDNSName
	}
	fqdn := n + "." + zone
	for _, l := range strings.Split(n, ".") {
		if l == "domaindnszones" || l == "forestdnszones" || l == "gc" {
			return "", "", ErrDNSProtected
		}
	}
	isDC := func(host string) bool {
		return slices.ContainsFunc(dcs, func(dc string) bool { return dc != "" && strings.EqualFold(strings.TrimSuffix(dc, "."), host) })
	}
	if isDC(fqdn) {
		return "", "", ErrDNSProtected
	}
	switch typ {
	case "A":
		ip, err := netip.ParseAddr(data)
		if err != nil || !ip.Is4() {
			return "", "", ErrDNSData
		}
		return n, ip.String(), nil
	default:
		host := strings.ToLower(strings.TrimSuffix(data, "."))
		if !dnsLabelName.MatchString(host) || strings.Count(host, ".") == 0 {
			return "", "", ErrDNSData // a CNAME or PTR target is a full name
		}
		if typ == "PTR" && isDC(host) {
			return "", "", ErrDNSProtected
		}
		return n, host + ".", nil
	}
}
