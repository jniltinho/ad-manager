// Package web embeds the built Vue SPA (frontend/ → web/dist) into the binary.
// Only web/dist/.gitkeep is versioned, so go:embed compiles without Node.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist returns the SPA rooted at dist/.
func Dist() (fs.FS, error) { return fs.Sub(distFS, "dist") }
