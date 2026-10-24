//! The page rules that do not need a GUI: labels, guards, filters, formats and the password generator — the same
//! rules as the Fyne app's pages, kept here so they are unit-tested once.

use std::sync::OnceLock;

use chrono::{DateTime, Datelike, Local, Utc};
use regex::Regex;

use crate::i18n::{self, t};
use crate::model::{Computer, Me, OrgUnit, SyncRun, User, is_set};
use crate::tf;

/// The color role of a text on screen (the pages draw with these, the UI maps them to the theme).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum Tone {
    #[default]
    Normal,
    Strong,
    Muted,
    Ok,
    Err,
    Warn,
}

/// The configured "no logon for N days" limit (90 when unset).
pub fn inactive_days(me: &Me) -> i64 {
    if me.settings.inactive_days > 0 { me.settings.inactive_days } else { 90 }
}

/// An enabled account without logon for more than `days` days (never logged on counts), as the web panel.
pub fn inactive(u: &User, days: i64, now: DateTime<Utc>) -> bool {
    u.enabled && (!is_set(&u.last_logon) || u.last_logon.is_some_and(|l| (now - l).num_hours() / 24 > days))
}

/// The status label of an account and its tone.
pub fn user_status(u: &User) -> (String, Tone) {
    if !u.enabled {
        (t("deactivated").to_uppercase(), Tone::Muted)
    } else if u.locked {
        (t("locked").to_uppercase(), Tone::Err)
    } else {
        (t("active").to_uppercase(), Tone::Ok)
    }
}

/// Built-in accounts and the operator's own login are not disabled, moved or deleted here (the panel refuses to
/// disable or delete them anyway), as the web Users page.
pub fn editable(u: &User, me: &Me) -> bool {
    !u.protected && !same_login(&u.sam, &me.user)
}

/// Logins compared case-insensitively, accented letters too (JOSÉ is josé), as Go's strings.EqualFold.
pub fn same_login(a: &str, b: &str) -> bool {
    a.chars().flat_map(char::to_lowercase).eq(b.chars().flat_map(char::to_lowercase))
}

/// The Users page filters, in the order of its select.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub enum UserFilter {
    #[default]
    All,
    Enabled,
    Disabled,
    Locked,
    Inactive,
}

impl UserFilter {
    pub const ALL: [UserFilter; 5] = [Self::All, Self::Enabled, Self::Disabled, Self::Locked, Self::Inactive];

    pub fn label(self, days: i64) -> String {
        match self {
            Self::All => t("All").into(),
            Self::Enabled => t("Enabled").into(),
            Self::Disabled => t("Disabled").into(),
            Self::Locked => t("Locked").into(),
            Self::Inactive => tf!("Inactive for %d+ days", days),
        }
    }

    pub fn keeps(self, u: &User, days: i64, now: DateTime<Utc>) -> bool {
        match self {
            Self::All => true,
            Self::Enabled => u.enabled,
            Self::Disabled => !u.enabled,
            Self::Locked => u.locked,
            Self::Inactive => inactive(u, days, now),
        }
    }
}

/// Whether the account matches the search text (login, name, e-mail or department, case-insensitive).
pub fn user_matches(u: &User, query: &str) -> bool {
    let q = query.trim().to_lowercase();
    q.is_empty() || format!("{} {} {} {}", u.sam, u.name, u.mail, u.department).to_lowercase().contains(&q)
}

/// The indexes of the accounts to list, sorted as the list is (by login).
pub fn filter_users(all: &[User], filter: UserFilter, query: &str, days: i64, now: DateTime<Utc>) -> Vec<usize> {
    (0..all.len()).filter(|&i| filter.keeps(&all[i], days, now) && user_matches(&all[i], query)).collect()
}

/// What can be done to an account from its row, in the order of the Enter menu.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum UserAction {
    Edit,
    Password,
    Copy,
    Unlock,
    Toggle,
    Move,
    Delete,
}

impl UserAction {
    /// The Enter menu label (Toggle says what it does to this account).
    pub fn label(self, u: &User) -> &'static str {
        match self {
            Self::Edit => t("Edit"),
            Self::Password => t("Password"),
            Self::Copy => t("Copy"),
            Self::Unlock => t("Unlock"),
            Self::Toggle if u.enabled => t("Disable"),
            Self::Toggle => t("Enable"),
            Self::Move => t("Move"),
            Self::Delete => t("Delete"),
        }
    }
}

