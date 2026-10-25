// Command gen writes archive/desktop-go/themes_gen.go (run by go generate in the desktop module).
package main

import (
	"log"
	"os"

	"ad-manager/desktop/internal/themegen"
)

func main() {
	src, err := themegen.Generate("../..")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("themes_gen.go", src, 0o644); err != nil {
		log.Fatal(err)
	}
}
