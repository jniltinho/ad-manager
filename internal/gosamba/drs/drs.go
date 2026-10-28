// Package drs reads password hashes from a domain controller over DRSUAPI, as python3-samba's
// drs_utils.drsuapi_connect, DsGetNCChanges and net.replicate_decrypt do: one object at a time with the
// single-object extended operation (EXOP_REPL_OBJ), read only (no replica flags, never UpdateRefs). It works the
// same against Samba and Windows.
//
// Authentication is Kerberos only, through internal/winrpc's options: the bind is DCE style and sealed with keys
// from the DC's service ticket, so a host in the middle can neither read the secrets nor forge a reply. Every
// socket goes through a dialer pinned to the DC's address: the endpoint mapper's answer can choose the port, never
// another host, and every socket opened (the mapper's included) is closed on Close or on failure. Decryption uses the patched go-msrpc that checks the payload's CRC32
// (third_party/go-msrpc/PATCHED.md): a wrong key or a corrupt reply is an error, never a wrong hash.
package drs

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/oiweiwei/go-msrpc/dcerpc"
	"github.com/oiweiwei/go-msrpc/msrpc/drsr/drsuapi/v4"
	"github.com/oiweiwei/go-msrpc/msrpc/dtyp"
	"github.com/oiweiwei/go-msrpc/msrpc/epm/epm/v3"
	"github.com/oiweiwei/go-msrpc/ndr"
	"github.com/oiweiwei/go-msrpc/ssp/gssapi"

	"ad-manager/internal/gosamba/samdb"
	"ad-manager/internal/winrpc"
)

// timeout bounds each call, including the steps the RPC library does not tie to a context.
const timeout = 30 * time.Second

// exopSuccess is EXOP_ERR_SUCCESS, the extended return of a successful extended operation ([MS-DRSR] 4.1.10.2.21).
const exopSuccess = 1

// The secret attributes, looked up through the reply's prefix table (ATTIDs are per DC).
const (
	oidUnicodePwd     = "1.2.840.113556.1.4.90"
	oidSupplementalCr = "1.2.840.113556.1.4.125" // supplementalCredentials
	oidSAMAccountName = "1.2.840.113556.1.4.221"
)

// Secrets is one object's password material as replicated: the NT hash (16 bytes) and the supplementalCredentials
// blob (USER_PROPERTIES, [MS-SAMR] 2.2.10.1), each nil when the object has none. SID and SAMAccountName come from the
// same sealed reply, so a caller can check that the object is the one it asked for (an LDAP listing is not
// integrity-protected).
type Secrets struct {
	NTHash         []byte
	Supplemental   []byte
	SID            string
	SAMAccountName string
}

// Zero overwrites the material, for the caller to call as soon as it is done with it.
func (s *Secrets) Zero() {
	if s == nil {
		return
	}
	clear(s.NTHash)
	clear(s.Supplemental)
}

// Reader is an open, sealed DRSUAPI binding to one DC. A Reader is used by one goroutine at a time.
type Reader struct {
	ctx    context.Context // the security context holding the session key
	cc     dcerpc.Conn
	cli    drsuapi.DrsuapiClient
	drs    *drsuapi.Handle
	host   string
	dialer *pinnedDialer
}

// pinnedDialer dials host on whatever TCP port it is asked for, ignoring the address it is given (an endpoint-mapper
// answer chooses the port, never the host), and keeps every socket it opened, to close them all.
type pinnedDialer struct {
	host  string
	d     net.Dialer
	mu    sync.Mutex
	conns []net.Conn
	done  bool
}

func (p *pinnedDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if n, err := strconv.Atoi(port); network != "tcp" || err != nil || n < 1 || n > 65535 {
		return nil, fmt.Errorf("refusing to dial %s %s", network, address)
	}
	c, err := p.d.DialContext(ctx, network, net.JoinHostPort(p.host, port))
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done {
		_ = c.Close()
		return nil, errors.New("connection closed")
	}
	p.conns = append(p.conns, c)
	return c, nil
}

// closeAll closes every socket, whatever state the RPC layer is in (it skips an unbound one).
func (p *pinnedDialer) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done = true
	for _, c := range p.conns {
		_ = c.Close()
	}
	p.conns = nil
}

