//! Interface language. Source strings are English; Portuguese comes from `assets/i18n/pt-BR.json` (seeded from the
//! Fyne app's catalog). The language follows, in order: the `lang` of desktop.toml, the panel's default, English.
//! Server messages (always English) are translated with the panel's own catalog (`/api/i18n`), with the algorithm of
//! `internal/i18n`: the whole message, then the first matching pattern, then part by part around ": ".

use std::collections::HashMap;
use std::fmt::Display;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{OnceLock, RwLock};

use chrono::{DateTime, Local, Utc};
use regex::Regex;
use serde::Deserialize;

static PT: AtomicBool = AtomicBool::new(false);

fn catalog() -> &'static HashMap<String, String> {
    static CAT: OnceLock<HashMap<String, String>> = OnceLock::new();
    CAT.get_or_init(|| serde_json::from_str(include_str!("../assets/i18n/pt-BR.json")).unwrap_or_default())
}

/// The interface language: "pt-BR" or "en".
pub fn lang() -> &'static str {
    if PT.load(Ordering::Relaxed) { "pt-BR" } else { "en" }
}

/// Switches the interface language ("pt-BR" or "en"); anything else is ignored.
pub fn set_lang(l: &str) {
    match l {
        "pt-BR" => PT.store(true, Ordering::Relaxed),
        "en" => PT.store(false, Ordering::Relaxed),
        _ => {}
    }
}

/// Translates an English source string.
pub fn t(s: &str) -> &str {
    if !PT.load(Ordering::Relaxed) {
        return s;
    }
    catalog().get(s).map_or(s, String::as_str)
}

/// Fills a Go-style format (`%s`, `%d`; any other `%` is literal) with args, in order.
pub fn fill(format: &str, args: &[&dyn Display]) -> String {
    let mut out = String::with_capacity(format.len() + 16);
    let mut next = args.iter();
    let mut chars = format.chars().peekable();
    while let Some(c) = chars.next() {
        if c == '%' && matches!(chars.peek(), Some('s' | 'd')) {
            chars.next();
            if let Some(a) = next.next() {
                out.push_str(&a.to_string());
            }
            continue;
        }
        out.push(c);
    }
    out
}

/// `tf!("format %s", arg, …)`: translates the format, then fills it.
#[macro_export]
macro_rules! tf {
    ($fmt:expr $(, $arg:expr)* $(,)?) => {
        $crate::i18n::fill($crate::i18n::t($fmt), &[$(&$arg as &dyn ::std::fmt::Display),*])
    };
}

/// The date/time format of the interface language.
pub fn date_layout() -> &'static str {
    if PT.load(Ordering::Relaxed) { "%d/%m/%Y %H:%M" } else { "%m/%d/%Y %I:%M %p" }
}

/// Formats a UTC time in the local zone, "—" when unset (the zero time of Go's JSON).
pub fn date(d: Option<DateTime<Utc>>) -> String {
    match d {
        Some(d) if d.timestamp() > 0 => d.with_timezone(&Local).format(date_layout()).to_string(),
        _ => "—".into(),
    }
}

/// A count with its thousands grouped as the web's toLocaleString in the interface language (9,768 or 9.768).
pub fn count(n: impl Into<i64>) -> String {
    group(n.into(), if PT.load(Ordering::Relaxed) { '.' } else { ',' })
}

fn group(n: i64, sep: char) -> String {
    let digits = n.unsigned_abs().to_string();
    let mut out = String::new();
    for (i, c) in digits.chars().enumerate() {
        if i > 0 && (digits.len() - i).is_multiple_of(3) {
            out.push(sep);
        }
        out.push(c);
    }
    if n < 0 { format!("-{out}") } else { out }
}

/// The translated yes/no.
pub fn yes_no(b: bool) -> &'static str {
    if b { t("yes") } else { t("no") }
}

/// Joins a list with ", " ("—" when empty).
pub fn join_list(l: &[String]) -> String {
    if l.is_empty() { "—".into() } else { l.join(", ") }
}

/// The panel's pt-BR catalog of server messages (`GET /api/i18n`).
#[derive(Debug, Default, Deserialize)]
pub struct ServerCatalog {
    #[serde(default)]
    pub messages: HashMap<String, String>,
    #[serde(default)]
    pub patterns: Vec<(String, String)>,
}

struct Compiled {
    messages: HashMap<String, String>,
    patterns: Vec<(Regex, String)>,
}

fn server() -> &'static RwLock<Option<Compiled>> {
    static S: OnceLock<RwLock<Option<Compiled>>> = OnceLock::new();
    S.get_or_init(|| RwLock::new(None))
}

/// Installs the panel's catalog (patterns that do not compile are skipped).
pub fn set_server_catalog(c: ServerCatalog) {
    let dollar = Regex::new(r"\$(\d+)").ok();
    let patterns = c
        .patterns
        .into_iter()
        .filter_map(|(re, tmpl)| {
            let tmpl = match &dollar {
                Some(d) => d.replace_all(&tmpl, "$${$1}").into_owned(),
                None => tmpl,
            };
            Regex::new(&re).ok().map(|r| (r, tmpl))
        })
        .collect();
    if let Ok(mut s) = server().write() {
        *s = Some(Compiled { messages: c.messages, patterns });
    }
}

