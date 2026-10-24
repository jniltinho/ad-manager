//! The theme's fonts, resolved as a browser does (design D7): the first family of the theme's CSS stack that is
//! installed in the system or user font folders, matched by family name and CSS weight — 400 for text, 600 for
//! headings, buttons, table heads and badges (a real 600 face, else a variable font at wght 600, else the next heavier
//! face). The embedded Inter 4.001 (the web's own file) answers at the Inter entry of a stack and is the last resort;
//! egui's monospace is the last resort of a monospace stack. Fonts are read where they are installed, never copied.

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex, OnceLock};

use egui::{FontData, FontDefinitions, FontFamily};
use skrifa::raw::FileRef;
use skrifa::{MetadataProvider, string::StringId};

/// The named egui family of semibold text.
pub const SEMIBOLD: &str = "semibold";

const INTER_400: &[u8] = include_bytes!("../../core/assets/fonts/Inter-Regular.ttf");
const INTER_600: &[u8] = include_bytes!("../../core/assets/fonts/Inter-SemiBold.ttf");
/// The web's Lucide icons (a subset: `crate::icons`).
const LUCIDE: &[u8] = include_bytes!("../../core/assets/fonts/lucide.ttf");

/// One face to register: its file bytes, the face index in a collection and a weight for a variable font.
#[derive(Clone)]
pub struct Face {
    pub bytes: Arc<[u8]>,
    pub index: u32,
    pub wght: Option<f32>,
}

impl std::fmt::Debug for Face {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "Face({} bytes, #{}, wght {:?})", self.bytes.len(), self.index, self.wght)
    }
}

/// A resolved family: the name the stack matched, the regular and semibold faces.
#[derive(Clone, Debug)]
pub struct Resolved {
    pub family: String,
    pub regular: Face,
    pub semibold: Face,
}

/// The generic CSS families: the browser's own fallback, not a font to look up.
fn generic(f: &str) -> bool {
    matches!(
        f.to_ascii_lowercase().as_str(),
        "sans-serif" | "serif" | "monospace" | "ui-monospace" | "system-ui" | "ui-sans-serif" | "cursive" | "fantasy"
    )
}

fn norm(s: &str) -> String {
    s.chars().filter(char::is_ascii_alphanumeric).map(|c| c.to_ascii_lowercase()).collect()
}

/// File-name prefixes a family is usually installed under (the name tables still decide).
fn file_keys(family: &str) -> Vec<String> {
    let n = norm(family);
    let mut v = vec![n.clone()];
    let extra: &[&str] = match n.as_str() {
        "applesystem" | "blinkmacsystemfont" | "sfprotext" => &["sfns", "sfpro", "sfnstext", "sfcompact"],
        "segoeui" => &["segoeui", "segui"],
        "consolas" => &["consola"],
        "couriernew" => &["cour"],
        "liberationmono" => &["liberationmono"],
        "sfmonoregular" => &["sfmono"],
        "cascadiamono" => &["cascadiamono", "cascadia"],
        _ => &[],
    };
    v.extend(extra.iter().map(|s| (*s).to_string()));
    v
}

/// The system and user font folders, as the browser sees them.
pub fn font_dirs() -> Vec<PathBuf> {
    let home = std::env::var_os(if cfg!(windows) { "USERPROFILE" } else { "HOME" }).map(PathBuf::from);
    let mut v = Vec::new();
    if cfg!(windows) {
        if let Some(w) = std::env::var_os("WINDIR") {
            v.push(PathBuf::from(w).join("Fonts"));
        }
        if let Some(l) = std::env::var_os("LOCALAPPDATA") {
            v.push(PathBuf::from(l).join("Microsoft").join("Windows").join("Fonts"));
        }
    } else if cfg!(target_os = "macos") {
        v.extend(["/System/Library/Fonts", "/Library/Fonts"].map(PathBuf::from));
        if let Some(h) = &home {
            v.push(h.join("Library/Fonts"));
        }
    } else {
        v.extend(["/usr/share/fonts", "/usr/local/share/fonts"].map(PathBuf::from));
        if let Some(d) = std::env::var_os("XDG_DATA_HOME") {
            v.push(PathBuf::from(d).join("fonts"));
        }
        if let Some(h) = &home {
            v.push(h.join(".local/share/fonts"));
            v.push(h.join(".fonts"));
        }
    }
    v
}