/// The actions a row offers: edit, password and copy always; unlock when locked; toggle, move and delete when
/// editable.
pub fn user_actions(u: &User, me: &Me) -> Vec<UserAction> {
    let mut a = vec![UserAction::Edit, UserAction::Password, UserAction::Copy];
    if u.locked {
        a.push(UserAction::Unlock);
    }
    if editable(u, me) {
        a.extend([UserAction::Toggle, UserAction::Move, UserAction::Delete]);
    }
    a
}

/// An OU or default container's DN as a readable path without the domain: "OU=Tatuape,OU=SP,DC=x" → "SP / Tatuape".
pub fn ou_path(dn: &str) -> String {
    let mut parts: Vec<&str> = split_dn(dn)
        .into_iter()
        .filter_map(|rdn| {
            let (k, v) = rdn.trim().split_once('=')?;
            (!k.eq_ignore_ascii_case("DC")).then_some(v)
        })
        .collect();
    parts.reverse();
    parts.join(" / ")
}

/// The name of the OU or container that holds an object, as the web's Users column: the first RDN after the object's
/// own that is not a DC, or "—".
pub fn ou_name(dn: &str) -> String {
    split_dn(dn)
        .into_iter()
        .skip(1)
        .filter_map(|rdn| rdn.trim().split_once('='))
        .find(|(k, _)| !k.eq_ignore_ascii_case("DC"))
        .map_or_else(|| "—".to_string(), |(_, v)| v.to_string())
}

/// The RDNs of a DN, keeping escaped commas inside their value.
fn split_dn(dn: &str) -> Vec<&str> {
    let mut out = Vec::new();
    let (mut start, mut esc) = (0, false);
    for (i, c) in dn.char_indices() {
        match c {
            _ if esc => esc = false,
            '\\' => esc = true,
            ',' => {
                out.push(&dn[start..i]);
                start = i + 1;
            }
            _ => {}
        }
    }
    out.push(&dn[start..]);
    out
}

/// The DN of the container of dn, skipping escaped commas ("CN=Silva\, Joao,OU=SP,…" → "OU=SP,…").
pub fn parent_dn(dn: &str) -> &str {
    let mut esc = false;
    for (i, c) in dn.char_indices() {
        match c {
            _ if esc => esc = false,
            '\\' => esc = true,
            ',' => return &dn[i + 1..],
            _ => {}
        }
    }
    ""
}

/// An OU, as opposed to a default container (CN=Users, CN=Computers).
pub fn is_ou(dn: &str) -> bool {
    dn.to_uppercase().starts_with("OU=")
}

/// An OU that may be offered for deletion: an OU (not a container) other than Domain Controllers.
pub fn ou_deletable(o: &OrgUnit) -> bool {
    is_ou(&o.dn) && !o.name.eq_ignore_ascii_case("Domain Controllers")
}

/// The DN of the default "Users" container ("" if absent).
pub fn users_container(ous: &[OrgUnit]) -> String {
    ous.iter().find(|o| o.name == "Users").map(|o| o.dn.clone()).unwrap_or_default()
}

/// OUs sorted by readable path (as the Fyne app's fetchOUs).
pub fn sort_ous(ous: &mut [OrgUnit]) {
    ous.sort_by_key(|o| ou_path(&o.dn));
}

/// A size in B/KB/MB/GB/TB with one decimal (decimal comma in Portuguese).
pub fn human_bytes(n: i64) -> String {
    const UNITS: [&str; 5] = ["B", "KB", "MB", "GB", "TB"];
    let (mut v, mut i) = (n as f64, 0);
    while v >= 1024.0 && i < UNITS.len() - 1 {
        v /= 1024.0;
        i += 1;
    }
    let mut s = if i == 0 {
        format!("{v:.0}")
    } else {
        let s = format!("{v:.1}");
        s.strip_suffix(".0").map(str::to_string).unwrap_or(s)
    };
    if i18n::lang() != "en" {
        s = s.replace('.', ",");
    }
    format!("{s} {}", UNITS[i])
}

