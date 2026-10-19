package main

import (
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

// setup shows the configuration form (panel and file servers). It opens on
// first run and from the "Configure" buttons; back is where Cancel returns
// (nil on first run, when there is nowhere to go back to).
func (u *ui) setup(back func()) {
	u.screen = "setup"
	c := *u.cfg
	c.Agents = append([]agentConfig(nil), u.cfg.Agents...)

	panelURL := newEntry()
	panelURL.SetPlaceHolder("https://panel.example.local:8443")
	panelURL.SetText(c.Panel.URL)
	insecure := newCheck(t("Accept self-signed certificate"), nil)
	insecure.SetChecked(c.Panel.Insecure)
	user := newEntry()
	user.SetPlaceHolder("administrator")
	user.SetText(c.Panel.User)
	result := newText("", mutedText, 12, false)
	langNames := []string{t("Panel default"), "Português", "English"}
	langCodes := []string{"", "pt-BR", "en"}
	language := newSelect(langNames, nil)
	language.SetSelectedIndex(max(0, slices.Index(langCodes, c.Lang)))

	test := newButton(t("Test connection"), theme.SearchIcon(), func() {
		result.Text, result.Color = t("Testing…"), mutedText
		result.Refresh()
		base := strings.TrimRight(strings.TrimSpace(panelURL.Text), "/")
		go func() {
			var h struct{ Domain, Version, LDAP string }
			err := newAPI(base, "", insecure.Checked).do(http.MethodGet, "/api/health", nil, &h)
			fyne.Do(func() {
				if err != nil {
					result.Text, result.Color = t("Failed: ")+err.Error(), errText
				} else {
					result.Text, result.Color = tf("OK: domain %s, panel %s, LDAP %s", h.Domain, h.Version, h.LDAP), okText
				}
				result.Refresh()
			})
		}()
	})

	// file servers: one editable row per agent
	rows := container.NewVBox()
	type agentRow struct {
		name, url, token *ringEntry
		insecure         *ringCheck
		box              fyne.CanvasObject
	}
	var agents []*agentRow
	var addRow func(a agentConfig)
	addRow = func(a agentConfig) {
		r := &agentRow{name: newEntry(), url: newEntry(), token: newPasswordEntry(), insecure: newCheck(t("self-signed"), nil)}
		r.name.SetPlaceHolder("fs01")
		r.name.SetText(a.Name)
		r.url.SetPlaceHolder("https://fs01.example.local:9443")
		r.url.SetText(a.URL)
		r.token.SetPlaceHolder(t("agent token"))
		r.token.SetText(a.Token)
		r.insecure.SetChecked(a.Insecure)
		remove := newButton("", theme.DeleteIcon(), nil)
		remove.Importance = widget.LowImportance
		r.box = container.NewBorder(nil, nil, fixedWidth(r.name, 120), container.NewHBox(r.insecure, remove),
			container.NewGridWithColumns(2, r.url, r.token))
		remove.OnTapped = func() {
			for i, x := range agents {
				if x == r {
					agents = append(agents[:i], agents[i+1:]...)
					break
				}
			}
			rows.Remove(r.box)
		}
		agents = append(agents, r)
		rows.Add(r.box)
	}
	for _, a := range c.Agents {
		addRow(a)
	}
	add := newButton(t("Add server"), theme.ContentAddIcon(), func() { addRow(agentConfig{}) })

	label := func(s string) fyne.CanvasObject { return newText(s, strongText, 12, true) }
	panelForm := container.New(layout.NewFormLayout(),
		label(t("Panel URL")), panelURL,
		layout.NewSpacer(), insecure,
		label(t("Default user")), user,
		label(t("Language")), language,
	)
	panelCard := card(t("Panel"), container.NewVBox(panelForm, container.NewBorder(nil, nil, test, nil, result)))
	agentsCard := card(t("File servers (agents)"), container.NewVBox(
		wrappedLabel(t("Optional: to see and create shares directly on the agent. Token: Settings → File server agent token → Generate token in the web panel (then run the command it shows on the file server), or on the server: sudo ad-manager agent token, then sudo systemctl restart ad-manager-agent")),
		rows, container.NewHBox(add)))

	errText := newText("", errText, 12, false)
	save := newButton(t("Save"), theme.DocumentSaveIcon(), func() {
		n := config{}
		n.Panel.URL = strings.TrimRight(strings.TrimSpace(panelURL.Text), "/")
		n.Panel.Insecure = insecure.Checked
		n.Panel.User = strings.TrimSpace(user.Text)
		n.Lang = langCodes[max(0, language.SelectedIndex())]
		for _, r := range agents {
			a := agentConfig{Name: strings.TrimSpace(r.name.Text), URL: strings.TrimRight(strings.TrimSpace(r.url.Text), "/"),
				Token: strings.TrimSpace(r.token.Text), Insecure: r.insecure.Checked}
			if a == (agentConfig{Insecure: a.Insecure}) {
				continue // empty row
			}
			n.Agents = append(n.Agents, a)
		}
		if msg := validateConfig(&n); msg != "" {
			errText.Text = msg
			errText.Refresh()
			return
		}
		if err := saveConfig(configPath, &n); err != nil {
			errText.Text = t("Could not write ") + configPath + ": " + err.Error()
			errText.Refresh()
			return
		}
		u.cfg = &n
		setLang(n.Lang)
		u.panel = newAPI(n.Panel.URL, "", n.Panel.Insecure)
		u.login()
	})
	save.Importance = widget.HighImportance
	buttons := []fyne.CanvasObject{layout.NewSpacer()}
	if back != nil {
		buttons = append(buttons, newButton(t("Cancel"), theme.CancelIcon(), back))
	}
	buttons = append(buttons, save)

	where := wrappedLabel(t("File: ") + configPath + t("  (another file: ad-manager-desktop --config path)"))
	where.Wrapping = fyne.TextWrapBreak
	form := container.NewVBox(panelCard, agentsCard, where, errText, container.NewHBox(buttons...))
	title := newText(t("Configure AD Manager desktop"), strongText, 18, true)
	stripe := canvas.NewRectangle(accent)
	stripe.SetMinSize(fyne.NewSize(0, 3))
	body := container.NewBorder(container.NewVBox(title, stripe), nil, nil, nil, container.NewVScroll(form))
	u.w.SetTitle(t("AD Manager — configure"))
	u.w.SetContent(container.New(watchResize{u}, canvas.NewRectangle(col("surface-2")),
		container.NewPadded(container.NewCenter(fixedSize(container.NewPadded(body), 820, 640)))))
}

// validateConfig checks the form; it returns a user-facing message or "".
func validateConfig(c *config) string {
	if msg := checkURL(t("Panel URL"), c.Panel.URL); msg != "" {
		return msg
	}
	seen := map[string]bool{}
	for _, a := range c.Agents {
		switch {
		case a.Name == "" || a.URL == "" || a.Token == "":
			return t("File server: fill in name, URL and token (or remove the row).")
		case seen[strings.ToLower(a.Name)]:
			return t("Duplicated file server: ") + a.Name
		}
		seen[strings.ToLower(a.Name)] = true
		if msg := checkURL(t("URL of ")+a.Name, a.URL); msg != "" {
			return msg
		}
	}
	return ""
}

// checkURL accepts only https:// URLs with a host (the panel and the agents
// refuse plain HTTP outside loopback anyway).
func checkURL(field, s string) string {
	p, err := url.Parse(s)
	if err != nil || p.Scheme != "https" || p.Host == "" {
		return field + t(": enter an https://server:port URL")
	}
	return ""
}
