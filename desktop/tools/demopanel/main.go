// Command demopanel is a demo AD Manager panel for the desktop app's docs screenshots (make shots): HTTPS with a
// self-signed certificate made at start, a real sign-in (any user, the password "Secret#1"), and the read-only API
// the screens use with the fictitious domain.local of Example Company Ltd. It prints its URL and certificate pin.
// Never a real panel: it changes nothing and knows no real domain.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	first = []string{"Ana", "Bruno", "Carla", "Daniel", "Elisa", "Fabio", "Gabriela", "Hugo", "Isabel", "Joao", "Karina",
		"Lucas", "Marina", "Nelson", "Olivia", "Paulo"}
	last  = []string{"Silva", "Souza", "Costa", "Lima", "Rocha", "Alves", "Dias", "Melo"}
	depts = []string{"Finance", "Sales", "IT", "HR", "Legal", "Operations"}
	brand = map[string]any{"theme": "classic-blue", "flat": false, "company": "Example Company Ltd", "lang": "en", "logo": false}
)

// users are n demo accounts (the built-in ones first), sorted by login.
func users(n int) []map[string]any {
	v := []map[string]any{
		{"sam": "Administrator", "name": "Administrator", "dn": "CN=Administrator,CN=Users,DC=domain,DC=local", "enabled": true, "protected": true, "last_logon": "2026-10-22T09:12:00Z"},
		{"sam": "Guest", "name": "Guest", "dn": "CN=Guest,CN=Users,DC=domain,DC=local", "enabled": false, "protected": true},
		{"sam": "krbtgt", "name": "krbtgt", "dn": "CN=krbtgt,CN=Users,DC=domain,DC=local", "enabled": false, "protected": true},
	}
	for i := 0; i < n-3; i++ {
		f, l, d := first[i%len(first)], last[(i/len(first))%len(last)], depts[i%len(depts)]
		sam := strings.ToLower(f + "." + l)
		u := map[string]any{"sam": sam, "name": f + " " + l, "mail": sam + "@domain.local", "department": d,
			"dn": "CN=" + f + " " + l + ",OU=" + d + ",OU=Example,DC=domain,DC=local", "enabled": i%17 != 5, "locked": i%23 == 3,
			"upn": sam + "@domain.local", "title": "Analyst", "groups": []string{"Domain Users", d}}
		if i%11 != 0 {
			u["last_logon"] = fmt.Sprintf("2026-10-%02dT08:30:00Z", 1+i%20)
		}
		v = append(v, u)
	}
	sort.Slice(v, func(a, b int) bool {
		return strings.ToLower(v[a]["sam"].(string)) < strings.ToLower(v[b]["sam"].(string))
	})
	return v
}

