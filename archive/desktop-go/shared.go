package main

import (
	"net/http"
	"slices"
	"strings"
)

// orgUnit is an OU or default container (GET /api/ous).
type orgUnit struct {
	DN          string `json:"dn"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// fetchOUs returns the OUs and default containers sorted by path.
func (u *ui) fetchOUs() ([]orgUnit, error) {
	var r []orgUnit
	if err := u.panel.do(http.MethodGet, "/api/ous", nil, &r); err != nil {
		return nil, err
	}
	slices.SortFunc(r, func(a, b orgUnit) int { return strings.Compare(ouPath(a.DN), ouPath(b.DN)) })
	return r, nil
}

// ouPath shows a DN as a readable path without the domain part:
// "OU=Tatuape,OU=SP,DC=example,DC=local" → "SP / Tatuape".
func ouPath(dn string) string {
	var parts []string
	for _, rdn := range strings.Split(dn, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(rdn), "=")
		if !ok || strings.EqualFold(k, "DC") {
			continue
		}
		parts = append([]string{v}, parts...)
	}
	return strings.Join(parts, " / ")
}

// ouSelect builds a select of OUs showing readable paths; selected returns the
// DN of the current choice ("" when nothing is selected).
func ouSelect(ous []orgUnit, current string) (sel *ringSelect, selected func() string) {
	labels := make([]string, len(ous))
	byLabel := map[string]string{}
	for i, o := range ous {
		labels[i] = ouPath(o.DN)
		byLabel[labels[i]] = o.DN
	}
	sel = newSelect(labels, nil)
	for _, o := range ous {
		if strings.EqualFold(o.DN, current) {
			sel.SetSelected(ouPath(o.DN))
		}
	}
	return sel, func() string { return byLabel[sel.Selected] }
}

// parentDN returns the DN of the container of dn, skipping escaped commas
// ("CN=Silva\\, Joao,OU=SP,…" → "OU=SP,…").
func parentDN(dn string) string {
	for i := 0; i < len(dn); i++ {
		switch dn[i] {
		case '\\':
			i++ // the next character is escaped
		case ',':
			return dn[i+1:]
		}
	}
	return ""
}
