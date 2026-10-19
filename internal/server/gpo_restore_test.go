package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"ad-manager/internal/ad"
	"ad-manager/internal/store"
	"ad-manager/internal/winshares"
)

// gpoDir has the default GPO and two DCs.
type gpoDir struct{ computersDir }

func (gpoDir) GPOs() ([]ad.GPO, []ad.GPOContainer, error) {
	return []ad.GPO{{Name: "Default Domain Policy"}}, nil, nil
}

func (gpoDir) Computers(string) ([]ad.Computer, error) {
	return []ad.Computer{{Name: "DC1", DNSName: "dc1.empresa.test", IsDC: true}, {Name: "DC2", DNSName: "DC2.empresa.test", IsDC: true},
		{Name: "FS01", DNSName: "fs01.empresa.test"}}, nil
}

func TestGPORestore(t *testing.T) {
	restore := true // the agent's write mode
	var got struct {
		Name    string
		Archive []byte
	}
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/health":
			_ = json.NewEncoder(w).Encode(map[string]any{"dc": true, "realm": "EMPRESA.TEST", "gpo_restore": restore})
		case "/api/dc/gpos/restore":
			_ = json.NewDecoder(r.Body).Decode(&got)
			_, _ = w.Write([]byte(`{"guid":"{91048D73-A843-4FF2-B88F-9BCC2FB86614}","dc":"dc1.empresa.test"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer agent.Close()
	s, h := newTestStore(t)
	if err := s.store.SaveAgent(&store.Agent{Name: "dc1", URL: agent.URL, Token: "t"}); err != nil {
		t.Fatal(err)
	}
	s.dial = func(_ *ad.Config, _, pass string) (directory, error) {
		if pass != goodPassword {
			return nil, ad.ErrBadLogin
		}
		return &gpoDir{computersDir{fakeDir{admin: true}}}, nil
	}
	c := login(t, h)
	body := func(name string, archive []byte) string {
		b, _ := json.Marshal(map[string]any{"name": name, "archive": base64.StdEncoding.EncodeToString(archive)})
		return string(b)
	}

	rec := do(h, http.MethodPost, "/api/gpos/restore", body("Copy of Default", []byte{0x1f, 0x8b}), c)
	var res struct {
		GUID string
		Sync []string
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &res) != nil || res.GUID == "" ||
		len(res.Sync) != 1 || res.Sync[0] != "dc2.empresa.test" || got.Name != "Copy of Default" || string(got.Archive) != "\x1f\x8b" {
		t.Fatalf("restore: %d %s (sent %+v)", rec.Code, rec.Body, got)
	}
	for _, tt := range []struct {
		name, body string
		code       int
	}{
		{"name in use", body("default domain policy", []byte{1}), http.StatusConflict},
		{"bad name", body(" x", []byte{1}), http.StatusBadRequest},
		{"no archive", body("x", nil), http.StatusBadRequest},
		{"not JSON", "{", http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got.Name = ""
			if rec := do(h, http.MethodPost, "/api/gpos/restore", tt.body, c); rec.Code != tt.code || got.Name != "" {
				t.Fatalf("%d, want %d (%s); agent got %q", rec.Code, tt.code, rec.Body, got.Name)
			}
		})
	}
	restore = false
	if rec := do(h, http.MethodPost, "/api/gpos/restore", body("x", []byte{1}), c); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("agent without write mode: %d %s", rec.Code, rec.Body)
	}
}

func TestPlanGPOs(t *testing.T) {
	src := []ad.GPO{{GUID: "{31B2F340-016D-11D2-945F-00C04FB984F9}", Name: "Default Domain Policy"},
		{GUID: "{AAAAAAAA-0000-0000-0000-000000000001}", Name: "Mapeamento Financeiro"},
		{GUID: "{AAAAAAAA-0000-0000-0000-000000000002}", Name: "mapeamento financeiro"}, // same name twice in the source
		{GUID: "{AAAAAAAA-0000-0000-0000-000000000003}", Name: ""}}
	plan := planGPOs(src, []ad.GPO{{Name: "default domain policy"}})
	want := []string{"skip", "gpo.import", "skip", "skip"}
	for i, a := range plan {
		if a.Kind != want[i] {
			t.Fatalf("%d: %+v, want %s", i, a, want[i])
		}
	}
	if plan[1].Target != "Mapeamento Financeiro" || !strings.HasPrefix(plan[1].Detail, "{AAAAAAAA-0000-0000-0000-000000000001} ") {
		t.Fatalf("import action %+v", plan[1])
	}
	for in, want := range map[[2]string]string{{"admin", "ORIGEM"}: `ORIGEM\admin`, {"admin@origem.lab", "ORIGEM"}: "admin@origem.lab",
		{`ORIGEM\admin`, "X"}: `ORIGEM\admin`, {"admin", ""}: "admin"} {
		if got := sourceUser(in[0], in[1]); got != want {
			t.Errorf("sourceUser(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestImportADSharesInput(t *testing.T) {
	_, h := newTest(t, true)
	c := login(t, h)
	for _, body := range []string{
		`{"url":"ldaps://192.0.2.1","domain":"o.lab","user":"a","password":"p","objects":false,"shares":{"host":"-c","agent":1,"map":"E:\\=/srv/shares"}}`,
		`{"url":"ldaps://192.0.2.1","domain":"o.lab","user":"a","password":"p","objects":false,"shares":{"host":"fs01","agent":1,"map":"E:\\=srv"}}`,
		`{"url":"ldaps://192.0.2.1","domain":"o.lab","user":"a","password":"p","objects":false}`,
	} {
		if rec := do(h, http.MethodPost, "/api/import/ad/plan", body, c); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
}

func TestMapIdentities(t *testing.T) {
	p := winshares.SharePlan{Full: []string{`ORIGEM\Domain Admins`}, RW: []string{`ORIGEM\Financeiro`, `OUTRO\Financeiro`, `BUILTIN\Power Users`},
		RO: []string{`destino\Leitura`}, ROUsers: []string{`FS01\localuser`}}
	mapIdentities(&p, "ORIGEM", "DESTINO")
	if !slices.Equal(p.Full, []string{`DESTINO\Domain Admins`}) || !slices.Equal(p.RW, []string{`DESTINO\Financeiro`}) ||
		!slices.Equal(p.RO, []string{`DESTINO\Leitura`}) || len(p.ROUsers) != 0 || len(p.Warnings) != 3 {
		t.Fatalf("%+v", p)
	}
}
