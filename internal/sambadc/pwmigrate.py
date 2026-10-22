# AD Manager — password-material migration helper.
#
# Embedded in the Go binary (internal/sambadc) and started by the DC agent, as
# root, with `python3 -I -s -B -c <this code>`, only when the dedicated
# password-migration write mode is on. It reads one JSON request on stdin and
# writes one JSON response on stdout; it never prints a secret, and a Samba
# error is reduced to its type (no replicated value, no stack trace).
#
# It does two things, both on a NEW-DOMAIN cutover (the source and the local
# domain share the realm but have different SIDs):
#   plan    authenticated DRSUAPI connectivity to the source and non-secret
#           metadata only (which local users would be set, which are missing,
#           which are built-in). It reads NO password material.
#   apply   for each matching user, a read-only DCSync of that one object over
#           a sealed DRSUAPI GetNCChanges (extended op REPL_SECRET), decrypt in
#           memory, and set the NT hash (and the Kerberos keys when their salt
#           transfers) on the LOCAL sam.ldb in one transaction per user, with
#           pwdLastSet non-zero. Nothing is written to the source: no
#           UpdateRefs, no DRS_ADD_REF, no join/provision.
#
# Request:  {"op":"plan"|"apply", "source":{"host":str,"user":str,"password":str},
#            "dest_sid":str}
# Secrets:  the source replication account password arrives in the request on
#           stdin — never on the command line, never a file on disk, never an
#           environment variable. The destination write is a privileged local
#           write (the agent runs as root on the DC); the dedicated gate is the
#           authorization, and dest_sid pins the destination domain.
# Response: {"ok":true,"result":{...}} or {"ok":false,"error":str,"code":str}.
import json
import sys

MAX_INPUT = 1 << 20

# RIDs never migrated, on both sides: Administrator, Guest, krbtgt.
PROTECTED_RIDS = {500, 501, 502}
# Account types that are not ordinary users: machine and trust accounts.
MACHINE_TYPES = {0x30000001, 0x30000002, 0x30000003}  # workstation/server/interdomain trust
# Kerberos packages copied verbatim when the salt transfers; everything else in
# supplementalCredentials (WDigest, Packages, CLEARTEXT) is never copied.
KERBEROS_PACKAGES = ("Primary:Kerberos-Newer-Keys", "Primary:Kerberos")


class PwError(Exception):
    def __init__(self, msg, code="invalid"):
        super().__init__(msg)
        self.code = code


def rid_of(sid):
    """The RID (last sub-authority) of a dom_sid."""
    return sid.sub_auths[sid.num_auths - 1]


def load_local(lp):
    """Open the local sam.ldb with the system session; return (samdb, domain_sid str, realm, invocation_id)."""
    from samba.samdb import SamDB
    from samba.auth import system_session
    samdb = SamDB(session_info=system_session(), lp=lp)
    dom = samdb.get_domain_sid()
    realm = lp.get("realm")
    inv = samdb.get_invocation_id()
    return samdb, str(dom), str(realm).upper(), inv


def source_creds(lp, user, password):
    """Credentials for the source replication account (password from the stdin request, never a file or argument)."""
    from samba.credentials import Credentials, DONT_USE_KERBEROS
    creds = Credentials()
    creds.guess(lp)
    creds.set_username(user)
    creds.set_password(password)
    # NTLM is enough and works against a 2008 R2 source without a shared realm.
    creds.set_kerberos_state(DONT_USE_KERBEROS)
    return creds


def source_connect(host, lp, creds):
    """A sealed DRSUAPI bind to the source and an LDAP handle for its metadata (read only, no UpdateRefs)."""
    from samba import drs_utils
    from samba.samdb import SamDB
    drs, drs_handle, _exts = drs_utils.drsuapi_connect(host, lp, creds)
    src = SamDB(url="ldap://%s" % host, credentials=creds, lp=lp)
    return drs, drs_handle, src


def source_users(src):
    """Every ordinary user of the source: {sam_lower: {sam, dn, guid, rid}} — built-ins and machine/trust accounts
    left out (by RID and sAMAccountType, never by name)."""
    import ldb
    base = src.get_default_basedn()
    res = src.search(base=base, scope=ldb.SCOPE_SUBTREE,
                     expression="(sAMAccountType=805306368)",  # SAM_NORMAL_USER_ACCOUNT
                     attrs=["sAMAccountName", "objectSid", "objectGUID", "sAMAccountType"])
    from samba.dcerpc import security
    from samba.ndr import ndr_unpack
    out = {}
    for e in res:
        rid = rid_of(ndr_unpack(security.dom_sid, e["objectSid"][0]))
        if rid in PROTECTED_RIDS:
            continue
        sam = str(e["sAMAccountName"][0])
        guid = src.schema_format_value("objectGUID", e["objectGUID"][0]).decode()
        out[sam.lower()] = {"sam": sam, "dn": str(e.dn), "guid": guid.strip("{}"), "rid": rid}
    return out


