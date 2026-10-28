package sambadc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"ad-manager/internal/gosamba/drs"
	"ad-manager/internal/gosamba/suppcreds"
)

// PwSource is the source domain to read the password material from.
type PwSource struct {
	Host string `json:"host"`
	User string `json:"user"`
}

// PwUser is one user's outcome. Outcome is one of: would_set (plan only), set,
// ntlm_only, same, missing, builtin, no_password, rejected, error.
type PwUser struct {
	Sam     string `json:"sam"`
	Outcome string `json:"outcome"`
	// Reason is a short, secret-free explanation of the outcome (e.g. why a user is ntlm_only or error); it never
	// contains a key, hash or salt or password.
	Reason string `json:"reason,omitempty"`
}

// PwResult is the result of a password-material plan or apply: the domain SIDs
// (to confirm source ≠ destination and the pin) and the per-user outcomes with
// their counts. It never carries any password material.
type PwResult struct {
	DestSID   string         `json:"dest_sid"`
	SourceSID string         `json:"source_sid"`
	Users     []PwUser       `json:"users"`
	Counts    map[string]int `json:"counts"`
}

// ErrRights is the single refusal when the source account lacks "Replicating Directory Changes (All)": nothing is
// written then.
var ErrRights = errors.New("the source account lacks Replicating Directory Changes (All)")

// Built-in accounts (Administrator, Guest, krbtgt) and the machine and trust account types are never migrated.
var protectedRIDs = map[uint32]bool{500: true, 501: true, 502: true}

const (
	samNormalUser = 0x30000000 // SAM_NORMAL_USER_ACCOUNT (805306368)
	samMachine    = 0x30000001
	samTrust      = 0x30000002
	samInterdom   = 0x30000003
)

// srcUser is one ordinary user of the source (built-ins left out by RID).
type srcUser struct {
	Sam  string
	GUID []byte // objectGUID as LDAP returns it
}

// localUser is the destination's entry for a sAMAccountName.
type localUser struct {
	DN         string
	Protected  bool // built-in RID or a machine/trust account
	PwdLastSet int64
}

// source is the open source domain: its SID, realm and users, and its secrets over DRSUAPI.
type source interface {
	SID() string
	Realm() string
	Users() []srcUser
	Secrets(ctx context.Context, guid []byte) (*drs.Secrets, error)
	Close()
}

// pwEnv is everything MigratePasswords touches outside its own logic: the local directory (through Samba's ldb
// tools) and the source.
type pwEnv interface {
	localDomain(ctx context.Context) (sid, realm, base string, err error)
	localUsers(ctx context.Context, base string) (map[string]localUser, error)
	currentState(ctx context.Context, dn string) (nt, supp []byte, err error)
	write(ctx context.Context, dn string, nt, supp []byte, pwdLastSet int64) error
	openSource(ctx context.Context, src PwSource, password string) (source, error)
}

// MigratePasswords plans (apply=false) or applies (apply=true) the password-material migration on this domain
// controller, in Go: the source's secrets are read over a sealed, read-only DRSUAPI (gosamba/drs) and written to the
// local sam.ldb by Samba's ldbmodify with the password-hash bypass control, the material only on its standard input.
//
// destSID pins the destination domain and is required: it must match this DC and differ from the source. The plan
// reads no secret. Built-ins, machine and trust accounts are never set and no user is created; the source is never
// written. The source password is used only for the source binds.
func MigratePasswords(ctx context.Context, apply bool, src PwSource, srcPassword, destSID string) (*PwResult, error) {
	env, err := newLdbEnv(ctx)
	if err != nil {
		return nil, fmt.Errorf("password migration: %w", err)
	}
	return migrate(ctx, env, apply, src, srcPassword, destSID)
}

