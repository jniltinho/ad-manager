package importad

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"ad-manager/internal/ad"
)

func TestPlanDNS(t *testing.T) {
	r := func(zone, name, typ, data string, static bool) ad.DNSRecord {
		return ad.DNSRecord{Zone: zone, Name: name, Type: typ, Data: data, TTL: 900, Static: static}
	}
	src := []ad.DNSRecord{
		r("origem.lab", "@", "A", "10.0.0.1", true),
		r("origem.lab", "_ldap._tcp", "TYPE33", "", true),
		r("_msdcs.origem.lab", "gc", "A", "10.0.0.1", true),
		r("origem.lab", "dc01", "A", "10.0.0.1", true),      // the source DC
		r("origem.lab", "intranet", "A", "10.0.0.50", true), // new
		r("origem.lab", "web", "A", "10.0.0.60", true),      // round robin: both added
		r("origem.lab", "web", "A", "10.0.0.61", true),      //
		r("origem.lab", "wiki", "CNAME", "intranet.origem.lab.", true),
		r("origem.lab", "erp", "A", "10.0.0.70", true),      // here with another address: conflict
		r("origem.lab", "mail", "A", "10.0.0.80", true),     // already here
		r("origem.lab", "pc-ana", "A", "10.0.0.101", false), // dynamic
		r("10.0.10.in-addr.arpa", "50", "PTR", "intranet.origem.lab.", true),
		r("10.0.10.in-addr.arpa", "51", "PTR", "x.origem.lab.", true),
	}
	dst := []ad.DNSRecord{
		r("origem.lab", "@", "A", "10.0.0.2", true),
		r("origem.lab", "erp", "A", "10.0.0.99", true),
		r("origem.lab", "mail", "A", "10.0.0.80", true),
	}
	p := PlanDNS(src, dst, []string{"dc01.origem.lab", "dc02.origem.lab"}, false, false)
	var got []string
	for _, a := range p.Actions {
		got = append(got, a.Kind+" "+a.Target+" "+a.Detail)
	}
	want := []string{
		"zone.missing 10.0.10.in-addr.arpa zone not on this DNS server: its records are left out",
		"skip dc01 (origem.lab) record of a domain controller",
		"dns.conflict erp (origem.lab) here: A 10.0.0.99; source: A 10.0.0.70",
		"dns.add intranet (origem.lab) A 10.0.0.50",
		"dns.add web (origem.lab) A 10.0.0.60",
		"dns.add web (origem.lab) A 10.0.0.61",
		"dns.add wiki (origem.lab) CNAME intranet.origem.lab.",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("plan:\n%q\nwant\n%q", got, want)
	}
	if c := p.Changes[3]; c != (DNSChange{Zone: "origem.lab", Name: "intranet", Type: "A", Data: "10.0.0.50", TTL: 900}) {
		t.Fatalf("change %+v", c)
	}
	if !reflect.DeepEqual(p.Counts, map[string]int{"dns_other": 3, "dns_dynamic": 1, "dns_present": 1, "dns_zone_missing": 2}) {
		t.Fatalf("counts %v", p.Counts)
	}
	if d := PlanDNS(src, dst, []string{"dc01.origem.lab"}, true, false); len(d.Changes) != 5 {
		t.Fatalf("with includeDynamic the workstation record is added: %+v", d.Actions)
	}
	// createZones: the missing reverse zone is created before its PTRs; _msdcs never
	z := PlanDNS(src, dst, []string{"dc01.origem.lab"}, false, true)
	if z.Actions[0].Kind != "zone.create" || z.Zones[0] != "10.0.10.in-addr.arpa" || len(z.Zones) != 1 ||
		z.Actions[1].Kind != "dns.add" || z.Changes[1].Zone != "10.0.10.in-addr.arpa" || len(z.Changes) != 6 {
		t.Fatalf("with createZones: %+v", z.Actions)
	}
}

// ApplyDNS creates the zones first; the records of a zone that failed are not tried.
func TestApplyDNS(t *testing.T) {
	p := DNSPlan{
		Actions: []ad.Action{{Kind: "zone.create"}, {Kind: "dns.add"}, {Kind: "dns.add"}},
		Zones:   map[int]string{0: "z.lab"},
		Changes: map[int]DNSChange{1: {Zone: "Z.lab", Name: "a"}, 2: {Zone: "y.lab", Name: "b"}},
	}
	var added []string
	ApplyDNS(p, func(string) error { return errors.New("denied") },
		func(ch DNSChange) error { added = append(added, ch.Name); return nil }, func(string, string, error) {})
	if !slices.Equal(added, []string{"b"}) || p.Actions[0].OK || p.Actions[1].OK || !p.Actions[2].OK {
		t.Fatalf("added %v, actions %+v", added, p.Actions)
	}
}

func TestFingerprint(t *testing.T) {
	a := []ad.Action{{Kind: "dns.add", Target: "x (z)", Detail: "A 10.0.0.1"}}
	pl := map[string]any{"users": []map[string]string{{"login": "ana", "email": "a@x"}}}
	f := Fingerprint("ldaps://dc1.destino.lab", "ana", a, pl)
	if f != Fingerprint("LDAPS://DC1.destino.lab", "Ana", a, pl) {
		t.Error("case must not matter")
	}
	for _, other := range []string{
		Fingerprint("ldaps://dc2.destino.lab", "ana", a, pl),
		Fingerprint("ldaps://dc1.destino.lab", "bia", a, pl),
		Fingerprint("ldaps://dc1.destino.lab", "ana", []ad.Action{{Kind: "dns.add", Target: "x (z)", Detail: "A 10.0.0.2"}}, pl),
		// the same actions, another e-mail in the source row: the apply would write something else
		Fingerprint("ldaps://dc1.destino.lab", "ana", a, map[string]any{"users": []map[string]string{{"login": "ana", "email": "b@x"}}}),
	} {
		if other == f {
			t.Error("destination, operator and actions are part of the fingerprint")
		}
	}
}

// TestSourceReadOnly: the source exposes only the reads of the import (a new method must be added here on purpose).
func TestSourceReadOnly(t *testing.T) {
	var names []string
	tp := reflect.TypeOf(&Source{})
	for i := range tp.NumMethod() {
		names = append(names, tp.Method(i).Name)
	}
	want := []string{"Close", "DCNames", "DNSRecords", "Export", "GPOs", "Host", "Inventory", "NetBIOS"}
	if !slices.Equal(names, want) {
		t.Fatalf("methods %v, want %v", names, want)
	}
}
