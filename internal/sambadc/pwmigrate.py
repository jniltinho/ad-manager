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
    # parse_string handles both DOMAIN\user and user@realm and sets the domain (set_username alone does not, which
    # makes a cross-domain NTLM bind fail with LOGON_FAILURE).
    creds.parse_string(user)
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
            users.append({"sam": u["sam"], "outcome": "missing", "reason": "no matching user in this domain (import the objects first)"})
        elif lu["protected"]:
            users.append({"sam": u["sam"], "outcome": "builtin", "reason": "built-in or machine/trust account (skipped)"})
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


def get_ncchanges_secret(drs, drs_handle, src_dsa_inv, dn, guid):
    """One object's attributes over a sealed GetNCChanges with the REPL_OBJ extended op (the canonical single-object
    DCSync): read only, makes us no replica (replica_flags = 0), never UpdateRefs/DRS_ADD_REF. The object is named by
    its GUID (with the DN alongside). Returns the DsReplicaObjectListItemEx or None."""
    from samba.dcerpc import drsuapi, misc
    req8 = drsuapi.DsGetNCChangesRequest8()
    req8.destination_dsa_guid = misc.GUID()
    req8.source_dsa_invocation_id = src_dsa_inv
    nc = drsuapi.DsReplicaObjectIdentifier()
    nc.dn = dn
    nc.guid = misc.GUID(guid)  # REPL_OBJ names the one object by GUID
    req8.naming_context = nc
    hwm = drsuapi.DsReplicaHighWaterMark()
    hwm.tmp_highest_usn = 0
    hwm.reserved_usn = 0
    hwm.highest_usn = 0
    req8.highwatermark = hwm
    req8.uptodateness_vector = None
    req8.replica_flags = 0  # no WRIT_REP, no ADD_REF — we are not becoming a replica
    req8.max_object_count = 1
    req8.max_ndr_size = 402116
    req8.extended_op = drsuapi.DRSUAPI_EXOP_REPL_OBJ
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


def realm_salt_matches(salt, dest_realm, src_realm):
    """Whether a principal's Kerberos keys transfer. The salt is the realm concatenated with the principal name and
    has no separator, so it cannot by itself establish the realm boundary (CRIARENET.NETWORK vs CRIARENET.NET). The
    authoritative test is therefore the SOURCE realm equalling the DESTINATION realm exactly (case-insensitive) —
    the new-domain cutover keeps the same realm. The salt is only a sanity check that it is the default
    realm-prefixed salt (starts with the realm on a component boundary), so a custom salt is not blindly copied."""
    if not salt or not dest_realm or not src_realm:
        return False
    if src_realm.upper() != dest_realm.upper():
        return False
    s, r = salt.upper(), dest_realm.upper()
    return s.startswith(r) and not s.startswith(r + ".")


