// Package tlsutil builds the TLS setup for the panel and the agent: either a
// provided certificate (cert_file/key_file) or, with self_signed, a
// self-signed one generated on first start and kept so the fingerprint stays
// the same.
package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Config returns the tls.Config (nil means no TLS) and the certificate's
// SHA-256 fingerprint.
func Config(certFile, keyFile string, selfSigned bool, dir string) (*tls.Config, string, error) {
	if certFile == "" && selfSigned {
		certFile, keyFile = filepath.Join(dir, "selfsigned.crt"), filepath.Join(dir, "selfsigned.key")
		if !isValid(certFile) {
			if err := generate(certFile, keyFile); err != nil {
				return nil, "", fmt.Errorf("self-signed certificate in %s: %w", dir, err)
			}
		}
	}
	if certFile == "" || keyFile == "" {
		return nil, "", nil
	}
	c, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(c.Certificate[0])
	return &tls.Config{Certificates: []tls.Certificate{c}, MinVersion: tls.VersionTLS12}, hex.EncodeToString(sum[:]), nil
}

// isValid reports whether the certificate exists and is valid for more than 30 days.
func isValid(certFile string) bool {
	b, err := os.ReadFile(certFile)
	if err != nil {
		return false
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return false
	}
	c, err := x509.ParseCertificate(blk.Bytes)
	return err == nil && time.Until(c.NotAfter) > 30*24*time.Hour
}

// generate creates an ECDSA P-256 pair valid for 825 days covering the
// hostname, localhost and the machine's IP addresses.
func generate(certFile, keyFile string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	host, _ := os.Hostname()
	names := []string{"localhost"}
	if host != "" {
		names = append(names, host)
		if addrs, err := net.LookupHost(host); err == nil && len(addrs) > 0 {
			if cn, err := net.LookupAddr(addrs[0]); err == nil && len(cn) > 0 && trimDot(cn[0]) != host {
				names = append(names, trimDot(cn[0]))
			}
		}
	}
	var ips []net.IP
	if as, err := net.InterfaceAddrs(); err == nil {
		for _, a := range as {
			if n, ok := a.(*net.IPNet); ok && !n.IP.IsLinkLocalUnicast() {
				ips = append(ips, n.IP)
			}
		}
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	tpl := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: host, Organization: []string{"AD Manager (auto-assinado)"}},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(825 * 24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: names, IPAddresses: ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(certFile), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		return err
	}
	return os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

// trimDot removes a trailing dot from a DNS name.
func trimDot(s string) string {
	if len(s) > 0 && s[len(s)-1] == '.' {
		return s[:len(s)-1]
	}
	return s
}