/// Font files under `dirs` (a few levels deep), with their normalized file stems.
fn files(dirs: &[PathBuf]) -> Vec<(String, PathBuf)> {
    fn walk(d: &Path, depth: u8, out: &mut Vec<(String, PathBuf)>) {
        let Ok(rd) = std::fs::read_dir(d) else { return };
        for e in rd.flatten() {
            let p = e.path();
            if p.is_dir() {
                if depth < 4 {
                    walk(&p, depth + 1, out);
                }
            } else if let Some(ext) = p.extension().and_then(|x| x.to_str())
                && matches!(ext.to_ascii_lowercase().as_str(), "ttf" | "otf" | "ttc" | "otc")
                && let Some(stem) = p.file_stem().and_then(|s| s.to_str())
            {
                out.push((norm(stem), p));
            }
        }
    }
    let mut out = Vec::new();
    for d in dirs {
        walk(d, 0, &mut out);
    }
    out
}

/// A face found on disk.
struct Candidate {
    face: Face,
    weight: f32,
    /// A variable font's wght range.
    wght: Option<(f32, f32)>,
}

/// The upright faces of `family` among the files whose names suggest it, validated by parsing (an unreadable,
/// invalid or wrongly indexed file is skipped).
fn candidates(family: &str, all: &[(String, PathBuf)]) -> Vec<Candidate> {
    let keys = file_keys(family);
    let want = norm(family);
    let mut out = Vec::new();
    for (stem, path) in all {
        if !keys.iter().any(|k| stem.starts_with(k.as_str())) {
            continue;
        }
        let Ok(bytes) = std::fs::read(path) else { continue };
        let bytes: Arc<[u8]> = bytes.into();
        let Ok(file) = FileRef::new(&bytes) else { continue };
        for (index, font) in file.fonts().enumerate() {
            let Ok(font) = font else { continue };
            let name = |id| font.localized_strings(id).english_or_first().map(|s| s.chars().collect::<String>());
            let fam = name(StringId::TYPOGRAPHIC_FAMILY_NAME).or_else(|| name(StringId::FAMILY_NAME));
            let attrs = font.attributes();
            let same = fam.as_deref().map(norm).is_some_and(|f| f == want || keys.contains(&f));
            // the face must draw text: a charmap that maps 'a'
            if !same || attrs.style != skrifa::attribute::Style::Normal || font.charmap().map('a').is_none() {
                continue;
            }
            let wght = font
                .axes()
                .iter()
                .find(|a| a.tag() == skrifa::Tag::new(b"wght"))
                .map(|a| (a.min_value(), a.max_value()));
            let Ok(index) = u32::try_from(index) else { continue };
            out.push(Candidate {
                face: Face { bytes: bytes.clone(), index, wght: None },
                weight: attrs.weight.value(),
                wght,
            });
        }
    }
    out
}

/// The CSS font-matching pick for `target` (400 or 600): exact, else a variable face covering it, else (for 600) the
/// next heavier then lighter, (for 400) 500 then lighter then heavier.
fn pick(c: &[Candidate], target: f32) -> Option<Face> {
    if let Some(x) = c.iter().find(|x| x.wght.is_none() && (x.weight - target).abs() < 1.0) {
        return Some(x.face.clone());
    }
    if let Some(x) = c.iter().find(|x| x.wght.is_some_and(|(lo, hi)| lo <= target && target <= hi)) {
        return Some(Face { wght: Some(target), ..x.face.clone() });
    }
    let mut v: Vec<&Candidate> = c.iter().filter(|x| x.wght.is_none()).collect();
    let key = |x: &&Candidate| {
        let w = x.weight;
        if target >= 600.0 {
            if w >= target { (0, w as i32) } else { (1, -(w as i32)) }
        } else if (target..=500.0).contains(&w) {
            (0, w as i32)
        } else if w < target {
            (1, -(w as i32))
        } else {
            (2, w as i32)
        }
    };
    v.sort_by_key(key);
    v.first().map(|x| x.face.clone())
}

