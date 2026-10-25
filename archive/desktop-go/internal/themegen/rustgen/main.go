// Command rustgen writes desktop/crates/core/assets/themes.json (run from archive/desktop-go: go run
// ./internal/themegen/rustgen).
package main

import (
	"log"
	"os"

	"ad-manager/desktop/internal/themegen"
)

func main() {
	src, err := themegen.Rust("../..") // the repository root (this module is archive/desktop-go)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("../../desktop/crates/core/assets/themes.json", src, 0o644); err != nil {
		log.Fatal(err)
	}
}
