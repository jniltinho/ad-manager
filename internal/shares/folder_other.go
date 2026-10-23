//go:build !linux

package shares

import (
	"context"
	"errors"

	"ad-manager/internal/sambabridge"
)

// CreateFolder is only implemented on Linux: the agent runs on the Samba file
// server.
func (m *Manager) CreateFolder(context.Context, FolderReq) error {
	return errors.New("the shares agent only works on Linux")
}

// ReadACL is only implemented on Linux.
func (m *Manager) ReadACL(context.Context, string) (*sambabridge.ACL, error) {
	return nil, sambabridge.ErrUnavailable
}

// FolderACLs is only implemented on Linux.
func (m *Manager) FolderACLs(context.Context, []string) map[string]*sambabridge.ACL {
	return map[string]*sambabridge.ACL{}
}

// SID is only implemented on Linux (winbind on the file server).
func (m *Manager) SID(context.Context, string, bool) (string, error) {
	return "", sambabridge.ErrUnavailable
}

// adminsName is only known on Linux (winbind on the file server).
func (m *Manager) adminsName(context.Context) (string, error) { return "", nil }

// ACLHistory is only implemented on Linux.
func (m *Manager) ACLHistory(string) ([]ACLChange, error) { return nil, sambabridge.ErrUnavailable }

// ValidateRestore is only implemented on Linux.
func (m *Manager) ValidateRestore(*RestoreReq) error { return sambabridge.ErrUnavailable }

// RestoreACL is only implemented on Linux.
func (m *Manager) RestoreACL(context.Context, RestoreReq) (*sambabridge.ACL, error) {
	return nil, sambabridge.ErrUnavailable
}

func (m *Manager) Reinherit(context.Context, string) (*sambabridge.ACL, error) {
	return nil, sambabridge.ErrUnavailable
}

func (m *Manager) ReinheritPaths(context.Context, string, []string) (*sambabridge.ACL, error) {
	return nil, sambabridge.ErrUnavailable
}
