package drs

import (
	"bytes"
	"context"
	"encoding/hex"
	"io"
	"net"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/oiweiwei/go-msrpc/msrpc/drsr/drsuapi/v4"
	"github.com/oiweiwei/go-msrpc/msrpc/dtyp"
)

func TestMSGUID(t *testing.T) {
	b, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	g, err := msGUID(b)
	if err != nil || g.Data1 != 0x33221100 || g.Data2 != 0x5544 || g.Data3 != 0x7766 ||
		!bytes.Equal(g.Data4, b[8:]) {
		t.Fatalf("msGUID = %+v, %v", g, err)
	}
	if _, err := msGUID(b[:15]); err == nil {
		t.Fatal("15-byte GUID accepted")
	}
}

// The reply checks come before any decryption: wrong object, errors and malformed replies give no hash.
func TestHashRejectsBadReplies(t *testing.T) {
	want, _ := msGUID(bytes.Repeat([]byte{1}, 16))
	other, _ := msGUID(bytes.Repeat([]byte{2}, 16))
	sid := &drsuapi.NT4SID{Data: append([]byte{1, 1, 0, 0, 0, 0, 0, 5}, 0x50, 0x04, 0, 0)}
	ok := func(g *dtyp.GUID) *drsuapi.MessageGetNCChangesReplyV6 {
		return &drsuapi.MessageGetNCChangesReplyV6{ExtendedReturn: exopSuccess, ObjectsCount: 1,
			Objects: &drsuapi.ReplicationEntityInfoList{EntityInfo: &drsuapi.EntityInfo{
				Name: &drsuapi.DSName{GUID: g, SID: sid}}}}
	}
	r := &Reader{}
	if nt, err := r.hash(ok(want), want); err != nil || nt != nil {
		t.Fatalf("object without attributes: %x, %v", nt, err)
	}
	cases := map[string]*drsuapi.MessageGetNCChangesReplyV6{
		"other object":   ok(other),
		"DRS error":      func() *drsuapi.MessageGetNCChangesReplyV6 { r := ok(want); r.DRSError = 8453; return r }(),
		"extended error": func() *drsuapi.MessageGetNCChangesReplyV6 { r := ok(want); r.ExtendedReturn = 2; return r }(),
		"no object":      {ExtendedReturn: exopSuccess},
		"two objects":    func() *drsuapi.MessageGetNCChangesReplyV6 { r := ok(want); r.ObjectsCount = 2; return r }(),
		"chained objects": func() *drsuapi.MessageGetNCChangesReplyV6 {
			r := ok(want)
			r.Objects.NextEntityInfo = ok(want).Objects
			return r
		}(),
		"no SID": func() *drsuapi.MessageGetNCChangesReplyV6 {
			r := ok(want)
			r.Objects.EntityInfo.Name.SID = nil
			return r
		}(),
		"malformed SID": func() *drsuapi.MessageGetNCChangesReplyV6 {
			r := ok(want)
			r.Objects.EntityInfo.Name.SID = &drsuapi.NT4SID{Data: []byte{1, 5, 0}}
			return r
		}(),
	}
	for name, reply := range cases {
		if nt, err := r.hash(reply, want); err == nil {
			t.Errorf("%s: accepted (%x)", name, nt)
		}
	}
}

// Read only: the package calls DRSBind, DRSGetNCChanges and DRSUnbind and nothing else, and binds the DRSUAPI client
// only with the sealed Kerberos options.
func TestOnlyReadOpnums(t *testing.T) {
	src, err := os.ReadFile("drs.go")
	if err != nil {
		t.Fatal(err)
	}
	var calls []string
	for _, m := range regexp.MustCompile(`\bcli\.([A-Z]\w*)\(`).FindAllSubmatch(src, -1) {
		calls = append(calls, string(m[1]))
	}
	slices.Sort(calls)
	if calls = slices.Compact(calls); !slices.Equal(calls, []string{"Bind", "Conn", "GetNCChanges", "Unbind"}) {
		t.Fatalf("DRSUAPI calls: %v", calls)
	}
	if !strings.Contains(string(src), "drsuapi.NewDrsuapiClient(sctx, cc, rpcSec...)") ||
		strings.Count(string(src), "WithInsecure") != 1 {
		t.Fatal("the DRSUAPI client must bind with rpcSec; only the endpoint mapper may be unauthenticated")
	}
}