def local_user(samdb, sam):
    """The local user's entry (DN, RID) or None; built-ins and machine/trust accounts are reported protected."""
    import ldb
    from samba.dcerpc import security
    from samba.ndr import ndr_unpack
    res = samdb.search(base=samdb.get_default_basedn(), scope=ldb.SCOPE_SUBTREE,
                       expression="(sAMAccountName=%s)" % ldb.binary_encode(sam),
                       attrs=["objectSid", "sAMAccountType", "pwdLastSet"])
    if len(res) != 1:
        return None
    e = res[0]
    rid = rid_of(ndr_unpack(security.dom_sid, e["objectSid"][0]))
    atype = int(e["sAMAccountType"][0]) if "sAMAccountType" in e else 0
    protected = rid in PROTECTED_RIDS or atype in MACHINE_TYPES
    pwd_last_set = int(e["pwdLastSet"][0]) if "pwdLastSet" in e else 0
    return {"dn": str(e.dn), "rid": rid, "protected": protected, "pwd_last_set": pwd_last_set}


def classify(samdb, susers):
    """Per-user outcome without reading any secret: would_set, missing or builtin."""
    users = []
    for key in sorted(susers):
        u = susers[key]
        lu = local_user(samdb, u["sam"])
        if lu is None:
            users.append({"sam": u["sam"], "outcome": "missing"})
        elif lu["protected"]:
            users.append({"sam": u["sam"], "outcome": "builtin"})
        else:
            users.append({"sam": u["sam"], "outcome": "would_set"})
    return users


def plan(req, lp):
    samdb, dom_sid, _realm, _inv = load_local(lp)
    creds = source_creds(lp, req["source"]["user"], req["source"]["password"])
    drs, drs_handle, src = source_connect(req["source"]["host"], lp, creds)
    try:
        src_sid = str(src.get_domain_sid())
        pin_check(req.get("dest_sid"), dom_sid, src_sid)
        users = classify(samdb, source_users(src))
    finally:
        drs_unbind(drs, drs_handle)
    counts = {"would_set": 0, "missing": 0, "builtin": 0}
    for u in users:
        counts[u["outcome"]] = counts.get(u["outcome"], 0) + 1
    return {"dest_sid": dom_sid, "source_sid": src_sid, "users": users, "counts": counts}


def pin_check(dest_pin, dom_sid, src_sid):
    """Refuse before anything secret is read: the caller must supply the destination's domain SID (established
    independently of the agent, which is selected only by realm), it must match this DC's local database, and the
    source must not be the destination."""
    if not dest_pin:
        raise PwError("destination domain SID (dest_sid) is required", "pin")
    if dest_pin != dom_sid:
        raise PwError("destination does not match the pinned domain SID", "pin")
    if src_sid == dom_sid:
        raise PwError("source and destination are the same domain", "same-domain")


def drs_unbind(drs, handle):
    try:
        drs.DsUnbind(handle)
    except Exception:
        pass


def get_ncchanges_secret(drs, drs_handle, src_dsa_inv, guid):
    """One object's attributes over a sealed GetNCChanges with the REPL_SECRET extended op: read only, makes us no
    replica (replica_flags = 0), never UpdateRefs/DRS_ADD_REF. Returns the DsReplicaObjectListItemEx or None."""
    from samba.dcerpc import drsuapi, misc
    req8 = drsuapi.DsGetNCChangesRequest8()
    req8.destination_dsa_guid = misc.GUID()
    req8.source_dsa_invocation_id = src_dsa_inv
    nc = drsuapi.DsReplicaObjectIdentifier()
    nc.guid = misc.GUID(guid)  # pull this one object by GUID (REPL_SECRET extended op)
    req8.naming_context = nc
    hwm = drsuapi.DsReplicaHighWaterMark()
    hwm.tmp_highest_usn = 0
    hwm.reserved_usn = 0
    hwm.highest_usn = 0
    req8.highwatermark = hwm
    req8.uptodateness_vector = None
    req8.replica_flags = 0  # REPL_SECRET: no WRIT_REP, no ADD_REF — we are not becoming a replica
    req8.max_object_count = 1
    req8.max_ndr_size = 402116
    req8.extended_op = drsuapi.DRSUAPI_EXOP_REPL_SECRET
    req8.fsmo_info = 0
    req8.partial_attribute_set = None
    req8.partial_attribute_set_ex = None
    req8.mapping_ctr.num_mappings = 0
    req8.mapping_ctr.mappings = None
    (_level, ctr) = drs.DsGetNCChanges(drs_handle, 8, req8)
    if ctr.object_count == 0 or ctr.first_object is None:
        return None
    return ctr.first_object.object


