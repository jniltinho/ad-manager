// Command ad-manager-desktop is the desktop client managers run on their own
// machine. It talks to the panel API (login with the AD account) for users and
// groups, and directly to the file-server agents (token from the config file)
// for shares.
package main

import (
	"errors"
	"fmt"
	"image/color"
	"net/http"
	"os"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

var version = "dev" // -ldflags "-X main.version=vX.Y.Z"

// ui holds the application window and the session with the panel.
type ui struct {
	a      fyne.App
	w      fyne.Window
	cfg    *config
	panel  *api
	me     me
	status *canvas.Text
	goTo   func(page int) // switches the sidebar page (links on the Home page)

	// public brand of the panel (/api/health) for the login screen, the panel it came from and what is on screen;
	// all read and written on the UI goroutine (fyne.Do)
	brand struct {
		theme, domain, netbios string
		flat, setup            bool          // setup: the panel answered in setup mode
		logo                   fyne.Resource // the company logo (nil: the AD Manager one)
	}
	brandFor string
	screen   string            // "login", "setup" or "shell"
	loginBox fyne.CanvasObject // the login box on screen (tests check where it is)
	typed    [2]string         // user and password typed on the login screen, kept when it is rebuilt
	// the sign-in in progress outlives a rebuilt login screen (the panel's brand arriving): its result goes to the
	// message and button on screen then
	signingIn  bool
	signInDone chan struct{} // signaled once the attempt's result is on screen (tests wait on it)
	loginMsg   *[2]any       // the last result shown (text, bad), shown again on a rebuilt screen
	loginSay   func(string, bool)
	loginBtn   *volButton
	sheets     []sheet // open dialogs: kept at 10% of the height, centered, when the window resizes
}

func main() {
	parseFlags(os.Args[1:])
	a := app.NewWithID("io.github.jniltinho.admanager")
	applyLook(a, "default", false) // until the panel tells its theme (/api/health)
	a.SetIcon(resAppIcon)
	w := a.NewWindow("AD Manager")
	w.Resize(fyne.NewSize(1280, 800))
	cfg, needsSetup, err := loadConfig()
	if err != nil {
		msg := wrappedLabel(err.Error())
		msg.Wrapping = fyne.TextWrapBreak // long paths have no spaces to break on
		w.SetContent(container.NewCenter(fixedWidth(card(t("Configuration"), msg), 640)))
		w.ShowAndRun()
		return
	}
	u := &ui{a: a, w: w, cfg: cfg, panel: panelAPI(cfg),
		status: newText("", col("footer-strong"), 11, false)}
	setLang(cfg.Lang)
	if needsSetup {
		u.setup(nil)
	} else {
		u.login() // also asks the panel for its theme and default language (public /api/health)
	}
	w.ShowAndRun()
}

// panelBrand reads the company theme (Settings → Theme, flat look), logo, domain and default language from the
// panel's public health endpoint (also when it answers 503: the directory is down, the brand is still there; the
// language only when the config sets none). Everything after the requests runs on the UI goroutine: the brand is
// kept for the login screen, and only a login screen still showing redraws (with what was typed); a late answer
// never touches the setup screen or a signed-in session, and one from a panel that is no longer configured is
// dropped.
func (u *ui) panelBrand(p *api, url string) {
	var h struct {
		Domain, Netbios string
		Setup           bool // a fresh installation: the panel serves only its setup wizard (browser)
		Brand           struct {
			Lang, Theme string
			Flat, Logo  bool
		} `json:"brand"`
	}
	if p.health(&h) != nil {
		return
	}
	var logo fyne.Resource
	if h.Brand.Logo {
		logo, _ = p.image("/api/brand/logo") // nil on any error: the AD Manager logo stays
	}
	fyne.Do(func() {
		if url != u.cfg.Panel.URL {
			return
		}
		newLang := u.cfg.Lang == "" && h.Brand.Lang != "" && h.Brand.Lang != lang
		newBrand := h.Brand.Theme != u.brand.theme || h.Brand.Flat != u.brand.flat || h.Domain != u.brand.domain ||
			h.Netbios != u.brand.netbios || (logo == nil) != (u.brand.logo == nil)
		u.brand.theme, u.brand.flat, u.brand.domain, u.brand.netbios, u.brand.logo = h.Brand.Theme, h.Brand.Flat, h.Domain, h.Netbios, logo
		if newLang {
			setLang(h.Brand.Lang)
		}
		if h.Setup && !u.brand.setup && u.screen == "login" {
			u.showInfo(t("Panel being set up"), tf("The panel at %s is being set up: finish its setup wizard in a browser, with the token from the server (sudo cat /opt/ad-manager/data/setup-token). Then sign in here.", url))
		}
		u.brand.setup = h.Setup
		if (newLang || newBrand) && u.screen == "login" {
			u.login()
		}
	})
}

// setStatus writes s to the status bar.
func (u *ui) setStatus(s string) {
	u.status.Text = s
	u.status.Refresh()
}

// handleError shows err; an expired session goes back to the login screen.
func (u *ui) handleError(err error) {
	if errors.Is(err, errSessionExpired) {
		u.login()
		return
	}
	u.showError(err)
}

// ── Login like the web login (Login.vue): the theme's box with its stripe at 10% of the window height, the logo,
// "ACTIVE DIRECTORY" with NETBIOS (domain), the error under the box, the hint with the user formats ──

// loginW and loginH are the web login box: 480 wide, at least 267 high.
const loginW, loginH = 480, 267

// loginMsgLines is the most lines of the message under the box: with them the login fits an 800 × 600 window.
const loginMsgLines = 4

// loginPad is the room around the box for its shadow, the same in every theme so the box never moves.
const loginPad = 26

// atTenth places its only object centered across, its top at 10% of the height (as the web login form).
type atTenth struct{}

func (atTenth) MinSize(objs []fyne.CanvasObject) fyne.Size { return objs[0].MinSize() }

func (atTenth) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	m := objs[0].MinSize()
	objs[0].Resize(m)
	objs[0].Move(fyne.NewPos(max(0, (size.Width-m.Width)/2), max(0, size.Height*0.1-loginPad)))
}

