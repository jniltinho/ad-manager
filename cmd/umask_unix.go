//go:build !windows

package cmd

import "syscall"

func restrictUmask() { syscall.Umask(0o077) }
