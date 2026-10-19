// Package i18n translates the server's messages, which are in English, for the
// interfaces in Brazilian Portuguese: the web panel (catalog served at
// /api/i18n), the desktop app and the CLI. A message is translated whole, by a
// pattern, or part by part (parts are separated by ": ", as in wrapped
// errors); unknown parts (LDAP errors, names) stay as they are.
package i18n

import (
	"regexp"
	"strings"
)

// pattern is a regular expression (valid in Go and JavaScript) and its
// translation, with $1, $2 for the captures.
type pattern struct{ re, tmpl string }

var compiled = func() []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(ptPatterns))
	for i, p := range ptPatterns {
		out[i] = regexp.MustCompile(p.re)
	}
	return out
}()

// dollar turns "$1" into "${1}" for Go (in Go "$1x" would be a group named "1x").
var dollar = regexp.MustCompile(`\$(\d+)`)

// T translates s into lang ("pt-BR"); other languages get s unchanged.
func T(lang, s string) string {
	if !strings.HasPrefix(strings.ToLower(lang), "pt") || s == "" {
		return s
	}
	return pt(s)
}

func pt(s string) string {
	if v, ok := ptBR[s]; ok {
		return v
	}
	for i, re := range compiled {
		if re.MatchString(s) {
			return re.ReplaceAllString(s, dollar.ReplaceAllString(ptPatterns[i].tmpl, "$${$1}"))
		}
	}
	if head, tail, ok := strings.Cut(s, ": "); ok {
		return pt(head) + ": " + pt(tail)
	}
	return s
}

// Catalog is the pt-BR catalog for the web panel, which translates on its side.
type Catalog struct {
	Messages map[string]string `json:"messages"`
	Patterns [][2]string       `json:"patterns"` // [regexp, translation]
}

// PTCatalog returns the pt-BR catalog.
func PTCatalog() Catalog {
	c := Catalog{Messages: ptBR, Patterns: make([][2]string, len(ptPatterns))}
	for i, p := range ptPatterns {
		c.Patterns[i] = [2]string{p.re, p.tmpl}
	}
	return c
}
