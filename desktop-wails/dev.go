//go:build !production

package main

import "os"

// devServer is the Vite dev server Wails proxies to in a development build (FRONTEND_DEVSERVER_URL).
func devServer() string { return os.Getenv("FRONTEND_DEVSERVER_URL") }
