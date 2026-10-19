package cmd

import "testing"

// TestTrLocale checks that the CLI speaks English unless the locale is Portuguese.
func TestTrLocale(t *testing.T) {
	tests := []struct{ all, messages, lang, want string }{
		{"", "", "", "1 action(s) failed"},
		{"", "", "en_US.UTF-8", "1 action(s) failed"},
		{"", "", "pt_BR.UTF-8", "1 ação(ões) falharam"},
		{"", "pt_BR.UTF-8", "en_US.UTF-8", "1 ação(ões) falharam"},
		{"C", "pt_BR.UTF-8", "pt_BR.UTF-8", "1 action(s) failed"},
	}
	for _, tt := range tests {
		t.Setenv("LC_ALL", tt.all)
		t.Setenv("LC_MESSAGES", tt.messages)
		t.Setenv("LANG", tt.lang)
		if got := tr("1 action(s) failed"); got != tt.want {
			t.Errorf("LC_ALL=%q LC_MESSAGES=%q LANG=%q: %q, want %q", tt.all, tt.messages, tt.lang, got, tt.want)
		}
	}
}