/// A size in KiB/MiB/GiB (backups, sync).
pub fn size_text(n: i64) -> String {
    if n >= 1 << 30 {
        format!("{:.1} GiB", n as f64 / f64::from(1u32 << 30))
    } else if n >= 1 << 20 {
        format!("{:.1} MiB", n as f64 / f64::from(1u32 << 20))
    } else {
        format!("{} KiB", (n + 1023) / 1024)
    }
}

/// Systems without support (NTLMv1/SMB1/Netlogon risks), as in the web panel.
pub fn legacy_os(os: &str) -> bool {
    static RE: OnceLock<Option<Regex>> = OnceLock::new();
    RE.get_or_init(|| Regex::new(r"XP|2003|Windows Server® 2008 Standard|Windows 7").ok())
        .as_ref()
        .is_some_and(|r| r.is_match(os))
}

/// A computer with no logon for more than `days` days (never counts).
pub fn computer_inactive(c: &Computer, days: i64, now: DateTime<Utc>) -> bool {
    match c.last_logon {
        Some(l) if is_set(&c.last_logon) => (now - l).num_hours() > days * 24,
        _ => true,
    }
}

/// The OS label of a computer ("not reported" when empty).
pub fn os_label(os: &str) -> String {
    if os.is_empty() { t("not reported").into() } else { os.into() }
}

const PW_CLASSES: [&str; 4] = ["ABCDEFGHJKLMNPQRSTUVWXYZ", "abcdefghijkmnopqrstuvwxyz", "23456789", "@#%+=!?"];

/// A uniform random number below `upper` from the OS generator (rejection sampling, no modulo bias).
fn rnd(upper: usize) -> Result<usize, getrandom::Error> {
    let upper = upper.max(1) as u64;
    let zone = u64::MAX - (u64::MAX % upper);
    loop {
        let mut b = [0u8; 8];
        getrandom::fill(&mut b)?;
        let v = u64::from_le_bytes(b);
        if v < zone {
            return Ok((v % upper) as usize);
        }
    }
}

/// `n` random characters with at least one of each class (upper, lower, digit, symbol), which satisfies AD password
/// complexity; ambiguous characters (I, O, l, 0, 1) are left out.
pub fn generate_password(n: usize) -> Result<String, getrandom::Error> {
    let all: Vec<u8> = PW_CLASSES.concat().into_bytes();
    let mut b: Vec<u8> = Vec::with_capacity(n.max(PW_CLASSES.len()));
    for c in PW_CLASSES {
        b.push(c.as_bytes()[rnd(c.len())?]);
    }
    while b.len() < n {
        b.push(all[rnd(all.len())?]);
    }
    for i in (1..b.len()).rev() {
        b.swap(i, rnd(i + 1)?);
    }
    Ok(b.into_iter().map(char::from).collect())
}

/// The password length the panel's Settings ask for (14 when unset).
pub fn password_length(me: &Me) -> usize {
    usize::try_from(me.settings.password_length).ok().filter(|&n| n > 0).unwrap_or(14)
}

/// "a, b ,c" → its non-empty trimmed items.
pub fn split_list(s: &str) -> Vec<String> {
    s.split(',').map(str::trim).filter(|g| !g.is_empty()).map(str::to_string).collect()
}

/// A run's length, short: 38s, 4m38s, 1h02m.
pub fn short_duration(secs: i64) -> String {
    let n = secs.max(0);
    let (h, m) = (n / 3600, n % 3600 / 60);
    if h > 0 {
        format!("{h}h{m:02}m")
    } else if m > 0 {
        format!("{m}m{:02}s", n % 60)
    } else {
        format!("{n}s")
    }
}

/// When a run ended (with the date when not the day it started) and how long it took; "—" while it goes on.
pub fn sync_finished(x: &SyncRun) -> String {
    let (Some(f), true) = (x.finished, is_set(&x.finished)) else { return "—".into() };
    if x.status == "running" || x.status == "queued" {
        return "—".into();
    }
    let fl = f.with_timezone(&Local);
    let mut at = if i18n::lang() == "en" { fl.format("%I:%M %p").to_string() } else { fl.format("%H:%M").to_string() };
    let Some(s) = x.started.filter(|_| is_set(&x.started)) else { return at };
    let sl = s.with_timezone(&Local);
    if sl.year() != fl.year() || sl.ordinal() != fl.ordinal() {
        at = fl.format(i18n::date_layout()).to_string();
    }
    format!("{at} · {}", short_duration((f - s).num_seconds()))
}