fn inter() -> Resolved {
    Resolved {
        family: "Inter".into(),
        regular: Face { bytes: Arc::from(INTER_400), index: 0, wght: None },
        semibold: Face { bytes: Arc::from(INTER_600), index: 0, wght: None },
    }
}

/// Resolves a stack against the fonts in `dirs`; `None` when only generic families are left (the caller's last
/// resort applies).
pub fn resolve(stack: &[String], dirs: &[PathBuf]) -> Option<Resolved> {
    let all = OnceLock::new();
    for f in stack {
        if generic(f) {
            continue;
        }
        if norm(f) == "inter" {
            return Some(inter()); // the web serves its own Inter file, whatever is installed
        }
        let c = candidates(f, all.get_or_init(|| files(dirs)));
        let (Some(regular), Some(semibold)) = (pick(&c, 400.0), pick(&c, 600.0)) else { continue };
        return Some(Resolved { family: f.clone(), regular, semibold });
    }
    None
}

/// The resolution of a stack, once per stack (the font folders are read the first time a theme needs them).
fn cached(stack: &[String], mono: bool) -> Option<Resolved> {
    type Cache = Mutex<HashMap<(bool, Vec<String>), Option<Resolved>>>;
    static C: OnceLock<Cache> = OnceLock::new();
    let m = C.get_or_init(Default::default);
    let key = (mono, stack.to_vec());
    if let Some(r) = m.lock().ok().and_then(|g| g.get(&key).cloned()) {
        return r;
    }
    let r = resolve(stack, &font_dirs());
    if let Ok(mut g) = m.lock() {
        g.insert(key, r.clone());
    }
    r
}

fn data(f: &Face) -> FontData {
    let mut d = FontData::from_owned(f.bytes.to_vec());
    d.index = f.index;
    if let Some(w) = f.wght {
        d.tweak.coords.push("wght", w);
    }
    d
}

/// Wide system faces tried, in order, for the symbols the theme's face lacks.
const SYMBOLS: [&str; 6] =
    ["DejaVu Sans", "Noto Sans", "Segoe UI Symbol", "Segoe UI", "Lucida Grande", "Arial Unicode MS"];

/// The egui fonts of a theme: its sans stack as the proportional family (and `semibold`), its mono stack as the
/// monospace family; egui's bundled fonts stay behind them for symbols and emoji.
pub fn definitions(sans: &[String], mono: &[String]) -> (FontDefinitions, String) {
    let mut d = FontDefinitions::default();
    let fallback_prop = d.families.get(&FontFamily::Proportional).cloned().unwrap_or_default();
    let fallback_mono = d.families.get(&FontFamily::Monospace).cloned().unwrap_or_default();
    let s = cached(sans, false).unwrap_or_else(inter);
    d.font_data.insert("adm-sans".into(), Arc::new(data(&s.regular)));
    d.font_data.insert("adm-sans-600".into(), Arc::new(data(&s.semibold)));
    // the icons right after the theme's face: their private-use characters are found nowhere else
    d.font_data.insert("adm-icons".into(), Arc::new(egui::FontData::from_static(LUCIDE)));
    let mut prop = vec!["adm-sans".to_string(), "adm-icons".to_string()];
    let mut bold = vec!["adm-sans-600".to_string(), "adm-icons".to_string()];
    // a wide system face after the theme's (the embedded Inter is a latin subset): arrows and other symbols of the
    // web's texts (→, ✕) draw as the browser's fallback draws them
    let symbols: Vec<String> = SYMBOLS.iter().map(|f| (*f).to_string()).collect();
    if let Some(w) = cached(&symbols, false) {
        d.font_data.insert("adm-symbols".into(), Arc::new(data(&w.regular)));
        prop.push("adm-symbols".into());
        bold.push("adm-symbols".into());
    }
    prop.extend(fallback_prop.iter().cloned());
    bold.extend(fallback_prop.iter().cloned());
    let mut mono_fam = fallback_mono.clone();
    mono_fam.push("adm-icons".into());
    if let Some(m) = cached(mono, true) {
        d.font_data.insert("adm-mono".into(), Arc::new(data(&m.regular)));
        mono_fam.insert(0, "adm-mono".into());
    }
    d.families.insert(FontFamily::Proportional, prop);
    d.families.insert(FontFamily::Name(SEMIBOLD.into()), bold);
    d.families.insert(FontFamily::Monospace, mono_fam);
    (d, s.family)
}

