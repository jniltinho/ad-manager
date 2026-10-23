# AD Manager — Samba bridge.
#
# Embedded in the Go binary (internal/sambabridge) and started by the agent, as
# root, with `python3 -I -s -B -c <this code>`, once: it serves requests one at a
# time on the Unix socket it inherits as fd 3 (each message: a 4-byte little-endian
# length and the JSON; the request's folders come with it as SCM_RIGHTS
# descriptors), so Samba's modules are imported once, not per request. It only
# does what Samba's own code does correctly:
# Windows (NT) ACLs written through smbd's VFS stack (acl_xattr), which keeps
# the NTACL xattr and the equivalent POSIX ACL in step; and shares in Samba's
# registry configuration (the one `net conf` edits) through its smbconf API,
# one transaction per change.
#
# Request:  {"op": "capabilities" | "get_acl" | "get_acls" | "set_folder_acl" | "set_acl_sddl" | "reinherit_paths" |
#            "list_shares" | "set_share" | "delete_share", "args": {...}}
# Response: {"ok": true, "result": ...} or {"ok": false, "error": "...", "code": "..."}
#
# The folder is passed as an open file descriptor (sent with the request by the
# agent, which opened it without following links and checked it); the path is
# only used to double-check that the descriptor is the expected folder inside
# an allowed root. Every input is validated again here. Propagating a new ACL to
# the folder's contents walks the tree from that descriptor without following
# symbolic links or leaving the file system (os.fwalk + O_NOFOLLOW).
import errno
import json
import os
import re
import stat
import sys

MAX_INPUT = 1 << 20
SOCK_FD = 3  # the socket to the agent
MAX_ITEMS = 1000000  # a propagation stops (with an error) past this many entries
MAX_BATCH = 250  # folders per get_acls (descriptors sent with the request; Linux takes 253 per message)
REQ_FDS = []  # the descriptors of the request being served, in the order of its folders


def fd_at(i):
    """The descriptor of the request's i-th folder."""
    if i >= len(REQ_FDS):
        raise BridgeError("folder descriptor missing")
    return REQ_FDS[i]

# full domain SIDs, Unix SIDs of a standalone server (S-1-22-1 users, S-1-22-2
# groups) and a few well-known ones (BA, SYSTEM, CREATOR OWNER)
SID_RE = re.compile(r"^S-1-(5-21-\d+-\d+-\d+-\d+|22-[12]-\d+|5-32-544|5-18|3-0)$")

# Windows access masks (as shown by Explorer)
FULL = 0x001F01FF     # Full control
MODIFY = 0x001301BF   # Modify
READ_EXEC = 0x001200A9  # Read & execute
ACCESS = {"full": FULL, "rw": MODIFY, "ro": READ_EXEC}


class BridgeError(Exception):
    def __init__(self, msg, code="invalid"):
        super().__init__(msg)
        self.code = code


def samba_modules():
    from samba.samba3 import param as s3param
    from samba.samba3 import smbd
    from samba.dcerpc import security
    from samba.auth_util import system_session_unix
    return s3param, smbd, security, system_session_unix


def loadparm(s3param):
    from samba import param
    lp = s3param.get_context()
    lp.load(param.default_path())
    return lp


def capabilities(_args):
    caps = {"nt_acl": False, "acl_xattr": False, "samba": "", "python": sys.version.split()[0]}
    try:
        import samba
        caps["samba"] = getattr(samba, "version", "")
        s3param, _smbd, _security, _session = samba_modules()
        lp = loadparm(s3param)
        vfs = " ".join(lp.get("vfs objects") or []) if isinstance(lp.get("vfs objects"), list) else str(lp.get("vfs objects") or "")
        caps["acl_xattr"] = "acl_xattr" in vfs.split()
        caps["role"] = str(lp.get("server role") or "")
        name = lp.get("acl_xattr:security_acl_name")  # None when not set; "" when set empty (Samba keeps it)
        caps["ntacl_xattr"] = None if name is None else str(name)
        caps["vfs_objects"] = vfs.split()
        caps["nt_acl"] = True
    except Exception as e:  # python3-samba missing or broken
        caps["error"] = str(e)
    return caps


