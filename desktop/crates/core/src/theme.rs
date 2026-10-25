//! The panel's 18 themes (`assets/themes.json`, generated from the web panel's theme CSS by
//! `archive/desktop-go/internal/themegen/rustgen`, the same source as the Fyne app's `themes_gen.go`): the color roles, the named
//! size tokens, each part's gradient and shadow layers, the font stacks and the parts the flat look clears. The UI
//! crate draws with it; this module only holds data and the contrast arithmetic, so it can be tested without a GUI.

use std::collections::HashMap;
use std::sync::OnceLock;

use serde::Deserialize;

/// An RGBA color, not premultiplied.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default)]
pub struct Rgba(pub [u8; 4]);

impl<'de> Deserialize<'de> for Rgba {
    fn deserialize<D: serde::Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        let s = String::deserialize(d)?;
        parse_hex(&s).ok_or_else(|| serde::de::Error::custom(format!("bad color {s}")))
    }
}

fn parse_hex(s: &str) -> Option<Rgba> {
    let h = s.strip_prefix('#')?;
    if h.len() != 8 {
        return None;
    }
    let b = |i: usize| u8::from_str_radix(h.get(i..i + 2)?, 16).ok();
    Some(Rgba([b(0)?, b(2)?, b(4)?, b(6)?]))
}

/// A CSS linear-gradient: direction, interpolation space and stops.
#[derive(Debug, Clone, PartialEq, Deserialize)]
pub struct Gradient {
    /// "to bottom", "to right", "to top", "to left" or "<n>deg".
    pub dir: String,
    /// "oklab" or "srgb".
    pub space: String,
    pub stops: Vec<Stop>,
}

/// A gradient stop at `pos` (0–1).
#[derive(Debug, Clone, Copy, PartialEq, Deserialize)]
pub struct Stop {
    pub color: Rgba,
    pub pos: f32,
}

/// A background repeated every 22 px (the web's `background-size`): a dot (an ellipse of radii `w`, `h` at the
/// tile's center) or horizontal lines `h` thick every `period` px.
#[derive(Debug, Clone, Copy, PartialEq, Deserialize)]
pub struct Pattern {
    pub kind: PatternKind,
    pub color: Rgba,
    #[serde(default)]
    pub w: f32,
    pub h: f32,
    #[serde(default)]
    pub period: f32,
}

#[derive(Debug, Clone, Copy, PartialEq, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum PatternKind {
    Dots,
    Lines,
}

/// One box-shadow layer, in CSS order (the first listed is on top).
#[derive(Debug, Clone, Copy, PartialEq, Deserialize)]
pub struct Layer {
    #[serde(default)]
    pub inset: bool,
    pub dx: f32,
    pub dy: f32,
    #[serde(default)]
    pub blur: f32,
    #[serde(default)]
    pub spread: f32,
    pub color: Rgba,
}

/// The theme's font stacks, in the web's order.
#[derive(Debug, Clone, Default, Deserialize)]
pub struct Fonts {
    #[serde(default)]
    pub sans: Vec<String>,
    #[serde(default)]
    pub mono: Vec<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct Palette {
    /// The registry's name ("Classic blue (XP)"), translated by the catalogs.
    #[serde(default)]
    pub label: String,
    pub scheme: String,
    /// No depth by design (High contrast): always flat.
    pub depthless: bool,
    /// The contrast text keeps (7 on the AAA themes).
    pub min_text: f64,
    /// The focus ring's width around a focused field, select or check.
    pub focus_ring: f32,
    /// The stripe on top of the Home tiles (none in Classic gray).
    pub card_stripe: f32,
    /// The mark of the current sidebar item (the web's current top tab band), if the theme has one.
    pub tab_mark: Option<Rgba>,
    #[serde(rename = "c")]
    roles: HashMap<String, Rgba>,
    /// Named sizes in px (`control-h`, `th-py`, `card-head-size`…).
    #[serde(default)]
    tokens: HashMap<String, f32>,
    /// Keyword parameters (`card-head-transform`, `dialog-head-pad`…).
    #[serde(default)]
    text: HashMap<String, String>,
    /// Each part's background: a gradient, or none.
    #[serde(default)]
    bg: HashMap<String, Option<Gradient>>,
    /// Each part's shadow layers.
    #[serde(default)]
    shadow: HashMap<String, Vec<Layer>>,
    /// Each part's repeated background (the page's dots), when it is one.
    #[serde(default)]
    pattern: HashMap<String, Pattern>,
    #[serde(default)]
    pub fonts: Fonts,
}

impl Palette {
    /// A role's color (transparent when the theme has none).
    pub fn col(&self, role: &str) -> Rgba {
        self.roles.get(role).copied().unwrap_or_default()
    }

    /// Whether the theme exports `role` (`col` answers transparent for one it does not).
    pub fn has(&self, role: &str) -> bool {
        self.roles.contains_key(role)
    }

    pub fn dark(&self) -> bool {
        self.scheme == "dark"
    }

    /// A size token in px, or `default` when the theme has none.
    pub fn px(&self, token: &str, default: f32) -> f32 {
        self.tokens.get(token).copied().unwrap_or(default)
    }