// The dialer always connects to the DC, whatever host the endpoint mapper names, and closes every socket it opened.
func TestPinnedDialer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	p := &pinnedDialer{host: "127.0.0.1"}
	ctx := context.Background()
	if _, err := p.DialContext(ctx, "tcp", "203.0.113.9:"+port); err != nil { // a forged tower's host
		t.Fatalf("redirected dial: %v", err)
	}
	server := <-accepted
	defer server.Close()
	for _, bad := range [][2]string{{"tcp", "127.0.0.1:0"}, {"tcp", "127.0.0.1:x"}, {"udp", "127.0.0.1:" + port}} {
		if _, err := p.DialContext(ctx, bad[0], bad[1]); err == nil {
			t.Errorf("dial %s %s accepted", bad[0], bad[1])
		}
	}
	p.closeAll()
	_ = server.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := server.Read(make([]byte, 1)); err != io.EOF {
		t.Errorf("socket not closed: %v", err)
	}
	if _, err := p.DialContext(ctx, "tcp", "127.0.0.1:"+port); err == nil {
		t.Error("dial after closeAll accepted")
	}
}

func TestGuard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	aborted := false
	_, err := guard(ctx, "dc", func() { aborted = true }, func(ctx context.Context) (int, error) {
		<-ctx.Done()
		time.Sleep(10 * time.Millisecond) // a late result is dropped, not raced
		return 1, nil
	})
	if err == nil || !aborted {
		t.Fatalf("timeout: err %v, aborted %v", err, aborted)
	}
	if _, err := guard(context.Background(), "dc", func() {}, func(context.Context) (int, error) { panic("bad reply") }); err == nil {
		t.Fatal("panic not turned into an error")
	}
}

// unicodePwd must be absent, empty (count 0, no values) or exactly one value; anything else is an error, never "no
// password". Its ATTID goes through the reply's prefix table.
func TestUnicodePwdValues(t *testing.T) {
	want, _ := msGUID(bytes.Repeat([]byte{1}, 16))
	prefix := &drsuapi.SchemaPrefixTable{PrefixCount: 1, PrefixEntry: []*drsuapi.PrefixTableEntry{
		{Index: 9, Prefix: &drsuapi.OID{Length: 8, Elements: []byte{0x2A, 0x86, 0x48, 0x86, 0xF7, 0x14, 0x01, 0x04}}}}}
	if oid, err := prefix.Build().AttributeToOID(0x9005A); err != nil || oid.String() != oidUnicodePwd {
		t.Fatalf("prefix table: %v, %v", oid, err)
	}
	reply := func(typ uint32, vb *drsuapi.AttributeValueBlock) *drsuapi.MessageGetNCChangesReplyV6 {
		return &drsuapi.MessageGetNCChangesReplyV6{ExtendedReturn: exopSuccess, ObjectsCount: 1, PrefixTableSource: prefix,
			Objects: &drsuapi.ReplicationEntityInfoList{EntityInfo: &drsuapi.EntityInfo{
				Name:           &drsuapi.DSName{GUID: want, SID: &drsuapi.NT4SID{Data: []byte{1, 1, 0, 0, 0, 0, 0, 5, 0x50, 0x04, 0, 0}}},
				AttributeBlock: &drsuapi.AttributeBlock{Attribute: []*drsuapi.Attribute{{AttributeType: typ, AttributeValue: vb}}}}}}
	}
	r := &Reader{}
	if nt, err := r.hash(reply(0x9005A, &drsuapi.AttributeValueBlock{}), want); err != nil || nt != nil {
		t.Fatalf("empty unicodePwd: %x, %v", nt, err)
	}
	for name, rp := range map[string]*drsuapi.MessageGetNCChangesReplyV6{
		"count without values": reply(0x9005A, &drsuapi.AttributeValueBlock{ValueCount: 1}),
		"two values": reply(0x9005A, &drsuapi.AttributeValueBlock{ValueCount: 2,
			Values: []*drsuapi.AttributeValue{{Value: []byte{1}}, {Value: []byte{2}}}}),
		"empty value":         reply(0x9005A, &drsuapi.AttributeValueBlock{ValueCount: 1, Values: []*drsuapi.AttributeValue{{}}}),
		"no value block":      reply(0x9005A, nil),
		"ATTID not in prefix": reply(0x63005A, &drsuapi.AttributeValueBlock{}),
	} {
		if nt, err := r.hash(rp, want); err == nil {
			t.Errorf("%s: accepted (%x)", name, nt)
		}
	}
}