func migrate(ctx context.Context, env pwEnv, apply bool, src PwSource, srcPassword, destSID string) (*PwResult, error) {
	if destSID == "" {
		return nil, errors.New("password migration: destination domain SID (dest_sid) is required")
	}
	domSID, realm, base, err := env.localDomain(ctx)
	if err != nil {
		return nil, fmt.Errorf("password migration: local directory: %w", err)
	}
	if destSID != domSID {
		return nil, errors.New("password migration: destination does not match the pinned domain SID")
	}
	s, err := env.openSource(ctx, src, srcPassword)
	if err != nil {
		return nil, fmt.Errorf("password migration: source: %w", err)
	}
	defer s.Close()
	if s.SID() == domSID {
		return nil, errors.New("password migration: source and destination are the same domain")
	}
	local, err := env.localUsers(ctx, base)
	if err != nil {
		return nil, fmt.Errorf("password migration: local directory: %w", err)
	}
	users := s.Users()
	sort.Slice(users, func(i, j int) bool { return strings.ToLower(users[i].Sam) < strings.ToLower(users[j].Sam) })
	res := &PwResult{DestSID: domSID, SourceSID: s.SID(), Counts: map[string]int{}}
	run := &applyRun{env: env, src: s, realm: realm, destSID: domSID}
	for _, u := range users {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("password migration: stopped after %d of %d users: %w", len(res.Users), len(users), err)
		}
		out := PwUser{Sam: u.Sam}
		lu, ok := local[strings.ToLower(u.Sam)]
		switch {
		case !ok:
			out.Outcome, out.Reason = "missing", "no matching user in this domain (import the objects first)"
		case lu.Protected:
			out.Outcome, out.Reason = "builtin", "built-in or machine/trust account (skipped)"
		case !apply:
			out.Outcome = "would_set"
		default:
			out.Outcome, out.Reason, err = run.one(ctx, u, lu)
			if err != nil {
				return nil, err // missing rights, a tampered listing or a cancelled run stop it; users already set stay set
			}
		}
		res.Users = append(res.Users, out)
		res.Counts[out.Outcome]++
	}
	return res, nil
}

// applyRun carries what every user of one apply shares.
type applyRun struct {
	env     pwEnv
	src     source
	realm   string
	destSID string
	read    bool // a secret was read already: a later refusal is that user's, not missing rights
}

// one reads one user's secrets and sets them: (outcome, reason). A failure of this user alone is its outcome
// ("error"). Missing replication rights (seen before any read succeeded), a replicated object that is not the one the
// listing named, and a cancelled run are returned as errors and stop the run.
func (r *applyRun) one(ctx context.Context, u srcUser, lu localUser) (string, string, error) {
	sec, err := r.src.Secrets(ctx, u.GUID)
	if err != nil {
		// cancelled, or the reader's own timeout (it closed its sockets): no later user can be read either
		if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return "", "", fmt.Errorf("password migration: stopped at %s: %w", u.Sam, err)
		}
		if !r.read && isRightsError(err) {
			return "", "", ErrRights
		}
		return "error", reasonOf(err), nil
	}
	defer sec.Zero()
	r.read = true
	// the LDAP listing (names, GUIDs, the source SID) is not integrity-protected; the sealed DRS reply is: the object
	// must be this account of the source domain, never one of this domain
	rid, _ := ridOf(sec.SID)
	if !strings.EqualFold(sec.SAMAccountName, u.Sam) || domainOf(sec.SID) != r.src.SID() || domainOf(sec.SID) == r.destSID ||
		protectedRIDs[rid] || sec.SAMAccountType != samNormalUser {
		return "", "", errors.New("password migration: the source's replicated object does not match its LDAP listing; stopped")
	}
	if sec.NTHash == nil {
		// an account created without a password (e.g. dsadd without -pwd): the target keeps the one it was created with
		return "no_password", "the source user has no password", nil
	}
	in := suppcreds.Inspect(sec.Supplemental, r.realm, r.src.Realm())
	defer clear(in.Rebuilt)
	if in.Cleartext {
		return "rejected", "source stores a reversible/cleartext password (Primary:CLEARTEXT)", nil
	}
	// idempotent: pwdLastSet non-zero, the NT hash already the same, and the stored Kerberos state already the
	// intended one (the rebuilt blob, stored verbatim by a successful set) or none (an NTLM-only user, including one
	// whose keys Samba refused before: the outcome would not change)
	if lu.PwdLastSet != 0 {
		nt, supp, err := r.env.currentState(ctx, lu.DN)
		if err != nil { // unknown state: no write on a guess (it would reset pwdLastSet)
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			return "error", "the user's current state is unreadable", nil
		}
		same := nt != nil && equalBytes(nt, sec.NTHash) && ((in.Kerberos && equalBytes(supp, in.Rebuilt)) || len(supp) == 0)
		clear(nt)
		clear(supp)
		if same {
			return "same", "", nil
		}
	}
	now := fileTimeNow()
	if in.Kerberos {
		err := r.env.write(ctx, lu.DN, sec.NTHash, in.Rebuilt, now)
		if err == nil {
			return "set", "", nil
		}
		if ctx.Err() != nil { // a write cut short: no second write over an uncertain one
			return "", "", ctx.Err()
		}
		// Samba re-serializes the blob and refuses one that differs (or a key count/salt it does not accept): the NT
		// hash alone still lets the production password work, and the Kerberos keys rebuild on the next change
		if err := r.env.write(ctx, lu.DN, sec.NTHash, nil, now); err != nil {
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			return "error", reasonOf(err), nil
		}
		return "ntlm_only", "Samba rejected the copied Kerberos keys (" + reasonOf(err) + ") → NTLM only", nil
	}
	if err := r.env.write(ctx, lu.DN, sec.NTHash, nil, now); err != nil {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		return "error", reasonOf(err), nil
	}
	return "ntlm_only", in.Reason, nil
}