def decrypt_secrets(drs, obj, rid):
    """Decrypt unicodePwd and supplementalCredentials in memory with the DRS connection's session key
    (net.replicate_decrypt takes the drsuapi bind). Returns (nt_hash bytes|None, supp_blob bytes|None)."""
    from samba.dcerpc import drsuapi
    nt_hash = None
    supp = None
    for i in range(obj.attribute_ctr.num_attributes):
        attr = obj.attribute_ctr.attributes[i]
        if attr.attid == drsuapi.DRSUAPI_ATTID_unicodePwd:
            net.replicate_decrypt(drs, attr, rid)
            if attr.value_ctr.num_values == 1:
                nt_hash = bytes(attr.value_ctr.values[0].blob)
        elif attr.attid == drsuapi.DRSUAPI_ATTID_supplementalCredentials:
            net.replicate_decrypt(drs, attr, rid)
            if attr.value_ctr.num_values == 1:
                supp = bytes(attr.value_ctr.values[0].blob)
    return nt_hash, supp


def inspect_supplemental(supp, realm):
    """Classify the decrypted supplementalCredentials: (has_cleartext, kerberos_ok). A Primary:CLEARTEXT package is
    a rejection; the Kerberos keys count as transferable only when their salt names the destination realm."""
    from samba.dcerpc import drsblobs
    from samba.ndr import ndr_unpack
    if not supp:
        return False, False
    scb = ndr_unpack(drsblobs.supplementalCredentialsBlob, supp)
    has_cleartext = False
    kerberos_ok = False
    for p in scb.sub.packages:
        if p.name == "Primary:CLEARTEXT":
            has_cleartext = True
        elif p.name in KERBEROS_PACKAGES:
            try:
                data = p.data.decode() if isinstance(p.data, bytes) else p.data  # the package data is a hex string
                raw = bytes.fromhex(data)
                kp = ndr_unpack(drsblobs.package_PrimaryKerberosBlob, raw)
                salt = kp.ctr.salt.string or ""
                # the default salt is REALM + the principal name; a salt naming the source realm would not match
                kerberos_ok = realm.upper() in salt.upper()
            except Exception:
                kerberos_ok = False
    return has_cleartext, kerberos_ok


def set_user(samdb, dn, nt_hash, supp, copy_kerberos):
    """Set the password material on the local user in one transaction: the NT hash and, when copy_kerberos, the
    Kerberos keys verbatim; otherwise the destination's supplementalCredentials is cleared so stale Kerberos keys
    (from the object import's generated password) cannot authenticate with the new NT hash. The bypass control
    stores the values verbatim and pwdLastSet is set non-zero."""
    import ldb
    from samba import dsdb
    msg = ldb.Message()
    msg.dn = ldb.Dn(samdb, dn)
    msg["unicodePwd"] = ldb.MessageElement(nt_hash, ldb.FLAG_MOD_REPLACE, "unicodePwd")
    if copy_kerberos and supp:
        msg["supplementalCredentials"] = ldb.MessageElement(supp, ldb.FLAG_MOD_REPLACE, "supplementalCredentials")
    else:
        msg["supplementalCredentials"] = ldb.MessageElement([], ldb.FLAG_MOD_REPLACE, "supplementalCredentials")
    # an explicit current FILETIME (non-zero): the bypass control skips the module's "-1 means now" conversion, so
    # "-1" would be stored verbatim as an invalid timestamp. This means "no change at next sign-in".
    import time
    now_ft = int((time.time() + 11644473600) * 10_000_000)
    msg["pwdLastSet"] = ldb.MessageElement(str(now_ft), ldb.FLAG_MOD_REPLACE, "pwdLastSet")
    controls = [dsdb.DSDB_CONTROL_BYPASS_PASSWORD_HASH_OID]
    samdb.transaction_start()
    try:
        samdb.modify(msg, controls=controls)
        samdb.transaction_commit()
    except Exception:
        samdb.transaction_cancel()
        raise