/// Shows a panel/agent message in the interface language.
pub fn server_text(s: &str) -> String {
    if !PT.load(Ordering::Relaxed) || s.is_empty() {
        return s.to_string();
    }
    match server().read() {
        Ok(g) => match g.as_ref() {
            Some(c) => pt_server(c, s),
            None => s.to_string(),
        },
        Err(_) => s.to_string(),
    }
}

fn pt_server(c: &Compiled, s: &str) -> String {
    if let Some(v) = c.messages.get(s) {
        return v.clone();
    }
    for (re, tmpl) in &c.patterns {
        if re.is_match(s) {
            return re.replace_all(s, tmpl.as_str()).into_owned();
        }
    }
    if let Some((head, tail)) = s.split_once(": ") {
        return format!("{}: {}", pt_server(c, head), pt_server(c, tail));
    }
    s.to_string()
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;
    use std::sync::Mutex;

    /// Tests that switch the language hold this lock (the language is global).
    pub(crate) static LANG: Mutex<()> = Mutex::new(());

    /// Holds the language lock with English on (tests that read English messages).
    pub(crate) fn english() -> std::sync::MutexGuard<'static, ()> {
        let g = LANG.lock().unwrap_or_else(std::sync::PoisonError::into_inner);
        set_lang("en");
        g
    }

    #[test]
    fn fill_formats_in_order() {
        assert_eq!(fill("%d of %d", &[&3, &10]), "3 of 10");
        assert_eq!(fill("Most deleted, % of the destination", &[]), "Most deleted, % of the destination");
        assert_eq!(fill("%s and %s", &[&"a"]), "a and ");
    }

    #[test]
    fn translates_and_falls_back() {
        let _g = LANG.lock().unwrap_or_else(std::sync::PoisonError::into_inner);
        set_lang("pt-BR");
        assert_eq!(t("Delete"), "Excluir");
        assert_eq!(t("no such string here"), "no such string here");
        assert_eq!(crate::tf!("%d of %d", 1, 2), "1 de 2");
        set_lang("en");
        assert_eq!(t("Delete"), "Delete");
    }

    #[test]
    fn server_text_whole_pattern_and_parts() {
        let _g = LANG.lock().unwrap_or_else(std::sync::PoisonError::into_inner);
        set_server_catalog(ServerCatalog {
            messages: HashMap::from([("access denied".into(), "acesso negado".into())]),
            patterns: vec![(r"^member of ([^:]+)$".into(), "membro de $1".into())],
        });
        set_lang("pt-BR");
        assert_eq!(server_text("access denied"), "acesso negado");
        assert_eq!(server_text("member of Sales"), "membro de Sales");
        assert_eq!(server_text("member of Sales: access denied"), "membro de Sales: acesso negado");
        assert_eq!(server_text("LDAP 49: access denied"), "LDAP 49: acesso negado");
        set_lang("en");
        assert_eq!(server_text("access denied"), "access denied");
    }

    /// Every `t("…")` and `tf!("…"` literal in the sources has a pt-BR entry.
    #[test]
    fn every_source_string_is_translated() {
        let re = Regex::new(r#"\b(?:t|tf!|App)\(\s*"((?:\\.|[^"\\])*)""#).unwrap_or_else(|e| panic!("{e}"));
        let mut missing = Vec::new();
        let mut files = Vec::new();
        // every crate of the workspace that draws or says something
        for dir in ["/src", "/../ui/src", "/../../apps/ad-manager-desktop-rs/src"] {
            collect_rs(&std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join(dir.trim_start_matches('/')), &mut files);
        }
        for f in files {
            let src = std::fs::read_to_string(&f).unwrap_or_default();
            // the tests of this module use made-up strings on purpose
            let src = src.split("#[cfg(test)]").next().unwrap_or_default();
            let src: String = src.lines().filter(|l| !l.trim_start().starts_with("//")).collect::<Vec<_>>().join("\n");
            for c in re.captures_iter(&src) {
                let s = c[1].replace("\\\"", "\"").replace("\\n", "\n").replace("\\\\", "\\");
                if !catalog().contains_key(&s) {
                    missing.push(format!("{}: {s}", f.display()));
                }
            }
        }
        missing.sort();
        missing.dedup();
        assert!(missing.is_empty(), "missing pt-BR entries:\n{}", missing.join("\n"));
    }

    fn collect_rs(dir: &std::path::Path, out: &mut Vec<std::path::PathBuf>) {
        for e in std::fs::read_dir(dir).into_iter().flatten().flatten() {
            let p = e.path();
            if p.is_dir() {
                collect_rs(&p, out);
            } else if p.extension().is_some_and(|x| x == "rs") {
                out.push(p);
            }
        }
    }
}

#[cfg(test)]
mod count_tests {
    #[test]
    fn groups_thousands() {
        assert_eq!(super::group(9768, ','), "9,768");
        assert_eq!(super::group(1_234_567, ','), "1,234,567");
        assert_eq!(super::group(999, ','), "999");
        assert_eq!(super::group(-1000, ','), "-1,000");
        assert_eq!(super::group(9768, '.'), "9.768");
    }
}
