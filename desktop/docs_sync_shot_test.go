//go:build docsshots

package main

// The desktop Sync page for docs/screenshots from fixtures (no panel or agent): a fake panel answers the sync API with
// a running run and finished ones (their end and duration), the fictitious domain.local demo, English, Classic blue
// (XP), at 2×:
//
//	DOCS_SHOTS_DIR=../docs/screenshots DOCS_VERSION=$(git describe --tags --abbrev=0) \
//	  go test -tags docsshots -run TestDocsSyncShot -count=1 .

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

func TestDocsSyncShot(t *testing.T) {
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
	var done atomic.Bool // after the capture: no run going on, so the page stops polling before the server closes
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/agents":
			fmt.Fprint(rw, `[{"id":1,"name":"dc01","url":"https://192.168.10.20:9443","role":"dc"}]`)
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

	a := test.NewApp()
	defer a.Quit()
	w := a.NewWindow("AD Manager")
	defer w.Close()
	w.Resize(fyne.NewSize(1280, 800))
	w.Canvas().(test.WindowlessCanvas).SetScale(2)
	defer func(l string, loc *time.Location, v string) { setLang(l); time.Local, version = loc, v }(lang, time.Local, version)
	setLang("en")
	if v := os.Getenv("DOCS_VERSION"); v != "" {
		version = v
	}
	time.Local = time.FixedZone("-03", -3*3600) // São Paulo, as the fixtures
	cfg := &config{}
	cfg.Panel.URL = srv.URL
	u := &ui{a: a, w: w, cfg: cfg, panel: newAPI(srv.URL, "", false), status: newText("", col("footer-strong"), 11, false)}
	u.me.User, u.me.Domain, u.me.Netbios = "Administrator", "domain.local", "EXAMPLE"
	u.me.Brand.Theme, u.me.Brand.Company, u.me.Settings.Lang = "classic-blue", "Example Company Ltd", "en"
	fyne.DoAndWait(func() {
		applyLook(a, u.me.Brand.Theme, false)
		u.shell()
		u.goTo(8)
	})
	time.Sleep(3 * time.Second)
	fyne.DoAndWait(func() { // the other pages' calls the fake panel does not answer: their error dialogs, and the status
		for o := w.Canvas().Overlays().Top(); o != nil; o = w.Canvas().Overlays().Top() {
			w.Canvas().Overlays().Remove(o)
		}
		u.sheets = nil
		u.setStatus("dc01: 1 sync job")
	})
	time.Sleep(300 * time.Millisecond)
	f, err := os.Create(filepath.Join(dir, "desktop-15-sync.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, w.Canvas().Capture()); err != nil {
		t.Fatal(err)
	}
	done.Store(true)
	time.Sleep(4 * time.Second) // the page's next reload (3 s while a run goes on) finds none and stops polling
}
