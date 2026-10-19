// Package winrpc reads Windows RPC services of the domain with the operator's own credential: the DNS zones and
// records of a domain controller (MS-DNSP; A, CNAME and PTR records can also be added and deleted) and the SMB
// sessions of a server (MS-SRVS).
//
// Authentication is Kerberos only, for the target's own service principal (cifs/ and host/ of its DNS name in the
// directory): the ticket only opens that server, and no NTLM response is ever sent, so a host reached through a
// forged DNS answer gets nothing it can use. Every call goes over SMB (signed; encrypted when the server supports
// it) to a named pipe — no endpoint mapper, whose answer could point elsewhere.
//
// Integrity does not rest on the SMB session: go-msrpc v1.6.4 does not insist on the server's AP-REP and its SMB
// client could end up in a guest session, so every RPC bind is itself Kerberos-authenticated (DCE style) and sealed
// with keys from the service ticket. Only the real server can read that ticket: a host in the middle can neither
// read the requests nor forge a sealed answer, and the call fails instead of showing its data.
package winrpc

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	mdns "github.com/miekg/dns"
	"github.com/oiweiwei/go-msrpc/dcerpc"
	"github.com/oiweiwei/go-msrpc/msrpc/dnsp"
	"github.com/oiweiwei/go-msrpc/msrpc/dnsp/dnsserver/v5"
	"github.com/oiweiwei/go-msrpc/msrpc/dnsp/record"
	"github.com/oiweiwei/go-msrpc/msrpc/srvs/srvsvc/v3"
	"github.com/oiweiwei/go-msrpc/ndr"
	"github.com/oiweiwei/go-msrpc/smb2"
	"github.com/oiweiwei/go-msrpc/ssp"
	"github.com/oiweiwei/go-msrpc/ssp/credential"
	"github.com/oiweiwei/go-msrpc/ssp/gssapi"
	"github.com/oiweiwei/go-msrpc/ssp/krb5"
	"github.com/oiweiwei/gokrb5.fork/v9/config"

	_ "github.com/oiweiwei/go-msrpc/msrpc/erref/win32" // readable Windows errors
)

// Cred is the operator's credential (the panel session's) and the Kerberos realm and KDC to get tickets from.
type Cred struct {
	Realm    string `json:"realm"` // the AD domain in upper case
	KDC      string `json:"kdc"`   // host:88 of a domain controller
	User     string `json:"user"`
	Password string `json:"password"`
}

// Target is the server to call: the address dialed and its DNS name in the directory (for its principal).
type Target struct {
	Addr string `json:"addr"`
	Host string `json:"host"`
}

// timeout bounds every call, including the SMB steps the RPC library does not tie to a context.
const timeout = 20 * time.Second

// realmRe is a Kerberos realm: an upper-case DNS name.
var realmRe = regexp.MustCompile(`^[A-Z0-9]([A-Z0-9-]*[A-Z0-9])?(\.[A-Z0-9]([A-Z0-9-]*[A-Z0-9])?)+$`)

// clientLonghorn is DNS_CLIENT_VERSION_LONGHORN (Windows Server 2008 and later replies).
const clientLonghorn = 0x00070000

// errMoreData is ERROR_MORE_DATA: the DNS server returned part of the names; ask again from the last one.
const errMoreData = 234