func main() {
	port := flag.Int("port", 18643, "port on 127.0.0.1")
	n := flag.Int("users", 40, "demo users")
	flag.Parse()
	list := users(*n)
	count := func(k string, want bool) int {
		c := 0
		for _, u := range list {
			if v, _ := u[k].(bool); v == want {
				c++
			}
		}
		return c
	}
	var mu sync.Mutex
	sessions := map[string]bool{}
	signedIn := func(r *http.Request) bool {
		c, err := r.Cookie("adm_session")
		mu.Lock()
		defer mu.Unlock()
		return err == nil && sessions[c.Value]
	}
	reply := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	me := map[string]any{"user": "administrator", "domain": "domain.local", "netbios": "DOMAIN", "version": "v0.27.0", "brand": brand,
		"asynqmon": false, "settings": map[string]any{"lang": "en", "inactive_days": 90, "password_length": 14, "default_ou": "", "session_minutes": 30}}
	public := map[string]any{
		"/api/health": map[string]any{"domain": "domain.local", "netbios": "DOMAIN", "version": "v0.27.0", "ldap": "up", "setup": false, "brand": brand},
		"/api/i18n":   map[string]any{"messages": map[string]string{}, "patterns": [][]string{}},
	}
	private := map[string]any{
		"/api/users": list,
		"/api/summary": map[string]any{"users": map[string]int{"enabled": count("enabled", true), "disabled": count("enabled", false), "locked": count("locked", true)},
			"groups": 6, "computers": 4, "policy": map[string]any{"complexity": true, "min_length": 7}, "server": "ldaps://dc01.domain.local",
			"dcs": []map[string]any{{"name": "DC01", "sam": "DC01$", "dn": "CN=DC01,OU=Domain Controllers,DC=domain,DC=local", "dns_name": "dc01.domain.local",
				"os": "Windows Server 2022 Standard", "os_version": "10.0 (20348)", "enabled": true, "is_dc": true, "last_logon": "2026-10-22T07:00:00Z"}},
			"os": map[string]int{"Windows 11 Pro": 11, "Windows 10 Pro": 4, "Windows Server 2022 Standard": 1, "Samba": 1}},
		"/api/groups": []map[string]any{
			{"name": "Domain Admins", "type": "security", "scope": "global", "protected": true, "member_count": 2, "description": "Designated administrators of the domain"},
			{"name": "Domain Users", "type": "security", "scope": "global", "protected": true, "member_count": *n, "description": "All domain users"},
			{"name": "Finance", "type": "security", "scope": "global", "member_count": 7, "description": "Finance team"},
			{"name": "Sales", "type": "security", "scope": "global", "member_count": 7, "description": "Sales team"},
			{"name": "IT", "type": "security", "scope": "global", "member_count": 7, "description": "IT staff"},
			{"name": "Newsletter", "type": "distribution", "scope": "universal", "member_count": 30, "description": "Company news"}},
		"/api/ous": []map[string]any{
			{"dn": "CN=Users,DC=domain,DC=local", "name": "Users", "description": "Default container for users"},
			{"dn": "OU=Example,DC=domain,DC=local", "name": "Example", "description": "Example Company Ltd"},
			{"dn": "OU=Finance,OU=Example,DC=domain,DC=local", "name": "Finance"},
			{"dn": "OU=Sales,OU=Example,DC=domain,DC=local", "name": "Sales"},
			{"dn": "OU=IT,OU=Example,DC=domain,DC=local", "name": "IT"}},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/login":
			var in struct{ User, Password string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			if in.Password != "Secret#1" {
				reply(w, http.StatusUnauthorized, map[string]string{"error": "wrong user or password"})
				return
			}
			b := make([]byte, 16)
			_, _ = rand.Read(b)
			id := hex.EncodeToString(b)
			mu.Lock()
			sessions[id] = true
			mu.Unlock()
			http.SetCookie(w, &http.Cookie{Name: "adm_session", Value: id, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode})
			reply(w, http.StatusOK, map[string]bool{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/api/logout":
			if c, err := r.Cookie("adm_session"); err == nil {
				mu.Lock()
				delete(sessions, c.Value)
				mu.Unlock()
			}
			w.WriteHeader(http.StatusNoContent)
		case public[r.URL.Path] != nil:
			reply(w, http.StatusOK, public[r.URL.Path])
		case !signedIn(r):
			reply(w, http.StatusUnauthorized, map[string]string{"error": "sign in"})
		case r.URL.Path == "/api/me":
			reply(w, http.StatusOK, me)
		case private[r.URL.Path] != nil:
			reply(w, http.StatusOK, private[r.URL.Path])
		default:
			reply(w, http.StatusOK, []any{})
		}
	})
	cert, pin := selfSigned()
	ln, err := tls.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port), &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("url = \"https://localhost:%d\"\nfingerprint = \"%s\"\n", *port, pin)
	log.Fatal(http.Serve(ln, mux))
}

// selfSigned is a fresh certificate for panel.domain.local and localhost, and its pin.
func selfSigned() (tls.Certificate, string) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "panel.domain.local"},
		Issuer: pkix.Name{CommonName: "AD Manager demo CA (domain.local)"}, DNSNames: []string{"panel.domain.local", "localhost"},
		IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		log.Fatal(err)
	}
	sum := sha256.Sum256(der)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, "sha256:" + hex.EncodeToString(sum[:])
}
