# Regenerates ntacl_corpus.json: xattr_NTACL v4 blobs packed by Samba's own NDR (python3-samba) and what the helper's
# describe() answers for each (after get_nt_acl's filter: no SACL). TestNTACLCorpus compares the Go reader with it.
#   python3 -I internal/sambabridge/testdata/gen_ntacl_corpus.py > internal/sambabridge/testdata/ntacl_corpus.json
import json
import random
from samba.dcerpc import security, xattr
from samba.ndr import ndr_pack

FULL, MODIFY, READ_EXEC = 0x1F01FF, 0x1301BF, 0x1200A9
DOM = "S-1-5-21-1004336348-1177238915-682003330"
SIDS = ["S-1-4294967295-1", "S-1-0x123456789abc-7", "SY", "BA", "BU", "WD", "CO", "AU", "S-1-5-32-580", "S-1-16-12288", DOM + "-512", DOM + "-513", DOM + "-1105",
        "S-1-22-1-0", "S-1-22-2-3000000", "S-1-5-21-1-2-3-4-5-6-7-8-9-10-11-12-13-14"]
MASKS = ["FA", "FR", "FW", "FX", "0x1301bf", "0x1200a9", "GA", "GR", "GX", "GW", "RCSD", "WDWO", "CCDCLC", "0x100000",
         "0x1f01ff", "0x10000", "0x1"]
FLAGS = ["", "OI", "CI", "OICI", "OICIID", "ID", "OICIIO", "CIIONP", "NP", "OICINPID"]


def describe(sd):
    aces = []
    for ace in (sd.dacl.aces if sd.dacl else []):
        kind = {security.SEC_ACE_TYPE_ACCESS_ALLOWED: "allow", security.SEC_ACE_TYPE_ACCESS_DENIED: "deny"}.get(ace.type, str(ace.type))
        aces.append({"sid": str(ace.trustee), "type": kind, "mask": ace.access_mask, "flags": ace.flags,
                     "inherited": bool(ace.flags & security.SEC_ACE_FLAG_INHERITED_ACE),
                     "access": {FULL: "full", MODIFY: "rw", READ_EXEC: "ro"}.get(ace.access_mask, "custom")})
    return {"owner": str(sd.owner_sid), "group": str(sd.group_sid), "sddl": sd.as_sddl(), "aces": aces,
            "protected": bool(sd.type & security.SEC_DESC_DACL_PROTECTED), "null_dacl": sd.dacl is None}


def blob(sd, desc="posix_acl"):
    x = xattr.NTACL()
    x.version = 4
    h = xattr.security_descriptor_hash_v4()
    h.sd = sd
    h.hash_type = 1
    h.hash = [random.randrange(256) for _ in range(32)] + [0] * 32
    h.description = desc
    h.time = 0
    h.sys_acl_hash = [random.randrange(256) for _ in range(32)] + [0] * 32
    x.info = h
    return ndr_pack(x), bytes(h.sys_acl_hash[:32])


random.seed(7)
out = []
sddls = ["O:BAG:SYD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)", "NULL-DACL", "DACL-NOT-PRESENT", "O:BAG:SY",
         "O:%s-512G:%s-513D:AI(D;OICI;0x1301bf;;;%s-1105)(A;OICIID;FA;;;SY)" % (DOM, DOM, DOM),
         "O:BAG:BAD:ARAI(A;;0x0;;;WD)"]
for _ in range(120):
    ctl = random.choice(["", "P", "AI", "PAI", "ARAI", "PARAI"])
    aces = "".join("(%s;%s;%s;;;%s)" % (random.choice("AAAD"), random.choice(FLAGS), random.choice(MASKS), random.choice(SIDS))
                   for _ in range(random.randrange(0, 9)))
    sddls.append("O:%sG:%sD:%s%s" % (random.choice(SIDS), random.choice(SIDS), ctl, aces))


def parse(s):
    if s == "NULL-DACL":  # DACL present, but NULL (everyone has access)
        sd = security.descriptor.from_sddl("O:BAG:SY", security.dom_sid(DOM))
        sd.type |= security.SEC_DESC_DACL_PRESENT
        return sd
    if s == "DACL-NOT-PRESENT":  # an ACL stored without the present flag
        sd = security.descriptor.from_sddl("O:BAG:SYD:(A;;FA;;;WD)", security.dom_sid(DOM))
        sd.type &= ~security.SEC_DESC_DACL_PRESENT
        return sd
    return security.descriptor.from_sddl(s, security.dom_sid(DOM))


for i, s in enumerate(sddls):
    sd = parse(s)
    b, sys_hash = blob(sd, "posix_acl" if i % 5 else "a longer description, odd length")
    if sd.sacl is not None:
        continue  # a SACL: the Go reader leaves those to the helper
    filtered = parse(s)
    if filtered.dacl is not None:  # make_sec_desc on the way out of the blob (vfs_acl_common.c parse_acl_blob)
        filtered.type |= security.SEC_DESC_DACL_PRESENT
    out.append({"blob": b.hex(), "sys_acl_hash": sys_hash.hex(), "acl": describe(filtered)})
print(json.dumps(out, indent=0))
