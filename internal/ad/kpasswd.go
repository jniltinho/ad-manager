package ad

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/jcmturner/gokrb5/v8/client"
	"github.com/jcmturner/gokrb5/v8/config"
	"github.com/jcmturner/gokrb5/v8/crypto"
	"github.com/jcmturner/gokrb5/v8/kadmin"
	"github.com/jcmturner/gokrb5/v8/messages"
	"github.com/jcmturner/gokrb5/v8/types"
)

// PrepareLinkReset starts a self-service password change: it sets temp as the
// password with "must change at next logon" and clears a lockout, so that only
// ChangePasswordKerberos with temp can set the next one. It returns the user's
// display name. Protected accounts (built-in, DCs, the operator) and anything
// that is not a user are refused.
func (c *Conn) PrepareLinkReset(sam, temp string) (string, error) {
	e, err := c.object(sam)
	if err != nil {
		return "", err
	}
	if c.protected(sam, e) {
		return "", ErrProtected
	}
	u, err := c.User(sam) // computers and groups are not found here
	if err != nil {
		return "", err
	}
	if err := c.setPassword(e.DN, temp, true); err != nil {
		return "", err
	}
	if err := c.Unlock(sam); err != nil {
		return "", err
	}
	return u.Name, nil
}

// ChangePasswordKerberos changes sam's password with the kpasswd protocol
// (RFC 3244), as Windows does at logon. Unlike an LDAP change it works when the
// account must change its password and ignores the minimum password age in
// that case. The KDC is the host of cfg.URL (ports 88 and 464, TCP).
func ChangePasswordKerberos(cfg *Config, sam, oldPwd, newPwd string) error {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("%w: AD URL", ErrWrongInput)
	}
	host, realm := u.Hostname(), strings.ToUpper(cfg.Domain)
	kc, err := config.NewFromString(fmt.Sprintf(`[libdefaults]
 default_realm = %[1]s
 dns_lookup_kdc = false
 dns_lookup_realm = false
 udp_preference_limit = 1
[realms]
 %[1]s = {
  kdc = %[2]s
 }
`, realm, net.JoinHostPort(host, "88")))
	if err != nil {
		return err
	}
	cl := client.NewWithPassword(sam, realm, oldPwd, kc, client.DisablePAFXFAST(true))
	defer cl.Destroy()
	asReq, err := messages.NewASReqForChgPasswd(realm, kc, cl.Credentials.CName())
	if err != nil {
		return err
	}
	asRep, err := cl.ASExchange(realm, asReq, 0)
	if err != nil {
		return kpasswdErr(err)
	}
	return kpasswdSend(net.JoinHostPort(host, "464"), realm, cl.Credentials.CName(), asRep, newPwd)
}

// kpasswdSend sends the change request over TCP. gokrb5's own ChangePasswd
// always names the target principal, which Samba treats as an administrative
// "set password" and refuses; without a target it is the user's own change.
func kpasswdSend(server, realm string, cname types.PrincipalName, asRep messages.ASRep, newPwd string) error {
	data, err := (&kadmin.ChangePasswdData{NewPasswd: []byte(newPwd)}).Marshal()
	if err != nil {
		return err
	}
	auth, err := types.NewAuthenticator(realm, cname)
	if err != nil {
		return err
	}
	et, err := crypto.GetEtype(asRep.DecryptedEncPart.Key.KeyType)
	if err != nil {
		return err
	}
	if err := auth.GenerateSeqNumberAndSubKey(et.GetETypeID(), et.GetKeyByteSize()); err != nil {
		return err
	}
	apReq, err := messages.NewAPReq(asRep.Ticket, asRep.DecryptedEncPart.Key, auth)
	if err != nil {
		return err
	}
	priv := messages.NewKRBPriv(messages.EncKrbPrivPart{UserData: data, Timestamp: auth.CTime, Usec: auth.Cusec, SequenceNumber: auth.SeqNumber})
	if err := priv.EncryptEncPart(auth.SubKey); err != nil {
		return err
	}
	req := kadmin.Request{APREQ: apReq, KRBPriv: priv}
	b, err := req.Marshal()
	if err != nil {
		return err
	}
	conn, err := net.DialTimeout("tcp", server, 10*time.Second)
	if err != nil {
		return fmt.Errorf("kpasswd: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := conn.Write(binary.BigEndian.AppendUint32(nil, uint32(len(b)))); err != nil {
		return err
	}
	if _, err := conn.Write(b); err != nil {
		return err
	}
	var n [4]byte
	if _, err := io.ReadFull(conn, n[:]); err != nil {
		return fmt.Errorf("kpasswd: %w", err)
	}
	size := binary.BigEndian.Uint32(n[:])
	if size > 64<<10 { // a reply is a few hundred bytes; refuse to allocate what the network announces
		return fmt.Errorf("kpasswd: reply of %d bytes", size)
	}
	rb := make([]byte, size)
	if _, err := io.ReadFull(conn, rb); err != nil {
		return fmt.Errorf("kpasswd: %w", err)
	}
	var r kadmin.Reply
	if err := r.Unmarshal(rb); err != nil {
		return fmt.Errorf("kpasswd: %w", err)
	}
	if err := r.Decrypt(auth.SubKey); err != nil {
		return fmt.Errorf("kpasswd: %w", err)
	}
	switch r.ResultCode {
	case 0:
		return nil
	case 4: // KRB5_KPASSWD_SOFTERROR: length, history, complexity or age
		return ErrPwdPolicy
	}
	return fmt.Errorf("kpasswd: code %d: %s", r.ResultCode, r.Result)
}

// kpasswdErr maps gokrb5 AS errors, which only carry codes in their text.
func kpasswdErr(err error) error {
	s := err.Error()
	if strings.Contains(s, "KDC_ERR_PREAUTH_FAILED") || strings.Contains(s, "KDC_ERR_C_PRINCIPAL_UNKNOWN") ||
		strings.Contains(s, "KDC_ERR_CLIENT_REVOKED") {
		return ErrBadLogin
	}
	return fmt.Errorf("kerberos: %w", err)
}

// TempPassword returns a random password that satisfies the complexity rules
// (upper, lower, digit and symbol; 24 characters, ~95 bits). Nobody ever sees
// it.
func TempPassword() string {
	var b strings.Builder
	for _, set := range []string{"ABCDEFGHJKLMNPQRSTUVWXYZ", "abcdefghijkmnopqrstuvwxyz", "23456789", "-_.!@#%+="} {
		for range 6 {
			n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(set))))
			b.WriteByte(set[n.Int64()])
		}
	}
	return b.String()
}
