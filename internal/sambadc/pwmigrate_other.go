//go:build !linux

package sambadc

import (
	"context"
	"errors"
)

// newLdbEnv: the password migration writes a Samba DC's local database, so it runs only there (Linux).
func newLdbEnv(context.Context) (pwEnv, error) {
	return nil, errors.New("the password migration runs on a Samba domain controller (Linux)")
}