/// A sync/backup status label and its tone.
pub fn run_status(s: &str) -> (&'static str, Tone) {
    match s {
        "ok" => (t("ok"), Tone::Ok),
        "error" => (t("error"), Tone::Err),
        "cancelled" => (t("cancelled"), Tone::Muted),
        "queued" => (t("queued"), Tone::Warn),
        _ => (t("running"), Tone::Warn),
    }
}

pub fn backup_kind_name(k: &str) -> String {
    match k {
        "dc" => t("Domain controller").into(),
        "fileserver-config" => t("Configuration and permissions").into(),
        "fileserver-data" => t("Data folders").into(),
        "panel" => t("Panel").into(),
        "samba-files" => t("Samba files (before the setup)").into(),
        "all" => t("Complete (configuration + all share roots, full)").into(),
        _ => k.into(),
    }
}

/// Where a backup came from.
pub fn backup_origin(o: &str) -> String {
    match o {
        "manual" => t("manual").into(),
        "upload" => t("sent").into(),
        "pre-restore" => t("before a restoration").into(),
        "setup" => t("before the setup").into(),
        _ => match o.strip_prefix("schedule:") {
            Some(id) => format!("{} {id}", t("schedule")),
            None => o.into(),
        },
    }
}

/// The code a destructive backup action asks to type: the last 8 characters of the id.
pub fn backup_code(id: &str) -> &str {
    let n = id.chars().count();
    let skip = n.saturating_sub(8);
    id.char_indices().nth(skip).map_or(id, |(i, _)| &id[i..])
}

/// A sync job id from its name: lowercase, runs of other characters as "-", at most 32 characters.
pub fn slug(s: &str) -> String {
    let mut out = String::new();
    let mut dash = false;
    for c in s.to_lowercase().chars() {
        if c.is_ascii_lowercase() || c.is_ascii_digit() {
            out.push(c);
            dash = false;
        } else if !dash {
            out.push('-');
            dash = true;
        }
    }
    let s = out.trim_matches('-');
    s.chars().take(32).collect()
}

/// The UNC of a sync source (\\host\share\path).
pub fn unc(host: &str, share: &str, path: &str) -> String {
    let mut s = format!(r"\\{host}");
    if !share.is_empty() {
        s.push('\\');
        s.push_str(share);
    }
    if !path.is_empty() {
        s.push('\\');
        s.push_str(&path.replace('/', "\\"));
    }
    s
}

/// The default share folder for a new share name: /srv/shares/<lowercase, spaces as dashes>.
pub fn share_folder(name: &str) -> String {
    format!("/srv/shares/{}", name.trim().replace(' ', "-").to_lowercase())
}

#[cfg(test)]
mod tests {

    #[test]
    fn ou_name_as_the_web() {
        assert_eq!(super::ou_name("CN=Ana Costa,OU=IT,OU=Example,DC=domain,DC=local"), "IT");
        assert_eq!(super::ou_name("CN=Administrator,CN=Users,DC=domain,DC=local"), "Users");
        assert_eq!(super::ou_name("CN=x,DC=domain,DC=local"), "—");
    }
    use super::*;
    use chrono::TimeZone;

    fn user(sam: &str) -> User {
        User { sam: sam.into(), name: sam.to_uppercase(), enabled: true, ..Default::default() }
    }

    fn me(login: &str) -> Me {
        Me { user: login.into(), ..Default::default() }
    }

    #[test]
    fn editable_and_actions() {
        let _g = crate::i18n::tests::english();
        let mut admin = user("Administrator");
        admin.protected = true;
        admin.locked = true;
        assert!(!editable(&admin, &me("ana")));
        assert_eq!(
            user_actions(&admin, &me("ana")),
            [UserAction::Edit, UserAction::Password, UserAction::Copy, UserAction::Unlock]
        );
        // the operator's own login, any case, accented letters too
        assert!(!editable(&user("ANA"), &me("ana")));
        assert!(!editable(&user("josé"), &me("JOSÉ")));
        let bob = user("bob");
        assert_eq!(user_actions(&bob, &me("ana")).len(), 6);
        assert_eq!(UserAction::Toggle.label(&bob), "Disable");
    }

