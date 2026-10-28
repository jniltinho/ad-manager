package syncer

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

// apiMounts are the kernel and private mounts a confined unit has writable besides its folders.
var apiMounts = []string{"/proc", "/sys", "/dev", "/tmp", "/var/tmp"}

// CheckConfinement is run by the worker in its unit before anything else: every writable mount must be an API or
// private one, or the destination or the quarantine — and those must be the folders the agent opened (a folder
// swapped while systemd set the unit up is caught here).
func CheckConfinement(sp Spec) error {
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return err
	}
	allowed := map[string]bool{sp.Dest: true}
	if sp.Quarantine != "" {
		allowed[sp.Quarantine] = true
	}
	if err := checkMounts(string(b), allowed); err != nil {
		return err
	}
	if err := sameID(sp.Dest, sp.DestID); err != nil {
		return err
	}
	if sp.Quarantine != "" {
		return sameID(sp.Quarantine, sp.QuarantineID)
	}
	return nil
}

// checkMounts refuses a writable mount that is neither allowed nor an API one. A mount covered by another one on the
// same mount point (and whatever hangs under it) is unreachable by any path, so it does not count: systemd's
// ProtectSystem makes only the top one read-only when a folder is mounted twice (a Vagrant /vagrant, an automount).
func checkMounts(mountinfo string, allowed map[string]bool) error {
	type mount struct{ parent, point, opts string }
	mounts := map[string]mount{}
	var order []string
	for _, l := range strings.Split(mountinfo, "\n") {
		f := strings.Fields(l)
		if len(f) < 6 {
			continue
		}
		mounts[f[0]] = mount{parent: f[1], point: unescapeMount(f[4]), opts: f[5]}
		order = append(order, f[0])
	}
	covered := map[string]bool{} // mount ids with another mount on top of them, on the same point
	for _, id := range order {
		if p, ok := mounts[mounts[id].parent]; ok && p.point == mounts[id].point {
			covered[mounts[id].parent] = true
		}
	}
	// reachable: not covered, and every mount above it on the way to the root reachable at its mount point (a mount
	// stacked on its parent's point is the one covering that parent, so the parent's cover does not hide it)
	reachable := func(id string) bool {
		if covered[id] {
			return false
		}
		for seen := 0; seen <= len(mounts); seen++ { // a malformed loop of parents ends here
			m := mounts[id]
			p, ok := mounts[m.parent]
			if !ok || m.parent == id {
				return true // the root of this namespace
			}
			if p.point != m.point && covered[m.parent] {
				return false
			}
			id = m.parent
		}
		return true
	}
	for _, id := range order {
		m := mounts[id]
		if !strings.HasPrefix(m.opts, "rw") || allowed[m.point] || !reachable(id) {
			continue
		}
		api := false
		for _, a := range apiMounts {
			if m.point == a || strings.HasPrefix(m.point, a+"/") {
				api = true
			}
		}
		if !api {
			return fmt.Errorf("confinement: %s is writable to the sync worker", m.point)
		}
	}
	return nil
}

func sameID(p, want string) error {
	var st syscall.Stat_t
	if err := syscall.Lstat(p, &st); err != nil {
		return fmt.Errorf("confinement: %s: %w", p, err)
	}
	if got := fmt.Sprintf("%d:%d", st.Dev, st.Ino); got != want {
		return fmt.Errorf("confinement: %s is not the folder the agent opened", p)
	}
	return nil
}

// unescapeMount undoes mountinfo's octal escapes (\040 for a space).
func unescapeMount(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			var n int
			if _, err := fmt.Sscanf(s[i+1:i+4], "%03o", &n); err == nil {
				out.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		out.WriteByte(s[i])
	}
	return out.String()
}
