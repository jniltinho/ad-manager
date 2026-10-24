package main

import (
	"crypto/tls"
	"errors"
	"image/color"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// setup shows the configuration form: the panel's URL, the account and the language. Test reaches the panel, checks
// its certificate (one the system does not trust is pinned once the operator accepts it) and signs in; Save writes
// desktop.toml (no password, no token) and opens the app with that session. It opens on first run and from the
// "Configure" buttons; back is where Cancel returns (nil on first run, when there is nowhere to go back to).
func (u *ui) setup(back func()) {
	signedIn := u.screen == "shell" // opened from the shell: a language change alone keeps the session
	u.screen = "setup"
	c := *u.cfg

	panelURL := newEntry()
	panelURL.SetPlaceHolder("https://panel.example.local:8443")
	panelURL.SetText(c.Panel.URL)
	user := newEntry()
	user.SetPlaceHolder("administrator")
	user.SetText(c.Panel.User)
	pwd := newPasswordEntry()
	result := newText("", mutedText, 12, false)
	resultBox := container.NewVBox(result)
	langNames := []string{t("Panel default"), "Português", "English"}
	langCodes := []string{"", "pt-BR", "en"}
	language := newSelect(langNames, nil)
	language.SetSelectedIndex(max(0, slices.Index(langCodes, c.Lang)))

	// what a successful test leaves for Save: its client (signed in), the account and the pin to store
	var (
		tested   *api
		testedMe me
		savePin  string
		accepted = c.Panel.Fingerprint // the certificate the operator trusts (the config's, or one accepted here)
		gen      int                   // bumped by every edit: a test of older values never enables Save
	)
	errLine := newText("", errText, 12, false)
	var save *volButton
	sameAccount := func() bool {
		return strings.TrimRight(strings.TrimSpace(panelURL.Text), "/") == c.Panel.URL &&
			strings.TrimSpace(user.Text) == c.Panel.User && pwd.Text == ""
	}
	canSave := func() {
		if tested != nil || (signedIn && sameAccount()) {
			save.Enable()
		} else {
			save.Disable()
		}
	}
	say := func(s string, c color.Color, extra ...fyne.CanvasObject) {
		result.Text, result.Color = s, c
		result.Refresh()
		resultBox.Objects = append([]fyne.CanvasObject{result}, extra...)
		resultBox.Refresh()
		u.w.Content().Refresh() // the card grows or shrinks: lay the form out again
	}
	// abandon ends the session of a successful test that will not be used (edited, tested again, cancelled)
	abandon := func() {
		if c := tested; c != nil {
			tested = nil
			go func() { _ = c.do(http.MethodPost, "/api/logout", nil, nil) }()
		}
	}
	edited := func(string) {
		gen++
		abandon()
		canSave()
	}
	panelURL.OnChanged, user.OnChanged, pwd.OnChanged = edited, edited, edited

	var runTest func()
	runTest = func() {
		base := strings.TrimRight(strings.TrimSpace(panelURL.Text), "/")
		account := strings.TrimSpace(user.Text)
		if msg := checkURL(t("Panel URL"), base); msg != "" {
			say(msg, errText)
			return
		}
		if account == "" || pwd.Text == "" {
			say(t("Enter the user and the password to test the sign-in (the password is not saved)."), errText)
			return
		}
		say(t("Testing…"), mutedText)
		gen++ // this attempt: an earlier one still running, or any edit after this, never counts
		abandon()
		canSave()
		start, password, pin := gen, pwd.Text, accepted
		go func() {
			client, cert, h, m, err := testPanel(base, account, password, pin)
			onUI(func() {
				if start != gen { // edited or tested again meanwhile: this result is for older values
					if client != nil {
						go func() { _ = client.do(http.MethodPost, "/api/logout", nil, nil) }()
					}
					return
				}
				if cert != nil && !cert.Trusted && cert.Pin != pin {
					trust := newButton(t("Trust this certificate"), theme.ConfirmIcon(), func() {
						accepted = cert.Pin
						runTest()
					})
					say(t("Certificate not trusted"), errText,
						wrappedLabel(t("This computer does not trust the panel's certificate (self-signed). Check its fingerprint with the panel's administrator before trusting it:")),
						certDetails(cert), container.NewHBox(trust))
					return
				}
				if err != nil {
					say(t("Failed: ")+err.Error(), errText)
					return
				}
				tested, testedMe, savePin = client, m, ""
				if !cert.Trusted {
					savePin = cert.Pin
				}
				say(tf("OK: %s signed in to %s (panel %s)", m.User, m.Domain, h.Version), okText)
				canSave()
			})
		}()
	}
	test := newButton(t("Test connection"), theme.SearchIcon(), runTest)

	label := func(s string) fyne.CanvasObject { return newText(s, strongText, 12, true) }
	panelForm := container.New(layout.NewFormLayout(),
		label(t("Panel URL")), panelURL,
		label(t("User")), user,
		label(t("Password")), pwd,
		label(t("Language")), language,
	)
	hint := wrappedLabel(t("Test signs in with the account (Domain Admins). The password is used only for the test and the session; it is never saved."))
	panelCard := card(t("Panel"), container.NewVBox(panelForm, hint, container.NewHBox(test), resultBox))

	save = newButton(t("Save"), theme.DocumentSaveIcon(), func() {
		n := config{}
		n.Graphics = c.Graphics // the Rust desktop's choice, kept
		n.Panel.URL = strings.TrimRight(strings.TrimSpace(panelURL.Text), "/")
		n.Panel.User = strings.TrimSpace(user.Text)
		n.Lang = langCodes[max(0, language.SelectedIndex())]
		if tested == nil { // signed in, same account: only the language changes, the session stays
			n.Panel.Fingerprint, n.Panel.Insecure = c.Panel.Fingerprint, c.Panel.Insecure
		} else {
			n.Panel.Fingerprint = savePin
		}
		if err := saveConfig(configPath, &n); err != nil {
			errLine.Text = t("Could not write ") + configPath + ": " + err.Error()
			errLine.Refresh()
			return
		}
		u.cfg = &n
		setLang(n.Lang)
		if tested == nil {
			u.shell()
			return
		}
		client, m := tested, testedMe
		tested = nil // handed to the shell: clearing the password (an edit) must not end it
		if old := u.panel; signedIn && old != client {
			go func() { _ = old.do(http.MethodPost, "/api/logout", nil, nil) }() // the replaced session
		}
		pwd.SetText("")
		// the next sign-in screen asks this panel for its brand (the tested one came from the session)
		u.panel, u.me, u.brandFor = client, m, ""
		applyLook(u.a, u.me.Brand.Theme, u.me.Brand.Flat)
		u.shell()
	})
	save.Importance = widget.HighImportance
	canSave()
	buttons := []fyne.CanvasObject{layout.NewSpacer()}
	if back != nil {
		buttons = append(buttons, newButton(t("Cancel"), theme.CancelIcon(), func() {
			pwd.SetText("") // an edit: also ends the session of a test that will not be used
			back()
		}))
	}
	buttons = append(buttons, save)

	where := wrappedLabel(t("File: ") + configPath + t("  (another file: ad-manager-desktop --config path)"))
	where.Wrapping = fyne.TextWrapBreak
	form := container.NewVBox(panelCard, where)
	title := newText(t("Configure AD Manager desktop"), strongText, 18, true)
	stripe := canvas.NewRectangle(accent)
	stripe.SetMinSize(fyne.NewSize(0, 3))
	// Save and Cancel stay in view under the form, however long the test's result (a certificate to trust) makes it
	body := container.NewBorder(container.NewVBox(title, stripe), container.NewVBox(errLine, container.NewHBox(buttons...)),
		nil, nil, container.NewVScroll(form))
	u.w.SetTitle(t("AD Manager — configure"))
	u.w.SetContent(container.New(watchResize{u}, canvas.NewRectangle(col("surface-2")),
		container.NewPadded(container.NewCenter(fixedSize(container.NewPadded(body), 820, 640)))))
	u.w.Canvas().Focus(pwd)
}

// panelHealth is what Configure shows of the panel's public /api/health.
type panelHealth struct {
	Domain, Version string
	Setup           bool
}

// testPanel is Configure's test: the panel's certificate (cert, also when the system does not trust it and pin is
// not its fingerprint, then nothing else is tried), its health and a real sign-in with the account. client is
// signed in on success.
func testPanel(base, account, password, pin string) (client *api, cert *panelCert, h panelHealth, m me, err error) {
	if cert, err = probeCert(base); err != nil {
		return nil, nil, h, m, err
	}
	switch {
	case cert.Trusted:
		client = newClient(base, "", &tls.Config{MinVersion: tls.VersionTLS12})
	case cert.Pin == pin:
		client = newClient(base, "", pinnedTLS(pin))
	default:
		return nil, cert, h, m, errors.New("untrusted certificate")
	}
	if err = client.health(&h); err != nil {
		return nil, cert, h, m, err
	}
	if h.Setup {
		return nil, cert, h, m, errors.New(t("the panel is being set up: finish its setup wizard in a browser first"))
	}
	if err = client.do(http.MethodPost, "/api/login", map[string]string{"user": account, "password": password}, nil); err != nil {
		return nil, cert, h, m, err
	}
	if err = client.do(http.MethodGet, "/api/me", nil, &m); err != nil {
		return nil, cert, h, m, err
	}
	return client, cert, h, m, nil
}

// certDetails shows a certificate for the operator to compare: fingerprint, subject, issuer and validity.
func certDetails(c *panelCert) fyne.CanvasObject {
	label := func(k string) fyne.CanvasObject { return newText(k, strongText, 12, true) }
	return container.New(layout.NewFormLayout(),
		label(t("Fingerprint")), selectableText(strings.TrimPrefix(c.Pin, "sha256:")),
		label(t("Subject")), selectableText(c.Subject),
		label(t("Issuer")), selectableText(c.Issuer),
		label(t("Valid")), selectableText(c.NotBefore.Local().Format(dateLayout())+" – "+c.NotAfter.Local().Format(dateLayout())))
}

// selectableText is a read-only entry: the operator can select and copy the value (a fingerprint to compare).
func selectableText(s string) fyne.CanvasObject {
	e := newEntry()
	e.SetText(s)
	e.Disable()
	return e
}

// checkURL accepts only https:// URLs with a host (the panel refuses plain HTTP outside loopback anyway).
func checkURL(field, s string) string {
	p, err := url.Parse(s)
	if err != nil || p.Scheme != "https" || p.Host == "" {
		return field + t(": enter an https://server:port URL")
	}
	return ""
}
