#!/usr/bin/env python3
"""Install Samba 4 (distro packages, Ubuntu 26.04) ready for ad-manager — docs/samba-install.md.

Roles:
  packages   install the packages and stop the member daemons (idempotent)
  new-dc     new domain                     --realm --domain --ip [--panel [--files --share-root] --deb]
  dc-join    additional DC of a domain      --realm --domain --ip --src-dc [--panel [--files --share-root] --deb]
  restore-dc  the DC from a backup of it, on a new machine  --realm --ip --backup dc.tar.bz2 [--panel [--files]]
  member     file server (domain member)    --realm --domain --dns-dc [--agent --deb]
  panel      ad-manager panel on this DC    --realm [--deb]
  connect    panel of an existing AD        --realm --url ldaps://dc [--ca-file ca.pem | --insecure]
  agent      ad-manager agent on a member   --realm --dns-dc [--deb]
  verify     check the installed role
  diagnose   what is installed and running (JSON on stdout; the panel's setup wizard reads it)

The old Portuguese names (pacotes, dc-novo, dc-restaurar, membro, painel, conectar, agente, verificar,
diagnostico, --painel, --agente, --arquivos, --novo-token) are still accepted.

The domain admin password is read from the terminal, from AD_PASSWORD_FILE (a 0600
file) or from the first line of stdin — never from an argument.
Only the standard library is used (python3 ships with Ubuntu).
"""

import argparse
import getpass
import ipaddress
import json
import os
import secrets
import re
import shutil
import socket
import stat
import tarfile
import subprocess
import sys
import time
from pathlib import Path

OPT = Path("/opt/ad-manager")
CONFIG = OPT / "config.toml"
IMPORT_PASS = OPT / "import.pass"  # the former --import-ready password file (no longer written; see import_gates)
SAM_LDB = Path("/var/lib/samba/private/sam.ldb")
SAMBA_CA = Path("/var/lib/samba/private/tls/ca.pem")
PACKAGES = ("samba samba-ad-dc samba-ad-provision winbind libnss-winbind libpam-winbind krb5-user smbclient "
            "ldb-tools tdb-tools acl attr chrony dnsutils python3-setproctitle").split()  # tdb-tools: DC backup

DRY = False
# SETUP_JSON=1 (the panel's setup wizard, through the agent): steps and errors as JSON lines, the commands' own
# output as plain lines in between
JSON_LOG = os.environ.get("SETUP_JSON") == "1"


def log(msg):
    if JSON_LOG:
        print(json.dumps({"step": msg}, ensure_ascii=False), flush=True)
        return
    print(f"\033[1;34m==>\033[0m {msg}", flush=True)


def fail(msg):
    if JSON_LOG:
        print(json.dumps({"error": msg}, ensure_ascii=False), flush=True)
        sys.exit(1)
    print(f"\033[1;31mERROR:\033[0m {msg}", file=sys.stderr)
    sys.exit(1)


def provisioned_realm():
    """The realm of the Samba DC already provisioned here (lowercase), or "" when there is none."""
    if not SAM_LDB.exists():
        return ""
    try:
        m = re.search(r"^\s*realm\s*=\s*(\S+)", smb_conf().read_text(), re.I | re.M)
    except OSError:
        return ""
    return m.group(1).lower() if m else ""


def set_admin_password(admin, pw):
    """Set the administrator's password through Samba's Python API on the local database (never an argument)."""
    if DRY:
        print(f"[dry-run] password of {admin} through the Samba API")
        return
    from samba.auth import system_session
    from samba.param import LoadParm
    from samba.samdb import SamDB
    lp = LoadParm()
    lp.load_default()
    db = SamDB(url=str(SAM_LDB), session_info=system_session(), lp=lp)
    db.setpassword(f"(sAMAccountName={admin})", pw, force_change_at_next_login=False, username=admin)


def sh(*cmd, check=True, capture=False, env=None, quiet=False, secret=None):
    """Run cmd (no shell). In --dry-run only print it; secret is masked in the echo."""
    shown = " ".join(cmd).replace(secret, "***") if secret else " ".join(cmd)
    if DRY:
        print(f"[dry-run] {shown}")
        return ""
    full_env = {**os.environ, **(env or {})}
    out = subprocess.DEVNULL if quiet else None
    r = subprocess.run(cmd, check=False, text=True, env=full_env,
                       stdout=subprocess.PIPE if capture else out, stderr=subprocess.STDOUT if capture else out)
    if check and r.returncode != 0:
        fail(f"failed ({r.returncode}): {shown}" + (f"\n{r.stdout}" if capture and r.stdout else ""))
    return r.stdout or ""


def write(path, text, mode=0o644):
    if DRY:
        print(f"[dry-run] write {path}")
        return
    Path(path).write_text(text)
    os.chmod(path, mode)


def read_password(args):
    if DRY:
        return "dry-run"  # nothing is sent to the domain
    f = os.environ.get("AD_PASSWORD_FILE")
    if f:
        if stat.S_IMODE(os.stat(f).st_mode) != 0o600:
            fail(f"{f} must be 0600")
        pw = Path(f).read_text().splitlines()[0]
    elif sys.stdin.isatty():
        pw = getpass.getpass(f"Password of {args.domain}\\{args.admin}: ")
    else:
        pw = sys.stdin.readline().rstrip("\n")
    if not pw:
        fail("empty password")
    return pw


def need(args, *names):
    for n in names:
        if not getattr(args, n.replace("-", "_")):
            fail(f"missing --{n}")


def port_open(host, port):
    try:
        with socket.create_connection((host, port), timeout=3):
            return True
    except OSError:
        return False


# ── Samba ────────────────────────────────────────────────────────────────────

def packages(args):
    log("distro packages")
    realm = (args.realm or "").upper()
    if realm and DRY:
        print(f"[dry-run] debconf: krb5-config/default_realm = {realm}")
    elif realm:
        subprocess.run(["debconf-set-selections"], input=f"krb5-config krb5-config/default_realm string {realm}\n",
                       text=True, check=True)
    sh("apt-get", "update", "-qq")
    sh("apt-get", "install", "-y", "-qq", *PACKAGES, quiet=True)
    sh("systemctl", "disable", "--now", "smbd", "nmbd", "winbind", "samba-ad-dc", check=False, quiet=True)


