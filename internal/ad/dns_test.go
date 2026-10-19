package ad

import (
	"errors"
	"testing"
)

func TestCheckDNSRecord(t *testing.T) {
	dcs := []string{"DC1.empresa.net", "adm.empresa.net."}
	for _, tt := range []struct {
		name, zone, rname, typ, data, want string
		err                                error
		sent                               string // the name sent to the server (default rname)
	}{
		{"A", "empresa.net", "intranet", "A", "192.168.10.50", "192.168.10.50", nil, ""},
		{"CNAME", "empresa.net.", "wiki", "CNAME", "intranet.empresa.net", "intranet.empresa.net.", nil, ""},
		{"PTR", "10.168.192.in-addr.arpa", "50", "PTR", "intranet.empresa.net.", "intranet.empresa.net.", nil, ""},
		{"SRV refused", "empresa.net", "_ldap._tcp", "SRV", "0 100 389 dc1.empresa.net.", "", ErrDNSType, ""},
		{"NS refused", "empresa.net", "sub", "NS", "dc1.empresa.net.", "", ErrDNSType, ""},
		{"SOA refused", "empresa.net", "@", "SOA", "x", "", ErrDNSType, ""},
		{"zone apex", "empresa.net", "@", "A", "192.168.10.91", "", ErrDNSName, ""},
		{"locator name", "empresa.net", "_kerberos", "A", "10.0.0.1", "", ErrDNSName, ""},
		{"msdcs zone", "_msdcs.empresa.net", "x", "A", "10.0.0.1", "", ErrDNSZone, ""},
		{"DC name", "empresa.net", "dc1", "A", "10.0.0.1", "", ErrDNSProtected, ""},
		{"other DC name, delete", "empresa.net", "ADM", "A", "192.168.10.91", "", ErrDNSProtected, ""},
		{"DC name as CNAME", "empresa.net", "dc1", "CNAME", "intranet.empresa.net", "", ErrDNSProtected, ""},
		{"domain dns zones", "empresa.net", "DomainDnsZones", "A", "10.0.0.1", "", ErrDNSProtected, ""},
		{"gc", "empresa.net", "gc", "A", "10.0.0.1", "", ErrDNSProtected, ""},
		{"PTR to a DC", "10.168.192.in-addr.arpa", "91", "PTR", "adm.empresa.net", "", ErrDNSProtected, ""},
		{"PTR in forward zone", "empresa.net", "50", "PTR", "x.empresa.net", "", ErrDNSZone, ""},
		{"A in reverse zone", "10.168.192.in-addr.arpa", "50", "A", "10.0.0.1", "", ErrDNSZone, ""},
		{"IPv6 in A", "empresa.net", "x", "A", "::1", "", ErrDNSData, ""},
		{"mapped IPv4 in A", "empresa.net", "x", "A", "::ffff:10.0.0.1", "", ErrDNSData, ""},
		{"short CNAME target", "empresa.net", "x", "CNAME", "intranet", "", ErrDNSData, ""},
		{"bad name", "empresa.net", "a b", "A", "10.0.0.1", "", ErrDNSName, ""},
		{"full name made relative", "empresa.net", "Intranet.EMPRESA.net", "A", "10.0.0.1", "10.0.0.1", nil, "intranet"},
		{"DC by full name", "empresa.net", "dc1.empresa.net", "A", "10.0.0.1", "", ErrDNSProtected, ""},
		{"zone name as the apex", "empresa.net", "empresa.net", "A", "10.0.0.1", "", ErrDNSName, ""},
		{"absolute zone name", "empresa.net", "empresa.net.", "A", "10.0.0.1", "", ErrDNSName, ""},
		{"absolute DC name", "empresa.net", "dc1.empresa.net.", "A", "10.0.0.1", "", ErrDNSName, ""},
		{"partial zone suffix", "empresa.net", "dc1.net", "A", "10.0.0.1", "", ErrDNSName, ""},
		{"partial suffix, other host", "empresa.net", "web.foo.net", "A", "10.0.0.1", "", ErrDNSName, ""},
		{"partial suffix in a reverse zone", "10.168.192.in-addr.arpa", "91.arpa", "PTR", "x.empresa.net", "", ErrDNSName, ""},
		{"dotted relative name", "empresa.net", "web.sub", "A", "10.0.0.1", "10.0.0.1", nil, "web.sub"},
		{"zone twice", "empresa.net", "dc1.empresa.net.empresa.net", "A", "10.0.0.1", "", ErrDNSName, ""},
		{"DC in a reverse zone by full name", "10.168.192.in-addr.arpa", "91.10.168.192.in-addr.arpa", "PTR", "adm.empresa.net", "", ErrDNSProtected, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sent, got, err := CheckDNSRecord(tt.zone, tt.rname, tt.typ, tt.data, dcs)
			if tt.sent == "" && err == nil {
				tt.sent = tt.rname
			}
			if !errors.Is(err, tt.err) || got != tt.want || sent != tt.sent {
				t.Errorf("CheckDNSRecord(%q, %q, %q, %q) = %q, %q, %v; want %q, %q, %v", tt.zone, tt.rname, tt.typ, tt.data, sent, got, err, tt.sent, tt.want, tt.err)
			}
		})
	}
}
