// Command rustgen writes desktop-rust/crates/core/assets/themes.json (run from the desktop module: go run
// ./internal/themegen/rustgen).
package main

import (
	"log"
	"os"

	"ad-manager/desktop/internal/themegen"
)

func main() {
	src, err := themegen.Rust("..")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("../desktop-rust/crates/core/assets/themes.json", src, 0o644); err != nil {
		log.Fatal(err)
	}
}