#[cfg(test)]
mod tests {
    #![allow(clippy::unwrap_used)]
    use super::*;

    fn stack(s: &[&str]) -> Vec<String> {
        s.iter().map(|x| (*x).to_string()).collect()
    }

    /// A folder of fonts made from the embedded Inter files under other names (a stand-in for installed families).
    fn fake_dir(name: &str, files: &[(&str, &[u8])]) -> PathBuf {
        let d = std::env::temp_dir().join(format!("adm-fonts-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&d);
        std::fs::create_dir_all(&d).unwrap();
        for (f, b) in files {
            std::fs::write(d.join(f), b).unwrap();
        }
        d
    }

    #[test]
    fn inter_answers_at_its_position_and_generic_is_skipped() {
        let r = resolve(&stack(&["Inter", "Tahoma", "sans-serif"]), &[]).unwrap();
        assert_eq!(r.family, "Inter");
        assert!(resolve(&stack(&["Tahoma", "sans-serif"]), &[]).is_none(), "nothing installed: the caller's fallback");
    }

    #[test]
    fn installed_family_by_name_table_with_semibold_600() {
        // files named like the family; their name tables say "Inter", so the stack entry "Inter Display" is not them
        let d = fake_dir("named", &[("InterDisplay-Regular.ttf", INTER_400), ("InterDisplay-SemiBold.ttf", INTER_600)]);
        assert!(resolve(&stack(&["Inter Display"]), std::slice::from_ref(&d)).is_none(), "the name table decides");
        let d2 = fake_dir("inter", &[("Inter-Regular.ttf", INTER_400), ("Inter-SemiBold.ttf", INTER_600)]);
        let c = candidates("Inter", &files(std::slice::from_ref(&d2)));
        assert_eq!(c.len(), 2);
        let semi = pick(&c, 600.0).unwrap();
        assert_eq!(semi.bytes.len(), INTER_600.len(), "the real 600 face, not a heavier one");
        let _ = std::fs::remove_dir_all(d);
        let _ = std::fs::remove_dir_all(d2);
    }

    #[test]
    fn broken_files_are_skipped() {
        let d = fake_dir("broken", &[("Tahoma.ttf", b"not a font"), ("Tahomabd.ttf", &INTER_400[..100])]);
        assert!(resolve(&stack(&["Tahoma", "Verdana"]), std::slice::from_ref(&d)).is_none(), "no panic, next family");
        let _ = std::fs::remove_dir_all(d);
    }

    #[test]
    fn weight_matching_follows_css() {
        // each face's single byte tells which one was picked
        let face = |id: u8, w: f32| Candidate {
            face: Face { bytes: Arc::from(&[id][..]), index: 0, wght: None },
            weight: w,
            wght: None,
        };
        let c = [face(4, 400.0), face(7, 700.0), face(3, 300.0)];
        assert_eq!(pick(&c, 600.0).unwrap().bytes[0], 7, "600 missing: the next heavier");
        let c = [face(3, 300.0), face(5, 500.0)];
        assert_eq!(pick(&c, 400.0).unwrap().bytes[0], 5, "400 missing: 500 first");
        let c = [face(3, 300.0), face(2, 200.0)];
        assert_eq!(pick(&c, 600.0).unwrap().bytes[0], 3, "nothing heavier: the next lighter");
        let var = Candidate {
            face: Face { bytes: Arc::from(&[1u8][..]), index: 0, wght: None },
            weight: 400.0,
            wght: Some((100.0, 900.0)),
        };
        assert_eq!(pick(&[var], 600.0).unwrap().wght, Some(600.0), "a variable font at wght 600");
    }

    #[test]
    fn definitions_have_the_semibold_family() {
        let (d, fam) = definitions(&stack(&["Inter"]), &stack(&["monospace"]));
        assert_eq!(fam, "Inter");
        assert!(d.families.contains_key(&FontFamily::Name(SEMIBOLD.into())));
        assert_eq!(d.families[&FontFamily::Proportional][0], "adm-sans");
    }
}
