package main

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// ── Password policy ─────────────────────────────────────────────────────────

// policy is the domain password policy (GET/PUT /api/policy).
type policy struct {
	Complexity           bool `json:"complexity"`
	MinLength            int  `json:"min_length"`
	History              int  `json:"history"`
	MinAgeDays           int  `json:"min_age_days"`
	MaxAgeDays           int  `json:"max_age_days"`
	LockoutThreshold     int  `json:"lockout_threshold"`
	LockoutMinutes       int  `json:"lockout_minutes"`
	LockoutWindowMinutes int  `json:"lockout_window_minutes"`
}

// policyDialog opens the domain's default password policy in a dialog, as the web panel's Users toolbar does: it
// reads the policy first, then saves it after a confirmation.
func (u *ui) policyDialog() {
	var p policy
	fields := []struct {
		value  *int
		label  string
		unit   string
		lo, hi int
		entry  *ringEntry
	}{
		{&p.MinLength, "Minimum length", "characters", 0, 255, nil},
		{&p.History, "History", "passwords remembered (0-24)", 0, 24, nil},
		{&p.MinAgeDays, "Minimum age", "days", 0, 998, nil},
		{&p.MaxAgeDays, "Maximum age", "days (0 = never expires)", 0, 999, nil},
		{&p.LockoutThreshold, "Lock out after", "attempts (0 = never)", 0, 999, nil},
		{&p.LockoutMinutes, "Lockout duration", "minutes", 0, 99999, nil},
		{&p.LockoutWindowMinutes, "Counting window", "minutes", 0, 99999, nil},
	}
	complexity := newCheck(t("Require complexity (uppercase, lowercase, digit/symbol)"), nil)
	warning := wrappedLabel(t("Recommended: complexity on, at least 8 characters and lockout after 10 attempts — after the cutover to Samba."))
	warning.Importance = widget.WarningImportance
	checkWeak := func() {
		n, _ := strconv.Atoi(strings.TrimSpace(fields[0].entry.Text))
		if !complexity.Checked || n < 8 {
			warning.Show()
		} else {
			warning.Hide()
		}
	}
	complexity.OnChanged = func(bool) { checkWeak() }
	grid := container.New(layout.NewFormLayout())
	for i := range fields {
		f := &fields[i]
		f.entry = newEntry()
		f.entry.OnChanged = func(string) { checkWeak() }
		grid.Add(newText(t(f.label), strongText, 13, true))
		grid.Add(container.NewBorder(nil, nil, fixedWidth(f.entry, 100), nil, newText(t(f.unit), mutedText, 12, false)))
	}
	// read copies the form into p, validating the ranges.
	read := func() error {
		p.Complexity = complexity.Checked
		for _, f := range fields {
			n, err := strconv.Atoi(strings.TrimSpace(f.entry.Text))
			if err != nil || n < f.lo || n > f.hi {
				return errors.New(tf("%s: enter a number from %d to %d.", t(f.label), f.lo, f.hi))
			}
			*f.value = n
		}
		return nil
	}
	u.bgDo(func() error { return u.panel.do(http.MethodGet, "/api/policy", nil, &p) }, func() {
		complexity.SetChecked(p.Complexity)
		for _, f := range fields {
			f.entry.SetText(strconv.Itoa(*f.value))
		}
		checkWeak()
		u.showModal(t("Domain's default password policy"), container.NewVBox(complexity, grid, warning), t("Save policy"), false, 640, func() bool {
			if err := read(); err != nil {
				u.showError(err)
				return false
			}
			in := p
			u.confirm(t("Password policy"), t("Change the domain's password policy? It applies to the next password changes.\nWhile there is a Windows DC, the Default Domain Policy may reapply the old values."),
				t("Change policy"), false, func() {
					u.bgDo(func() error { return u.panel.do(http.MethodPut, "/api/policy", in, nil) }, func() { u.setStatus(t("Policy updated.")) })
				})
			return true
		})
	})
}
