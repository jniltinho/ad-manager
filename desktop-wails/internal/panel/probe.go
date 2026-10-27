package panel

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// Cert is the certificate a panel presents, read without trusting it, and whether the system trusts it for that
// host name (then nothing needs pinning).
type Cert struct {
	Pin       string    `json:"pin"`
	Subject   string    `json:"subject"`
	Issuer    string    `json:"issuer"`
	NotBefore time.Time `json:"notBefore"`
	NotAfter  time.Time `json:"notAfter"`
	Trusted   bool      `json:"trusted"`
}

// ProbeCert opens a TLS connection to the panel at base only to read its certificate (nothing is sent over it).
func ProbeCert(ctx context.Context, base string) (*Cert, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return nil, ErrURL
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "443")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	d := tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}} //nolint:gosec // read only, verified below
	conn, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, errors.New("the panel presented no certificate")
	}
	leaf, inter := certs[0], x509.NewCertPool()
	for _, c := range certs[1:] {
		inter.AddCert(c)
	}
	_, verr := leaf.Verify(x509.VerifyOptions{DNSName: u.Hostname(), Intermediates: inter})
	return &Cert{Pin: CertPin(leaf.Raw), Subject: leaf.Subject.String(), Issuer: leaf.Issuer.String(),
		NotBefore: leaf.NotBefore, NotAfter: leaf.NotAfter, Trusted: verr == nil}, nil
}

// Call sends in (JSON, when not nil) to method path on session gen and decodes the answer into out (when not nil);
// a panel error comes back as its message.
func (p *Panel) Call(ctx context.Context, gen uint64, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	hc, req, done, err := p.request(ctx, gen, method, path, body)
	if err != nil {
		return err
	}
	defer done()
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := hc.Do(req)
	if err != nil {
		return cause(err)
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return panelError(res)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out)
}

// Login signs in on a new session (the password is only sent, never kept) and returns its generation.
func (p *Panel) Login(ctx context.Context, user, password string) (uint64, error) {
	s := p.reset()
	in := struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}{user, password}
	return s.gen, p.Call(ctx, s.gen, http.MethodPost, "/api/login", in, nil)
}

// Logout ends session gen on the panel and here (best effort: a panel that does not answer still loses it here).
func (p *Panel) Logout(gen uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = p.Call(ctx, gen, http.MethodPost, "/api/logout", struct{}{}, nil)
	p.resetIf(gen)
}
