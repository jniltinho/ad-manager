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
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 6 || !strings.HasPrefix(f[5], "rw") {
			continue
		}
		mp := unescapeMount(f[4])
		if allowed[mp] {
			continue
		}
		api := false
		for _, a := range apiMounts {
			if mp == a || strings.HasPrefix(mp, a+"/") {
				api = true
			}
		}
		if !api {
			return fmt.Errorf("confinement: %s is writable to the sync worker", mp)
		}
	}
	if err := sameID(sp.Dest, sp.DestID); err != nil {
		return err
	}
	if sp.Quarantine != "" {
		return sameID(sp.Quarantine, sp.QuarantineID)
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
