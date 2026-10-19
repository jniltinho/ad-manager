package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"ad-manager/internal/ad"
	"ad-manager/internal/winrpc"
)

// fakeRPC records the host and credential each call used.
type fakeRPC struct {
	host, addr string
	cred       winrpc.Cred
	rec        winrpc.Record
	del        bool
	zonesMade  []string
}

func (f *fakeRPC) DNSZones(_ context.Context, t winrpc.Target, c winrpc.Cred) ([]winrpc.Zone, error) {
	f.host, f.addr, f.cred = t.Host, t.Addr, c
	return []winrpc.Zone{{Name: "empresa.test"}}, nil
}

func (f *fakeRPC) Records(_ context.Context, t winrpc.Target, zone, node string, c winrpc.Cred) ([]winrpc.Record, []winrpc.Node, error) {
	f.host, f.addr, f.cred = t.Host, t.Addr, c
	return []winrpc.Record{{Name: node, Type: "A", Data: "10.0.0.1"}}, nil, nil
}

func (f *fakeRPC) UpdateRecord(_ context.Context, t winrpc.Target, zone string, r winrpc.Record, del bool, c winrpc.Cred) error {
	f.addr, f.host, f.cred, f.rec, f.del = t.Addr, t.Host, c, r, del
	return nil
}

func (f *fakeRPC) CreateZone(_ context.Context, t winrpc.Target, zone string, c winrpc.Cred) error {
	f.zonesMade = append(f.zonesMade, zone)
	return nil
}

func (f *fakeRPC) Sessions(_ context.Context, t winrpc.Target, c winrpc.Cred) ([]winrpc.Session, error) {
	f.host, f.addr, f.cred = t.Host, t.Addr, c
	return []winrpc.Session{{Client: "10.0.0.5", User: "ana"}}, nil
}

// computersDir knows one computer of the domain with a DNS name, one without, and the DC's name.
type computersDir struct{ fakeDir }

func (computersDir) Computers(string) ([]ad.Computer, error) {
	return []ad.Computer{{Name: "FS01", DNSName: "fs01.empresa.test"}, {Name: "OLD"},
		{Name: "DC2", DNSName: "dc2.empresa.test", IsDC: true}}, nil
}

func (computersDir) DCHostName() (string, error) { return "dc1.empresa.test", nil }

