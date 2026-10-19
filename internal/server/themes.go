package server

import "strings"

// Theme is a visual theme of the panel. The registry below is the single list:
// settings validation, the public brand (/api/health, /api/me), the Settings
// select and the theme served in index.html all read it.
type Theme struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Scheme string `json:"scheme"` // "light" or "dark": color-scheme of the page
}

// themes lists the registered themes; the first one is the default.
var themes = []Theme{
	{ID: themeDefault, Label: "Default", Scheme: "light"},
	{ID: themeADManager, Label: "ADManager", Scheme: "light"},
	{ID: "breeze", Label: "Breeze", Scheme: "light"},
	{ID: "breeze-dark", Label: "Breeze (dark)", Scheme: "dark"},
	{ID: "violet", Label: "Violet", Scheme: "light"},
	{ID: "violet-dark", Label: "Violet (dark)", Scheme: "dark"},
	{ID: "aurora", Label: "Aurora", Scheme: "light"},
	{ID: "aurora-dark", Label: "Aurora (dark)", Scheme: "dark"},
	{ID: "crystal", Label: "Crystal", Scheme: "light"},
	{ID: "crystal-dark", Label: "Crystal (dark)", Scheme: "dark"},
	{ID: "cone", Label: "Cone (dark)", Scheme: "dark"},
	{ID: "classic-blue", Label: "Classic blue (XP)", Scheme: "light"},
	{ID: "classic-gray", Label: "Classic gray (98)", Scheme: "light"},
	{ID: "graphite", Label: "Graphite (dark)", Scheme: "dark"},
}

// themeByID returns the registered theme, or the default one and false.
func themeByID(id string) (Theme, bool) {
	for _, t := range themes {
		if t.ID == id {
			return t, true
		}
	}
	return themes[0], false
}

// themeList is the human list of theme ids for messages: "a, b or c".
func themeList() string {
	ids := make([]string, len(themes))
	for i, t := range themes {
		ids[i] = t.ID
	}
	if len(ids) == 1 {
		return ids[0]
	}
	return strings.Join(ids[:len(ids)-1], ", ") + " or " + ids[len(ids)-1]
}

// validTheme reports whether id is in the registry.
func validTheme(id string) bool {
	_, ok := themeByID(id)
	return ok
}