// Dial binds to the DRSUAPI interface of the DC t (t.Addr is its address, t.Host its DNS name in the directory)
// with the credential c, which needs the "Replicating Directory Changes" and "Replicating Directory Changes All"
// rights on the domain.
func Dial(ctx context.Context, t winrpc.Target, c winrpc.Cred) (*Reader, error) {
	sec, rpcSec, err := winrpc.Kerberos(t, c)
	if err != nil {
		return nil, err
	}
	dialer := &pinnedDialer{host: t.Addr, d: net.Dialer{Timeout: 10 * time.Second}}
	r, err := guard(ctx, t.Host, dialer.closeAll, func(ctx context.Context) (*Reader, error) {
		sctx := gssapi.NewSecurityContext(context.WithoutCancel(ctx), sec...)
		pinned := dcerpc.WithDialer(dialer)
		cc, err := dcerpc.Dial(sctx, "ncacn_ip_tcp:"+t.Addr, pinned,
			epm.EndpointMapper(sctx, net.JoinHostPort(t.Addr, "135"), dcerpc.WithInsecure(), pinned))
		if err != nil {
			return nil, err
		}
		cli, err := drsuapi.NewDrsuapiClient(sctx, cc, rpcSec...)
		if err != nil {
			return nil, err
		}
		ext, err := ndr.Marshal(&drsuapi.ExtensionsInt{
			Flags: drsuapi.ExtGetNCChangesRequestV8 | drsuapi.ExtStrongEncryption | drsuapi.ExtGetNCChangesReplyV6,
		}, ndr.Opaque)
		if err != nil {
			return nil, err
		}
		resp, err := cli.Bind(ctx, &drsuapi.BindRequest{Client: &drsuapi.Extensions{Data: ext}})
		if err == nil && resp.Return != 0 {
			err = fmt.Errorf("DRSBind: Windows error 0x%08x", resp.Return)
		}
		if err != nil {
			return nil, err
		}
		return &Reader{ctx: cli.Conn().Context(), cc: cc, cli: cli, drs: resp.DRS, host: t.Host, dialer: dialer}, nil
	})
	if err != nil {
		dialer.closeAll()
		return nil, err
	}
	return r, nil
}

// NTHash returns the NT hash (16 bytes) of the object whose objectGUID is guid (16 bytes as LDAP returns them),
// or nil when it has none (a user that never had a password). The reply must name that same object.
func (r *Reader) NTHash(ctx context.Context, guid []byte) ([]byte, error) {
	s, err := r.Secrets(ctx, guid)
	if err != nil {
		return nil, err
	}
	clear(s.Supplemental)
	return s.NTHash, nil
}

// Secrets returns the password material of the object whose objectGUID is guid, read in the same request.
func (r *Reader) Secrets(ctx context.Context, guid []byte) (*Secrets, error) {
	want, err := msGUID(guid)
	if err != nil {
		return nil, err
	}
	return guard(ctx, r.host, r.dialer.closeAll, func(ctx context.Context) (*Secrets, error) {
		resp, err := r.cli.GetNCChanges(ctx, &drsuapi.GetNCChangesRequest{
			Handle:    r.drs,
			InVersion: 8,
			In: &drsuapi.MessageGetNCChangesRequest{Value: &drsuapi.MessageGetNCChangesRequest_V8{
				V8: &drsuapi.MessageGetNCChangesRequestV8{
					NC:                &drsuapi.DSName{GUID: want},
					MaxObjectsCount:   1,
					ExtendedOperation: drsuapi.ExtendedOperationReplicationObject,
				},
			}},
		})
		if err != nil {
			return nil, err
		}
		if resp.Return != 0 {
			return nil, fmt.Errorf("DRSGetNCChanges: Windows error 0x%08x", resp.Return)
		}
		reply, ok := resp.Out.GetValue().(*drsuapi.MessageGetNCChangesReplyV6)
		if !ok || resp.OutVersion != 6 {
			return nil, fmt.Errorf("DRSGetNCChanges: reply version %d", resp.OutVersion)
		}
		return r.secrets(reply, want)
	})
}