func TestWinRPC(t *testing.T) {
	s, h := newTest(t, true)
	s.dial = func(_ *ad.Config, _, pass string) (directory, error) {
		if pass != goodPassword {
			return nil, ad.ErrBadLogin
		}
		return &computersDir{fakeDir{admin: true}}, nil
	}
	rpc := &fakeRPC{}
	s.rpc = rpc
	c := login(t, h)

	if rec := do(h, http.MethodGet, "/api/dns/zones", "", c); rec.Code != http.StatusOK || rpc.addr != "127.0.0.1" ||
		rpc.host != "dc1.empresa.test" || rpc.cred.User != "admin" || rpc.cred.Password != goodPassword ||
		rpc.cred.Realm != "EMPRESA.TEST" || rpc.cred.KDC != "127.0.0.1:88" {
		t.Fatalf("zones: %d %s (addr %q host %q, cred %+v)", rec.Code, rec.Body, rpc.addr, rpc.host, rpc.cred)
	}
	for _, tt := range []struct {
		name, path string
		code       int
	}{
		{"records of the zone", "/api/dns/records?zone=empresa.test", http.StatusOK},
		{"records of a node", "/api/dns/records?zone=_msdcs.empresa.test&node=dc", http.StatusOK},
		{"zone with a space", "/api/dns/records?zone=a%20b", http.StatusBadRequest},
		{"no zone", "/api/dns/records", http.StatusBadRequest},
		{"computer by DNS name", "/api/computers/sessions?host=fs01.empresa.test", http.StatusOK},
		{"computer by name", "/api/computers/sessions?host=fs01", http.StatusOK},
		{"a host outside the domain", "/api/computers/sessions?host=evil.example.com", http.StatusBadRequest},
		{"an address", "/api/computers/sessions?host=10.0.0.66", http.StatusBadRequest},
		{"a computer without a DNS name", "/api/computers/sessions?host=old", http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rpc.host = ""
			rec := do(h, http.MethodGet, tt.path, "", c)
			if rec.Code != tt.code {
				t.Fatalf("%d, want %d (%s)", rec.Code, tt.code, rec.Body)
			}
			if tt.code != http.StatusOK && rpc.host != "" {
				t.Fatalf("contacted %s", rpc.host)
			}
			if strings.Contains(tt.path, "sessions") && tt.code == http.StatusOK && (rpc.host != "fs01.empresa.test" || rpc.addr != rpc.host) {
				t.Fatalf("sessions went to %q as %q", rpc.addr, rpc.host)
			}
		})
	}

	for _, tt := range []struct {
		name, method, path, body string
		code                     int
		want                     winrpc.Record
	}{
		{"add A", http.MethodPost, "/api/dns/records", `{"zone":"empresa.test","name":"intranet","type":"a","data":"10.0.0.50"}`,
			http.StatusNoContent, winrpc.Record{Name: "intranet", Type: "A", TTL: 3600, Data: "10.0.0.50"}},
		{"add CNAME", http.MethodPost, "/api/dns/records", `{"zone":"empresa.test","name":"wiki","type":"CNAME","data":"intranet.empresa.test","ttl":600}`,
			http.StatusNoContent, winrpc.Record{Name: "wiki", Type: "CNAME", TTL: 600, Data: "intranet.empresa.test."}},
		{"delete A", http.MethodDelete, "/api/dns/records?zone=empresa.test&name=intranet&type=A&data=10.0.0.50", "",
			http.StatusNoContent, winrpc.Record{Name: "intranet", Type: "A", TTL: 3600, Data: "10.0.0.50"}},
		{"the panel's DC", http.MethodDelete, "/api/dns/records?zone=empresa.test&name=dc1&type=A&data=127.0.0.1", "", http.StatusBadRequest, winrpc.Record{}},
		{"another DC", http.MethodPost, "/api/dns/records", `{"zone":"empresa.test","name":"DC2","type":"A","data":"10.0.0.2"}`, http.StatusBadRequest, winrpc.Record{}},
		{"SRV", http.MethodPost, "/api/dns/records", `{"zone":"empresa.test","name":"_ldap._tcp","type":"SRV","data":"0 100 389 x.empresa.test."}`, http.StatusBadRequest, winrpc.Record{}},
		{"zone apex", http.MethodDelete, "/api/dns/records?zone=empresa.test&name=@&type=A&data=127.0.0.1", "", http.StatusBadRequest, winrpc.Record{}},
		{"full name of the zone", http.MethodPost, "/api/dns/records", `{"zone":"empresa.test","name":"Wiki.empresa.test","type":"A","data":"10.0.0.7"}`,
			http.StatusNoContent, winrpc.Record{Name: "wiki", Type: "A", TTL: 3600, Data: "10.0.0.7"}},
		{"DC by full name", http.MethodDelete, "/api/dns/records?zone=empresa.test&name=dc2.empresa.test&type=A&data=10.0.0.2", "", http.StatusBadRequest, winrpc.Record{}},
		{"the apex by name", http.MethodPost, "/api/dns/records", `{"zone":"empresa.test","name":"empresa.test","type":"A","data":"10.0.0.2"}`, http.StatusBadRequest, winrpc.Record{}},
		{"TTL too long", http.MethodPost, "/api/dns/records", `{"zone":"empresa.test","name":"x","type":"A","data":"10.0.0.1","ttl":9999999}`, http.StatusBadRequest, winrpc.Record{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			*rpc = fakeRPC{}
			rec := do(h, tt.method, tt.path, tt.body, c)
			if rec.Code != tt.code {
				t.Fatalf("%d, want %d (%s)", rec.Code, tt.code, rec.Body)
			}
			if tt.code != http.StatusNoContent {
				if rpc.host != "" {
					t.Fatalf("contacted %s", rpc.host)
				}
				return
			}
			if rpc.rec != tt.want || rpc.del != (tt.method == http.MethodDelete) || rpc.host != "dc1.empresa.test" || rpc.cred.User != "admin" {
				t.Fatalf("sent %+v del=%v to %q as %q", rpc.rec, rpc.del, rpc.host, rpc.cred.User)
			}
		})
	}
}