def smb_conf():
    return Path(os.path.realpath("/etc/samba/smb.conf"))


def smbconf_global(line):
    """Set a parameter in the [global] section of smb.conf (idempotent): an earlier value of the same parameter goes,
    so the new one is the one in effect."""
    if DRY:
        print(f"[dry-run] smb.conf [global]: {line}")
        return
    p = smb_conf()
    text = p.read_text()
    if f"   {line}\n" in text:
        return
    key = line.split("=", 1)[0].strip()
    text = re.sub(rf"(?mi)^[ \t]*{re.escape(key)}[ \t]*=.*\n", "", text)
    p.write_text(text.replace("[global]\n", f"[global]\n   {line}\n", 1))


def auto_interfaces(args):
    """A DC with more than one network card (a VM with NAT, a second LAN) would register every address in the
    domain's DNS, and the other DCs would replicate through one they cannot reach: Samba stays on the card that has
    --ip (interfaces = lo <card>, bind interfaces only), unless --interfaces says otherwise."""
    if args.interfaces or not args.ip or DRY:
        return
    out = sh("ip", "-o", "addr", "show", "scope", "global", capture=True, check=False)
    cards = {line.split()[1] for line in out.splitlines() if len(line.split()) > 1}
    mine = next((line.split()[1] for line in out.splitlines() if f" {args.ip}/" in line), None)
    if mine and len(cards) > 1:
        args.interfaces = f"lo {mine}"
        log(f"more than one network card: Samba stays on {mine} ({args.ip}) only")


def hosts_dc(args):
    """This DC's names resolve to --ip, as Samba asks of a DC: a 127.x line that names this host (Vagrant and some
    installers write 127.0.1.1 or 127.0.2.1 <host>) loses the name, and one line <ip> <fqdn> <host> is set. With
    them on a loopback address, the other DCs and the DRS tools reach a port this DC does not listen on."""
    if not args.ip or not args.realm:
        return
    short, full = socket.gethostname().split(".")[0].lower(), fqdn(args.realm)
    if DRY:
        print(f"[dry-run] /etc/hosts: {args.ip} {full} {short}")
        return
    p = Path("/etc/hosts")
    text = p.read_text()
    out = []
    for line in text.splitlines():
        body, _, comment = line.partition("#")
        f = body.split()
        if f and f[0] == args.ip:
            continue  # set again below
        if f and f[0].startswith("127."):
            names = [n for n in f[1:] if n.lower() not in (short, full)]
            if names != f[1:]:
                if not names:
                    continue
                line = " ".join([f[0], *names]) + (f" #{comment}" if comment else "")
        out.append(line)
    out.append(f"{args.ip} {full} {short}")
    new = "\n".join(out) + "\n"
    if new != text:
        backup = Path("/etc/hosts.before-samba")
        if not backup.exists():
            shutil.copy2(p, backup)
        p.write_text(new)
        log(f"/etc/hosts: {full} and {short} → {args.ip}")


def iface_options(args):
    """The --option arguments of samba-tool for the cards chosen (provision, join, restore register DNS with them)."""
    if not args.interfaces:
        return []
    return [f"--option=interfaces = {args.interfaces}", "--option=bind interfaces only = yes"]


def legacy_windows(args):
    """Exceptions for old Windows clients and multi-NIC DCs (docs/samba-install.md)."""
    if args.interfaces:
        smbconf_global(f"interfaces = {args.interfaces}")
        smbconf_global("bind interfaces only = yes")
    for m in args.legacy_schannel.split():
        smbconf_global(f"server reject md5 schannel:{m} = no")
        smbconf_global(f"allow nt4 crypto:{m} = yes")
    if args.xp_smb1:
        smbconf_global("server min protocol = NT1")


def resolv(nameserver, realm):
    """Point the resolver at nameserver (an address); a resolved symlink is replaced by a file."""
    try:
        ipaddress.ip_address(nameserver)
    except ValueError:
        fail(f"{nameserver}: give the DC's IP (it becomes this host's DNS)")
    if DRY:
        print(f"[dry-run] /etc/resolv.conf → {nameserver}")
        return
    p = Path("/etc/resolv.conf")
    if p.is_symlink():
        p.unlink()
    p.write_text(f"nameserver {nameserver}\nsearch {realm}\n")


def chrony_dc():
    conf = Path("/etc/chrony/chrony.conf")
    if DRY:
        print("[dry-run] chrony with ntpsigndsocket")
        return
    sh("install", "-d", "-m", "0750", "-o", "root", "-g", "_chrony", "/var/lib/samba/ntp_signd", check=False)
    if "ntpsigndsocket" not in conf.read_text():
        with conf.open("a") as f:
            f.write("# Samba AD: sign NTP replies for domain members\nbindcmdaddress 0.0.0.0\nallow 0.0.0.0/0\n"
                    "ntpsigndsocket /var/lib/samba/ntp_signd\n")
    sh("systemctl", "restart", "chrony")


def set_dc_os():
    """Fill operatingSystem on this DC's account when empty (the panel shows the OS)."""
    if DRY:
        return
    name = socket.gethostname().split(".")[0].upper()
    out = sh("ldbsearch", "-H", str(SAM_LDB), f"(sAMAccountName={name}$)", "dn", "operatingSystem", capture=True, check=False)
    dn = re.search(r"^dn: (.+)$", out, re.M)
    if not dn or "\noperatingSystem:" in out:
        return
    ver = sh("samba", "--version", capture=True).split()[-1].split("-")[0]
    ldif = (f"dn: {dn.group(1)}\nchangetype: modify\nreplace: operatingSystem\noperatingSystem: Samba\n-\n"
            f"replace: operatingSystemVersion\noperatingSystemVersion: {ver}\n")
    subprocess.run(["ldbmodify", "-H", str(SAM_LDB)], input=ldif, text=True, stdout=subprocess.DEVNULL, check=False)