def inspect_supplemental(supp, realm, src_realm):
    """Classify the decrypted supplementalCredentials: (has_cleartext, kerberos_ok, reason, kerb_blob).

    A Primary:CLEARTEXT package is a rejection. The Kerberos keys count as transferable only when their salt names
    the destination realm. When they do, kerb_blob is a REBUILT supplementalCredentials blob containing ONLY the
    allowlisted Kerberos packages (WDigest, the Packages listing and anything else are dropped — the allowlist), so
    nothing outside the allowlist is copied to the target. Any layout this helper cannot parse (no Kerberos package,
    a WDigest-only blob, an unreadable blob, a non-matching salt) is NOT an error — kerberos_ok=False with a
    sanitized reason, so the user falls back to NTLM-only. The reason never contains a key, hash or salt value."""
    from samba.dcerpc import drsblobs
    from samba.ndr import ndr_unpack, ndr_pack
    if not supp:
        return False, False, "no supplementalCredentials (NTLM only)", None
    try:
        scb = ndr_unpack(drsblobs.supplementalCredentialsBlob, supp)
        packages = [p for p in scb.sub.packages]
    except Exception as e:
        return False, False, "supplementalCredentials unreadable: %s (NTLM only)" % type(e).__name__, None
    has_cleartext = any(p.name == "Primary:CLEARTEXT" for p in packages)
    names = [p.name for p in packages]
    kerb = [p for p in packages if p.name in KERBEROS_PACKAGES]
    if not kerb:
        return has_cleartext, False, "no Kerberos package (have: %s) → NTLM only" % (", ".join(names) or "none"), None
    # prefer Newer-Keys; fall back to the older Primary:Kerberos
    for name in ("Primary:Kerberos-Newer-Keys", "Primary:Kerberos"):
        p = next((x for x in kerb if x.name == name), None)
        if p is None:
            continue
        try:
            data = p.data.decode() if isinstance(p.data, bytes) else p.data  # the package data is a hex string
            raw = bytes.fromhex(data)
            # both Primary:Kerberos (v3) and Primary:Kerberos-Newer-Keys (v4) unpack as this blob; the ctr union
            # resolves to ctr3/ctr4 by version once the real data sets it, and both carry .salt.string
            kp = ndr_unpack(drsblobs.package_PrimaryKerberosBlob, raw)
            salt = kp.ctr.salt.string or ""
        except Exception as e:
            return has_cleartext, False, "%s parse failed: %s → NTLM only" % (name, type(e).__name__), None
        if not salt:
            return has_cleartext, False, "%s has no salt → NTLM only" % name, None
        # the default salt is REALM + the principal name (no separator), so a transferable salt begins with the
        # destination realm on a component boundary. A plain substring/prefix test would wrongly accept a different
        # realm that merely contains it (criarenet.net.br, sub.criarenet.net, criarenet.network), so require the
        # source realm to equal the destination realm exactly (realm_salt_matches).
        if not realm_salt_matches(salt, realm, src_realm):
            return has_cleartext, False, "%s salt does not match realm %s → NTLM only" % (name, realm), None
        # transferable: rebuild the blob with only the allowlisted Kerberos packages. Samba's write path requires
        # the non-secret "Packages" index (a NUL-joined list of the Primary:* names) and checks
        # num_packages == num_names + 1, so rebuild that index to name only the kept Kerberos packages and drop
        # WDigest (and anything else) — nothing outside the allowlist is copied.
        try:
            kerb_names = [x.name for x in packages if x.name in KERBEROS_PACKAGES]
            index = next((x for x in packages if x.name == "Packages"), None)
            keep = [x for x in packages if x.name in KERBEROS_PACKAGES]
            if index is not None:
                index.data = "\0".join(kerb_names).encode("utf-16-le").hex().upper()
                keep.append(index)
            scb.sub.packages = keep
            scb.sub.num_packages = len(keep)
            kerb_blob = ndr_pack(scb)
        except Exception as e:
            return has_cleartext, False, "could not rebuild Kerberos-only blob: %s → NTLM only" % type(e).__name__, None
        return has_cleartext, True, None, kerb_blob
    return has_cleartext, False, "no usable Kerberos package → NTLM only", None


def write_material(samdb, dn, nt_hash, supp):
    """One atomic write of the password material by the bypass control: the NT hash and, when supp is given, the
    Kerberos keys verbatim; supp=None clears supplementalCredentials so stale keys (from the object import's
    generated password) cannot authenticate with the new NT hash. pwdLastSet is set non-zero."""
    import ldb
    import time
    from samba import dsdb
    msg = ldb.Message()
    msg.dn = ldb.Dn(samdb, dn)
    msg["unicodePwd"] = ldb.MessageElement(nt_hash, ldb.FLAG_MOD_REPLACE, "unicodePwd")
    if supp:
        msg["supplementalCredentials"] = ldb.MessageElement(supp, ldb.FLAG_MOD_REPLACE, "supplementalCredentials")
    else:
        msg["supplementalCredentials"] = ldb.MessageElement([], ldb.FLAG_MOD_REPLACE, "supplementalCredentials")
    # an explicit current FILETIME (non-zero): the bypass control skips the module's "-1 means now" conversion, so
    # "-1" would be stored verbatim as an invalid timestamp. This means "no change at next sign-in".
    now_ft = int((time.time() + 11644473600) * 10_000_000)
    msg["pwdLastSet"] = ldb.MessageElement(str(now_ft), ldb.FLAG_MOD_REPLACE, "pwdLastSet")
    # ldb takes an arbitrary control as "local_oid:<oid>:<critical>" (bare OIDs are rejected), as Samba's own tests do
    controls = ["local_oid:%s:0" % dsdb.DSDB_CONTROL_BYPASS_PASSWORD_HASH_OID]
    samdb.transaction_start()
    try:
        samdb.modify(msg, controls=controls)
        samdb.transaction_commit()
    except Exception:
        samdb.transaction_cancel()
        raise


