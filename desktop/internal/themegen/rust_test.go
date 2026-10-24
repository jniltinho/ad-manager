package themegen

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestParseGradientAndShadows(t *testing.T) {
	g, err := parseGradient("linear-gradient(to right in oklab, #0054e3 0%, #1f6ee0 100%)")
	if err != nil || g.Dir != "to right" || g.Space != "oklab" || len(g.Stops) != 2 || g.Stops[1].Pos != 1 || g.Stops[0].Color != "#0054e3ff" {
		t.Fatalf("gradient: %+v %v", g, err)
	}
	if g, err := parseGradient("none"); g != nil || err != nil {
		t.Fatalf("none: %+v %v", g, err)
	}
	if _, err := parseGradient("radial-gradient(#000 1px, transparent 1px)"); err == nil {
		t.Fatal("a radial gradient is not a linear one")
	}
	s, err := parseShadows("0 0 0 1px #0b0b10, 0 4px 24px rgba(0, 0, 0, 0.7), inset 0 3px #e68b2c, 0 0 #0000")
	if err != nil || len(s) != 3 {
		t.Fatalf("shadows: %+v %v", s, err)
	}
	if s[0].Spread != 1 || s[0].Inset || s[1].Blur != 24 || !s[2].Inset || s[2].DY != 3 {
		t.Fatalf("layers in CSS order with spread, blur and inset: %+v", s)
	}
}

// TestRustThemesUpToDate: desktop-rust's themes.json is what the web's CSS gives now (run make themes in desktop-rust).
func TestRustThemesUpToDate(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	want, err := Rust(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "desktop-rust", "crates", "core", "assets", "themes.json"))
	if err != nil {
		t.Skip("desktop-rust not in this checkout")
	}
	if !bytes.Equal(got, want) {
		t.Fatal("desktop-rust/crates/core/assets/themes.json is stale: run make themes in desktop-rust")
	}
}