// ridOf is the last sub-authority of a SID in its text form.
func ridOf(sid string) (uint32, bool) {
	i := strings.LastIndex(sid, "-")
	if !strings.HasPrefix(sid, "S-1-") || i < 0 {
		return 0, false
	}
	v, err := strconv.ParseUint(sid[i+1:], 10, 32)
	return uint32(v), err == nil
}

// domainOf is the domain part of an account's SID (without its RID).
func domainOf(sid string) string {
	if i := strings.LastIndex(sid, "-"); i > 0 {
		return sid[:i]
	}
	return ""
}

// fileTimeNow is the current time as a FILETIME: the bypass control skips the "-1 means now" conversion, so the
// value is explicit and non-zero (no change at next sign-in).
var fileTimeNow = func() int64 {
	t := time.Now()
	return (t.Unix()+11644473600)*10_000_000 + int64(t.Nanosecond()/100)
}

func equalBytes(a, b []byte) bool { return len(a) == len(b) && string(a) == string(b) }

// isRightsError: a refusal of the replication itself (access denied, or Windows' "bad DN" for an object the account
// can read over LDAP) — counted as missing rights only while no secret has been read yet.
func isRightsError(err error) bool {
	t := strings.ToUpper(err.Error())
	return strings.Contains(t, "ACCESS_DENIED") || strings.Contains(t, "0X00002105") || strings.Contains(t, "0X2105") ||
		strings.Contains(t, "ERROR_DS_DRA_BAD_DN") || strings.Contains(t, "0X000020F7")
}

// knownErrors: allowlisted, secret-free descriptions keyed by a marker of a Samba/ldb/RPC error; the raw text is
// never forwarded, so no hash, key, salt or password can ride along in a reason.
var knownErrors = []struct{ marker, desc string }{
	{"supplementalcredentialsblob", "Samba rejected the supplementalCredentials blob"},
	{"supplementalcredentials", "Samba rejected the supplementalCredentials blob"},
	{"memcmp", "Samba rejected the supplementalCredentials blob"},
	{"num_names", "supplementalCredentials package index mismatch"},
	{"num_packages", "supplementalCredentials package index mismatch"},
	{"primary:packages", "supplementalCredentials package index missing"},
	{"num_keys", "unexpected Kerberos key count"},
	{"kerberos", "Kerberos package error"},
	{"logon_failure", "source authentication failed"},
	{"access_denied", "access denied by the source"},
	{"werr_access", "access denied by the source"},
	{"werr_ds_dra", "directory replication error"},
	{"drsgetncchanges", "directory replication error"},
	{"constraint", "directory constraint violation"},
	{"deadline exceeded", "timed out"},
}

func reasonOf(err error) string {
	t := strings.ToLower(err.Error())
	for _, k := range knownErrors {
		if strings.Contains(t, k.marker) {
			return k.desc
		}
	}
	return "write or read failed (see the agent's journal)"
}
