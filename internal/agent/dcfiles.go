package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// fileMode decides whether this agent may change files (folders, shares, ACLs, syncs, data backups and restores):
// a file server always; a domain controller only with dc_file_server and everything a DC file server needs —
// Windows ACLs through Samba, registry shares served (registry shares = yes), and share roots clear of Samba's own
// directories (private, state, lock, cache, sysvol, netlogon, /etc/samba). An unknown role never. why says what is
// missing.
func (a *Agent) fileMode(ctx context.Context) (ok bool, why string) {
	switch a.role {
	case "standalone server", "member server":
		return true, ""
	case roleDC:
	default:
		return false, "the server's role is not known"
	}
	if !a.cfg.DCFileServer {
		return false, "a domain controller: files are off (dc_file_server)"
	}
	if !a.caps.Usable() {
		return false, "a domain controller serves files only with Windows ACLs through Samba (python3-samba, acl_xattr)"
	}
	if !strings.EqualFold(testparm(ctx, "registry shares"), "yes") {
		return false, "a domain controller serves the agent's shares only with registry shares = yes in smb.conf"
	}
	if len(a.cfg.ShareRoots) == 0 {
		return false, "no share_roots"
	}
	protected := sambaPaths(ctx)
	for _, r := range a.cfg.ShareRoots {
		rr, err := filepath.EvalSymlinks(r)
		if err != nil {
			return false, fmt.Sprintf("share root %s: %v", r, err)
		}
		for _, p := range protected {
			if overlaps(rr, p) {
				return false, fmt.Sprintf("share root %s overlaps Samba's %s", r, p)
			}
		}
	}
	return true, ""
}

// sambaPaths are the directories of a DC's Samba that file shares must stay clear of (their real paths).
func sambaPaths(ctx context.Context) []string {
	set := []string{"/etc/samba", "/var/lib/samba"}
	for _, p := range []string{"private dir", "state directory", "lock directory", "cache directory", "binddns dir"} {
		if v := testparm(ctx, p); v != "" {
			set = append(set, v)
		}
	}
	for _, sec := range []string{"sysvol", "netlogon"} {
		if v := testparmSection(ctx, sec, "path"); v != "" {
			set = append(set, v)
		}
	}
	var out []string
	for _, p := range set {
		if rp, err := filepath.EvalSymlinks(p); err == nil {
			p = rp
		}
		out = append(out, filepath.Clean(p))
	}
	return out
}

// overlaps tells whether a and b are the same folder or one holds the other.
func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/") || a == "/" || b == "/"
}

// staticShares are the share names defined in smb.conf itself and the files it includes (they win over registry
// shares of the same name; on a DC: sysvol, netlogon): the agent never creates or deletes one of these names.
func staticShares(path string) []string {
	var out []string
	seen := map[string]bool{}
	var read func(path string)
	read = func(path string) {
		if seen[path] || len(seen) > 32 {
			return
		}
		seen[path] = true
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]") {
				if n := strings.TrimSpace(l[1 : len(l)-1]); !strings.EqualFold(n, "global") {
					out = append(out, n)
				}
				continue
			}
			// include = <file>: its sections count too (not "registry", nor a per-client name with % macros)
			if k, v, ok := strings.Cut(l, "="); ok && strings.EqualFold(strings.Join(strings.Fields(k), " "), "include") {
				if v = strings.TrimSpace(v); v != "" && !strings.EqualFold(v, "registry") && !strings.Contains(v, "%") {
					read(v)
				}
			}
		}
	}
	read(path)
	return out
}

var errDCSessions = errors.New("on a domain controller the agent does not end sessions (the same process serves SYSVOL and NETLOGON)")
