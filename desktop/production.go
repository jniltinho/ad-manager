//go:build production

package main

// devServer is never used by a production build: it serves only the embedded SPA.
func devServer() string { return "" }
