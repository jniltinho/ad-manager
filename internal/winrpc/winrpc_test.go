package winrpc

import (
	"bytes"
	"os"
	"testing"

	"github.com/oiweiwei/go-msrpc/dcerpc"
)

func TestRelName(t *testing.T) {
	for _, tt := range []struct{ name, node, zone, want string }{
		{"", "@", "empresa.net", "@"},
		{"dc1", "@", "empresa.net", "dc1"},
		{"_ldap", "_tcp", "empresa.net", "_ldap._tcp"},
		{"empresa.net.", "@", "empresa.net", "@"},
		{"dc1.empresa.net.", "_tcp", "empresa.net", "dc1"},
		{"Dc1.EMPRESA.net.", "@", "empresa.net", "Dc1"},
		{"mail.example.com.", "@", "empresa.net", "mail.example.com."},
		{"xempresa.net.", "@", "empresa.net", "xempresa.net."},
	} {
		if got := relName(tt.name, tt.node, tt.zone); got != tt.want {
			t.Errorf("relName(%q, %q, %q) = %q, want %q", tt.name, tt.node, tt.zone, got, tt.want)
		}
	}
}

// TestEveryBindSealed keeps every RPC client on the sealed Kerberos bind: integrity must not rest on the SMB session
// (go-msrpc does not insist on its AP-REP and may accept a guest session).
func TestEveryBindSealed(t *testing.T) {
	src, err := os.ReadFile("winrpc.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(src, []byte("WithInsecure")) {
		t.Error("winrpc.go binds an RPC client without the sealed Kerberos options (rpcSec)")
	}
}

// TestMsrpcPatched fails to build without the patched go-msrpc (go.mod replace, third_party/go-msrpc/PATCHED.md),
// whose client takes only a verified response PDU as a call's answer (no fault without status, bind_ack or
// alter_context_resp).
func TestMsrpcPatched(t *testing.T) {
	if dcerpc.ErrFaultWithoutStatus == nil || dcerpc.ErrUnexpectedPDU == nil {
		t.Fatal("no patch")
	}
}