def start_dc(args):
    legacy_windows(args)
    sh("cp", "/var/lib/samba/private/krb5.conf", "/etc/krb5.conf")
    sh("systemctl", "disable", "--now", "systemd-resolved", check=False, quiet=True)  # the DC serves port 53
    resolv("127.0.0.1", args.realm)
    sh("systemctl", "unmask", "samba-ad-dc", quiet=True)
    sh("systemctl", "enable", "--now", "samba-ad-dc", quiet=True)
    chrony_dc()
    if DRY:
        return
    srv = f"_ldap._tcp.{args.realm}"
    for _ in range(30):
        if subprocess.run(["host", "-t", "SRV", srv, "127.0.0.1"], capture_output=True).returncode == 0:
            break
        time.sleep(2)
    else:
        fail("Samba's DNS did not answer within 60 s (journalctl -u samba-ad-dc)")
    set_dc_os()


def dc_new(args):
    need(args, "realm", "domain", "ip")
    have = provisioned_realm()
    if have and have != args.realm.lower():
        fail(f"this host is already a DC of another domain ({have})")
    if SAM_LDB.exists() and not have:
        fail("there is a Samba database on this host with no realm in smb.conf: check it before going on")
    packages(args)
    auto_interfaces(args)
    hosts_dc(args)
    pw = read_password(args)
    if args.import_ready:
        args.admin_pw = pw  # for import_gates: stdin is read once
    if have:
        log(f"{args.realm} is already provisioned here: going on to the service")
        # a run that stopped after the provisioning left the temporary password: the chosen one is set again
        log("administrator password")
        set_admin_password(args.admin, pw)
    else:
        log(f"provisioning {args.realm} ({args.domain})")
        if not DRY:
            Path("/etc/samba/smb.conf").unlink(missing_ok=True)
        # samba-tool provision only takes the password as an argument: a random one goes there (it changes right
        # after), and the chosen one is set through Samba's API, never visible in ps
        tmp = secrets.token_urlsafe(18) + "aA1!"
        sh("samba-tool", "domain", "provision", f"--realm={args.realm.upper()}", f"--domain={args.domain}", "--server-role=dc",
           "--dns-backend=SAMBA_INTERNAL", "--use-rfc2307", f"--host-ip={args.ip}", f"--adminpass={tmp}",
           f"--option=dns forwarder = {args.forwarder}", *iface_options(args), secret=tmp, quiet=True)
        log("administrator password")
        set_admin_password(args.admin, pw)
    start_dc(args)


def backup_realm(path):
    """Check a DC backup (samba-tool domain backup): a tar with the database and smb.conf, no absolute path nor ..
    nor link leaving it; returns its realm and NetBIOS domain from that smb.conf."""
    try:
        tf = tarfile.open(path)
    except (OSError, tarfile.TarError) as e:
        fail(f"{path}: not a DC backup ({e})")
    with tf:
        names, conf = set(), None
        for m in tf.getmembers():
            n = m.name
            while n.startswith("./"):  # only a literal ./ prefix: ../ is never trimmed away
                n = n[2:]
            if m.name.startswith("/") or ".." in m.name.split("/") or m.isdev() or (
                    (m.issym() or m.islnk()) and (m.linkname.startswith("/") or ".." in m.linkname.split("/"))):
                fail(f"{path}: path not allowed in the backup ({m.name})")
            names.add(n)
            if n == "etc/smb.conf" and m.isfile():
                conf = tf.extractfile(m).read().decode("utf-8", "replace")
    if conf is None or "private/sam.ldb" not in names:
        fail(f"{path}: not a DC backup (etc/smb.conf or private/sam.ldb missing)")
    get = lambda k: (re.search(rf"^\s*{k}\s*=\s*(\S+)", conf, re.I | re.M) or [None, ""])[1]
    return get("realm").lower(), get("workgroup").upper()


def dc_restore(args):
    """A DC from a backup of the panel (samba-tool domain backup offline) on a new machine: the domain comes back
    with this host as its only DC (samba-tool domain backup restore --newservername), in the usual places."""
    need(args, "realm", "ip", "backup")
    if SAM_LDB.exists():
        fail("this host already has a Samba database: restore the DC on a new machine")
    backup = os.path.realpath(args.backup)
    if not os.path.isfile(backup):
        fail(f"backup not found: {args.backup}")
    if any(backup == d or backup.startswith(d + "/") for d in ("/var/lib/samba", "/etc/samba")):
        fail(f"{args.backup}: copy the backup outside /var/lib/samba and /etc/samba (those folders are replaced)")
    realm, domain = backup_realm(backup)
    if realm != args.realm.lower():
        fail(f"the backup is of the domain {realm or '?'}, not of {args.realm}")
    args.domain = args.domain or domain
    packages(args)
    auto_interfaces(args)
    hosts_dc(args)
    host = socket.gethostname().split(".")[0].upper()
    log(f"restoring {realm} ({domain}) as {host}")
    lib = Path("/var/lib/samba")
    if lib.exists() and any(lib.iterdir()):  # the package's empty folders: samba-tool wants an empty target
        aside = f"/var/lib/samba.before-restore-{time.strftime('%Y%m%d%H%M%S')}"
        sh("mv", str(lib), aside)
        log(f"the package's {lib} kept in {aside}")
    sh("samba-tool", "domain", "backup", "restore", f"--backup-file={backup}", f"--targetdir={lib}",
       f"--newservername={host}", f"--host-ip={args.ip}", *iface_options(args), quiet=True)
    if not DRY:
        conf = Path("/etc/samba/smb.conf")
        if conf.exists():
            shutil.copy2(conf, f"{conf}.before-restore")
        shutil.copy2(lib / "etc/smb.conf", conf)
        # Samba's autogenerated certificate came with the backup, made for the old DC's name: set aside, so Samba
        # makes one for this server (LDAPS for the panel); a certificate of the company (tls certfile) stays
        tls = lib / "private/tls"
        cert = tls / "cert.pem"
        if cert.exists() and "temporary autogenerated" in sh("openssl", "x509", "-in", str(cert), "-noout", "-subject",
                                                             capture=True, check=False):
            aside = lib / f"private/tls.before-restore-{time.strftime('%Y%m%d%H%M%S')}"
            sh("mv", str(tls), str(aside))
            sh("install", "-d", "-m", "0755", str(tls))
            log(f"the old DC's autogenerated certificate kept in {aside}")
    start_dc(args)