def set_user(samdb, dn, nt_hash, supp, copy_kerberos):
    """Set the password material, returning (copied_kerberos, reason). When copying the source's Kerberos keys, the
    bypass path re-serializes the supplementalCredentials blob and requires it byte-identical (password_hash.c):
    a foreign blob that does not round-trip is rejected ("supplementalCredentialsBlob ... differ"). Rather than fail
    the user, retry with the NT hash only — the production password still works for NTLM/LDAP/Kerberos-less logon,
    and the Kerberos keys rebuild on the next password change. Any other write error is raised (→ error)."""
    if copy_kerberos and supp:
        try:
            write_material(samdb, dn, nt_hash, supp)
            return True, None
        except Exception as e:
            # Any failure writing the Kerberos blob (Samba's byte-identical memcmp, a key-count/salt validation,
            # etc.) falls back to the NT hash only: the production password still works for NTLM/LDAP logon and the
            # Kerberos keys rebuild on the next change. Only if the NT-only write ALSO fails is it a real error.
            why = reason_of(e)
            write_material(samdb, dn, nt_hash, None)
            return False, "Samba rejected the copied Kerberos keys (%s) → NTLM only" % why
    write_material(samdb, dn, nt_hash, None)
    return False, None


def current_state(samdb, dn):
    """The local user's current NT hash and stored supplementalCredentials, to detect a full 'same'. The NT hash is
    unreadable without privilege; a system-session search on the local sam.ldb returns it. Returns
    (nt_hash bytes|None, supp bytes|None). A successful Kerberos 'set' stores the source blob verbatim (Samba's
    memcmp requires it), so a later run can compare the stored blob to decide 'same'."""
    import ldb
    nt_hash = None
    supp = None
    try:
        res = samdb.search(base=dn, scope=ldb.SCOPE_BASE, attrs=["unicodePwd", "supplementalCredentials"])
        if "unicodePwd" in res[0]:
            nt_hash = bytes(res[0]["unicodePwd"][0])
        if "supplementalCredentials" in res[0] and len(res[0]["supplementalCredentials"][0]) > 0:
            supp = bytes(res[0]["supplementalCredentials"][0])
    except Exception:
        pass
    return nt_hash, supp


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
        src_realm = str(src.domain_dns_name())  # the source realm, to decide Kerberos transfer by exact realm match
        susers = source_users(src)
        for key in sorted(susers):
            u = susers[key]
            out = {"sam": u["sam"]}
            lu = local_user(samdb, u["sam"])
            if lu is None:
                out["outcome"], reason = "missing", "no matching user in this domain (import the objects first)"
            elif lu["protected"]:
                out["outcome"], reason = "builtin", "built-in or machine/trust account (skipped)"
            else:
                try:
                    out["outcome"], reason = apply_one(samdb, drs, drs_handle, src_inv, u, lu, realm, src_realm)
                except Exception as e:
                    # a missing replication right (access denied on GetNCChanges) is not a per-user problem — fail the
                    # whole operation once, clearly, with nothing written (spec: "Replication rights required")
                    if is_access_denied(e):
                        raise PwError("the source account lacks Replicating Directory Changes (All)", "rights")
                    # any other single-user failure (its own transaction already rolled back) never aborts the rest
                    out["outcome"], reason = "error", reason_of(e)
            if reason:
                out["reason"] = reason
            counts[out["outcome"]] = counts.get(out["outcome"], 0) + 1
            users.append(out)
    finally:
        drs_unbind(drs, drs_handle)
    return {"dest_sid": dom_sid, "source_sid": src_sid, "users": users, "counts": counts}