def checked_folder(args, fd=None):
    """Validate path/roots and that the descriptor fd (the request's first folder by default) is that very folder;
    return the fd path."""
    path, roots = args.get("path"), args.get("roots")
    if not isinstance(path, str) or not path.startswith("/") or ".." in path.split("/") or "\0" in path:
        raise BridgeError("path must be absolute, without ..")
    if not isinstance(roots, list) or not roots:
        raise BridgeError("roots missing")
    path = os.path.normpath(path)
    for forbidden in ("/etc", "/var/lib/samba", "/proc", "/sys", "/dev", "/usr", "/boot", "/root"):
        if path == forbidden or path.startswith(forbidden + "/"):
            raise BridgeError("%s is a system path" % path)
    inside = False
    for r in roots:
        if not isinstance(r, str) or not r.startswith("/") or os.path.normpath(r) == "/":
            raise BridgeError("invalid root")
        r = os.path.normpath(r)
        if path.startswith(r + "/"):
            inside = True
    if not inside:
        raise BridgeError("%s is outside the allowed roots" % path)
    if fd is None:
        fd = fd_at(0)
    try:
        st = os.fstat(fd)
        real = os.readlink("/proc/self/fd/%d" % fd)
    except OSError:
        raise BridgeError("folder descriptor missing")
    if not stat.S_ISDIR(st.st_mode) or real != path:
        raise BridgeError("%s changed (now %s)" % (path, real))
    return "/proc/self/fd/%d" % fd


def service_arg(args):
    svc = args.get("service")
    if svc in (None, ""):
        return None
    if not isinstance(svc, str) or not re.match(r"^[A-Za-z0-9][A-Za-z0-9 ._-]{0,78}\$?$", svc):
        raise BridgeError("invalid share name")
    return svc


def sid_arg(v, what):
    if not isinstance(v, str) or not SID_RE.match(v):
        raise BridgeError("invalid SID for %s: %r" % (what, v))
    return v


def describe(sd, security):
    aces = []
    for ace in (sd.dacl.aces if sd.dacl else []):
        kind = {security.SEC_ACE_TYPE_ACCESS_ALLOWED: "allow", security.SEC_ACE_TYPE_ACCESS_DENIED: "deny"}.get(ace.type, str(ace.type))
        aces.append({"sid": str(ace.trustee), "type": kind, "mask": ace.access_mask, "flags": ace.flags,
                     "inherited": bool(ace.flags & security.SEC_ACE_FLAG_INHERITED_ACE),
                     "access": {FULL: "full", MODIFY: "rw", READ_EXEC: "ro"}.get(ace.access_mask, "custom")})
    return {"owner": str(sd.owner_sid), "group": str(sd.group_sid), "sddl": sd.as_sddl(), "aces": aces,
            "protected": bool(sd.type & security.SEC_DESC_DACL_PROTECTED), "null_dacl": sd.dacl is None}


def get_acl(args):
    fdpath = checked_folder(args)
    s3param, smbd, security, system_session_unix = samba_modules()
    loadparm(s3param)
    info = security.SECINFO_OWNER | security.SECINFO_GROUP | security.SECINFO_DACL
    sd = smbd.get_nt_acl(fdpath, info, system_session_unix(), service=service_arg(args))
    return describe(sd, security)


def get_acls(args):
    """get_acl of several folders in one process (descriptors 3, 4, … in order): the share list reads them all with
    one import and one smb.conf load. Each folder answers {"acl"} or {"error", "code"}."""
    items, roots = args.get("items"), args.get("roots")
    if not isinstance(items, list) or not items or len(items) > MAX_BATCH:
        raise BridgeError("items: 1 to %d folders" % MAX_BATCH)
    s3param, smbd, security, system_session_unix = samba_modules()
    loadparm(s3param)
    info = security.SECINFO_OWNER | security.SECINFO_GROUP | security.SECINFO_DACL
    out = []
    for i, it in enumerate(items):
        try:
            if not isinstance(it, dict):
                raise BridgeError("invalid item")
            fdpath = checked_folder({"path": it.get("path"), "roots": roots}, fd_at(i))
            sd = smbd.get_nt_acl(fdpath, info, system_session_unix(), service=service_arg(it))
            out.append({"acl": describe(sd, security)})
        except BridgeError as e:
            out.append({"error": str(e), "code": e.code})
        except Exception as e:  # Samba errors (NTSTATUS, RuntimeError) — no stack trace to the caller
            out.append({"error": "%s: %s" % (type(e).__name__, e), "code": "samba"})
    return {"items": out}


