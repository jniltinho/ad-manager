package main

import (
	"fmt"
	"strings"

	"ad-manager/internal/i18n"
)

// Interface language. Source strings are English; Portuguese translations are
// registered per file with addPT in an init() — the same scheme as the web
// panel (frontend/src/i18n.js). lang follows, in order: the desktop.toml
// "lang", the panel default (Settings) and finally English.
var (
	lang = "en"
	pt   = map[string]string{}
)

// addPT registers Portuguese translations (English source → Portuguese).
func addPT(m map[string]string) {
	for k, v := range m {
		pt[k] = v
	}
}

// setLang switches the interface language ("pt-BR" or "en"); anything else is ignored.
func setLang(l string) {
	if l == "pt-BR" || l == "en" {
		lang = l
	}
}

// t translates an English source string.
func t(s string) string {
	if lang == "en" {
		return s
	}
	if v, ok := pt[s]; ok {
		return v
	}
	return s
}

// serverText shows a panel/agent message (always English) in the interface
// language, with the catalog shared with the panel and the CLI.
func serverText(s string) string { return i18n.T(lang, s) }

// tf translates format and fills it like fmt.Sprintf.
func tf(format string, args ...any) string { return fmt.Sprintf(t(format), args...) }

// dateLayout is the date/time format of the interface language.
func dateLayout() string {
	if lang == "en" {
		return "01/02/2006 03:04 PM"
	}
	return "02/01/2006 15:04"
}

// yesNo returns the translated yes/no.
func yesNo(b bool) string {
	if b {
		return t("yes")
	}
	return t("no")
}

// joinList joins a list with ", " (empty lists show as "—").
func joinList(l []string) string {
	if len(l) == 0 {
		return "—"
	}
	return strings.Join(l, ", ")
}