# the domain functional levels (msDS-Behavior-Version) a Samba DC can join with, as "ad dc functional level" names
# them; a domain below 2008 R2 (2003, 2008) is joined with 2008_R2
DC_LEVELS = {"2008_R2": 4, "2012": 5, "2012_R2": 6, "2016": 7}


def join_level(args):
    """The capability this DC joins with: the source domain's functional level (its rootDSE, read without a bind), or
    --dc-level, refused below the domain's level before anything changes."""
    out = sh("ldbsearch", "-H", f"ldap://{args.src_dc}", "-s", "base", "-b", "", "domainFunctionality",
             "forestFunctionality", capture=True, check=False)
    m = re.search(r"^domainFunctionality: (\d+)", out, re.M)
    if not m:
        if DRY:
            return args.dc_level or "2008_R2"
        fail(f"could not read the domain functional level on {args.src_dc} (rootDSE)")
    dom = int(m.group(1))
    if dom > max(DC_LEVELS.values()):
        fail(f"the domain is at a functional level ({dom}) above what Samba can join (2016)")
    want = next(name for name, v in sorted(DC_LEVELS.items(), key=lambda kv: kv[1]) if v >= max(dom, 4))
    if args.dc_level:
        if args.dc_level not in DC_LEVELS:
            fail(f"--dc-level: {', '.join(DC_LEVELS)}")
        if DC_LEVELS[args.dc_level] < dom:
            fail(f"--dc-level {args.dc_level} below the domain's level ({want}): the domain would refuse the join")
        want = args.dc_level
    log(f"domain functional level: {dom}; this DC joins with {want}")
    return want


def dc_join(args):
    need(args, "realm", "domain", "ip", "src-dc")
    have = provisioned_realm()
    if have == args.realm.lower():
        log(f"this host is already a DC of {args.realm}: going on to the service")
        auto_interfaces(args)
        hosts_dc(args)
        start_dc(args)
        return
    if SAM_LDB.exists():
        fail("there is already a Samba database on this host (of another domain)")
    if f" {args.ip}/" not in sh("ip", "-4", "addr", capture=True) and not DRY:
        fail(f"{args.ip} is not configured on this host")
    for p in (53, 88, 135, 389, 445, 636):
        if not port_open(args.src_dc, p):
            fail(f"port {p} closed on {args.src_dc}")
    if subprocess.run(["host", "-t", "SRV", f"_ldap._tcp.{args.realm}", args.src_dc], capture_output=True).returncode:
        fail(f"{args.src_dc} does not resolve the domain's SRV record")
    packages(args)
    level = join_level(args)  # before anything changes in the domain
    if not DRY and "Leap status     : Normal" not in sh("chronyc", "tracking", capture=True, check=False):
        fail("clock not synchronized (Kerberos requires < 5 min)")
    pw = read_password(args)
    auto_interfaces(args)
    hosts_dc(args)
    if DRY:
        log("--dry-run: prerequisites ok, nothing was changed in the domain")
        return
    resolv(args.src_dc, args.realm)
    Path("/etc/samba/smb.conf").unlink(missing_ok=True)
    log(f"joining as an additional DC of {args.realm}")
    sh("samba-tool", "domain", "join", args.realm, "DC", "-U", f"{args.domain}\\{args.admin}", f"--server={args.src_dc}",
       "--dns-backend=SAMBA_INTERNAL", f"--option=dns forwarder = {args.forwarder}",
       f"--option=ad dc functional level = {level}", *iface_options(args), env={"PASSWD": pw})
    start_dc(args)
    sh("samba-tool", "drs", "showrepl", "--summary", check=False)
    log("next: docs/migration-runbook.md step 4 (SYSVOL)")


MEMBER_CONF = """[global]
   workgroup = {domain}
   realm = {realm}
   security = ADS
   kerberos method = secrets and keytab
   winbind use default domain = no
   winbind refresh tickets = yes
   idmap config * : backend = tdb
   idmap config * : range = 3000-7999
   idmap config {domain} : backend = rid
   idmap config {domain} : range = 10000-999999
   template shell = /bin/bash
   vfs objects = acl_xattr
   map acl inherit = yes
   store dos attributes = yes
   server min protocol = SMB2_10
   # shares live in the registry (net conf): created by the ad-manager agent
   registry shares = yes
   include = registry
"""


def member(args):
    need(args, "realm", "domain", "dns-dc")
    packages(args)
    pw = read_password(args)
    log(f"file server, member of {args.realm}")
    write("/etc/samba/smb.conf", MEMBER_CONF.format(domain=args.domain, realm=args.realm.upper()))
    sh("install", "-d", "-m", "0755", args.share_root)
    resolv(args.dns_dc, args.realm)
    if not DRY:
        ns = Path("/etc/nsswitch.conf")
        ns.write_text(re.sub(r"^(passwd|group):.*$", r"\1:         files systemd winbind", ns.read_text(), flags=re.M))
    sh("net", "ads", "join", "-U", f"{args.domain}\\{args.admin}", env={"PASSWD": pw})
    sh("systemctl", "unmask", "smbd", "winbind", quiet=True)
    sh("systemctl", "enable", "--now", "winbind", "smbd", quiet=True)
    # Domain Admins can edit NTFS permissions from Windows Explorer (acl_xattr). Right after the join winbind may not
    # reach the DC yet (NT_STATUS_NO_LOGON_SERVERS): wait for its trust check, then try the grant a few times
    if not DRY:
        for _ in range(30):
            if subprocess.run(["wbinfo", "-t"], capture_output=True).returncode == 0:
                break
            time.sleep(2)
        else:
            fail("winbind did not confirm the trust with the DC within 60 s (wbinfo -t; journalctl -u winbind)")
    grant = ["net", "rpc", "rights", "grant", f"{args.domain}\\Domain Admins", "SeDiskOperatorPrivilege",
             "-U", f"{args.domain}\\{args.admin}", "-S", "localhost"]
    if DRY:
        sh(*grant)
    for attempt in range(5 if not DRY else 0):
        r = subprocess.run(grant, env={**os.environ, "PASSWD": pw}, capture_output=True, text=True)
        if r.returncode == 0:
            break
        if attempt == 4:
            fail(f"failed ({r.returncode}): {' '.join(grant)}\n{r.stdout}{r.stderr}")
        time.sleep(3)


