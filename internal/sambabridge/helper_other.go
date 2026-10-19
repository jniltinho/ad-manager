//go:build !linux

package sambabridge

import (
	"context"
	"os"
)

// exec: the helper runs only on the Linux file servers.
func (b *Bridge) exec(context.Context, []byte, []*os.File) ([]byte, error) {
	return nil, ErrUnavailable
}

func readNTACL(*os.File, string, []string) (*ACL, error) { return nil, errNotMine }
