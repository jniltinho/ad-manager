//go:build docsshots

package main

// Desktop screenshots for docs/screenshots from fixtures (no real panel or agent): a fake panel answers the API with
// the fictitious domain.local demo — the Shares page (its file server reached through the panel), the Sync page (a
// running run and finished ones with their end and duration) and Configure (a self-signed panel's certificate to
// trust). English, Classic blue (XP), Example Company Ltd, at 2×:
//
//	DOCS_SHOTS_DIR=../docs/screenshots DOCS_VERSION=$(git describe --tags --abbrev=0) \
//	  go test -tags docsshots -run TestDocsFixtureShots -count=1 .

import (
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestDocsFixtureShots(t *testing.T) {
	dir := os.Getenv("DOCS_SHOTS_DIR")
	if dir == "" {
		t.Skip("DOCS_SHOTS_DIR not set")
	}
	at := func(hms string) string { return "2026-10-23T" + hms + "-03:00" }
	runs := fmt.Sprintf(`[
	 {"id":"r6","job":"finance","status":"running","started":%q,"progress":{"phase":"copying","transfers":338,"total":15828,"bytes":235000000}},
	 {"id":"r5","job":"finance","status":"ok","started":%q,"finished":%q,"result":{"transfers":0,"bytes":0,"deleted":0}},
	 {"id":"r4","job":"finance","status":"cancelled","error":"cancelled","started":%q,"finished":%q,"result":{"transfers":0,"deleted":0}},
	 {"id":"r3","job":"finance","status":"ok","started":%q,"finished":%q,"result":{"transfers":15828,"bytes":2740000000,"deleted":0}},
	 {"id":"r2","job":"finance","status":"ok","started":%q,"finished":%q,"result":{"transfers":187,"bytes":33900000,"deleted":0}},
	 {"id":"r1","job":"finance","status":"ok","preview":true,"started":%q,"finished":%q,"result":{"plan":{"New":187,"NewBytes":33900000}}}]`,
		at("00:22:10"), at("00:19:02"), at("00:22:05"), at("00:17:12"), at("00:17:51"), at("00:09:30"), at("00:14:08"),
		at("00:01:20"), at("00:02:02"), at("00:00:40"), at("00:00:52"))
	job := `{"id":"finance","name":"Finance","source":{"host":"fs-old.domain.local","share":"Finance","auth":"kerberos"},` +
		`"dest":"/srv/shares/Finance","mode":"copy","enabled":true,"cron":"0 2 * * *","rev":1,"approved_rev":1}`
	shares := `[
	 {"name":"Finance","path":"/srv/shares/finance","comment":"Spreadsheets and invoices","rw_groups":["DOMAIN\\Finance"],"browseable":true},
	 {"name":"Sales","path":"/srv/shares/sales","comment":"Proposals and contracts","rw_groups":["DOMAIN\\Sales"],"ro_groups":["DOMAIN\\Board"],"browseable":true},
	 {"name":"Board","path":"/srv/shares/board","comment":"Board documents","full_groups":["DOMAIN\\Board"],"browseable":true},
	 {"name":"HR","path":"/srv/shares/hr","comment":"Personnel (restricted)","rw_groups":["DOMAIN\\HR"],"ro_groups":["DOMAIN\\Board"],"abe":true},
	 {"name":"Design","path":"/srv/shares/design","comment":"Creative files","rw_groups":["DOMAIN\\Design"],"ro_groups":["DOMAIN\\Sales"],"browseable":true},
	 {"name":"Public","path":"/srv/shares/public","comment":"Files for everyone","rw_groups":["DOMAIN\\Domain Users"],"browseable":true}]`
	var done atomic.Bool // after the captures: no run going on, so the Sync page stops polling before the server closes
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/agents":
			fmt.Fprint(rw, `[{"id":1,"name":"dc01","url":"https://192.168.10.20:9443"}]`)
		case "/api/agents/1/shares":
			fmt.Fprint(rw, shares)
		case "/api/agents/1/sizes":
			fmt.Fprint(rw, `{"sizes":{"Finance":1288490188,"Sales":734003200,"Board":52428800,"HR":314572800,"Design":4294967296,"Public":629145600}}`)
		case "/api/agents/1/syncs":
			if done.Load() {
				fmt.Fprintf(rw, `{"jobs":[%s],"runs":[],"roots":["/srv/shares"],"file_server":true}`, job)
				return
			}
			fmt.Fprintf(rw, `{"jobs":[%s],"runs":%s,"roots":["/srv/shares"],"file_server":true,"kerberos":true,"machine":"DC01$"}`, job, runs)
		default:
			fmt.Fprint(rw, `{}`)
		}
	}))
	defer srv.Close()
	panel := httptest.NewTLSServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {})) // Configure's self-signed panel
	defer panel.Close()

	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("AD Manager")
	defer w.Close()
	w.Resize(fyne.NewSize(1280, 800))
	w.Canvas().(test.WindowlessCanvas).SetScale(2)
	defer func(l string, loc *time.Location, v, p string) {
		setLang(l)
		time.Local, version, configPath = loc, v, p
	}(lang, time.Local, version, configPath)
	setLang("en")
	if v := os.Getenv("DOCS_VERSION"); v != "" {
		version = v
	}
	time.Local = time.FixedZone("-03", -3*3600) // São Paulo, as the fixtures
	configPath = "/home/operator/ad-manager/.config/desktop.toml"
	cfg := &config{}
	cfg.Panel.URL, cfg.Panel.User = "https://dc1.domain.local:8443", "Administrator"
	u := &ui{a: a, w: w, cfg: cfg, panel: newAPI(srv.URL, "", false), status: newText("", col("footer-strong"), 11, false)}
	u.me.User, u.me.Domain, u.me.Netbios = "Administrator", "domain.local", "EXAMPLE"
	u.me.Brand.Theme, u.me.Brand.Company, u.me.Settings.Lang = "classic-blue", "Example Company Ltd", "en"

	shot := func(name, status string) {
		time.Sleep(3 * time.Second)
		fyne.DoAndWait(func() { // the other pages' calls the fake panel does not answer: their error dialogs
			for o := w.Canvas().Overlays().Top(); o != nil; o = w.Canvas().Overlays().Top() {
				w.Canvas().Overlays().Remove(o)
			}
			u.sheets = nil
			if status != "" {
				u.setStatus(status)
			}
		})
		time.Sleep(300 * time.Millisecond)
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, w.Canvas().Capture()); err != nil {
			t.Fatal(err)
		}
		t.Log("ok", name)
	}

	fyne.DoAndWait(func() {
		applyLook(a, u.me.Brand.Theme, false)
		u.shell()
		u.goTo(5)
	})
	shot("desktop-08-shares", "dc01: 6 share(s)")
	fyne.DoAndWait(func() { u.goTo(7) })
	shot("desktop-15-sync", "dc01: 1 sync job")
	done.Store(true)
	time.Sleep(4 * time.Second) // the Sync page's next reload (3 s while a run goes on) finds none and stops polling

	// Configure on a first run, the panel's self-signed certificate on screen to compare and trust
	fyne.DoAndWait(func() {
		u.screen = "login"
		u.setup(nil)
		es := entriesIn(w.Content())
		es[0].SetText(panel.URL)
		es[2].SetText("password")
		buttonIn(w.Content(), "Test connection").press()
	})
	for range 60 {
		var b *volButton
		fyne.DoAndWait(func() { b = buttonIn(w.Content(), "Trust this certificate") })
		if b != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	fyne.DoAndWait(func() { entriesIn(w.Content())[0].SetText("https://dc1.domain.local:8443") }) // the demo's name, not the fixture's port
	shot("desktop-02-configure", "")
}
