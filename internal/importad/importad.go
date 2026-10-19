// Package importad imports another Active Directory (or Samba 4) into the current domain: the source is opened only
// for the reads the import needs, and the plan — objects and DNS — is the same for the panel, the CLI and the MCP.
package importad

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"

	"ad-manager/internal/ad"
)

// Source is the other AD, open only for the reads the import uses: it exposes no method that writes (the *ad.Conn
// inside would allow it, so it is never handed out).
type Source struct {
	c    *ad.Conn
	host string
}

// Open connects to the source (url, its domain and an account of it).
func Open(rawURL, domain, user, password string, insecure bool) (*Source, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return nil, errors.New("invalid source AD URL")
	}
	c, err := ad.Dial(&ad.Config{URL: rawURL, Domain: domain, InsecureSkipVerify: insecure}, user, password)
	if err != nil {
		return nil, err
	}
	return &Source{c: c, host: u.Hostname()}, nil
}

// Close ends the connection.
func (s *Source) Close() { s.c.Close() }

// Host is the source DC's address, as in its URL.
func (s *Source) Host() string { return s.host }

// NetBIOS is the source domain's NetBIOS name.
func (s *Source) NetBIOS() string { return s.c.NetBIOS() }

// Export reads users, groups, OUs and computers.
func (s *Source) Export() (*ad.CSVSet, error) { return s.c.Export() }

// GPOs lists the GPOs.
func (s *Source) GPOs() ([]ad.GPO, error) {
	g, _, err := s.c.GPOs()
	return g, err
}

// Inventory reads users, groups and memberships (share access is worked out from them).
func (s *Source) Inventory() (*ad.Inventory, error) { return s.c.Inventory("import") }

// DNSRecords reads the records of the AD-integrated zones.
func (s *Source) DNSRecords() ([]ad.DNSRecord, int, error) { return s.c.DNSRecords() }

// DCNames are the DNS names of the source's domain controllers.
func (s *Source) DCNames() ([]string, error) {
	cs, err := s.c.Computers("")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, k := range cs {
		if k.IsDC && k.DNSName != "" {
			out = append(out, k.DNSName)
		}
	}
	return out, nil
}

// DNSChange is a record to write, aligned with its dns.add action.
type DNSChange struct {
	Zone, Name, Type, Data string
	TTL                    uint32
}

// DNSPlan is the DNS part of a plan: actions (zone.create, dns.add, dns.conflict, zone.missing, skip) with the zones
// to create, the records to add and what was left out without an action of its own.
type DNSPlan struct {
	Actions []ad.Action
	Changes map[int]DNSChange // index in Actions → record (dns.add only)
	Zones   map[int]string    // index in Actions → zone (zone.create only)
	// counts of what was not listed: other types and locator/zone names, dynamic records, records already here,
	// records of zones missing here
	Counts map[string]int
}

