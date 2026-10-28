//go:build linux

package sambadc

import (
	"crypto"
	"crypto/md5" //nolint:gosec // RFC 4121 4.1.1.2: the channel-binding hash is MD5 by definition
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"

	ldapgssapi "github.com/go-ldap/ldap/v3/gssapi"
	krbclient "github.com/jcmturner/gokrb5/v8/client"
	"github.com/jcmturner/gokrb5/v8/iana/chksumtype"
	"github.com/jcmturner/gokrb5/v8/iana/flags"
	"github.com/jcmturner/gokrb5/v8/messages"
	"github.com/jcmturner/gokrb5/v8/types"
)

// cbGSSAPI is a go-ldap GSSAPIClient that binds the Kerberos authenticator to the TLS channel (RFC 4121 4.1.1.2,
// "tls-server-end-point" of RFC 5929), which Samba requires for a SASL bind over LDAPS. Only the first token differs
// from go-ldap's own client; the AP-REP and the final SASL step are its.
type cbGSSAPI struct {
	*ldapgssapi.Client
	bnd [16]byte
}

func newCBGSSAPI(cl *krbclient.Client, cert *x509.Certificate) (*cbGSSAPI, error) {
	app, err := tlsServerEndPoint(cert)
	if err != nil {
		return nil, err
	}
	// gss_channel_bindings_struct: no addresses, then the application data, as Windows and Samba hash it
	b := make([]byte, 20, 20+len(app))
	binary.LittleEndian.PutUint32(b[16:], uint32(len(app)))
	return &cbGSSAPI{Client: &ldapgssapi.Client{Client: cl}, bnd: md5.Sum(append(b, app...))}, nil //nolint:gosec
}

// tlsServerEndPoint is "tls-server-end-point:" and the certificate's hash by its signature's hash, SHA-256 for MD5 and
// SHA-1 (RFC 5929 4.1).
func tlsServerEndPoint(cert *x509.Certificate) ([]byte, error) {
	if cert == nil {
		return nil, errors.New("no server certificate")
	}
	h := crypto.SHA256
	switch cert.SignatureAlgorithm {
	case x509.SHA384WithRSA, x509.ECDSAWithSHA384, x509.SHA384WithRSAPSS:
		h = crypto.SHA384
	case x509.SHA512WithRSA, x509.ECDSAWithSHA512, x509.SHA512WithRSAPSS:
		h = crypto.SHA512
	}
	var sum []byte
	if h == crypto.SHA256 {
		s := sha256.Sum256(cert.Raw)
		sum = s[:]
	} else {
		w := h.New()
		w.Write(cert.Raw)
		sum = w.Sum(nil)
	}
	return append([]byte("tls-server-end-point:"), sum...), nil
}

func (c *cbGSSAPI) InitSecContext(target string, input []byte) ([]byte, bool, error) {
	return c.InitSecContextWithOptions(target, input, nil)
}

// InitSecContextWithOptions sends the AP-REQ with the channel binding; the server's AP-REP goes to go-ldap's client,
// which needs the service ticket's session key it keeps: the first call hands it over through its own first step.
func (c *cbGSSAPI) InitSecContextWithOptions(target string, input []byte, apOptions []int) ([]byte, bool, error) {
	if input != nil {
		return c.Client.InitSecContextWithOptions(target, input, apOptions)
	}
	// go-ldap's first step keeps the session key (unexported) for the AP-REP and the final step; its token is dropped
	if _, _, err := c.Client.InitSecContextWithOptions(target, nil, apOptions); err != nil {
		return nil, false, err
	}
	tkt, key, err := c.Client.GetServiceTicket(target)
	if err != nil {
		return nil, false, err
	}
	auth, err := types.NewAuthenticator(c.Client.Credentials.Domain(), c.Client.Credentials.CName())
	if err != nil {
		return nil, false, err
	}
	// RFC 4121 4.1.1: Lgth, Bnd, Flags (mutual, integrity, confidentiality, as go-ldap asks)
	ck := make([]byte, 24)
	binary.LittleEndian.PutUint32(ck[0:], 16)
	copy(ck[4:20], c.bnd[:])
	binary.LittleEndian.PutUint32(ck[20:], 2|16|32)
	auth.Cksum = types.Checksum{CksumType: chksumtype.GSSAPI, Checksum: ck}
	req, err := messages.NewAPReq(tkt, key, auth)
	if err != nil {
		return nil, false, err
	}
	types.SetFlag(&req.APOptions, flags.APOptionMutualRequired)
	for _, o := range apOptions {
		types.SetFlag(&req.APOptions, o)
	}
	body, err := req.Marshal()
	if err != nil {
		return nil, false, err
	}
	// the GSS-API initial context token (RFC 2743 3.1): the Kerberos v5 OID, TOK_ID 01 00, the AP-REQ
	inner := append([]byte{0x06, 0x09, 0x2a, 0x86, 0x48, 0x86, 0xf7, 0x12, 0x01, 0x02, 0x02, 0x01, 0x00}, body...)
	return append(append([]byte{0x60}, derLen(len(inner))...), inner...), true, nil
}

func derLen(n int) []byte {
	if n < 0x80 {
		return []byte{byte(n)}
	}
	var b []byte
	for v := n; v > 0; v >>= 8 {
		b = append([]byte{byte(v)}, b...)
	}
	if len(b) > 4 {
		panic(fmt.Sprintf("GSS token of %d bytes", n))
	}
	return append([]byte{0x80 | byte(len(b))}, b...)
}
