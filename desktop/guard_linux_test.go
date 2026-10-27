//go:build linux && !gtk3

package main

import "testing"

func TestAppURL(t *testing.T) {
	for s, want := range map[string]bool{
		"wails://localhost/": true, "wails://localhost/users?q=1": true,
		"wails://evil/": false, "wails://localhost.evil/": false, "wails://localhost:81/": false,
		"wails://u@localhost/": false, "https://localhost/": false, "about:blank": false, "": false,
	} {
		if appURL(s) != want {
			t.Errorf("%q: %v", s, !want)
		}
	}
}