def current_state(samdb, dn):
    """The local user's current NT hash and whether it already carries Kerberos keys, to detect a full 'same'. The
    NT hash is unreadable without privilege; a system-session search on the local sam.ldb returns it. Returns
    (nt_hash bytes|None, has_kerberos bool)."""
    import ldb
    nt_hash = None
    has_kerberos = False
    try:
        res = samdb.search(base=dn, scope=ldb.SCOPE_BASE, attrs=["unicodePwd", "supplementalCredentials"])
        if "unicodePwd" in res[0]:
            nt_hash = bytes(res[0]["unicodePwd"][0])
        has_kerberos = "supplementalCredentials" in res[0] and len(res[0]["supplementalCredentials"][0]) > 0
    except Exception:
        pass
    return nt_hash, has_kerberos


def apply(req, lp):
    samdb, dom_sid, realm, _inv = load_local(lp)
    creds = source_creds(lp, req["source"]["user"], req["source"]["password"])
    drs, drs_handle, src = source_connect(req["source"]["host"], lp, creds)
    users = []
    counts = {"set": 0, "ntlm_only": 0, "same": 0, "missing": 0, "builtin": 0, "rejected": 0, "error": 0}
    try:
        from samba.dcerpc import misc
        src_sid = str(src.get_domain_sid())
        pin_check(req.get("dest_sid"), dom_sid, src_sid)
        src_inv = misc.GUID(src.get_invocation_id())  # the binding returns a string; the request wants a GUID object
        susers = source_users(src)
        for key in sorted(susers):
            u = susers[key]
            out = {"sam": u["sam"]}
            lu = local_user(samdb, u["sam"])
            if lu is None:
                out["outcome"] = "missing"
            elif lu["protected"]:
                out["outcome"] = "builtin"
            else:
                try:
                    out["outcome"] = apply_one(samdb, drs, drs_handle, src_inv, u, lu, realm)
                except Exception:
                    # one user's failure (its own transaction already rolled back in set_user) never aborts the rest
                    out["outcome"] = "error"
            counts[out["outcome"]] = counts.get(out["outcome"], 0) + 1
            users.append(out)
    finally:
        drs_unbind(drs, drs_handle)
    return {"dest_sid": dom_sid, "source_sid": src_sid, "users": users, "counts": counts}


def apply_one(samdb, drs, drs_handle, src_inv, u, lu, realm):
    """Extract one user's secrets and set them; returns the outcome. Secrets live only in these locals."""
    obj = get_ncchanges_secret(drs, drs_handle, src_inv, u["guid"])
    if obj is None:
        return "error"
    nt_hash, supp = decrypt_secrets(drs, obj, u["rid"])
    try:
        if nt_hash is None:
            return "error"
        has_cleartext, kerberos_ok = inspect_supplemental(supp, realm)
        if has_cleartext:
            return "rejected"
        # full-state idempotency: skip only an NTLM-only user whose state already matches — pwdLastSet non-zero, the
        # NT hash equal, and no Kerberos keys present (so none to add and none stale). When we would copy Kerberos
        # keys we cannot cheaply prove the stored keys/salt are identical, so we re-apply (reporting "set") rather
        # than risk a false "same" that leaves stale keys.
        if not kerberos_ok:
            cur_hash, cur_kerberos = current_state(samdb, lu["dn"])
            if lu["pwd_last_set"] != 0 and cur_hash is not None and cur_hash == nt_hash and not cur_kerberos:
                return "same"
        set_user(samdb, lu["dn"], nt_hash, supp if kerberos_ok else None, kerberos_ok)
        return "set" if kerberos_ok else "ntlm_only"
    finally:
        del nt_hash
        del supp


# net is imported once (replicate_init/replicate_decrypt live on the module).
net = None


def handle(req, lp):
    global net
    import samba.net as _net
    net = _net.Net(None, lp)  # replicate_init/replicate_decrypt live on this instance
    op = req.get("op")
    if op == "plan":
        return plan(req, lp)
    if op == "apply":
        return apply(req, lp)
    raise PwError("unknown operation")


def main():
    raw = sys.stdin.buffer.read(MAX_INPUT)
    try:
        req = json.loads(raw)
        if not isinstance(req, dict):
            raise PwError("invalid request")
        from samba import param
        lp = param.LoadParm()
        lp.load(param.default_path())
        out = {"ok": True, "result": handle(req, lp)}
    except PwError as e:
        out = {"ok": False, "error": str(e), "code": e.code}
    except Exception as e:  # Samba/NTSTATUS errors: only the type, never a replicated value
        out = {"ok": False, "error": type(e).__name__, "code": "samba"}
    sys.stdout.write(json.dumps(out))
    sys.stdout.flush()


main()