def apply_one(samdb, drs, drs_handle, src_inv, u, lu, realm, src_realm):
    """Extract one user's secrets and set them; returns (outcome, reason). reason is a short, secret-free note (no
    key, hash or salt value) explaining an ntlm_only/rejected classification, or None. Secrets live only in these
    locals and are discarded in the finally."""
    obj = get_ncchanges_secret(drs, drs_handle, src_inv, u["dn"], u["guid"])
    if obj is None:
        return "error", "source returned no object for this user"
    nt_hash, supp = decrypt_secrets(drs, obj, u["rid"])
    try:
        if nt_hash is None:
            return "error", "no NT hash in the replicated object"
        has_cleartext, kerberos_ok, kerb_reason, kerb_blob = inspect_supplemental(supp, realm, src_realm)
        if has_cleartext:
            return "rejected", "source stores a reversible/cleartext password (Primary:CLEARTEXT)"
        # full-state idempotency: skip when pwdLastSet is non-zero, the NT hash matches, and the stored
        # supplementalCredentials already equals the intended state — for a Kerberos user that means the stored
        # blob equals the source blob (a successful 'set' stores it verbatim), and for an NTLM-only user it means
        # no Kerberos keys are present (none to add, none stale).
        if lu["pwd_last_set"] != 0:
            cur_hash, cur_supp = current_state(samdb, lu["dn"])
            if cur_hash is not None and cur_hash == nt_hash:
                # already migrated and matching:
                #  - set: the dest stores exactly the rebuilt Kerberos blob;
                #  - ntlm_only (terminal): the NT hash matches and the dest has no Kerberos — either there were no
                #    transferable keys, or Samba rejected them before (deterministic), so a re-apply would not
                #    change the outcome. Either way nothing to write.
                if kerberos_ok and cur_supp == kerb_blob:
                    return "same", None
                if not cur_supp:
                    return "same", None
        copied, fb_reason = set_user(samdb, lu["dn"], nt_hash, kerb_blob if kerberos_ok else None, kerberos_ok)
        if copied:
            return "set", None
        # not copied: either we never had transferable keys, or Samba rejected the blob and we fell back to NT-only
        return "ntlm_only", fb_reason or kerb_reason
    finally:
        del nt_hash
        del supp


# Allowlisted, secret-free descriptions keyed by a marker that may appear in a Samba/ldb error message. The reason
# emitted to the API/journal is ONLY one of these descriptions plus the exception class name — never the raw
# message, so no hash/key/salt/password (however it was quoted or encoded) can ride along.
_KNOWN_ERRORS = (
    ("supplementalcredentialsblob", "Samba rejected the supplementalCredentials blob"),
    ("supplementalcredentials", "Samba rejected the supplementalCredentials blob"),
    ("memcmp", "Samba rejected the supplementalCredentials blob"),
    ("num_names", "supplementalCredentials package index mismatch"),
    ("num_packages", "supplementalCredentials package index mismatch"),
    ("primary:packages", "supplementalCredentials package index missing"),
    ("num_keys", "unexpected Kerberos key count"),
    ("kerberos", "Kerberos package error"),
    ("logon_failure", "source authentication failed"),
    ("access_denied", "access denied by the source"),
    ("werr_access", "access denied by the source"),
    ("werr_ds_dra", "directory replication error"),
    ("constraint", "directory constraint violation"),
)


def is_access_denied(e):
    """True when a DRS error means the source account lacks the replication right (Replicating Directory Changes
    All) — a single up-front refusal is clearer than marking every user 'error'."""
    try:
        text = " ".join(str(a) for a in getattr(e, "args", ()) if isinstance(a, str)).upper()
    except Exception:
        return False
    return "ACCESS_DENIED" in text or "WERR_DS_DRA_ACCESS_DENIED" in text or "NT_STATUS_ACCESS_DENIED" in text


def reason_of(e):
    """A short, secret-free reason for a per-user failure: an allowlisted description (matched by a marker in the
    error text) plus the exception class name. The raw exception text is never forwarded, so no hash, key, salt or
    password can leak into the reason regardless of how a Samba/ldb message quoted it."""
    name = type(e).__name__
    text = ""
    try:
        text = " ".join(str(a) for a in getattr(e, "args", ()) if isinstance(a, str)).lower()
    except Exception:
        text = ""
    for marker, desc in _KNOWN_ERRORS:
        if marker in text:
            return "%s (%s)" % (desc, name)
    return name


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
