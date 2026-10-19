package ad

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

// DNSRecord is a DNS record stored in the directory (a dnsRecord value of a dnsNode in an application partition).
type DNSRecord struct {
	Zone   string `json:"zone"`
	Name   string `json:"name"` // relative to the zone, "@" for the zone itself
	Type   string `json:"type"` // A, CNAME, PTR, or the numeric type of others ("TYPE33")
	Data   string `json:"data"` // IPv4 (A) or a host name with the final dot (CNAME, PTR); empty for other types
	TTL    uint32 `json:"ttl"`
	Static bool   `json:"static"` // timestamp 0: not registered dynamically by a client
}

// ErrDNSBlob reports a dnsRecord value that does not decode.
var ErrDNSBlob = errors.New("invalid dnsRecord value")

// maxDNSBlob caps one dnsRecord value (a record's data is at most 65535 bytes, plus the 24-byte header).
const maxDNSBlob = 24 + 65535

// DecodeDNSRecord decodes one dnsRecord value of the directory (MS-DNSP 2.3.2.2, not the DNS_RPC_RECORD of the RPC
// interface): little-endian header, big-endian TTL, timestamp 0 for a static record; the data of A, CNAME and PTR
// (an IPv4 address; a DNS_COUNT_NAME). Other types come back with their number and empty data.
func DecodeDNSRecord(b []byte) (typ, data string, ttl uint32, static bool, err error) {
	if len(b) < 24 || len(b) > maxDNSBlob {
		return "", "", 0, false, ErrDNSBlob
	}
	n := int(binary.LittleEndian.Uint16(b[0:2]))
	t := binary.LittleEndian.Uint16(b[2:4])
	if b[4] != 5 || 24+n > len(b) { // version 5 (DNS_RECORD)
		return "", "", 0, false, ErrDNSBlob
	}
	ttl = binary.BigEndian.Uint32(b[12:16])
	static = binary.LittleEndian.Uint32(b[20:24]) == 0
	d := b[24 : 24+n]
	switch t {
	case 1: // A
		if len(d) != 4 {
			return "", "", 0, false, ErrDNSBlob
		}
		return "A", netip.AddrFrom4([4]byte(d)).String(), ttl, static, nil
	case 5, 12: // CNAME, PTR
		name, err := countName(d)
		if err != nil {
			return "", "", 0, false, err
		}
		return map[uint16]string{5: "CNAME", 12: "PTR"}[t], name, ttl, static, nil
	}
	return fmt.Sprintf("TYPE%d", t), "", ttl, static, nil
}

// countName decodes a DNS_COUNT_NAME (MS-DNSP 2.2.2.2.2): total length, label count, then length-prefixed labels
// ending with an empty one. It returns the name with the final dot.
func countName(d []byte) (string, error) {
	if len(d) < 2 || int(d[0]) > len(d)-2 {
		return "", ErrDNSBlob
	}
	raw, want := d[2:2+int(d[0])], int(d[1])
	var labels []string
	for i := 0; i < len(raw); {
		l := int(raw[i])
		if l == 0 {
			break
		}
		if l > 63 || i+1+l > len(raw) {
			return "", ErrDNSBlob
		}
		lab := string(raw[i+1 : i+1+l])
		if strings.ContainsAny(lab, ". \x00") {
			return "", ErrDNSBlob
		}
		labels = append(labels, lab)
		i += 1 + l
	}
	name := strings.Join(labels, ".")
	if len(labels) != want || len(labels) == 0 || len(name) > 253 {
		return "", ErrDNSBlob
	}
	return name + ".", nil
}

// dnsContainers are where AD-integrated zones live: the domain and forest application partitions, and the legacy
// container of zones stored in the domain partition (Windows 2000 style).
func (c *Conn) dnsContainers() []string {
	base := c.cfg.Base()
	return []string{"CN=MicrosoftDNS,DC=DomainDnsZones," + base, "CN=MicrosoftDNS,DC=ForestDnsZones," + base, "CN=MicrosoftDNS,CN=System," + base}
}

// DNSRecords reads every record of the primary AD-integrated zones, skipping the root hints, the trust anchors,
// tombstoned names and values that do not decode (counted in skipped).
func (c *Conn) DNSRecords() (recs []DNSRecord, skipped int, err error) {
	for _, cont := range c.dnsContainers() {
		zones, err := c.search(cont, ldap.ScopeSingleLevel, "(objectClass=dnsZone)", []string{"dc"})
		if errors.Is(err, ErrNotFound) {
			continue // no such partition
		}
		if err != nil {
			return nil, 0, err
		}
		for _, z := range zones {
			zone := z.GetAttributeValue("dc")
			if zone == "RootDNSServers" || strings.HasPrefix(zone, "..") {
				continue
			}
			nodes, err := c.search(z.DN, ldap.ScopeSingleLevel, "(objectClass=dnsNode)", []string{"dc", "dnsRecord", "dNSTombstoned"})
			if err != nil {
				return nil, 0, err
			}
			for _, n := range nodes {
				if strings.EqualFold(n.GetAttributeValue("dNSTombstoned"), "TRUE") {
					continue
				}
				for _, v := range n.GetRawAttributeValues("dnsRecord") {
					typ, data, ttl, static, err := DecodeDNSRecord(v)
					if err != nil {
						skipped++
						continue
					}
					recs = append(recs, DNSRecord{Zone: zone, Name: n.GetAttributeValue("dc"), Type: typ, Data: data, TTL: ttl, Static: static})
				}
			}
		}
	}
	return recs, skipped, nil
}
