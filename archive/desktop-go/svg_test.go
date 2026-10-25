package main

import "testing"

// TestWellFormedSVG: a truncated or foreign document is not a logo (the product logo stays).
func TestWellFormedSVG(t *testing.T) {
	for in, want := range map[string]bool{`<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`: true, `<?xml version="1.0"?><svg><g></g></svg>`: true,
		`<svg><rect/>`: false, `<html><svg/></html>`: false, `not xml`: false} {
		if wellFormedSVG([]byte(in)) != want {
			t.Errorf("%q", in)
		}
	}
}

// TestDrawableSVG: an SVG Fyne's parser refuses, or one with no size, is not a logo either.
func TestDrawableSVG(t *testing.T) {
	for in, want := range map[string]bool{
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="5" height="5" fill="#f00"/></svg>`:     true,
		`<svg xmlns="http://www.w3.org/2000/svg" width="20" height="10"><rect width="5" height="5"/></svg>`:              true,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="5" opacity="invalid"/></svg>`:          false,
		`<svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`:                                                          false,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="5" height="5" fill="hsl(0,,)"/></svg>`: false,
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><text>Empresa</text></svg>`:                         false,
	} {
		if drawableSVG([]byte(in)) != want {
			t.Errorf("%q", in)
		}
	}
}