# ── ad-manager ───────────────────────────────────────────────────────────────

def install_ad_manager(args, extra):
    """Install the .deb (when given or not installed yet) plus extra packages."""
    pkgs = list(extra)
    if args.deb:
        deb = Path(args.deb).resolve()
        if not deb.is_file() and not DRY:
            fail(f"package not found: {deb}")
        pkgs.append(str(deb))
    elif not (OPT / "ad-manager").exists() and not DRY:
        fail("ad-manager is not installed: pass --deb ad-manager_X.Y.Z_amd64.deb (Releases page)")
    log("ad-manager and dependencies")
    sh("apt-get", "install", "-y", "-qq", "--no-install-recommends", "-o", "Dpkg::Options::=--force-confold", *pkgs, quiet=True)
    sh("systemctl", "enable", "--now", "redis-server", quiet=True)


def write_config(text):
    """Write /opt/ad-manager/config.toml (root:ad-manager 0640), keeping a backup."""
    if CONFIG.exists() and not DRY and CONFIG.read_text() != text:
        shutil.copy2(CONFIG, CONFIG.with_suffix(".toml.bak"))
        print(f"previous config saved in {CONFIG}.bak", flush=True)  # a note, not a step of the wizard's progress
    write(CONFIG, text, 0o640)
    sh("chown", "root:ad-manager", str(CONFIG))


def fqdn(realm):
    return f"{socket.gethostname().split('.')[0].lower()}.{realm.lower()}"


PANEL_CONF = """# generated by scripts/samba-setup.py (panel) — see config.toml.example
[ad]
url = "ldaps://{fqdn}"
domain = "{realm}"
ca_file = "/opt/ad-manager/tls/samba-ca.pem"   # CA of the Samba certificate (copy of {samba_ca})

[server]
listen = "0.0.0.0:{port}"
cert_file = "/opt/ad-manager/tls/tls.crt"
key_file = "/opt/ad-manager/tls/tls.key"
session_minutes = 30
audit_log = "/opt/ad-manager/logs/audit.log"

[db]
path = "/opt/ad-manager/data/ad-manager.db"

[redis]
addr = "127.0.0.1:6379"   # /asynqmon (agent queues on this Redis)

[samba]
log_dir = "/var/log/samba"
"""


def panel(args, start=True):
    """ad-manager panel on this DC: LDAPS with the Samba CA, Kerberos (password links) is local. start=False leaves
    the (re)start to panel_start, after the DC's other steps: the wizard reads the panel leaving setup mode as the end."""
    need(args, "realm")
    if not SAM_LDB.exists() and not DRY:
        fail("this host is not a Samba DC (run new-dc or dc-join first)")
    install_ad_manager(args, ["redis-server"])
    # Samba generates its own TLS certificate for LDAPS a few seconds after it starts; the panel validates it with
    # this CA
    if not DRY:
        for _ in range(60):
            if SAMBA_CA.exists():
                break
            time.sleep(1)
        else:
            fail(f"Samba did not generate {SAMBA_CA} within 60 s (journalctl -u samba-ad-dc)")
    sh("install", "-m", "0644", str(SAMBA_CA), str(OPT / "tls/samba-ca.pem"))
    name = fqdn(args.realm)
    write_config(PANEL_CONF.format(fqdn=name, realm=args.realm.lower(), samba_ca=SAMBA_CA, port=args.port))
    if start:
        panel_start(args)


def panel_start(args):
    """Enable and restart the panel, then check its port and LDAPS."""
    name = fqdn(args.realm)
    sh("systemctl", "enable", "--now", "ad-manager", quiet=True)
    sh("systemctl", "restart", "ad-manager")
    if not DRY:
        for _ in range(15):
            if port_open("127.0.0.1", args.port):
                break
            time.sleep(1)
        else:
            fail("the panel did not open its port (journalctl -u ad-manager)")
        if not port_open(name, 636):
            fail(f"LDAPS does not answer on {name}:636")
    log(f"panel: https://{args.ip or name}:{args.port}  (sign in with a Domain Admins user)")


CONNECT_CONF = """# generated by scripts/samba-setup.py (connect) — see config.toml.example
[ad]
url = "{url}"
domain = "{realm}"
{tls}

[server]
listen = "0.0.0.0:{port}"
cert_file = "/opt/ad-manager/tls/tls.crt"
key_file = "/opt/ad-manager/tls/tls.key"
session_minutes = 30
audit_log = "/opt/ad-manager/logs/audit.log"

[db]
path = "/opt/ad-manager/data/ad-manager.db"

[redis]
addr = "127.0.0.1:6379"
"""


def connect(args):
    """ad-manager panel of an AD that already exists (Windows or Samba elsewhere): only the configuration."""
    need(args, "realm", "url")
    if not re.match(r"^ldaps?://[A-Za-z0-9.-]+(:[0-9]{1,5})?$", args.url):
        fail("--url: ldaps://dc.example.local (or ldap:// for read-only)")
    host = re.sub(r"^ldaps?://", "", args.url).split(":")[0]
    port = int(args.url.rsplit(":", 1)[1]) if args.url.count(":") == 2 else (636 if args.url.startswith("ldaps") else 389)
    if not DRY and not port_open(host, port):
        fail(f"{host}:{port} does not answer")
    tls = 'insecure_skip_verify = true   # chosen in the setup: switch to ca_file as soon as you can'
    if args.ca_file:
        if not Path(args.ca_file).is_file() and not DRY:
            fail(f"CA not found: {args.ca_file}")
        sh("install", "-m", "0644", args.ca_file, str(OPT / "tls/ca.pem"))
        tls = 'ca_file = "/opt/ad-manager/tls/ca.pem"'
    elif not args.insecure:
        tls = "insecure_skip_verify = false   # the DC's certificate must come from a system CA"
    write_config(CONNECT_CONF.format(url=args.url, realm=args.realm.lower(), tls=tls, port=args.port))
    sh("systemctl", "enable", "--now", "ad-manager", quiet=True)
    sh("systemctl", "restart", "ad-manager")
    log(f"panel connected to {args.url} (sign in with a Domain Admins user)")