    /// A keyword parameter, or "".
    pub fn text(&self, name: &str) -> &str {
        self.text.get(name).map_or("", String::as_str)
    }

    /// A part's background gradient (`header`, `action`, `card-head`…); none when the part has a plain fill, or in the
    /// flat look when the flat override clears it.
    pub fn gradient(&self, part: &str, flat: bool) -> Option<&Gradient> {
        if flat && flat_clears(&format!("--bg-{part}")) {
            return None;
        }
        self.bg.get(part).and_then(Option::as_ref)
    }

    /// A part's repeated background (`page`); in the flat look the standard dots in the theme's page-dots color, as the
    /// web's flat override sets them, instead of the theme's own pattern (rain, steel lines).
    pub fn pattern(&self, part: &str, flat: bool) -> Option<Pattern> {
        if flat && flat_clears(&format!("--bg-{part}")) {
            let color = self.col("page-dots");
            return (part == "page").then_some(Pattern { kind: PatternKind::Dots, color, w: 1.0, h: 1.0, period: 0.0 });
        }
        self.pattern.get(part).copied()
    }

    /// A part's shadow layers (`btn`, `card`, `dialog`…), empty in the flat look when the flat override clears them.
    pub fn shadows(&self, part: &str, flat: bool) -> &[Layer] {
        if flat && flat_clears(&format!("--shadow-{part}")) {
            return &[];
        }
        self.shadow.get(part).map_or(&[], Vec::as_slice)
    }
}

/// The flat look clears this variable (`style.css`'s flat override list).
pub fn flat_clears(var: &str) -> bool {
    all().flat.iter().any(|v| v == var)
}

#[derive(Debug, Deserialize)]
struct Themes {
    order: Vec<String>,
    #[serde(default)]
    flat: Vec<String>,
    themes: HashMap<String, Palette>,
}

/// Every theme's id.
pub fn ids() -> impl Iterator<Item = &'static str> {
    all().themes.keys().map(String::as_str)
}

fn all() -> &'static Themes {
    static T: OnceLock<Themes> = OnceLock::new();
    T.get_or_init(|| {
        serde_json::from_str(include_str!("../assets/themes.json")).unwrap_or_else(|_| Themes {
            order: Vec::new(),
            flat: Vec::new(),
            themes: HashMap::new(),
        })
    })
}

/// Theme ids in the panel's registry order (the first is the default).
pub fn order() -> &'static [String] {
    &all().order
}

/// The palette of `id`, or Default's for an unknown id (and the id actually used).
pub fn palette(id: &str) -> Option<(&'static str, &'static Palette)> {
    let t = all();
    if let Some((k, p)) = t.themes.get_key_value(id) {
        return Some((k.as_str(), p));
    }
    let first = t.order.first()?;
    t.themes.get(first).map(|p| (first.as_str(), p))
}

fn channel(c: u8) -> f64 {
    let v = f64::from(c) / 255.0;
    if v <= 0.03928 { v / 12.92 } else { ((v + 0.055) / 1.055).powf(2.4) }
}

/// WCAG relative luminance.
pub fn lum(c: Rgba) -> f64 {
    0.2126 * channel(c.0[0]) + 0.7152 * channel(c.0[1]) + 0.0722 * channel(c.0[2])
}

/// WCAG contrast ratio of two opaque colors.
pub fn contrast(a: Rgba, b: Rgba) -> f64 {
    let (x, y) = (lum(a), lum(b));
    (x.max(y) + 0.05) / (x.min(y) + 0.05)
}

/// fg drawn over an opaque bg.
pub fn over(fg: Rgba, bg: Rgba) -> Rgba {
    let a = f64::from(fg.0[3]) / 255.0;
    let mix = |f: u8, b: u8| (f64::from(f) * a + f64::from(b) * (1.0 - a)).round().clamp(0.0, 255.0) as u8;
    Rgba([mix(fg.0[0], bg.0[0]), mix(fg.0[1], bg.0[1]), mix(fg.0[2], bg.0[2]), 255])
}

/// Contrast of text fg over bg, both made opaque over white first (as the Fyne test does).
pub fn text_contrast(fg: Rgba, bg: Rgba) -> f64 {
    let b = over(bg, Rgba([255, 255, 255, 255]));
    contrast(over(fg, b), b)
}

/// A dark color (light ink reads on it).
pub fn is_dark(c: Rgba) -> bool {
    lum(c) < 0.2
}

#[cfg(test)]
mod contrast_tests {
    use super::*;

