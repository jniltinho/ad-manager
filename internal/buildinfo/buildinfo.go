// Package buildinfo exposes build metadata injected via -ldflags.
package buildinfo

var (
	// Version is the release tag (default "dev").
	Version = "dev"
	// Commit is the short Git hash.
	Commit = "none"
	// BuildTime is the UTC build timestamp (ISO 8601).
	BuildTime = "unknown"
)