// secrets checks the reply and decrypts its unicodePwd and supplementalCredentials.
func (r *Reader) secrets(reply *drsuapi.MessageGetNCChangesReplyV6, want *dtyp.GUID) (*Secrets, error) {
	if reply.DRSError != 0 {
		return nil, fmt.Errorf("DRSGetNCChanges: DRS error 0x%08x", reply.DRSError)
	}
	if reply.ExtendedReturn != exopSuccess {
		return nil, fmt.Errorf("DRSGetNCChanges: extended operation error %d", reply.ExtendedReturn)
	}
	if reply.ObjectsCount != 1 || reply.Objects == nil || reply.Objects.EntityInfo == nil ||
		reply.Objects.EntityInfo.Name == nil || reply.Objects.NextEntityInfo != nil {
		return nil, errors.New("DRSGetNCChanges: the reply does not hold exactly one object")
	}
	ei := reply.Objects.EntityInfo
	if got := ei.Name.GUID; got == nil || !sameGUID(got, want) {
		return nil, errors.New("DRSGetNCChanges: the reply names another object")
	}
	if ei.Name.SID == nil {
		return nil, errors.New("DRSGetNCChanges: the object has no SID")
	}
	sid := ei.Name.SID.Data
	if len(sid) < 12 || len(sid) != 8+4*int(sid[1]) {
		return nil, errors.New("DRSGetNCChanges: malformed SID")
	}
	rid := binary.LittleEndian.Uint32(sid[len(sid)-4:])
	sidText := samdb.SID(sid)
	if sidText == "" {
		return nil, errors.New("DRSGetNCChanges: malformed SID")
	}
	out := &Secrets{SID: sidText}
	if reply.PrefixTableSource == nil || ei.AttributeBlock == nil {
		return out, nil
	}
	prefixes := reply.PrefixTableSource.Build()
	for _, a := range ei.AttributeBlock.Attribute {
		if a == nil {
			out.Zero()
			return nil, errors.New("DRSGetNCChanges: empty attribute")
		}
		oid, err := prefixes.AttributeToOID(a.AttributeType)
		if err != nil {
			out.Zero()
			return nil, fmt.Errorf("DRSGetNCChanges: attribute 0x%x not in the prefix table: %w", a.AttributeType, err)
		}
		name := oid.String()
		vb := a.AttributeValue
		if name == oidSAMAccountName {
			if vb == nil || vb.ValueCount != 1 || len(vb.Values) != 1 || vb.Values[0] == nil || len(vb.Values[0].Value)%2 != 0 {
				out.Zero()
				return nil, errors.New("DRSGetNCChanges: malformed sAMAccountName")
			}
			u := make([]uint16, len(vb.Values[0].Value)/2)
			for i := range u {
				u[i] = binary.LittleEndian.Uint16(vb.Values[0].Value[2*i:])
			}
			out.SAMAccountName = string(utf16.Decode(u))
			continue
		}
		if name != oidUnicodePwd && name != oidSupplementalCr {
			continue
		}
		switch {
		case vb != nil && vb.ValueCount == 0 && len(vb.Values) == 0: // Samba lists it with no value for a user without one
			continue
		case vb == nil || vb.ValueCount != 1 || len(vb.Values) != 1 || vb.Values[0] == nil || len(vb.Values[0].Value) == 0:
			out.Zero()
			return nil, fmt.Errorf("DRSGetNCChanges: malformed %s", map[string]string{oidUnicodePwd: "unicodePwd", oidSupplementalCr: "supplementalCredentials"}[name])
		}
		if name == oidUnicodePwd {
			out.NTHash, err = drsuapi.DecryptHash(r.ctx, rid, vb.Values[0].Value)
		} else {
			out.Supplemental, err = drsuapi.DecryptData(r.ctx, vb.Values[0].Value)
		}
		if err != nil {
			out.Zero()
			return nil, err
		}
	}
	return out, nil
}

// Close unbinds and closes the connection; every socket is closed even when the unbind fails.
func (r *Reader) Close(ctx context.Context) error {
	defer r.dialer.closeAll()
	_, err := guard(ctx, r.host, r.dialer.closeAll, func(ctx context.Context) (struct{}, error) {
		_, err := r.cli.Unbind(ctx, &drsuapi.UnbindRequest{DRS: r.drs})
		if cerr := r.cc.Close(r.ctx); err == nil {
			err = cerr
		}
		return struct{}{}, err
	})
	return err
}

// guard runs fn with a timeout and turns a panic on a malformed reply into an error. The result travels only through
// the channel; on a timeout abort closes the sockets so fn returns, and its late result is dropped.
func guard[T any](ctx context.Context, host string, abort func(), fn func(ctx context.Context) (T, error)) (T, error) {
	type result struct {
		v   T
		err error
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	done := make(chan result, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- result{err: fmt.Errorf("invalid reply (%v)", p)}
			}
		}()
		v, err := fn(ctx)
		done <- result{v, err}
	}()
	select {
	case res := <-done:
		if res.err != nil {
			return res.v, fmt.Errorf("%s: %w", host, res.err)
		}
		return res.v, nil
	case <-ctx.Done():
		abort()
		var zero T
		return zero, fmt.Errorf("%s: %w", host, ctx.Err())
	}
}

// msGUID converts an objectGUID (Windows mixed-endian bytes) to the RPC structure.
func msGUID(b []byte) (*dtyp.GUID, error) {
	if len(b) != 16 {
		return nil, fmt.Errorf("objectGUID of %d bytes", len(b))
	}
	return &dtyp.GUID{
		Data1: binary.LittleEndian.Uint32(b[0:4]),
		Data2: binary.LittleEndian.Uint16(b[4:6]),
		Data3: binary.LittleEndian.Uint16(b[6:8]),
		Data4: bytes.Clone(b[8:16]),
	}, nil
}

func sameGUID(a, b *dtyp.GUID) bool {
	return a.Data1 == b.Data1 && a.Data2 == b.Data2 && a.Data3 == b.Data3 && bytes.Equal(a.Data4, b.Data4)
}
