package main

import "testing"

func TestHumanBytesAndSplitList(t *testing.T) {
	defer setLang(lang)
	setLang("en")
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1 KB", 1536: "1.5 KB", 5 << 30: "5 GB"} {
		if got := humanBytes(n); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
	setLang("pt-BR")
	if got := humanBytes(1536); got != "1,5 KB" {
		t.Errorf("pt humanBytes = %q", got)
	}
	if got := splitList(` CN\a , ,b,`); len(got) != 2 || got[0] != `CN\a` || got[1] != "b" {
		t.Errorf("splitList = %q", got)
	}
}