AGENT_CONF = """# generated by scripts/samba-setup.py (agent) — see config.toml.example
[ad]
url = "ldaps://{dns_dc}"   # not used by the agent
domain = "{realm}"

[redis]
addr = "127.0.0.1:6379"

[agent]
listen = "0.0.0.0:9443"
cert_file = "/opt/ad-manager/tls/tls.crt"
key_file = "/opt/ad-manager/tls/tls.key"
token_file = "/opt/ad-manager/agent.token"
share_roots = ["{share_root}"]

[samba]
log_dir = "/var/log/samba"
"""


DC_AGENT_CONF = """
# generated by scripts/samba-setup.py (new-dc/dc-join --files): this DC also serves files (docs/dc-file-server.md)
[agent]
listen = "127.0.0.1:9443"   # only the panel beside it reaches the agent (registered as https://127.0.0.1:9443)
cert_file = "/opt/ad-manager/tls/tls.crt"
key_file = "/opt/ad-manager/tls/tls.key"
token_file = "/opt/ad-manager/agent.token"
queue = "fs-{host}"
share_roots = ["{share_root}"]
dc_file_server = true
"""


def check_dc_root(root):
    """A DC's share root must stay clear of Samba's own folders and of the system's: checked before anything changes."""
    r = os.path.realpath(root)
    if not r.startswith("/") or r in ("/", "/etc", "/var", "/var/lib", "/usr", "/opt", "/home", "/root", "/srv", "/run", "/tmp"):
        fail(f"--share-root {root}: choose a dedicated folder, e.g. /srv/shares")
    for p in ("/var/lib/samba", "/etc/samba", "/opt/ad-manager", "/var/backups"):
        if r == p or r.startswith(p + "/") or p.startswith(r + "/"):
            fail(f"--share-root {root} overlaps {p}")


def dc_files(args):
    """This DC also serves files: the root with a default Windows ACL, registry shares served, the agent in its DC file
    mode beside the panel, registered in it."""
    check_dc_root(args.share_root)
    sh("apt-get", "install", "-y", "-q", "python3-samba", "acl", env={"DEBIAN_FRONTEND": "noninteractive"})
    log(f"this DC also serves files: root {args.share_root}")
    new_root = not Path(args.share_root).exists()
    sh("install", "-d", "-m", "0755", args.share_root)
    if new_root or DRY:
        # Domain Admins and SYSTEM in control; authenticated users only see the root (each share sets its own)
        sh("samba-tool", "ntacl", "set", "O:DAG:DAD:P(A;OICI;FA;;;DA)(A;OICI;FA;;;SY)(A;;0x1200a9;;;AU)", args.share_root)
    smbconf_global("registry shares = yes")
    sh("smbcontrol", "all", "reload-config", check=False)
    if not DRY:
        text = CONFIG.read_text() if CONFIG.exists() else ""
        if "[agent]" not in text:
            write_config(text + DC_AGENT_CONF.format(host=socket.gethostname().split(".")[0].lower(), share_root=args.share_root))
    if (not (OPT / "agent.token").exists() or args.new_token) and not DRY:
        r = subprocess.run(["ad-manager", "agent", "token", "--config", str(CONFIG)], capture_output=True, text=True)
        if r.returncode:
            fail(f"ad-manager agent token: {r.stderr.strip()}")
        name = socket.gethostname().split(".")[0].lower() + " (DC)"
        # the panel's database stays its user's: registered as ad-manager
        reg = subprocess.run(["runuser", "-u", "ad-manager", "--", "ad-manager", "agent", "register-local", "--config", str(CONFIG),
                              "--name", name], input=r.stdout, capture_output=True, text=True, env=service_env())
        if reg.returncode:
            # without the registration the token is of no use: dropped, so running again makes and registers a new one
            (OPT / "agent.token").unlink(missing_ok=True)
            fail(f"registering the agent in the panel: {reg.stderr.strip()}")
        log(f"this DC's agent registered in the panel as {name}")
    sh("systemctl", "enable", "ad-manager-agent", quiet=True)
    if JSON_LOG:
        log("the agent restarts with the new configuration at the end of the setup")
    else:
        sh("systemctl", "restart", "ad-manager-agent")


IMPORT_GATES = """import_modes = true
"""


def write_secret(path, text):
    """A file only its creator (root) reads: a new 0600 file renamed over the old one, so no earlier owner, mode or
    open descriptor of the old file ever sees the new content."""
    tmp = f"{path}.{os.getpid()}.tmp"
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        with os.fdopen(fd, "w") as f:
            os.fchmod(f.fileno(), 0o600)
            f.write(text)
        os.replace(tmp, path)
    except BaseException:
        Path(tmp).unlink(missing_ok=True)
        raise


def service_env():
    """The environment the panel's unit runs with: ours plus EnvironmentFile=-/opt/ad-manager/env (AD_DB and the
    other overrides), so a command run as the service user opens the same database as the panel."""
    env = dict(os.environ)
    f = OPT / "env"
    for line in (f.read_text().splitlines() if f.exists() else []):
        k, sep, v = line.strip().partition("=")
        if sep and k and not k.startswith("#"):
            env[k.strip()] = v.strip().strip('"').strip("'")
    return env