    /// The web's contrast recipes (frontend/src/themes/matrix.json) hold on the colors the desktop draws with: each
    /// text role over its background layers (bottom one over white), 4.5:1 for text (7:1 on the AAA themes) and 3:1
    /// for components. Recipes of roles the desktop does not draw (toasts, tooltips) and of the segmented tabs are
    /// left out.
    #[test]
    fn every_theme_keeps_the_web_contrast() {
        #[derive(Deserialize)]
        struct Recipe {
            id: String,
            fg: String,
            bg: Vec<String>,
            size: String,
            #[serde(default, rename = "aaaOnly")]
            aaa_only: bool,
            #[serde(default, rename = "segmentedOnly")]
            segmented_only: bool,
        }
        #[derive(Deserialize)]
        struct Matrix {
            recipes: Vec<Recipe>,
            aaa: Vec<String>,
        }
        let m: Matrix = serde_json::from_str(include_str!("../../../../frontend/src/themes/matrix.json")).unwrap();
        let mut failed = Vec::new();
        let mut checked = 0;
        for id in order() {
            let (_, p) = palette(id).unwrap();
            let aaa = m.aaa.contains(id);
            for r in &m.recipes {
                if r.segmented_only || (r.aaa_only && !aaa) || !p.has(&r.fg) || r.bg.iter().any(|b| !p.has(b)) {
                    continue;
                }
                // the layers top→bottom: the bottom one over white, each above over the ones below
                let bg = r.bg.iter().rev().fold(Rgba([255, 255, 255, 255]), |acc, b| over(p.col(b), acc));
                let ratio = contrast(over(p.col(&r.fg), bg), bg);
                let need = match (r.size.as_str(), aaa) {
                    ("normal", true) => 7.0,
                    ("normal", false) => 4.5,
                    _ => 3.0,
                };
                checked += 1;
                // 0.05 of slack: the export rounds each color to 8 bits per channel (the web's test reads the CSS)
                if ratio + 0.05 < need {
                    failed.push(format!("{id} {}: {ratio:.2} < {need}", r.id));
                }
            }
        }
        assert!(checked > 1000, "the recipes ran ({checked})");
        assert!(failed.is_empty(), "contrast below the web's recipes:\n{}", failed.join("\n"));
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn unknown_theme_is_default() {
        let (id, _) = palette("no-such-theme").unwrap();
        assert_eq!(id, "default");
        let (id, p) = palette("classic-blue").unwrap();
        assert_eq!(id, "classic-blue");
        assert_eq!(p.col("dialog-head"), Rgba([0x01, 0x43, 0xcf, 0xff]));
        assert_eq!(order().len(), 18);
        let hc = palette("high-contrast").unwrap().1;
        assert!(hc.depthless && hc.min_text == 7.0 && hc.focus_ring == 3.0);
    }

    /// The recipe data the egui app draws with: tokens, gradients (direction, space), shadow layers in CSS order,
    /// fonts and the flat override.
    #[test]
    fn recipes_from_the_web_css() {
        let p = palette("classic-blue").unwrap().1;
        assert_eq!((p.px("control-h", 0.0), p.px("th-py", 0.0), p.px("tag-size", 0.0)), (28.0, 6.0, 10.0));
        let h = p.gradient("header", false).expect("Classic blue header gradient");
        assert_eq!((h.dir.as_str(), h.space.as_str(), h.stops.len()), ("to bottom", "oklab", 9));
        assert!(p.gradient("header", true).is_none(), "the flat look clears the header gradient");
        assert!(!p.shadows("btn", false).is_empty() && p.shadows("btn", true).is_empty());
        assert!(p.shadows("tab-on", true).iter().any(|l| l.inset), "the current tab's mark stays in the flat look");
        assert_eq!(p.fonts.sans.first().map(String::as_str), Some("Tahoma"));
        assert!(flat_clears("--shadow-card") && !flat_clears("--shadow-dialog"), "dialog shadows stay flat");
    }

    /// The text roles the pages draw keep the theme's minimum contrast on their backgrounds (the web's contrast
    /// matrix lists the pairs still pending per theme; those are allowed, as in the Fyne test).
    #[test]
    fn text_roles_keep_contrast() {
        let pending: HashMap<String, Vec<String>> =
            std::fs::read_to_string(concat!(env!("CARGO_MANIFEST_DIR"), "/../../../frontend/src/themes/matrix.json"))
                .ok()
                .and_then(|s| serde_json::from_str::<serde_json::Value>(&s).ok())
                .and_then(|v| serde_json::from_value(v.get("pending")?.clone()).ok())
                .unwrap_or_default();
        let mut bad = Vec::new();
        for id in order() {
            let p = &all().themes[id];
            let txt = p.min_text.max(4.5);
            let skip = pending.get(id).cloned().unwrap_or_default();
            for (recipe, fg, bg, min) in [
                ("text/surface", "fg", "surface", txt),
                ("field/text", "on-field", "field", txt),
                ("state/err", "err", "surface", txt),
                ("state/warn", "warn", "surface", txt),
                ("state/ok", "ok", "surface", txt),
                ("link/surface", "link", "surface", txt),
                ("action", "on-action", "action", txt),
                ("field/line", "field-line", "field", 3.0),
                ("selection", "on-selection", "selection", txt),
                ("nav", "on-nav-on", "nav-on", txt),
                ("header", "on-header", "header", txt),
                ("footer", "on-footer", "footer", txt),
            ] {
                let r = text_contrast(p.col(fg), p.col(bg));
                if r < min && !skip.iter().any(|s| s == recipe) {
                    bad.push(format!("{id}: {fg}/{bg} = {r:.2} (< {min})"));
                }
            }
        }
        assert!(bad.is_empty(), "{}", bad.join("\n"));
    }
}