// PlanDNS plans the source records against the destination's: only static A, CNAME and PTR (dynamic too with
// includeDynamic), never the _msdcs zone, the zone root, locator (_) names nor a record of a domain controller
// (dcs: both domains' DCs, ad.CheckDNSRecord); a record already here is left alone, a name here with other data of
// the same type, or a CNAME beside anything, is a conflict (not written); several records of one name in the source
// (round robin) are all added; a zone missing here is reported once with its records left out, or with createZones
// planned as zone.create (a primary zone integrated with the directory) before its records.
func PlanDNS(src, dst []ad.DNSRecord, dcs []string, includeDynamic, createZones bool) DNSPlan {
	p := DNSPlan{Changes: map[int]DNSChange{}, Zones: map[int]string{}, Counts: map[string]int{}}
	zonesHere := map[string]bool{}
	here := map[string][]ad.DNSRecord{} // zone|name → records
	for _, r := range dst {
		zonesHere[strings.ToLower(r.Zone)] = true
		k := strings.ToLower(r.Zone + "|" + r.Name)
		here[k] = append(here[k], r)
	}
	missing := map[string]bool{}
	src = slices.Clone(src)
	sort.SliceStable(src, func(i, j int) bool {
		return strings.ToLower(src[i].Zone+"|"+src[i].Name) < strings.ToLower(src[j].Zone+"|"+src[j].Name)
	})
	for _, r := range src {
		zl := strings.ToLower(r.Zone)
		switch {
		case r.Type != "A" && r.Type != "CNAME" && r.Type != "PTR", strings.HasPrefix(zl, "_msdcs."), r.Name == "@", strings.HasPrefix(r.Name, "_"):
			p.Counts["dns_other"]++
			continue
		case !r.Static && !includeDynamic:
			p.Counts["dns_dynamic"]++
			continue
		case !zonesHere[zl] && createZones:
			p.Zones[len(p.Actions)] = zl
			p.Actions = append(p.Actions, ad.Action{Kind: "zone.create", Target: zl, Detail: "primary zone integrated with the directory, secure updates"})
			zonesHere[zl] = true // its records follow as for any zone here (none here yet: no conflict)
		case !zonesHere[zl]:
			if !missing[zl] {
				missing[zl] = true
				p.Actions = append(p.Actions, ad.Action{Kind: "zone.missing", Target: r.Zone, Detail: "zone not on this DNS server: its records are left out"})
			}
			p.Counts["dns_zone_missing"]++
			continue
		}
		name, data, err := ad.CheckDNSRecord(r.Zone, r.Name, r.Type, r.Data, dcs)
		if errors.Is(err, ad.ErrDNSProtected) {
			p.Actions = append(p.Actions, ad.Action{Kind: "skip", Target: r.Name + " (" + r.Zone + ")", Detail: "record of a domain controller"})
			continue
		}
		if err != nil {
			p.Actions = append(p.Actions, ad.Action{Kind: "skip", Target: r.Name + " (" + r.Zone + ")", Detail: err.Error()})
			continue
		}
		target := name + " (" + r.Zone + ")"
		var same, other []string
		for _, h := range here[strings.ToLower(r.Zone+"|"+name)] {
			switch {
			case h.Type == r.Type && strings.EqualFold(strings.TrimSuffix(h.Data, "."), strings.TrimSuffix(data, ".")):
				same = append(same, h.Data)
			case h.Type == r.Type, h.Type == "CNAME" || r.Type == "CNAME": // another address, or a CNAME beside anything
				other = append(other, h.Type+" "+h.Data)
			}
		}
		switch {
		case len(same) > 0:
			p.Counts["dns_present"]++
		case len(other) > 0:
			p.Actions = append(p.Actions, ad.Action{Kind: "dns.conflict", Target: target,
				Detail: fmt.Sprintf("here: %s; source: %s %s", strings.Join(other, ", "), r.Type, data)})
		default:
			p.Changes[len(p.Actions)] = DNSChange{Zone: r.Zone, Name: name, Type: r.Type, Data: data, TTL: r.TTL}
			p.Actions = append(p.Actions, ad.Action{Kind: "dns.add", Target: target, Detail: r.Type + " " + data})
		}
	}
	return p
}

// ApplyDNS writes a DNS plan: the zones first, then the records (those of a zone that failed are not tried),
// recording each action's result and calling done for its audit line.
func ApplyDNS(p DNSPlan, createZone func(zone string) error, add func(DNSChange) error, done func(kind, what string, err error)) {
	failed := map[string]error{}
	for i, zone := range p.Zones {
		err := createZone(zone)
		if err != nil {
			failed[zone] = err
		}
		setResult(&p.Actions[i], err)
		done("import.zone.create", zone, err)
	}
	for i, ch := range p.Changes {
		err := failed[strings.ToLower(ch.Zone)]
		if err != nil {
			err = fmt.Errorf("zone not created: %w", err)
		} else {
			err = add(ch)
		}
		setResult(&p.Actions[i], err)
		done("import.dns.add", ch.Name+"."+ch.Zone+" "+ch.Type+" "+ch.Data, err)
	}
}

func setResult(a *ad.Action, err error) {
	a.OK, a.Result = err == nil, "ok"
	if err != nil {
		a.Result = err.Error()
	}
}

// Fingerprint identifies a plan for one destination and operator: applying a plan id later recomputes the plan and
// refuses if this changed (the source, the destination or who asks). payload is everything the apply writes beyond
// the actions' text (the source rows, the DNS records with their TTL), marshaled as JSON (map keys sorted).
func Fingerprint(destination, operator string, actions []ad.Action, payload any) string {
	type row struct{ K, T, D string }
	rows := make([]row, 0, len(actions))
	for _, a := range actions {
		rows = append(rows, row{a.Kind, a.Target, a.Detail})
	}
	b, _ := json.Marshal(struct {
		Dest, Op string
		Rows     []row
		Payload  any
	}{strings.ToLower(destination), strings.ToLower(operator), rows, payload})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