// call runs fn over a signed SMB connection to the named pipe of t, authenticated by Kerberos as cifs/<t.Host>. fn
// gets the connection and the options of an RPC bind authenticated as host/<t.Host>; the call ends after timeout.
func call(ctx context.Context, t Target, c Cred, pipe string, fn func(ctx context.Context, cc dcerpc.Conn, rpcSec []dcerpc.Option) error) error {
	var rpcSec []dcerpc.Option
	if t.Host == "" || strings.ContainsAny(t.Host, "/@ ") {
		return fmt.Errorf("invalid host %q", t.Host)
	}
	if !realmRe.MatchString(c.Realm) {
		return fmt.Errorf("invalid realm %q", c.Realm)
	}
	// built field by field (no configuration text to inject into): this realm, this KDC, no DNS lookups, TCP
	conf := config.New()
	conf.LibDefaults.DefaultRealm = c.Realm
	conf.LibDefaults.DNSLookupKDC, conf.LibDefaults.DNSLookupRealm = false, false
	conf.LibDefaults.UDPPreferenceLimit = 1
	conf.Realms = []config.Realm{{Realm: c.Realm, KDC: []string{c.KDC}}}
	krb := func(dce bool) *krb5.Config {
		return &krb5.Config{KRB5Config: krb5.ParsedLibDefaults(conf), DisablePAFXFAST: true, AnyServiceClassSPN: true, DCEStyle: dce,
			Flags: []int{int(gssapi.MutualAuthn), int(gssapi.Integrity), int(gssapi.Confidentiality)}}
	}
	cred := credential.NewFromPassword(c.User, c.Password, credential.Domain(c.Realm))
	// Kerberos only (never NTLM): SMB session setup, then the RPC bind itself (DCE style, sealed) where the
	// interface asks for it (the DNS server refuses an unauthenticated bind)
	sec := []gssapi.ContextOption{gssapi.WithCredential(cred), gssapi.WithMechanismFactory(ssp.KRB5, krb(false))}
	rpcSec = []dcerpc.Option{dcerpc.WithSeal(), dcerpc.WithTargetName("host/" + t.Host), dcerpc.WithCredentials(cred),
		dcerpc.WithMechanism(ssp.KRB5, krb(true))}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	done := make(chan error, 1)
	// ponytail: a server that stops answering mid-SMB leaves this goroutine waiting on its socket (the library does
	// not pass the context everywhere); the handler still returns at the timeout. Close the conn on cancel if it piles up.
	go func() {
		defer func() { // a malformed reply must fail this call, not the whole panel
			if r := recover(); r != nil {
				done <- fmt.Errorf("%s: invalid reply (%v)", t.Host, r)
			}
		}()
		sctx := gssapi.NewSecurityContext(ctx, sec...)
		dialer := smb2.NewDialer(smb2.WithSign(), smb2.WithSeal(),
			smb2.WithSecurity(append(sec, gssapi.WithTargetName("cifs/"+t.Host))...))
		cc, err := dcerpc.Dial(sctx, t.Addr, dcerpc.WithSMBDialer(dialer), dcerpc.WithEndpoint("ncacn_np:["+pipe+"]"))
		if err != nil {
			done <- err
			return
		}
		defer cc.Close(sctx)
		done <- fn(sctx, cc, rpcSec)
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", t.Host, ctx.Err())
	}
}

// Zone is a DNS zone of the DC.
type Zone struct {
	Name    string `json:"name"`
	Reverse bool   `json:"reverse"`
}

// winErr turns a non-zero Windows status of a reply into an error.
func winErr(code uint32) error {
	if code == 0 {
		return nil
	}
	return fmt.Errorf("Windows error %d (0x%08x)", code, code)
}

