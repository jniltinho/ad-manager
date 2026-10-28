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
	"time"

	krbclient "github.com/jcmturner/gokrb5/v8/client"
	krbcrypto "github.com/jcmturner/gokrb5/v8/crypto"
	"github.com/jcmturner/gokrb5/v8/gssapi"
	"github.com/jcmturner/gokrb5/v8/iana/chksumtype"
	"github.com/jcmturner/gokrb5/v8/iana/flags"
	"github.com/jcmturner/gokrb5/v8/iana/keyusage"
	"github.com/jcmturner/gokrb5/v8/messages"
	"github.com/jcmturner/gokrb5/v8/spnego"
	"github.com/jcmturner/gokrb5/v8/types"
)

// cbGSSAPI is a go-ldap GSSAPIClient for a SASL GSSAPI bind (RFC 4752) over LDAPS, with the authenticator bound to
// the TLS channel (RFC 4121 4.1.1.2, "tls-server-end-point" of RFC 5929), which Samba requires there. It checks the
// AP-REP against the authenticator it sent (RFC 4120 3.2.5) and selects "no security layer" (TLS protects the rest).
type cbGSSAPI struct {
	cl     *krbclient.Client
	bnd    [16]byte
	key    types.EncryptionKey // the service ticket's session key
	subkey types.EncryptionKey // the acceptor's subkey from the AP-REP, when it sent one
	ctime  time.Time
	cusec  int
	seq    uint64
}

func newCBGSSAPI(cl *krbclient.Client, cert *x509.Certificate) (*cbGSSAPI, error) {
	app, err := tlsServerEndPoint(cert)
	if err != nil {
		return nil, err
	}
	// gss_channel_bindings_struct: no addresses, then the application data, as Windows and Samba hash it
	b := make([]byte, 20, 20+len(app))
	binary.LittleEndian.PutUint32(b[16:], uint32(len(app)))
	return &cbGSSAPI{cl: cl, bnd: md5.Sum(append(b, app...))}, nil //nolint:gosec
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

// InitSecContextWithOptions: the AP-REQ with the channel binding first, then the server's AP-REP checked.
func (c *cbGSSAPI) InitSecContextWithOptions(target string, input []byte, apOptions []int) ([]byte, bool, error) {
	if input != nil {
		return c.apRep(input)
	}
	tkt, key, err := c.cl.GetServiceTicket(target)
	if err != nil {
		return nil, false, err
	}
	auth, err := types.NewAuthenticator(c.cl.Credentials.Domain(), c.cl.Credentials.CName())
	if err != nil {
		return nil, false, err
	}
	// RFC 4121 4.1.1: Lgth, Bnd, Flags (mutual, confidentiality, integrity)
	ck := make([]byte, 24)
	binary.LittleEndian.PutUint32(ck[0:], 16)
	copy(ck[4:20], c.bnd[:])
	binary.LittleEndian.PutUint32(ck[20:], uint32(gssapi.ContextFlagMutual|gssapi.ContextFlagConf|gssapi.ContextFlagInteg))
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
	c.key, c.ctime, c.cusec, c.seq = key, auth.CTime, auth.Cusec, uint64(auth.SeqNumber)
	// the GSS-API initial context token (RFC 2743 3.1): the Kerberos v5 OID, TOK_ID 01 00, the AP-REQ
	inner := append([]byte{0x06, 0x09, 0x2a, 0x86, 0x48, 0x86, 0xf7, 0x12, 0x01, 0x02, 0x02, 0x01, 0x00}, body...)
	return append(append([]byte{0x60}, derLen(len(inner))...), inner...), true, nil
}

// apRep checks the server's AP-REP: decrypted with the session key, its ctime/cusec those of the authenticator sent
// (RFC 4120 3.2.5); it keeps the acceptor's subkey.
func (c *cbGSSAPI) apRep(input []byte) ([]byte, bool, error) {
	var tok spnego.KRB5Token
	if err := tok.Unmarshal(input); err != nil {
		return nil, false, err
	}
	if tok.IsKRBError() {
		return nil, false, tok.KRBError
	}
	if !tok.IsAPRep() {
		return nil, false, errors.New("GSSAPI: the server sent no AP-REP")
	}
	plain, err := krbcrypto.DecryptEncPart(tok.APRep.EncPart, c.key, keyusage.AP_REP_ENCPART)
	if err != nil {
		return nil, false, err
	}
	var part messages.EncAPRepPart
	if err := part.Unmarshal(plain); err != nil {
		return nil, false, err
	}
	if part.CTime.Unix() != c.ctime.Unix() || part.Cusec != c.cusec {
		return nil, false, errors.New("GSSAPI: the AP-REP does not answer this authenticator")
	}
	c.subkey = part.Subkey
	return []byte{}, false, nil
}

// NegotiateSaslAuth is RFC 4752 3.1's last step: the server's wrap token (its checksum checked, a rotation undone),
// answered with "no security layer" (1) and a zero buffer size.
func (c *cbGSSAPI) NegotiateSaslAuth(input []byte, authzid string) ([]byte, error) {
	if len(input) < gssapi.HdrLen || input[0] != 0x05 || input[1] != 0x04 || input[3] != gssapi.FillerByte {
		return nil, errors.New("GSSAPI: not a wrap token")
	}
	fl := input[2]
	if fl&0x01 == 0 || fl&0x02 != 0 { // from the acceptor, not sealed
		return nil, errors.New("GSSAPI: unexpected wrap token flags")
	}
	ec, rrc := int(binary.BigEndian.Uint16(input[4:6])), int(binary.BigEndian.Uint16(input[6:8]))
	data := input[gssapi.HdrLen:]
	if len(data) < ec {
		return nil, errors.New("GSSAPI: short wrap token")
	}
	if len(data) > 0 && rrc > 0 { // RFC 4121 4.2.5: rotated right by RRC octets
		r := rrc % len(data)
		data = append(append([]byte{}, data[r:]...), data[:r]...)
	}
	key := c.key
	if fl&0x04 != 0 {
		if len(c.subkey.KeyValue) == 0 {
			return nil, errors.New("GSSAPI: the acceptor subkey is missing")
		}
		key = c.subkey
	}
	in := gssapi.WrapToken{Flags: fl, EC: uint16(ec), SndSeqNum: binary.BigEndian.Uint64(input[8:16]),
		Payload: data[:len(data)-ec], CheckSum: data[len(data)-ec:]}
	if _, err := in.Verify(key, keyusage.GSSAPI_ACCEPTOR_SEAL); err != nil {
		return nil, err
	}
	if len(in.Payload) != 4 || in.Payload[0]&0x01 == 0 {
		return nil, errors.New("GSSAPI: the server does not offer \"no security layer\"")
	}
	et, err := krbcrypto.GetEtype(key.KeyType)
	if err != nil {
		return nil, err
	}
	out := gssapi.WrapToken{Flags: fl & 0x04, EC: uint16(et.GetHMACBitLength() / 8), SndSeqNum: c.seq,
		Payload: append([]byte{1, 0, 0, 0}, authzid...)}
	if err := out.SetCheckSum(key, keyusage.GSSAPI_INITIATOR_SEAL); err != nil {
		return nil, err
	}
	return out.Marshal()
}

func (c *cbGSSAPI) DeleteSecContext() error {
	clear(c.key.KeyValue)
	clear(c.subkey.KeyValue)
	return nil
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