def import_gates(args):
    """--import-ready: this DC agent's GPO-import and password-migration write modes, for the "From another AD"
    import right after the setup (docs/password-migration.md). The agent gets the switch `import_modes = true` (no
    secret on disk); the administrator credential is sealed in the panel's database by `ad-manager
    import-credential`, run as the service user with the password on stdin (Settings → Import from an existing AD
    shows and removes it)."""
    log("write modes for the import from an existing AD (remove the credential in Settings after the import)")
    if DRY:
        print(f"[dry-run] [agent] import_modes = true in {CONFIG}")
        print("[dry-run] ad-manager import-credential --user <admin> (password on stdin, as ad-manager)")
        return
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]{0,63}", args.admin):
        fail(f"--admin {args.admin}: letters, digits, '.', '_' or '-' only")
    text = CONFIG.read_text()
    if not re.search(r"^\s*import_modes\s*=", text, re.M):
        tables = re.findall(r"^\s*\[+([^\]]+)\]", text, re.M)
        if not tables or tables[-1].strip() != "agent":
            fail(f"{CONFIG}: [agent] is not the last section; add import_modes = true by hand (docs/password-migration.md)")
        write_config(text.rstrip("\n") + "\n" + IMPORT_GATES)
    r = subprocess.run(["runuser", "-u", "ad-manager", "--", str(OPT / "ad-manager"), "--config", str(CONFIG),
                        "import-credential", "--user", args.admin], input=args.admin_pw + "\n", text=True,
                       capture_output=True, check=False, env=service_env())
    if r.returncode != 0:
        fail(f"import credential: {(r.stdout + r.stderr).strip()}")
    if IMPORT_PASS.exists():  # a former run's clear-text file: the credential is in the panel now
        IMPORT_PASS.unlink()
    if not JSON_LOG:  # the wizard's agent restarts at its end; from the command line it reloads the modes now
        sh("systemctl", "restart", "ad-manager-agent")


def agent(args):
    """ad-manager agent on this file server: queue on a local Redis, token shown once."""
    need(args, "realm", "dns-dc")
    if not DRY and subprocess.run(["wbinfo", "-t"], capture_output=True).returncode:
        fail("this host is not a domain member (run member first)")
    # python3-samba: Windows (NTFS) permissions through Samba's own API (docs/samba-api.md)
    install_ad_manager(args, ["redis-server", "acl", "python3-samba"])
    sh("install", "-d", "-m", "0755", args.share_root)
    write_config(AGENT_CONF.format(dns_dc=args.dns_dc, realm=args.realm.lower(), share_root=args.share_root))
    token = ""
    if (not (OPT / "agent.token").exists() or args.new_token) and not DRY:
        # the token goes to stdout, the notice to stderr
        r = subprocess.run(["ad-manager", "agent", "token", "--config", str(CONFIG)], capture_output=True, text=True)
        if r.returncode:
            fail(f"ad-manager agent token: {r.stderr.strip()}")
        token = r.stdout.strip()
    sh("systemctl", "enable", "ad-manager-agent", quiet=True)
    if JSON_LOG:
        # run by the agent's setup: restarting it here would end the job that follows this run; it restarts itself
        log("the agent restarts with the new configuration at the end of the setup")
    else:
        sh("systemctl", "restart", "ad-manager-agent")
    if not DRY and not JSON_LOG:
        for _ in range(15):
            if port_open("127.0.0.1", 9443):
                break
            time.sleep(1)
        else:
            fail("the agent did not open port 9443 (journalctl -u ad-manager-agent)")
    host = fqdn(args.realm)
    log(f"in the panel: Shares → + new → URL https://{host}:9443 (self-signed certificate: tick 'insecure')")
    if token and JSON_LOG:
        # the wizard's log is kept on disk: the token goes to a root-only file instead, read once
        once = OPT / "data/agent-token-once"
        write(once, token + "\n", 0o600)
        log(f"agent token in {once} (sudo cat; delete it after registering it in the panel)")
    elif token:
        print(f"\n   agent token (shown only now; keep it in the panel):\n   {token}\n")
    else:
        log("token already existed; to generate another: --new-token")


def verify(args):
    if SAM_LDB.exists():
        log("role: DC")
        sh("samba-tool", "domain", "level", "show", check=False)
        sh("samba-tool", "fsmo", "show", check=False)
        realm = args.realm or re.search(r"realm\s*=\s*(\S+)", smb_conf().read_text(), re.I).group(1).lower()
        sh("host", "-t", "SRV", f"_ldap._tcp.{realm}", "127.0.0.1", check=False)
        for p, what in ((636, "LDAPS"), (88, "Kerberos"), (464, "kpasswd")):
            print(f"   {what:9} {p}: {'ok' if port_open('127.0.0.1', p) else 'CLOSED'}")
    else:
        log("role: member")
        sh("wbinfo", "-t", check=False)
        sh("net", "conf", "list", check=False)
    for unit in ("ad-manager", "ad-manager-agent", "redis-server"):
        state = subprocess.run(["systemctl", "is-active", unit], capture_output=True, text=True).stdout.strip()
        print(f"   {unit:17} {state}")


def diagnose(args):
    """What is here, as JSON (packages, Samba role, ports, name, clock, disk, services): read by the setup wizard."""
    def installed(pkg):
        r = subprocess.run(["dpkg-query", "-W", "-f=${Status}", pkg], capture_output=True, text=True)
        return r.returncode == 0 and "install ok installed" in r.stdout
    def active(unit):
        return subprocess.run(["systemctl", "is-active", unit], capture_output=True, text=True).stdout.strip()
    role = ""
    if shutil.which("testparm") and Path("/etc/samba/smb.conf").exists():
        role = subprocess.run(["testparm", "-s", "--parameter-name=server role"], capture_output=True, text=True).stdout.strip()
    listening = subprocess.run(["ss", "-ltnH"], capture_output=True, text=True).stdout
    ports = {p: bool(re.search(rf":{p}\s", listening)) for p in (53, 88, 135, 389, 445, 636)}
    sync = subprocess.run(["timedatectl", "show", "-p", "NTPSynchronized", "--value"], capture_output=True, text=True).stdout.strip()
    st = os.statvfs("/var/lib")
    osrel = Path("/etc/os-release").read_text()
    out = {
        "hostname": socket.gethostname(),
        "fqdn": socket.getfqdn(),
        "os": next(iter(re.findall(r'^PRETTY_NAME="([^"]*)"', osrel, re.M)), ""),
        "packages": {p: installed(p) for p in ("samba", "samba-ad-dc", "winbind", "krb5-user", "python3-samba", "tdb-tools",
                                               "redis-server", "chrony")},
        "samba_role": role,
        "realm": provisioned_realm(),
        "samba_files": any(Path(p).exists() for p in ("/etc/samba/smb.conf", "/var/lib/samba/private/secrets.tdb", str(SAM_LDB))),
        "ports_in_use": {str(k): v for k, v in ports.items()},
        "clock_synced": sync == "yes",
        "disk_free_mib": st.f_bavail * st.f_frsize // (1 << 20),
        "services": {u: active(u) for u in ("samba-ad-dc", "smbd", "winbind", "redis-server", "ad-manager", "ad-manager-agent")},
        "ips": subprocess.run(["hostname", "-I"], capture_output=True, text=True).stdout.split(),
    }
    print(json.dumps(out, ensure_ascii=False))