    #[test]
    fn filters() {
        let now = Utc.with_ymd_and_hms(2026, 10, 1, 0, 0, 0).unwrap();
        let mut a = user("ana");
        a.mail = "ana@domain.local".into();
        a.last_logon = Some(now - chrono::Duration::days(10));
        let mut b = user("bruno");
        b.locked = true;
        b.department = "Anatomy".into();
        let mut c = user("carla");
        c.enabled = false;
        let all = vec![a, b, c];
        assert_eq!(filter_users(&all, UserFilter::All, "", 90, now), [0, 1, 2]);
        assert_eq!(filter_users(&all, UserFilter::Locked, "ana", 90, now), [1]);
        assert_eq!(filter_users(&all, UserFilter::Disabled, "", 90, now), [2]);
        // never logged on counts as inactive; a disabled account never does
        assert_eq!(filter_users(&all, UserFilter::Inactive, "", 90, now), [1]);
        assert_eq!(filter_users(&all, UserFilter::Enabled, "DOMAIN.LOCAL", 90, now), [0]);
    }

    #[test]
    fn dns() {
        assert_eq!(ou_path("OU=Tatuape,OU=SP,DC=example,DC=local"), "SP / Tatuape");
        assert_eq!(parent_dn(r"CN=Silva\, Joao,OU=SP,DC=x"), "OU=SP,DC=x");
        assert_eq!(ou_path(r"CN=Silva\, Joao,OU=SP,DC=x"), r"SP / Silva\, Joao");
        assert!(is_ou("ou=a,DC=x") && !is_ou("CN=Users,DC=x"));
        let dc = OrgUnit {
            dn: "OU=Domain Controllers,DC=x".into(),
            name: "Domain Controllers".into(),
            ..Default::default()
        };
        assert!(!ou_deletable(&dc));
    }

    #[test]
    fn sizes_and_durations() {
        let _g = crate::i18n::tests::english();
        assert_eq!(human_bytes(512), "512 B");
        assert_eq!(human_bytes(1536), "1.5 KB");
        assert_eq!(human_bytes(1 << 20), "1 MB");
        crate::i18n::set_lang("pt-BR");
        assert_eq!(human_bytes(1536), "1,5 KB");
        crate::i18n::set_lang("en");
        assert_eq!(size_text(5 << 30), "5.0 GiB");
        assert_eq!(size_text(1), "1 KiB");
        assert_eq!(
            (short_duration(38), short_duration(278), short_duration(3720)),
            ("38s".into(), "4m38s".into(), "1h02m".into())
        );
    }

    #[test]
    fn passwords() {
        for n in [4, 14, 32] {
            let p = generate_password(n).unwrap();
            assert_eq!(p.len(), n);
            for c in PW_CLASSES {
                assert!(p.chars().any(|x| c.contains(x)), "{p} lacks one of {c}");
            }
        }
        assert_eq!(generate_password(2).unwrap().len(), 4);
        assert_ne!(generate_password(20).unwrap(), generate_password(20).unwrap());
    }

    #[test]
    fn misc() {
        let _g = crate::i18n::tests::english();
        assert_eq!(split_list(" a, b ,,c "), ["a", "b", "c"]);
        assert_eq!(slug("Finance · OLD-FS 01!"), "finance-old-fs-01");
        assert_eq!(backup_code("panel-20261001-120000-abcdef12"), "abcdef12");
        assert_eq!(backup_code("short"), "short");
        assert_eq!(unc("fs", "dados", "a/b"), r"\\fs\dados\a\b");
        assert_eq!(share_folder("Finance Team"), "/srv/shares/finance-team");
        assert!(legacy_os("Windows XP Professional") && !legacy_os("Windows 11 Pro"));
        assert_eq!(backup_origin("schedule:nightly"), "schedule nightly");
    }
}
