package syncer

import (
	"strings"
	"testing"
)

// The worker's view as systemd leaves it, from the lab's samba-dst-fs: / read-only, /vagrant mounted twice (at boot and
// by Vagrant), the covered mount still rw and the top one read-only.
const labMountinfo = `488 470 252:1 / / ro,relatime shared:1 - ext4 /dev/sda1 rw
489 488 0:5 / /dev rw,nosuid shared:2 - devtmpfs udev rw
490 488 0:22 / /proc rw,nosuid,nodev,noexec shared:3 - proc proc rw
491 488 0:30 / /tmp rw,nosuid,nodev shared:4 - tmpfs tmpfs rw
492 488 252:2 / /srv/shares/Financeiro rw,relatime shared:5 - ext4 /dev/sdb1 rw
516 488 0:54 / /vagrant rw,nodev,relatime shared:448 master:309 - vboxsf vagrant rw,iocharset=utf8
517 516 0:59 / /vagrant ro,nodev,relatime shared:449 master:412 - vboxsf vagrant rw,iocharset=utf8
`

func TestCheckMounts(t *testing.T) {
	allowed := map[string]bool{"/srv/shares/Financeiro": true}
	if err := checkMounts(labMountinfo, allowed); err != nil {
		t.Fatalf("covered /vagrant refused: %v", err)
	}
	// the top mount writable: refused
	if err := checkMounts(strings.Replace(labMountinfo, "/vagrant ro,", "/vagrant rw,", 1), allowed); err == nil {
		t.Fatal("a reachable writable /vagrant was accepted")
	}
	// a writable folder that is not the destination: refused
	if err := checkMounts(labMountinfo+"518 488 252:3 / /srv/other rw,relatime - ext4 /dev/sdc1 rw\n", allowed); err == nil {
		t.Fatal("a writable /srv/other was accepted")
	}
	// a writable mount under a covered one is unreachable too
	under := labMountinfo + "519 516 0:60 / /vagrant/sub rw,relatime - tmpfs tmpfs rw\n"
	if err := checkMounts(under, allowed); err != nil {
		t.Fatalf("a mount under the covered /vagrant refused: %v", err)
	}
	// ...but one under the visible top mount is reachable
	if err := checkMounts(labMountinfo+"520 517 0:61 / /vagrant/sub rw,relatime - tmpfs tmpfs rw\n", allowed); err == nil {
		t.Fatal("a writable mount under the visible /vagrant was accepted")
	}
	// a space in a mount point (octal escape) is matched unescaped
	if err := checkMounts("1 0 8:1 / / ro - ext4 /dev/sda1 rw\n2 1 8:2 / /srv/My\\040Share rw - ext4 /dev/sdb1 rw\n",
		map[string]bool{"/srv/My Share": true}); err != nil {
		t.Fatalf("escaped destination: %v", err)
	}
	// a parent loop does not hang
	if err := checkMounts("1 2 8:1 / / ro - ext4 a rw\n2 1 8:2 / /x rw - ext4 b rw\n", nil); err == nil {
		t.Fatal("a writable /x in a parent loop was accepted")
	}
}
