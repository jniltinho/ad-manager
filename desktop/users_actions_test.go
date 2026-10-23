package main

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// usersPanel answers /api/users with one account of each kind and records the actions the page sends.
type usersPanel struct {
	*httptest.Server
	mu   sync.Mutex
	sent []string
}

func newUsersPanel(t *testing.T) *usersPanel {
	t.Helper()
	p := &usersPanel{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet {
			p.mu.Lock()
			p.sent = append(p.sent, r.Method+" "+r.URL.Path)
			p.mu.Unlock()
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if r.URL.Path == "/api/users" {
			_, _ = w.Write([]byte(`[
			 {"sam":"Administrator","name":"Administrator","enabled":true,"protected":true},
			 {"sam":"ana","name":"Ana","enabled":true,"locked":true},
			 {"sam":"bob","name":"Bob","enabled":true},
			 {"sam":"carol","name":"Carol","enabled":false},
			 {"sam":"Guest","name":"Guest","enabled":true,"locked":true,"protected":true},
			 {"sam":"OP","name":"The operator","enabled":true}]`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *usersPanel) requests() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.sent...)
}

// findTable is the first gridTable under o.
func findTable(o fyne.CanvasObject) *gridTable {
	if g, ok := o.(*gridTable); ok {
		return g
	}
	for _, k := range kids(o) {
		if g := findTable(k); g != nil {
			return g
		}
	}
	return nil
}

// TestUsersRowActions: each row's links follow its account (editable, built-in, the operator's own, locked) and
// act on that row's account — not the selected one, and still after the list changed during a confirmation; a
// cancelled confirmation sends nothing; Enter opens the selected row's actions; the toolbar has no row action; the
// action columns are icons on a 1280 px window.
func TestUsersRowActions(t *testing.T) {
	p := newUsersPanel(t)
	a := test.NewApp()
	w := a.NewWindow("t")
	defer onScreen(func() { w.Close(); a.Quit() })
	inLang(t, "en")
	u := &ui{a: a, w: w, cfg: &config{}, panel: newAPI(p.URL, "", false), status: newText("", col("footer-strong"), 11, false)}
	u.me.User = "op" // the own account, compared case-insensitively
	var tab *gridTable
	onScreen(func() {
		w.Resize(fyne.NewSize(2000, 700))
		w.SetContent(u.usersPage())
		tab = findTable(w.Content())
	})
	rows := 0
	for range 60 {
		onScreen(func() { rows, _ = tab.Length() })
		if rows == 6 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if rows != 6 {
		t.Fatalf("%d rows", rows)
	}
	const login, status, tag, password, cp, move, del = 0, 4, 5, 7, 8, 9, 10
	// sorted by login: Administrator, ana, bob, carol, Guest, OP
	const admin, ana, bob, carol, guest, own = 0, 1, 2, 3, 4, 5
	var links map[[2]int]*cellLink
	onScreen(func() {
		links = map[[2]int]*cellLink{}
		for r := range rows {
			for _, c := range []int{login, status, tag, password, cp, move, del} {
				links[[2]int{r, c}] = tab.link(r, c)
			}
		}
	})
	has := func(r, c int) bool { return links[[2]int{r, c}] != nil }
	for _, r := range []int{admin, ana, bob, carol, guest, own} {
		if !has(r, login) || !has(r, password) || !has(r, cp) {
			t.Errorf("row %d: login, password and copy are links on every account", r)
		}
	}
	for _, r := range []int{admin, guest, own} { // built-in or own: no toggle, move or delete
		if has(r, status) || has(r, move) || has(r, del) {
			t.Errorf("row %d: a built-in or own account offers status/move/delete", r)
		}
	}
	for _, r := range []int{ana, bob, carol} {
		if !has(r, status) || !has(r, move) || !has(r, del) {
			t.Errorf("row %d: an editable account lacks status/move/delete", r)
		}
	}
	if !has(ana, tag) || links[[2]int{ana, tag}].tag || has(bob, tag) {
		t.Error("unlock: a link on the locked account only")
	}
	var guestTag string
	onScreen(func() { guestTag = cellText(tab, guest, tag) })
	if l := links[[2]int{guest, tag}]; l == nil || !l.tag || guestTag != "system" {
		t.Errorf("a locked built-in account shows unlock and its system tag: %v %q", l, guestTag)
	}

	run := func(r, c int) { onScreen(links[[2]int{r, c}].run) }
	press := func(label string) {
		onScreen(func() {
			if b := buttonIn(w.Canvas().Overlays().Top(), label); b != nil {
				b.press()
			}
		})
	}
	waitSent := func(want string) bool {
		for range 60 {
			if slices.Contains(p.requests(), want) {
				return true
			}
			time.Sleep(50 * time.Millisecond)
		}
		return false
	}
	// the selected row is bob; carol's status link enables carol, at once
	onScreen(func() { tab.sel = bob })
	run(carol, status)
	if !waitSent("POST /api/users/carol/enable") {
		t.Fatalf("enable carol: %q", p.requests())
	}
	// bob's disable confirmation, cancelled: nothing sent
	before := len(p.requests())
	run(bob, status)
	press("Cancel")
	time.Sleep(200 * time.Millisecond)
	if len(p.requests()) != before {
		t.Fatalf("a cancelled confirmation sent %q", p.requests()[before:])
	}
	// bob's confirmation, then the list changes (another filter, another selection) before it is accepted
	run(bob, status)
	onScreen(func() {
		entriesIn(w.Content())[0].SetText("ana")
		tab.sel = 0
	})
	press("Disable")
	if !waitSent("POST /api/users/bob/disable") {
		t.Fatalf("disable bob after the list changed: %q", p.requests())
	}
	for _, r := range p.requests() {
		if strings.Contains(r, "/ana/") {
			t.Fatalf("the selected/visible row was acted on: %q", p.requests())
		}
	}

	// Enter on the selected row: its actions in a menu (an editable one: Disable, Move, Delete among them)
	onScreen(func() {
		entriesIn(w.Content())[0].SetText("")
		tab.sel = bob
		tab.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	})
	var menu []string
	onScreen(func() { menu = texts(w.Canvas().Overlays().Top()) })
	for _, want := range []string{"Edit", "Password", "Copy", "Disable", "Move", "Delete"} {
		if !slices.Contains(menu, want) {
			t.Fatalf("Enter menu lacks %q: %q", want, menu)
		}
	}
	onScreen(func() { w.Canvas().Overlays().Remove(w.Canvas().Overlays().Top()) })

	// the toolbar has no row action; the action columns are icons on a 1280 px window
	onScreen(func() {
		for _, label := range []string{"Copy", "Edit", "Password", "Unlock", "Disable", "Move", "Delete"} {
			if buttonIn(w.Content(), label) != nil {
				t.Errorf("toolbar still has %q", label)
			}
		}
	})
	var wide, narrow bool
	onScreen(func() { wide = tab.compact })
	onScreen(func() { w.Resize(fyne.NewSize(1280, 700)); w.Content().Refresh() })
	time.Sleep(200 * time.Millisecond)
	onScreen(func() { narrow = tab.compact })
	if wide || !narrow {
		t.Fatalf("compact: wide %v, 1280 px %v", wide, narrow)
	}
}

// cellText is the text the table's cell function gives for (row, col).
func cellText(g *gridTable, r, c int) string {
	o := g.CreateCell()
	g.UpdateCell(widget.TableCellID{Row: r, Col: c}, o)
	for _, s := range texts(o) {
		if s != "" {
			return s
		}
	}
	return ""
}
