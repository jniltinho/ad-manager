## 1. Fix

- [x] 1.1 `checkMounts`: covered mounts and the mounts under them do not count; the top mount of a stack does.
- [x] 1.2 Tests with the lab's `mountinfo`: covered `/vagrant` accepted; the top one writable, another writable folder
      and a writable mount under the visible top refused; escaped mount points; a parent loop ends.
- [x] 1.3 Lab: `lab-samba.sh path-a` and `validate` sync on `samba-dst-fs` after a reboot, file hashes equal.