ROLES = ("packages", "new-dc", "dc-join", "restore-dc", "member", "panel", "agent", "connect", "verify", "diagnose")
# the old Portuguese role names, still accepted
ROLE_ALIASES = {"pacotes": "packages", "dc-novo": "new-dc", "dc-restaurar": "restore-dc", "membro": "member",
                "painel": "panel", "agente": "agent", "conectar": "connect", "verificar": "verify",
                "diagnostico": "diagnose"}


def main():
    global DRY
    p = argparse.ArgumentParser(description=__doc__.split("\n\n")[0], formatter_class=argparse.RawDescriptionHelpFormatter,
                                epilog=__doc__.split("\n\n", 1)[1])
    p.add_argument("role", choices=[*ROLES, *ROLE_ALIASES], metavar="{" + ",".join(ROLES) + "}")
    p.add_argument("--url", help="connect: LDAP URL of the existing DC")
    p.add_argument("--ca-file", help="connect: CA of the DC's certificate")
    p.add_argument("--insecure", action="store_true", help="connect: accept a self-signed certificate (lab)")
    p.add_argument("--realm", help="DNS domain, e.g. example.local")
    p.add_argument("--domain", help="NetBIOS name, e.g. EXAMPLE")
    p.add_argument("--ip", help="this DC's IP")
    p.add_argument("--src-dc", help="existing DC (dc-join)")
    p.add_argument("--dns-dc", help="DC the member uses as DNS")
    p.add_argument("--backup", help="restore-dc: DC backup file (.tar.bz2) on this server")
    p.add_argument("--dc-level", help="dc-join: this DC's functional level (2008_R2, 2012, 2012_R2, 2016); default: the domain's")
    p.add_argument("--admin", default="Administrator", help="domain administrator (default: Administrator)")
    p.add_argument("--forwarder", default="1.1.1.1", help="DNS for names outside the domain")
    p.add_argument("--share-root", default="/srv/shares", help="root of the shares (member/agent)")
    p.add_argument("--interfaces", default="", help='DC with more than one network card, e.g. "lo eth0"')
    p.add_argument("--legacy-schannel", default="", help='accounts without AES in Netlogon, e.g. "ADM$ PCXP01$"')
    p.add_argument("--xp-smb1", action="store_true", help="enable SMB1 (XP); temporary")
    # the second name of each flag is the old Portuguese one, still accepted
    p.add_argument("--panel", "--painel", dest="panel", action="store_true", help="with new-dc/dc-join: install the panel on this DC")
    p.add_argument("--agent", "--agente", dest="agent", action="store_true", help="with member: install the agent on this server")
    p.add_argument("--import-ready", dest="import_ready", action="store_true",
                   help="with new-dc --panel --files: turn on this DC agent's GPO-import and password-migration write modes")
    p.add_argument("--files", "--arquivos", dest="files", action="store_true",
                   help="with new-dc/dc-join --panel: this DC also serves files (--share-root)")
    p.add_argument("--deb", help="the ad-manager_X.Y.Z_amd64.deb package")
    p.add_argument("--port", type=int, default=8443, help="panel port (default 8443)")
    p.add_argument("--new-token", "--novo-token", dest="new_token", action="store_true", help="agent: generate another token")
    p.add_argument("--dry-run", action="store_true", help="show what it would do, without changing anything")
    args = p.parse_args()
    args.role = ROLE_ALIASES.get(args.role, args.role)
    DRY = args.dry_run
    if args.files and not (args.panel and args.role in ("new-dc", "dc-join", "restore-dc")):
        fail("--files goes with new-dc/dc-join/restore-dc --panel (the DC's agent is registered in the local panel)")
    if args.import_ready and not (args.role == "new-dc" and args.panel and args.files):
        fail("--import-ready goes with new-dc --panel --files (the write modes are this DC agent's)")
    sys.stdout.reconfigure(line_buffering=True)  # keep the order with stderr and subprocess output
    if args.role == "diagnose":
        diagnose(args)
        return
    if os.geteuid() != 0 and not DRY:
        fail("run with sudo")
    osrel = Path("/etc/os-release").read_text()
    if 'VERSION_ID="26.04"' not in osrel:
        log("warning: tested on Ubuntu 26.04")
    {
        "packages": packages,
        "new-dc": lambda a: (a.files and check_dc_root(a.share_root), dc_new(a), a.panel and panel(a, start=False),
                             a.files and dc_files(a), a.import_ready and import_gates(a), a.panel and panel_start(a)),
        "dc-join": lambda a: (a.files and check_dc_root(a.share_root), dc_join(a), a.panel and panel(a, start=False),
                             a.files and dc_files(a), a.panel and panel_start(a)),
        "restore-dc": lambda a: (a.files and check_dc_root(a.share_root), dc_restore(a), a.panel and panel(a, start=False),
                             a.files and dc_files(a), a.panel and panel_start(a)),
        "member": lambda a: (member(a), a.agent and agent(a)),
        "panel": panel,
        "agent": agent,
        "connect": connect,
        "verify": verify,
    }[args.role](args)
    log("ok")


if __name__ == "__main__":
    main()