func (u *ui) login() {
	if u.screen != "login" {
		u.typed = [2]string{u.cfg.Panel.User, ""}
	}
	u.screen = "login"
	if u.brandFor != u.cfg.Panel.URL { // first login screen for this panel: forget the last one's brand, ask
		u.brandFor = u.cfg.Panel.URL
		u.brand.theme, u.brand.flat, u.brand.domain, u.brand.netbios, u.brand.logo = "", false, "", "", nil
		go u.panelBrand(u.panel, u.cfg.Panel.URL)
	}
	applyLook(u.a, u.brand.theme, u.brand.flat) // the panel's public theme (Default until it answers)
	usr := newEntry()
	usr.SetText(u.typed[0])
	usr.OnChanged = func(s string) { u.typed[0] = s }
	pwd := newPasswordEntry()
	pwd.SetText(u.typed[1])
	pwd.OnChanged = func(s string) { u.typed[1] = s }

	// the message under the box: "signing in" or the error, with a bar in the state's color (as the web .error)
	msgLines := container.NewVBox()
	msgBar := canvas.NewRectangle(accent)
	msgBar.SetMinSize(fyne.NewSize(4, 0))
	msgBox := container.NewStack(canvas.NewRectangle(col("surface")),
		container.NewBorder(nil, nil, msgBar, nil, container.New(layout.NewCustomPaddedLayout(6, 6, 10, 10), msgLines)))
	msgBox.Hide()
	say := func(s string, bad bool) {
		msgLines.RemoveAll()
		lines := wrapWords(s, 12, loginW-30) // lines measured here: a wrapped Label misreports its height
		if len(lines) > loginMsgLines {      // ponytail: the rest is cut; the whole text would need a scroll or a dialog
			lines = lines[:loginMsgLines]
			lines[loginMsgLines-1] = ellipsis(lines[loginMsgLines-1]+"…", 12, fyne.TextStyle{}, loginW-30)
		}
		for _, l := range lines {
			msgLines.Add(newText(l, col("fg"), 12, false))
		}
		msgBar.FillColor = map[bool]color.Color{true: col("err"), false: accent}[bad]
		msgBar.Refresh()
		msgBox.Show()
		u.w.Content().Refresh() // the block grows: lay it out again
	}
	var signIn *volButton
	try := func() {
		if u.signingIn {
			return
		}
		u.signingIn, u.loginMsg = true, nil
		done := make(chan struct{}, 1) // a signal, not close(): this package has a close() of its own (colors)
		u.signInDone = done
		signIn.Disable()
		say(t("Signing in…"), false)
		client := u.panel // the attempt belongs to this panel: a panel changed meanwhile (Configure) drops its result
		go func() {
			var me me
			err := client.do(http.MethodPost, "/api/login", map[string]string{"user": usr.Text, "password": pwd.Text}, nil)
			if err == nil {
				err = client.do(http.MethodGet, "/api/me", nil, &me)
			}
			fyne.Do(func() {
				defer func() { u.signingIn = false; done <- struct{}{} }() // over once its result is on screen
				if u.screen != "login" {
					return
				}
				if u.panel != client { // another panel now: its login screen, ready to sign in
					u.signingIn = false
					u.login()
					return
				}
				u.loginBtn.Enable()
				if err != nil {
					u.loginMsg = &[2]any{err.Error(), true}
					u.loginSay(err.Error(), true)
					return
				}
				u.me = me
				u.typed[1] = ""                                   // never keep the password around
				applyLook(u.a, u.me.Brand.Theme, u.me.Brand.Flat) // the panel's theme (Default when unknown)
				u.shell()
			})
		}()
	}
	signIn = newButton(t("Sign in"), theme.LoginIcon(), try)
	signIn.Importance = widget.WarningImportance // the theme's login button (brand action)
	u.loginSay, u.loginBtn = say, signIn
	switch { // rebuilt while signing in, or after an error: the new screen says so too
	case u.signingIn:
		signIn.Disable()
		say(t("Signing in…"), false)
	case u.loginMsg != nil:
		say(u.loginMsg[0].(string), u.loginMsg[1].(bool))
	}
	configure := newButton(t("Configure"), theme.SettingsIcon(), func() { u.setup(u.login) })
	configure.Importance = widget.LowImportance
	pwd.OnSubmitted = func(string) { try() }

	// the company logo when the panel has one, else the AD Manager logo in the ink that reads on the box
	res := u.brand.logo
	if res == nil {
		res = logoOn(col("login-box"))
	}
	logo := canvas.NewImageFromResource(res)
	logo.FillMode = canvas.ImageFillContain
	var logoObj fyne.CanvasObject = fixedSize(logo, 195, 65)
	if u.brand.logo != nil && col("logo-plate").A > 0 { // the company logo on the theme's plate (as the web logo-plate)
		logoObj = container.NewStack(canvas.NewRectangle(col("logo-plate")), container.New(layout.NewCustomPaddedLayout(4, 4, 6, 6), fixedSize(logo, 183, 57)))
	}
	right := func(o *canvas.Text) *canvas.Text { o.Alignment = fyne.TextAlignTrailing; return o }
	title := container.NewVBox(right(newText("ACTIVE DIRECTORY", col("login-title"), 12, true)))
	if u.brand.domain != "" {
		nb := u.brand.netbios
		if nb == "" {
			nb = strings.ToUpper(strings.SplitN(u.brand.domain, ".", 2)[0])
		}
		mono := fyne.TextStyle{Monospace: true}
		dom := right(newText(ellipsis(nb+" ("+u.brand.domain+")", 11, mono, loginW-195-60), col("on-login-box-muted"), 11, false))
		dom.TextStyle = mono
		title.Add(dom)
	}
	label := func(s string) fyne.CanvasObject { return right(newText(s, col("on-login-box"), 13, false)) }
	form := container.New(layout.NewFormLayout(), label(t("User")), usr, label(t("Password")), pwd)
	gap := canvas.NewRectangle(color.Transparent)
	gap.SetMinSize(fyne.NewSize(0, 18)) // fixed space between the logo and the fields
	inner := container.NewVBox(container.NewBorder(nil, nil, logoObj, container.NewCenter(title)),
		gap, form, container.NewCenter(signIn))
	stripe := canvas.NewRectangle(col("login-box-bar"))
	stripe.SetMinSize(fyne.NewSize(0, 4))

	// depth, unless the look is flat (which wins over everything: only the frame): the theme's shadow, the 98
	// relief, a light top edge on dark themes; the frame reads on the page (≥ 1.5:1, an internal criterion)
	flat := look.flat
	var frame color.Color = col("login-box-line")
	if !flat && look.p.scheme == "dark" {
		page := color.NRGBAModel.Convert(col("login-page")).(color.NRGBA)
		// opaque over the page (a translucent line would show the shadow under it)
		solid := func(c color.Color) color.Color { return over(color.NRGBAModel.Convert(c).(color.NRGBA), page) }
		frame = solid(frame)
		for _, c := range []color.Color{col("field-line"), col("on-login-box-muted")} {
			if contrast(frame, page) >= 1.5 {
				break
			}
			frame = solid(c)
		}
		for i := 0; contrast(frame, page) < 1.5 && i < 20; i++ { // still dark: lighter, same hue
			frame = lighten(frame, 0.1)
		}
	}
	var top, base color.Color = col("login-box"), col("login-box")
	if !flat {
		base = darken(base, 0.06)
	}
	gloss := canvas.NewRectangle(color.NRGBA{0xff, 0xff, 0xff, 0x40}) // light top edge of the stripe
	gloss.SetMinSize(fyne.NewSize(0, 1))
	gloss.Hidden = flat
	hi := canvas.NewRectangle(fade(col("on-login-box"), 0x26)) // dark themes: a light line under the stripe
	hi.SetMinSize(fyne.NewSize(0, 1))
	hi.Hidden = flat || look.p.scheme != "dark"
	layers := []fyne.CanvasObject{canvas.NewVerticalGradient(top, base),
		container.NewBorder(container.NewVBox(container.NewStack(stripe, container.NewBorder(gloss, nil, nil, nil)), hi), nil, nil, nil,
			container.NewPadded(container.NewPadded(inner)))}
	if !flat && len(look.p.loginRelief) > 0 {
		layers = append(layers, relief(look.p.loginRelief))
	}
	box := framed(container.NewStack(layers...), frame)
	h := max(float32(loginH), box.MinSize().Height)

	shadowImg := canvas.NewImageFromResource(loginShadowImage())
	shadowImg.FillMode = canvas.ImageFillStretch
	shadowImg.Hidden = flat
	sh := look.p.loginShadow
	if flat {
		sh = boxShadow{}
	}
	u.loginBox = box
	boxed := dropShadow(box, loginW, h, sh, shadowImg, 12, loginPad)

	nb, dom := u.brand.netbios, u.brand.domain
	if dom == "" {
		dom = t("domain")
	}
	if nb == "" {
		nb = strings.ToUpper(strings.SplitN(dom, ".", 2)[0])
	}
	hint := container.NewVBox()
	for _, s := range []string{tf("Use %s, %s@%s or %s\\%s.", t("user"), t("user"), dom, nb, t("user")),
		t("Domain administrators only; every action is audited.")} {
		lines := wrapWords(s, 12, loginW)
		if len(lines) > 2 { // a very long domain: two lines at most, so the login and an error fit 800 × 600
			lines = []string{lines[0], ellipsis(lines[1]+"…", 12, fyne.TextStyle{}, loginW)}
		}
		for _, l := range lines {
			line := newText(l, mutedText, 12, false)
			line.Alignment = fyne.TextAlignCenter
			hint.Add(line)
		}
	}
	where := newText(ellipsis(u.cfg.Panel.URL, 11, fyne.TextStyle{}, loginW), mutedText, 11, false)
	where.Alignment = fyne.TextAlignCenter
	block := container.NewVBox(boxed, container.New(layout.NewCustomPaddedLayout(0, 4, loginPad, loginPad), msgBox),
		hint, where, container.NewCenter(configure))
	u.w.SetTitle(t("AD Manager — sign in"))
	u.w.SetContent(container.New(watchResize{u}, canvas.NewRectangle(col("login-page")), container.New(atTenth{}, fixedWidth(block, loginW+2*loginPad))))
	u.w.Canvas().Focus(pwd)
}

