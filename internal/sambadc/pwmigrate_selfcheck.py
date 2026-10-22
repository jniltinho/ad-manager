# Self-check for pwmigrate.py's non-DRS logic, runnable on any box with python3-ldb (e.g. the destination DC):
#   python3 internal/sambadc/pwmigrate_selfcheck.py
# It loads pwmigrate.py without running main() and exercises the pieces that do not need a live DRS connection:
# the destination pin, the per-user transaction/rollback, the Kerberos-clearing on an NTLM-only set, and the
# full-state "same" rule (which must consider pwdLastSet). It is not part of `make test` (that runs no Python); it
# is a quick guard to run during lab validation.
import ast
import os
import sys

import ldb  # python3-ldb; present on any Samba box

HERE = os.path.dirname(os.path.abspath(__file__))


def load():
    src = open(os.path.join(HERE, "pwmigrate.py")).read()
    tree = ast.parse(src)
    tree.body = [n for n in tree.body if not (isinstance(n, ast.Expr) and isinstance(n.value, ast.Call)
                                              and getattr(n.value.func, "id", "") == "main")]
    ns = {}
    exec(compile(tree, "pwmigrate.py", "exec"), ns)
    return ns


class FakeDB(ldb.Ldb):
    """ldb.Ldb subclass recording the transaction sequence and the last modify message (so ldb.Dn/Message work)."""
    def __init__(self, fail=False, entry=None):
        super().__init__()
        self.fail = fail
        self.entry = entry or {}
        self.events = []
        self.writes = []

    def transaction_start(self):
        self.events.append("start")

    def transaction_commit(self):
        self.events.append("commit")

    def transaction_cancel(self):
        self.events.append("rollback")

    def modify(self, msg, controls):
        self.writes.append((msg, controls))
        if self.fail:
            raise RuntimeError("synthetic failure")
        # mimic Samba's bypass consistency check: reject a modify that carries a Kerberos blob
        if getattr(self, "kerb_reject", False) and "supplementalCredentials" in msg and len(msg["supplementalCredentials"]) > 0:
            raise Exception(53, "supplementalCredentialsBlob memcmp differ at source4/dsdb/samdb/ldb_modules/password_hash.c:681")

    def search(self, **kw):
        return [self.entry]


def check(cond, what):
    if not cond:
        print("FAIL:", what)
        sys.exit(1)
    print("ok:", what)


def main():
    ns = load()

    # 1. pin required and verified before anything is read
    for pin, dom, src, why in [
        (None, "S-1-5-21-D", "S-1-5-21-S", "empty pin refused"),
        ("S-1-5-21-X", "S-1-5-21-D", "S-1-5-21-S", "mismatched pin refused"),
        ("S-1-5-21-D", "S-1-5-21-D", "S-1-5-21-D", "source == destination refused"),
    ]:
        try:
            ns["pin_check"](pin, dom, src)
            check(False, why)
        except Exception:
            check(True, why)
    ns["pin_check"]("S-1-5-21-D", "S-1-5-21-D", "S-1-5-21-S")
    check(True, "matching pin with distinct source accepted")

    # 2. per-user transaction commits on success and rolls back on failure
    db = FakeDB()
    ns["set_user"](db, "CN=ana", b"x" * 16, None, False)
    check(db.events == ["start", "commit"], "successful set_user commits once")
    msg = db.writes[0][0]
    check("supplementalCredentials" in msg, "ntlm-only set clears supplementalCredentials (no stale Kerberos)")
    pls = str(msg["pwdLastSet"][0])
    check(pls.isdigit() and int(pls) > 0, "pwdLastSet is an explicit non-zero FILETIME, not -1 (bypass skips conversion)")
    db = FakeDB(fail=True)
    try:
        ns["set_user"](db, "CN=ana", b"x" * 16, None, False)
    except RuntimeError:
        pass
    check(db.events == ["start", "rollback"], "failed set_user rolls back")

    # 3. Kerberos copy: accepted → set (copied True)
    db = FakeDB()
    copied, reason = ns["set_user"](db, "CN=ana", b"x" * 16, b"FAKEKERBBLOB", True)
    check(copied is True and reason is None, "set_user copies Kerberos when Samba accepts the blob")

    # 4. Kerberos blob rejected by Samba's memcmp → fall back to NT-only (ntlm_only), never error
    db = FakeDB()
    db.kerb_reject = True
    copied, reason = ns["set_user"](db, "CN=ana", b"x" * 16, b"FAKEKERBBLOB", True)
    check(copied is False, "set_user falls back to NT-only when Samba rejects the Kerberos blob")
    check(bool(reason) and "NTLM only" in reason, "fallback carries a sanitized reason")
    last = db.writes[-1][0]
    check(len(last["supplementalCredentials"]) == 0, "fallback re-wrote NT hash only (supplementalCredentials cleared)")

    # 5. an UNRELATED write error is not swallowed (stays an error)
    db = FakeDB(fail=True)
    try:
        ns["set_user"](db, "CN=ana", b"x" * 16, b"FAKEKERBBLOB", True)
        check(False, "unrelated write error must propagate")
    except RuntimeError:
        check(True, "unrelated write error propagates (not masked as ntlm_only)")

    # 6. Kerberos salt ↔ realm matching is component-boundary aware (not a substring)
    m = ns["realm_salt_matches"]
    check(m("CRIARENET.NETjohn", "criarenet.net"), "same realm salt transfers")
    check(m("criarenet.netjohn", "CRIARENET.NET"), "salt match is case-insensitive")
    check(not m("CRIARENET.NET.BRjohn", "criarenet.net"), "criarenet.net.br is NOT the same realm")
    check(not m("SUB.CRIARENET.NETjohn", "criarenet.net"), "sub.criarenet.net is NOT the same realm")
    check(not m("OTHER.LOCALjohn", "criarenet.net"), "different realm → no transfer")
    check(not m("", "criarenet.net") and not m("CRIARENET.NETjohn", ""), "empty salt/realm → no transfer")

    print("pwmigrate self-check passed")


if __name__ == "__main__":
    main()
