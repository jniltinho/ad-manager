//go:build !linux

package syncer

import "errors"

// CheckConfinement: a confined run exists on Linux only.
func CheckConfinement(Spec) error { return errors.New("confinement: Linux only") }