// DNSZones lists the primary zones of the DNS server on t (a DC), forward zones first.
func DNSZones(ctx context.Context, t Target, c Cred) ([]Zone, error) {
	var zones []Zone
	err := call(ctx, t, c, "dnsserver", func(ctx context.Context, cc dcerpc.Conn, rpcSec []dcerpc.Option) error {
		cli, err := dnsserver.NewDNSServerClient(ctx, cc, rpcSec...)
		if err != nil {
			return err
		}
		// as samba-tool: the Longhorn client version and the primary zones (forward and reverse)
		resp, err := cli.ComplexOperation2(ctx, &dnsserver.ComplexOperation2Request{
			ClientVersion: clientLonghorn, ServerName: t.Host, Operation: "EnumZones", TypeIn: uint32(dnsp.TypeIDDword),
			DataIn: &dnsp.Union{Value: &dnsp.Union_Dword{Dword: uint32(dnsp.ZoneRequestFilterPrimary)}},
		})
		if err != nil {
			return err
		}
		if err := winErr(uint32(resp.Return)); err != nil {
			return err
		}
		list, ok := resp.DataOut.GetValue().(*dnsp.ZoneList)
		if !ok {
			return fmt.Errorf("unexpected reply %T", resp.DataOut.GetValue())
		}
		zones = []Zone{}
		for _, z := range list.ZoneArray {
			if z.ZoneName == "RootDNSServers" || z.ZoneName == "TrustAnchors" || z.ZoneName == ".." {
				continue
			}
			zones = append(zones, Zone{Name: z.ZoneName, Reverse: strings.HasSuffix(strings.ToLower(z.ZoneName), ".arpa")})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("DNS zones: %w", err)
	}
	slices.SortFunc(zones, func(a, b Zone) int {
		if a.Reverse != b.Reverse {
			return map[bool]int{true: 1, false: -1}[a.Reverse]
		}
		return strings.Compare(a.Name, b.Name)
	})
	return zones, nil
}

// Record is one DNS record of a node.
type Record struct {
	Name string `json:"name"` // relative to the zone ("@" for the zone itself)
	Type string `json:"type"`
	TTL  uint32 `json:"ttl"`
	Data string `json:"data"`
}

// Node is a child name of the listed node that has names below it (listed on request).
type Node struct {
	Name     string `json:"name"`
	Children int    `json:"children"`
}

// relName is a name of the reply relative to zone: absolute names (ending in ".") lose the zone suffix, relative
// ones get the queried node's.
func relName(name, node, zone string) string {
	if strings.HasSuffix(name, ".") {
		name = strings.TrimSuffix(name, ".")
		if strings.EqualFold(name, zone) {
			return "@"
		}
		if l := len(name) - len(zone) - 1; l > 0 && strings.EqualFold(name[l:], "."+zone) {
			return name[:l]
		}
		return name + "." // outside the zone: kept absolute
	}
	full := name
	if node != "@" {
		full = strings.TrimPrefix(name+"."+node, ".")
	}
	if full == "" {
		return "@"
	}
	return full
}

// Records lists the records of node ("@" = the zone itself) and the names below it that have names below them
// (to open next), asking again while the server answers ERROR_MORE_DATA.
func Records(ctx context.Context, t Target, zone, node string, c Cred) ([]Record, []Node, error) {
	recs, more := []Record{}, []Node{}
	err := call(ctx, t, c, "dnsserver", func(ctx context.Context, cc dcerpc.Conn, rpcSec []dcerpc.Option) error {
		cli, err := dnsserver.NewDNSServerClient(ctx, cc, rpcSec...)
		if err != nil {
			return err
		}
		start := ""
		for range 1000 {
			resp, err := cli.EnumRecords2(ctx, &dnsserver.EnumRecords2Request{
				ClientVersion: clientLonghorn, ServerName: t.Host, Zone: zone, NodeName: node, StartChild: start,
				RecordType: 0x00FF, SelectFlag: 0x00000001, // the zone's own data (no additional or glue records)
			})
			if err != nil {
				return err
			}
			if code := uint32(resp.Return); code != 0 && code != errMoreData {
				return winErr(code)
			}
			var nodes record.NodeList
			if len(resp.Buffer) > 0 {
				if err := ndr.Unmarshal(resp.Buffer, &nodes, ndr.Opaque); err != nil {
					return err
				}
			}
			for _, n := range nodes.DNSNodes {
				raw := string(n.DNSNodeName.DNSName)
				name := relName(raw, node, zone)
				if n.ChildCount > 0 {
					more = append(more, Node{Name: name, Children: int(n.ChildCount)})
				}
				for _, rr := range n.RRs() {
					h := rr.Header()
					f := strings.SplitN(rr.String(), "\t", 5) // name, ttl, class, type, data
					data := ""
					if len(f) == 5 {
						data = f[4]
					}
					recs = append(recs, Record{Name: name, Type: mdns.TypeToString[h.Rrtype], TTL: h.Ttl, Data: data})
				}
				start = raw
			}
			if uint32(resp.Return) != errMoreData {
				return nil
			}
			if len(nodes.DNSNodes) == 0 { // "more data" with no names: the list would come out incomplete
				return errors.New("incomplete answer from the DNS server")
			}
		}
		return errors.New("too many names")
	})
	if err != nil {
		return nil, nil, fmt.Errorf("DNS records of %s: %w", zone, err)
	}
	return recs, more, nil
}

// UpdateRecord adds r (or deletes it, del) at r.Name of zone on the DNS server of t (a DC): type A, CNAME or PTR
// with its data, as R_DnssrvUpdateRecord2. A delete matches the type and data (not the TTL). Callers check the
// record against the protection rules (ad.CheckDNSRecord) first.
func UpdateRecord(ctx context.Context, t Target, zone string, r Record, del bool, c Cred) error {
	typ, ok := mdns.StringToType[r.Type]
	if !ok || (typ != mdns.TypeA && typ != mdns.TypeCNAME && typ != mdns.TypePTR) {
		return fmt.Errorf("record type %q is not supported", r.Type)
	}
	rec, err := record.NewRecordFromString(typ, r.TTL, r.Data)
	if err != nil {
		return fmt.Errorf("invalid record data: %w", err)
	}
	// record.Record and dnsp.Record are the same DNS_RPC_RECORD on the wire (one typed, one opaque)
	b, err := ndr.Marshal(rec, ndr.Opaque)
	if err != nil {
		return err
	}
	wire := &dnsp.Record{}
	if err := ndr.Unmarshal(b, wire, ndr.Opaque); err != nil {
		return err
	}
	err = call(ctx, t, c, "dnsserver", func(ctx context.Context, cc dcerpc.Conn, rpcSec []dcerpc.Option) error {
		cli, err := dnsserver.NewDNSServerClient(ctx, cc, rpcSec...)
		if err != nil {
			return err
		}
		req := &dnsserver.UpdateRecord2Request{ClientVersion: clientLonghorn, ServerName: t.Host, Zone: zone, NodeName: r.Name}
		if del {
			req.DeleteRecord = wire
		} else {
			req.AddRecord = wire
		}
		resp, err := cli.UpdateRecord2(ctx, req)
		if err != nil {
			return err
		}
		return winErr(uint32(resp.Return))
	})
	if err != nil {
		return fmt.Errorf("DNS record %s of %s: %w", r.Name, zone, err)
	}
	return nil
}

// CreateZone creates zone on the DNS server of t (a DC) as samba-tool dns zonecreate does: a primary zone integrated
// with the directory, in the domain's DNS partition, secure updates only and no zone transfer. Never _msdcs (the DC
// makes its own).
func CreateZone(ctx context.Context, t Target, zone string, c Cred) error {
	zone = strings.ToLower(strings.TrimSuffix(zone, "."))
	if !zoneRe.MatchString(zone) || strings.HasPrefix(zone, "_msdcs.") {
		return fmt.Errorf("zone %q is not created here", zone)
	}
	err := call(ctx, t, c, "dnsserver", func(ctx context.Context, cc dcerpc.Conn, rpcSec []dcerpc.Option) error {
		cli, err := dnsserver.NewDNSServerClient(ctx, cc, rpcSec...)
		if err != nil {
			return err
		}
		resp, err := cli.Operation2(ctx, &dnsserver.Operation2Request{
			ClientVersion: clientLonghorn, ServerName: t.Host, Operation: "ZoneCreate", TypeID: uint32(dnsp.TypeIDZoneCreate),
			Data: &dnsp.Union{Value: &dnsp.Union_ZoneCreate{ZoneCreate: &dnsp.ZoneCreateInfo{
				RPCStructureVersion: 2, ZoneName: zone, ZoneType: zoneTypePrimary, DSIntegrated: 1, LoadExisting: 1,
				AllowUpdate: zoneUpdateSecure, SecureSecondaries: zoneNoTransfer, DPFlags: uint32(dnsp.DPDomainDefault)}}},
		})
		if err != nil {
			return err
		}
		if err := winErr(uint32(resp.Return)); err != nil {
			return err
		}
		// again on its own, as samba-tool does (Samba sets the zone's updates only through this property)
		resp, err = cli.Operation2(ctx, &dnsserver.Operation2Request{
			ClientVersion: clientLonghorn, ServerName: t.Host, Zone: zone, Operation: "ResetDwordProperty",
			TypeID: uint32(dnsp.TypeIDNameAndParam),
			Data:   &dnsp.Union{Value: &dnsp.Union_NameAndParam{NameAndParam: &dnsp.NameAndParam{NodeName: "AllowUpdate", Param: zoneUpdateSecure}}},
		})
		if err != nil {
			return err
		}
		return winErr(uint32(resp.Return))
	})
	if err != nil {
		return fmt.Errorf("DNS zone %s: %w", zone, err)
	}
	return nil
}

const (
	zoneTypePrimary  = 1 // DNS_ZONE_TYPE_PRIMARY
	zoneUpdateSecure = 2 // DNS_ZONE_UPDATE_SECURE
	zoneNoTransfer   = 3 // ZONE_SECSECURE_NO_XFER: no zone transfer to anyone
)

// zoneRe is a zone name: two or more labels of letters, digits, - and _.
var zoneRe = regexp.MustCompile(`^[a-z0-9_]([a-z0-9_-]{0,62})(\.[a-z0-9_]([a-z0-9_-]{0,62}))+$`)

// Session is an SMB session open on a server.
type Session struct {
	Client string `json:"client"`
	User   string `json:"user"`
	Opens  uint32 `json:"open_files"`
	Active uint32 `json:"active_seconds"`
	Idle   uint32 `json:"idle_seconds"`
}

// Sessions lists the SMB sessions of t (srvsvc, level 1 — Windows and Samba: client, user, open files, active and
// idle time). It needs an administrator of that server.
func Sessions(ctx context.Context, t Target, c Cred) ([]Session, error) {
	out := []Session{}
	err := call(ctx, t, c, "srvsvc", func(ctx context.Context, cc dcerpc.Conn, rpcSec []dcerpc.Option) error {
		cli, err := srvsvc.NewSrvsvcClient(ctx, cc, rpcSec...) // sealed at the RPC layer too, as the DNS calls
		if err != nil {
			return err
		}
		resp, err := cli.SessionEnum(ctx, &srvsvc.SessionEnumRequest{
			Info: &srvsvc.SessionEnum{Level: 1, SessionInfo: &srvsvc.SessionEnumUnion{
				Value: &srvsvc.SessionEnumUnion_Level1{Level1: &srvsvc.SessionInfo1Container{}}}},
			PreferredMaximumLength: 0xffffffff,
		})
		if err != nil {
			return err
		}
		if err := winErr(uint32(resp.Return)); err != nil {
			return err
		}
		if l, ok := resp.Info.SessionInfo.GetValue().(*srvsvc.SessionInfo1Container); ok && l != nil {
			for _, s := range l.Buffer {
				out = append(out, Session{Client: strings.TrimPrefix(s.ClientName, `\\`), User: s.UserName, Opens: s.NumOpens,
					Active: s.Time, Idle: s.IdleTime})
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("sessions of %s: %w", t.Host, err)
	}
	return out, nil
}
