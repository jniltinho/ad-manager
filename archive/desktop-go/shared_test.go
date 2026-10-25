package main

import "testing"

func TestOUPath(t *testing.T) {
	for in, want := range map[string]string{
		"OU=Tatuape,OU=SP,DC=empresa,DC=local": "SP / Tatuape",
		"CN=Users,DC=empresa,DC=local":         "Users",
		"DC=empresa,DC=local":                  "",
	} {
		if got := ouPath(in); got != want {
			t.Errorf("ouPath(%q) = %q, want %q", in, got, want)
		}
	}
	if parentDN("CN=ana,OU=SP,DC=x") != "OU=SP,DC=x" || parentDN(`CN=Silva\, Joao,OU=SP,DC=x`) != "OU=SP,DC=x" {
		t.Error("parentDN")
	}
}