def set_folder_acl(args):
    fdpath = checked_folder(args)
    owner = sid_arg(args.get("owner_sid"), "owner")
    group = sid_arg(args.get("group_sid"), "group")
    entries = args.get("entries") or []
    if not isinstance(entries, list) or len(entries) > 200:
        raise BridgeError("invalid entries")
    s3param, smbd, security, system_session_unix = samba_modules()
    loadparm(s3param)

    sd = security.descriptor()
    sd.owner_sid = security.dom_sid(owner)
    sd.group_sid = security.dom_sid(group)
    sd.type = security.SEC_DESC_SELF_RELATIVE | security.SEC_DESC_DACL_PRESENT | security.SEC_DESC_DACL_PROTECTED
    inherit = security.SEC_ACE_FLAG_OBJECT_INHERIT | security.SEC_ACE_FLAG_CONTAINER_INHERIT

    def allow(sid, mask, flags=inherit):
        ace = security.ace()
        ace.type = security.SEC_ACE_TYPE_ACCESS_ALLOWED
        ace.trustee = security.dom_sid(sid)
        ace.access_mask = mask
        ace.flags = flags
        sd.dacl_add(ace)

    # administrators and SYSTEM always keep full control (never lock admins out)
    allow("S-1-5-32-544", FULL)
    allow("S-1-5-18", FULL)
    seen = set()
    for e in entries:
        if not isinstance(e, dict) or e.get("access") not in ("rw", "ro", "full"):
            raise BridgeError("invalid entry: %r" % (e,))
        sid = sid_arg(e.get("sid"), "entry")
        if sid in ("S-1-5-32-544", "S-1-5-18") or sid in seen:
            continue
        seen.add(sid)
        allow(sid, ACCESS[e["access"]])
    return store(fdpath, sd, args, smbd, security, system_session_unix)


def set_acl_sddl(args):
    """Put back a security descriptor given as SDDL (the agent's ACL history)."""
    fdpath = checked_folder(args)
    sddl = args.get("sddl")
    if not isinstance(sddl, str) or not sddl or len(sddl) > 64 << 10:
        raise BridgeError("invalid SDDL")
    s3param, smbd, security, system_session_unix = samba_modules()
    loadparm(s3param)
    try:
        # the history's SDDL comes from as_sddl() without a domain SID: no domain-relative aliases to resolve
        sd = security.descriptor.from_sddl(sddl, security.dom_sid("S-1-5-21-0-0-0"))
    except Exception:
        raise BridgeError("invalid SDDL")
    if sd.as_sddl() != sddl or sd.owner_sid is None or sd.group_sid is None or sd.dacl is None:
        raise BridgeError("the SDDL must round-trip and have an owner, a group and a DACL")
    sd.type |= security.SEC_DESC_SELF_RELATIVE | security.SEC_DESC_DACL_PRESENT
    return store(fdpath, sd, args, smbd, security, system_session_unix)


def store(fdpath, sd, args, smbd, security, system_session_unix):
    """Write sd on the folder, optionally pass it on to the contents, and return what Samba stored."""
    # protection goes in sd.type (SECINFO_PROTECTED_DACL = 0x80000000 overflows the binding's signed int)
    info = security.SECINFO_OWNER | security.SECINFO_GROUP | security.SECINFO_DACL
    svc = service_arg(args)
    smbd.set_nt_acl(fdpath, info, sd, system_session_unix(), service=svc)
    stored = smbd.get_nt_acl(fdpath, info, system_session_unix(), service=svc)
    out = describe(stored, security)
    if args.get("propagate") is True:
        out["propagated"], out["skipped"] = propagate(smbd, security, system_session_unix, args.get("reset_unstored") is True)
    return out


