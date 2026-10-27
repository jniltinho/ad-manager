package main

import (
	"encoding/json"
	"os"
	"regexp"
	"testing"
)

// The Wails Go module and the @wailsio/runtime npm package must be the same exact version (they are released in
// lockstep; a mismatch breaks the runtime protocol).
func TestWailsVersionsInStep(t *testing.T) {
	mod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`github\.com/wailsapp/wails/v3 v(\S+)`).FindSubmatch(mod)
	if m == nil {
		t.Fatal("go.mod does not require github.com/wailsapp/wails/v3")
	}
	b, err := os.ReadFile("../frontend/package.json")
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		t.Fatal(err)
	}
	if npm := pkg.Dependencies["@wailsio/runtime"]; npm != string(m[1]) {
		t.Fatalf("go.mod has Wails v%s but frontend/package.json pins @wailsio/runtime %q: use the same exact version", m[1], npm)
	}
}