// shell lays out the main window: header, sidebar navigation with icons,
// content and footer.
func (u *ui) shell() {
	u.screen = "shell"
	u.w.SetTitle(fmt.Sprintf("AD Manager — %s — %s", u.me.Domain, u.me.User))
	if u.cfg.Lang == "" {
		setLang(u.me.Settings.Lang) // company default from the panel Settings
	}
	content, goTo := navigation([]page{
		{t("Home"), theme.HomeIcon(), u.homePage()},
		{t("Users"), theme.AccountIcon(), u.usersPage()},
		{t("Groups"), theme.ListIcon(), u.groupsPage()},
		{t("OUs"), theme.FolderOpenIcon(), u.ousPage()},
		{t("Computers"), theme.ComputerIcon(), u.computersPage()},
		{t("Shares"), theme.StorageIcon(), u.sharesPage()},
		{t("Password policy"), theme.SettingsIcon(), u.policyPage()},
		{t("Backup"), theme.HistoryIcon(), u.backupPage()},
		{t("Sync"), theme.ViewRefreshIcon(), u.syncPage()},
		{t("Logs"), theme.DocumentIcon(), u.logsPage()},
	})
	u.goTo = goTo

	logout := func() {
		_ = u.panel.do(http.MethodPost, "/api/logout", nil, nil)
		u.login()
	}
	u.status.Color = col("footer-strong")
	u.setStatus(t("Connected to ") + u.cfg.Panel.URL)
	if configNotice != "" { // said once: an older config's file-server tokens were taken off the disk
		u.setStatus(t(configNotice) + configNoticeErr)
		configNotice, configNoticeErr = "", ""
	}
	settings := func() { u.setup(u.shell) }
	u.w.SetContent(container.New(watchResize{u}, container.NewBorder(header(u.me.Domain, u.me.User, logout, settings), footer(u.status, version, u.me.Brand.Company), nil, nil, content)))
}