def reinherit_paths(args):
    """Reapply Windows inheritance to the given folder-relative paths and the dirs on the way to them, parents before
    children — the files a sync just wrote — instead of walking the whole tree. The folder's own ACL (the template) is
    left as it is; each target gets only the inherited ACEs (reset_unstored), so it ends as a full reinherit would."""
    checked_folder(args)  # validates the top descriptor and the roots
    rels = args.get("rels")
    if not isinstance(rels, list) or not rels:
        raise BridgeError("rels missing")
    only = set()
    for r in rels:
        if not isinstance(r, str) or not r or r.startswith("/") or "\0" in r or ".." in r.split("/"):
            raise BridgeError("invalid relative path")
        parts = os.path.normpath(r).split("/")
        if parts == ["."]:
            raise BridgeError("invalid relative path")
        for i in range(1, len(parts) + 1):  # the target and every ancestor dir
            only.add("/".join(parts[:i]))
    s3param, smbd, security, system_session_unix = samba_modules()
    loadparm(s3param)
    done, skipped = propagate(smbd, security, system_session_unix, reset_unstored=True, only=only)
    return {"propagated": done, "skipped": skipped}


def inherited(parent, child, is_dir, security, keep_explicit=True):
    """child's descriptor with its inherited ACEs recomputed from parent (as Explorer does); a protected child keeps
    its own. keep_explicit False drops the child's explicit ACEs (ones Samba synthesized from the mode bits of a
    file written without it, which no one set)."""
    from samba.ndr import ndr_pack, ndr_unpack
    if child.type & security.SEC_DESC_DACL_PROTECTED:
        return None
    CI, OI = security.SEC_ACE_FLAG_CONTAINER_INHERIT, security.SEC_ACE_FLAG_OBJECT_INHERIT
    NP, IO = security.SEC_ACE_FLAG_NO_PROPAGATE_INHERIT, security.SEC_ACE_FLAG_INHERIT_ONLY
    INH = security.SEC_ACE_FLAG_INHERITED_ACE
    # CREATOR OWNER / CREATOR GROUP stand for the child's own owner / group
    creators = {"S-1-3-0": child.owner_sid, "S-1-3-1": child.group_sid}
    sd = security.descriptor()
    sd.owner_sid, sd.group_sid = child.owner_sid, child.group_sid
    sd.type = security.SEC_DESC_SELF_RELATIVE | security.SEC_DESC_DACL_PRESENT | security.SEC_DESC_DACL_AUTO_INHERITED
    # an empty DACL (no access), never a null one (full access), when nothing is left
    sd.dacl = security.acl()
    sd.dacl.revision = security.SECURITY_ACL_REVISION_NT4

    keep = []  # dacl_add copies the ACE shallowly: its buffers (condition) must outlive the descriptor built here

    def add(src, trustee, flags):
        ace = ndr_unpack(security.ace, ndr_pack(src))  # a full copy (object, condition) with new trustee and flags
        ace.trustee, ace.flags = trustee, flags
        keep.append(ace)
        sd.dacl_add(ace)

    for ace in (child.dacl.aces if child.dacl and keep_explicit else []):
        if not ace.flags & INH:
            add(ace, ace.trustee, ace.flags)
    for ace in (parent.dacl.aces if parent.dacl else []):
        f = ace.flags
        creator = creators.get(str(ace.trustee))
        effective = creator or ace.trustee
        if not is_dir:
            if f & OI:
                add(ace, effective, INH)
        elif f & CI:
            if f & NP:
                add(ace, effective, INH)
            elif creator:  # the owner (group) gets it; the template goes on to the contents
                add(ace, effective, INH)
                add(ace, ace.trustee, (f & (CI | OI)) | IO | INH)
            else:
                add(ace, ace.trustee, (f & (CI | OI)) | INH)
        elif f & OI and not f & NP:  # only for the files further down
            add(ace, ace.trustee, OI | IO | INH)
    return ndr_unpack(security.descriptor, ndr_pack(sd))  # an independent copy while keep still holds the buffers


