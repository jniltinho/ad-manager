package server

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// The desktop app (desktop/) carries a copy of the theme registry for Configure when the panel cannot be
// reached: it must be this one.
func TestDesktopThemesInStep(t *testing.T) {
	b, err := os.ReadFile("../../frontend/src/desktop/themes.json")
	if err != nil {
		t.Fatal(err)
	}
	var desktop []Theme
	if err := json.Unmarshal(b, &desktop); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(desktop, themes) {
		t.Fatal("frontend/src/desktop/themes.json is not the theme registry of internal/server/themes.go: copy it")
	}
}
