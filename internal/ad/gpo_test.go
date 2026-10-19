package ad

import (
	"errors"
	"testing"
)

func TestParseGPLink(t *testing.T) {
	links, err := ParseGPLink("[LDAP://cn={31B2F340-016D-11D2-945F-00C04FB984F9},cn=policies,cn=system,DC=empresa,DC=net;0]" +
		"[LDAP://cn={6AC1786C-016F-11D2-945F-00C04FB984F9},cn=policies,cn=system,DC=empresa,DC=net;2][LDAP://cn={X},cn=policies;1]")
	if err != nil || len(links) != 3 {
		t.Fatalf("%+v %v", links, err)
	}
	if links[0].Enforced || links[0].Disabled || links[0].Order != 3 || links[0].Target != "cn={31B2F340-016D-11D2-945F-00C04FB984F9},cn=policies,cn=system,DC=empresa,DC=net" {
		t.Fatalf("first: %+v", links[0])
	}
	if !links[1].Enforced || links[1].Disabled || !links[2].Disabled || links[2].Order != 1 {
		t.Fatalf("options: %+v", links)
	}
	if l, err := ParseGPLink(" "); err != nil || len(l) != 0 {
		t.Fatalf("empty: %+v %v", l, err)
	}
	for _, bad := range []string{"[cn=x;0]", "[LDAP://cn=x]", "[LDAP://cn=x;a]"} {
		if _, err := ParseGPLink(bad); !errors.Is(err, ErrWrongInput) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