def has_stored_acl(fd):
    """Whether the file carries a Windows ACL of its own (security.NTACL), rather than one Samba synthesizes."""
    try:
        os.getxattr(fd, "security.NTACL")
        return True
    except OSError:
        return False


# what Samba reads an item's Windows ACL from, besides its owner, group and mode, in the order they are written: the
# POSIX ACLs (which rewrite the mode's group bits), the inheritance flags of `map acl inherit`, the NTACL last (its
# hash covers the owner, the mode and the POSIX ACLs)
ACL_XATTRS = ("system.posix_acl_access", "system.posix_acl_default", "user.SAMBA_PAI", "security.NTACL")
MEMO_VFS = {"acl_xattr", "dfs_samba4"}  # the global VFS stack propagate may copy Samba's result under
NOMEMO = object()  # a key whose copied state did not land: always set through Samba


def memo_safe():
    """Whether propagate may give an item the state Samba left on another: only when the global VFS stack keeps the
    ACL in acl_xattr's own xattr (another module, or another xattr name, would keep it where acl_state cannot see)."""
    from samba.samba3 import param as s3param
    lp = s3param.get_context()
    v = lp.get("vfs objects") or []
    mods = v if isinstance(v, list) else str(v).split()
    return "acl_xattr" in mods and set(mods) <= MEMO_VFS and lp.get("acl_xattr:security_acl_name") is None


def acl_state(fd):
    """An item's ACL state: owner, group, full mode and ACL_XATTRS (None: absent); None when one cannot be read."""
    st = os.fstat(fd)
    xs = []
    for name in ACL_XATTRS:
        try:
            xs.append(os.getxattr(fd, name))
        except OSError as e:
            if e.errno != errno.ENODATA:
                return None
            xs.append(None)
    return (st.st_uid, st.st_gid, st.st_mode, tuple(xs))


def put_state(fd, state):
    """Give an item an ACL state: the owner only when it differs (a chown clears setuid/setgid), the mode only when it
    differs, then ACL_XATTRS in order, removing those the state lacks."""
    uid, gid, mode, xs = state
    st = os.fstat(fd)
    if (st.st_uid, st.st_gid) != (uid, gid):
        os.fchown(fd, uid, gid)
    if stat.S_IMODE(os.fstat(fd).st_mode) != stat.S_IMODE(mode):
        os.fchmod(fd, stat.S_IMODE(mode))
    for name, value in zip(ACL_XATTRS, xs):
        if value is not None:
            os.setxattr(fd, name, value)
            continue
        try:
            os.removexattr(fd, name)
        except OSError as e:
            if e.errno != errno.ENODATA:
                raise


def propagate(smbd, security, system_session_unix, reset_unstored=False, only=None):
    """Recompute the inherited ACEs of everything under the folder (the request's first), parents before children. Each folder's
    children follow the ACL read from the very descriptor being walked (a folder swapped meanwhile passes on its own).
    Symbolic links, special files and other file systems are skipped; every entry counts toward MAX_ITEMS.
    reset_unstored: an entry with no stored Windows ACL (written without Samba: a sync) gets only the inherited
    ACEs. only: a set of folder-relative paths (each with its ancestor dirs) — when given, the walk descends only
    into those dirs and applies only to those entries (the delta of a sync, not the whole tree).
    set_nt_acl is slow on a DC (every SID goes through idmap) while what it leaves depends only on the descriptor set
    and the item's acl_state: every item is still read and its descriptor computed, but only the first of each
    (descriptor, state) goes through set_nt_acl; the next ones get the state Samba left on it (put_state), checked
    byte for byte, or go through Samba too when it does not land."""
    info = security.SECINFO_OWNER | security.SECINFO_GROUP | security.SECINFO_DACL
    session = system_session_unix()
    top = fd_at(0)
    dev = os.fstat(top).st_dev
    done = skipped = 0
    memo = {} if memo_safe() else None  # (descriptor SDDL, acl_state before) → acl_state Samba left, or NOMEMO
    for _dirpath, dirnames, filenames, dirfd in os.fwalk(".", dir_fd=top, follow_symlinks=False):
        if only is not None:  # descend only into the dirs that lead to a target (its ancestors are in the set)
            dirnames[:] = [d for d in dirnames if os.path.normpath(os.path.join(_dirpath, d)) in only]
        parent = smbd.get_nt_acl("/proc/self/fd/%d" % dirfd, info, session)
        for name, is_dir in [(n, True) for n in list(dirnames)] + [(n, False) for n in filenames]:
            if only is not None and os.path.normpath(os.path.join(_dirpath, name)) not in only:
                continue
            if done + skipped >= MAX_ITEMS:
                raise BridgeError("more than %d entries under the folder" % MAX_ITEMS, "samba")
            flags = os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC | (os.O_DIRECTORY if is_dir else 0)
            try:
                fd = os.open(name, flags, dir_fd=dirfd)
            except OSError:  # a link, or gone meanwhile
                skipped += 1
                if is_dir:
                    dirnames.remove(name)
                continue
            try:
                st = os.fstat(fd)
                if st.st_dev != dev or not (stat.S_ISDIR(st.st_mode) if is_dir else stat.S_ISREG(st.st_mode)):
                    skipped += 1
                    if is_dir:
                        dirnames.remove(name)
                    continue
                path = "/proc/self/fd/%d" % fd
                cur = smbd.get_nt_acl(path, info, session)
                new = inherited(parent, cur, is_dir, security, keep_explicit=not reset_unstored or has_stored_acl(fd))
                if new is None or new.as_sddl() == cur.as_sddl():
                    done += 1
                    continue
                pre = acl_state(fd) if memo is not None else None
                key = (new.as_sddl(), pre) if pre is not None else None
                got = memo.get(key) if key is not None else None
                if got is not None and got is not NOMEMO:
                    try:
                        put_state(fd, got)
                        if acl_state(fd) == got:
                            done += 1
                            continue
                    except OSError:
                        pass
                    memo[key] = got = NOMEMO  # did not land as Samba's result: this item and the next through Samba
                smbd.set_nt_acl(path, info, new, session)
                if key is not None and got is None:
                    post = acl_state(fd)
                    if post is not None:
                        memo[key] = post
                done += 1
            finally:
                os.close(fd)
    return done, skipped


# share parameters the agent may write (anything else is refused)
SHARE_PARAMS = {
    "path", "comment", "guest ok", "read only", "browseable", "valid users", "write list", "vfs objects",
    "access based share enum", "hide unreadable",
    "recycle:repository", "recycle:keeptree", "recycle:versions", "recycle:touch", "recycle:exclude",
    "recycle:exclude_dir", "recycle:directory_mode", "recycle:subdir_mode",
    "full_audit:prefix", "full_audit:success", "full_audit:failure", "full_audit:facility", "full_audit:priority",
}
SHARE_VFS = ("full_audit", "recycle")  # modules a share may add in front of the global ones


def share_name(v):
    if not isinstance(v, str) or not re.match(r"^[A-Za-z0-9][A-Za-z0-9 ._-]{0,78}\$?$", v):
        raise BridgeError("invalid share name")
    if v.lower() in ("global", "homes", "printers", "print$", "ipc$", "admin$", "sysvol", "netlogon"):
        raise BridgeError("%s is reserved" % v)
    return v


def registry():
    from samba.samba3 import param as s3param
    from samba.samba3 import smbconf as s3conf
    lp = loadparm(s3param)
    return s3conf.init_reg(None), lp


def list_shares(_args):
    conf, _lp = registry()
    out = []
    for name in conf.share_names():
        if name.lower() == "global":
            continue
        n, params = conf.get_share(name)
        out.append({"name": n, "params": [[k, v] for k, v in params]})
    return out


def set_share(args):
    """Create or replace a share in one registry transaction: the share never exists half-written."""
    name = share_name(args.get("name"))
    params = args.get("params")
    if not isinstance(params, list) or not params or len(params) > 64:
        raise BridgeError("invalid parameters")
    clean = []
    for kv in params:
        if not isinstance(kv, list) or len(kv) != 2 or not all(isinstance(x, str) for x in kv):
            raise BridgeError("invalid parameter: %r" % (kv,))
        k, v = kv
        if k not in SHARE_PARAMS or k == "vfs objects":
            raise BridgeError("parameter not allowed: %s" % k)
        if any(c in v for c in "\n\r\0") or len(v) > 8192:
            raise BridgeError("invalid value for %s" % k)
        clean.append((k, v))
    vfs = args.get("vfs") or []
    if not isinstance(vfs, list) or any(m not in SHARE_VFS for m in vfs):
        raise BridgeError("invalid vfs modules")
    conf, lp = registry()
    if vfs:  # a share's "vfs objects" replaces the global one: keep the global modules (acl_xattr) after these
        glob = lp.get("vfs objects") or []
        glob = glob if isinstance(glob, list) else str(glob).split()
        # full_audit first (requested or global): recycle turns a deletion into a rename for the modules after it
        mods = [m for m in SHARE_VFS if m in vfs or m in glob]
        clean.append(("vfs objects", " ".join(mods + [m for m in glob if m not in mods])))
    existing = [n for n in conf.share_names() if n.lower() == name.lower()]
    conf.transaction_start()
    try:
        for n in existing:
            conf.delete_share(n)
        conf.create_set_share(name, clean)
        conf.transaction_commit()
    except Exception:
        conf.transaction_cancel()
        raise
    n, params = conf.get_share(name)
    return {"name": n, "params": [[k, v] for k, v in params]}


def delete_share(args):
    name = share_name(args.get("name"))
    conf, _lp = registry()
    for n in conf.share_names():
        if n.lower() == name.lower():
            conf.delete_share(n)
    return {}


OPS = {"capabilities": capabilities, "get_acl": get_acl, "get_acls": get_acls, "set_folder_acl": set_folder_acl, "set_acl_sddl": set_acl_sddl,
       "reinherit_paths": reinherit_paths, "list_shares": list_shares, "set_share": set_share, "delete_share": delete_share}


def handle(body):
    """One request: its JSON in, the response object out."""
    try:
        req = json.loads(body)
        if not isinstance(req, dict) or set(req) - {"op", "args"} or req.get("op") not in OPS or not isinstance(req.get("args", {}), dict):
            raise BridgeError("unknown operation")
        return {"ok": True, "result": OPS[req["op"]](req.get("args") or {})}
    except BridgeError as e:
        return {"ok": False, "error": str(e), "code": e.code}
    except Exception as e:  # Samba errors (NTSTATUS, RuntimeError) — no stack trace to the caller
        return {"ok": False, "error": "%s: %s" % (type(e).__name__, e), "code": "samba"}


def recv_exact(sock, n, fds):
    """n bytes from sock, collecting the SCM_RIGHTS descriptors that come along into fds; None at a clean end."""
    import array
    import socket
    buf = b""
    while len(buf) < n:
        data, anc, _flags, _addr = sock.recvmsg(n - len(buf), socket.CMSG_SPACE(MAX_BATCH * 4))
        for level, kind, payload in anc:
            if level == socket.SOL_SOCKET and kind == socket.SCM_RIGHTS:
                got = array.array("i")
                got.frombytes(payload[: len(payload) - (len(payload) % got.itemsize)])
                fds.extend(got)
        if not data:
            if buf or fds:
                raise EOFError("request cut short")
            return None
        buf += data
    return buf


def main():
    import socket
    import struct
    global REQ_FDS
    sock = socket.socket(fileno=SOCK_FD)
    while True:
        fds = []
        try:
            head = recv_exact(sock, 4, fds)
            if head is None:  # the agent closed the socket: done
                return
            n = struct.unpack("<I", head)[0]
            if n > MAX_INPUT:
                return
            body = recv_exact(sock, n, fds)
            if body is None:
                return
            REQ_FDS = fds
            out = json.dumps(handle(body)).encode()
        finally:
            REQ_FDS = []
            for fd in fds:  # the request's folders are closed whatever happened
                try:
                    os.close(fd)
                except OSError:
                    pass
        sock.sendall(struct.pack("<I", len(out)) + out)


main()
