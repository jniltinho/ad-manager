//! The panel theme on egui: a [`Look`] (theme id, palette, the panel's flat look) turned into egui visuals, fonts and
//! sizes, the colors pages use for their text tones and the focus ring. Sizes come from the web's named tokens and the
//! depth (gradients, shadows) from the theme's recipe, cleared by the flat look as the web's flat override does.

use adm_core::logic::Tone;
use adm_core::theme::{self, Palette, Rgba};
use egui::{Color32, Context, CornerRadius, Id, LayerId, Order, Stroke, StrokeKind, Visuals};

/// The active theme: the panel's Settings choice, Default until the panel answers.
#[derive(Clone)]
pub struct Look {
    pub id: &'static str,
    pub p: &'static Palette,
    /// The panel's flat look (Settings): no decorative gradients and shadows; state marks and popups keep theirs.
    pub flat: bool,
}

/// The control height in force (the theme's `control-h`, in points), for the house widgets that have no look at hand.
static CONTROL_H: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(0x41e0_0000); // 28.0

/// The height of inputs, selects and buttons (28 by default).
pub fn control_h() -> f32 {
    f32::from_bits(CONTROL_H.load(std::sync::atomic::Ordering::Relaxed))
}

impl Look {
    /// Theme `id` (unknown: Default), in the full look.
    pub fn new(id: &str) -> Option<Self> {
        Self::with_flat(id, false)
    }

    /// Theme `id` in the full or the flat look.
    pub fn with_flat(id: &str, flat: bool) -> Option<Self> {
        let (id, p) = theme::palette(id)?;
        // a depthless theme's own recipes already have no depth (High contrast's window line is a frame): the flat
        // look only when asked, as the web's data-flat
        Some(Self { id, p, flat })
    }

    /// A size token in points (the web's px at zoom 1).
    pub fn px(&self, token: &str, default: f32) -> f32 {
        self.p.px(token, default)
    }

    /// A part's background gradient in this look.
    /// Paints the page's repeated background (the dots) over `rect`, when the theme has one in this look.
    pub fn page_pattern(&self, painter: &egui::Painter, rect: egui::Rect) {
        if let Some(p) = self.p.pattern("page", self.flat) {
            painter.extend(crate::depth::pattern_shapes(rect, &p));
        }
    }

    pub fn gradient(&self, part: &str) -> Option<&'static theme::Gradient> {
        self.p.gradient(part, self.flat)
    }

    /// A part's shadow layers in this look.
    pub fn shadows(&self, part: &str) -> &'static [theme::Layer] {
        self.p.shadows(part, self.flat)
    }

    /// Paints a part's box (its shadows, its gradient or `fill`, its inset layers) under what is drawn next.
    pub fn paint(&self, painter: &egui::Painter, rect: egui::Rect, part: &str, shadow: &str, fill: Color32) {
        crate::depth::paint_box(painter, rect, self.gradient(part), fill, self.shadows(shadow));
    }

    pub fn c(&self, role: &str) -> Color32 {
        c32(self.p.col(role))
    }

    /// The color of a text tone (the roles the Fyne app uses).
    pub fn tone(&self, t: Tone) -> Color32 {
        self.c(match t {
            Tone::Normal => "fg",
            Tone::Strong => "fg-strong",
            Tone::Muted => "fg-muted",
            Tone::Ok => "ok",
            Tone::Err => "err",
            Tone::Warn => "warn",
        })
    }

    /// Applies the theme to egui (square corners, the theme's roles).
    pub fn apply(&self, ctx: &Context) {
        let mut v = if self.p.dark() { Visuals::dark() } else { Visuals::light() };
        let fg = self.c("fg");
        v.override_text_color = Some(fg);
        v.panel_fill = self.c("surface-2");
        v.window_fill = self.c("surface");
        v.window_stroke = Stroke::new(1.0, self.c("line"));
        v.extreme_bg_color = self.c("field");
        v.text_edit_bg_color = Some(self.c("field"));
        // placeholders in the theme's --placeholder-color (the browser's field ink at 50% when it gives none)
        v.weak_text_color = Some(self.c("placeholder"));
        v.faint_bg_color = self.c("row-alt");
        v.code_bg_color = self.c("surface-2");
        v.hyperlink_color = self.c("link");
        v.warn_fg_color = self.c("warn");
        v.error_fg_color = self.c("err");
        v.selection.bg_fill = self.c("selection");
        // egui draws selected text (and a focused field's border) in this color: the ink that reads on the
        // selection; the theme's focus color is the ring drawn outside (focus_ring)
        v.selection.stroke = Stroke::new(1.0, self.c("on-selection"));
        // the browser's opacity-50 over white: egui's blend at 0.5 comes out darker (about 0.6 in the screenshot)
        v.disabled_alpha = 0.4;
        v.window_corner_radius = CornerRadius::ZERO;
        v.menu_corner_radius = CornerRadius::ZERO;
        // dialogs and popups keep their shadow in the flat look too (the web's flat override leaves them)
        v.window_shadow = egui_shadow(self.p.shadows("dialog", false));
        v.popup_shadow = egui_shadow(self.p.shadows("popup", false));
        let line = Stroke::new(1.0, self.c("field-line"));
        let w = &mut v.widgets;
        for (state, fill) in [
            (&mut w.noninteractive, self.c("surface")),
            // egui's own widgets left are selects and menus: the web draws a select as a field
            (&mut w.inactive, self.c("field")),
            (&mut w.hovered, self.c("row-hover")),
            (&mut w.active, self.c("selection")),
            (&mut w.open, self.c("field")),
        ] {
            state.corner_radius = CornerRadius::ZERO;
            state.bg_fill = fill;
            state.weak_bg_fill = fill;
            state.bg_stroke = line;
            state.fg_stroke = Stroke::new(1.0, self.c("on-field"));
            state.expansion = 0.0;
        }
        w.noninteractive.bg_stroke = Stroke::new(1.0, self.c("line"));
        w.noninteractive.fg_stroke = Stroke::new(1.0, fg);
        w.hovered.bg_stroke = Stroke::new(1.0, self.c("focus"));
        // the strong fill paints the scroll bar's handle (selects use the weak one): visible on the field, as the
        // browser's bar
        w.inactive.bg_fill = self.c("field-line");
        w.hovered.bg_fill = self.c("fg-muted");
        // both of egui's styles: set_visuals changes only the current light/dark one, and eframe follows the
        // system's theme (a dark Windows or Wine switched the app to egui's own visuals)
        ctx.all_styles_mut(|s| s.visuals = v.clone());
        let check = crate::widgets::CheckColors {
            field: self.c("field"),
            line: self.c("check-line"),
            on: self.c("action"),
            mark: self.c("on-action"),
            ink: fg,
        };
        ctx.data_mut(|d| d.insert_temp(Id::new("adm-check-colors"), check));
        ctx.data_mut(|d| d.insert_temp(Id::new(crate::widgets::SELECT_ARROW), self.c("select-arrow")));
        let (defs, _family) = crate::fonts::definitions(&self.p.fonts.sans, &self.p.fonts.mono);
        ctx.set_fonts(defs);
        let control = self.px("control-h", 28.0);
        CONTROL_H.store(control.to_bits(), std::sync::atomic::Ordering::Relaxed);
        let base = self.px("font-size-base", 13.0);
        let text = self.px("control-text", 12.0);
        ctx.all_styles_mut(|s| {
            use egui::{FontFamily, FontId, TextStyle};
            s.text_styles = [
                (TextStyle::Small, FontId::new(base - 2.0, FontFamily::Proportional)),
                (TextStyle::Body, FontId::new(base, FontFamily::Proportional)),
                (TextStyle::Button, FontId::new(text, FontFamily::Proportional)),
                (TextStyle::Heading, FontId::new(base + 2.0, FontFamily::Name(crate::fonts::SEMIBOLD.into()))),
                // the web's mono fields (.inp font-mono) at the control's 12 px; table cells set their 11 themselves
                (TextStyle::Monospace, FontId::new(text, FontFamily::Monospace)),
            ]
            .into();
            s.spacing.item_spacing = egui::vec2(self.px("toolbar-gap", 8.0), 6.0);
            // a 28 px border box: the label's line plus the padding, as the web's h-7 with px-3
            s.spacing.button_padding = egui::vec2(self.px("button-px", 12.0), ((control - text * 1.25) / 2.0).max(2.0));
            s.spacing.interact_size.y = control;
            // solid scroll bars: floating ones fade in and out over several frames after each scroll
            s.spacing.scroll = egui::style::ScrollStyle::solid();
            // the browser clips a scrolled box with a hard edge: no fade over its last 20 points
            s.spacing.scroll.fade.strength = 0.0;
            // no fades: every frame is a full redraw, an animation costs one per frame
            s.animation_time = 0.0;
        });
    }
}

/// The first outer layer of a recipe as egui's window or popup shadow.
fn egui_shadow(layers: &[theme::Layer]) -> egui::Shadow {
    layers.iter().find(|l| !l.inset).map_or(egui::Shadow::NONE, |l| egui::Shadow {
        offset: [l.dx.round().clamp(-128.0, 127.0) as i8, l.dy.round().clamp(-128.0, 127.0) as i8],
        blur: (2.0 * l.blur).round().clamp(0.0, 255.0) as u8, // the browser's blur radius (see depth::outer)
        spread: l.spread.round().clamp(0.0, 255.0) as u8,
        color: c32(l.color),
    })
}

/// Semibold text (the web's font-weight 600): the theme's semibold face at `size`.
pub fn semibold(text: impl Into<String>, size: f32) -> egui::RichText {
    egui::RichText::new(text).family(egui::FontFamily::Name(crate::fonts::SEMIBOLD.into())).size(size)
}

pub fn c32(c: Rgba) -> Color32 {
    let [r, g, b, a] = c.0;
    Color32::from_rgba_unmultiplied(r, g, b, a)
}

/// The memory key of the focused widget that draws its own focus ring.
pub const OWN_RING: &str = "adm-own-focus-ring";

/// Draws the theme's focus ring outside the focused widget (fields, selects, checks, buttons), at the theme's width.
pub fn focus_ring(ctx: &Context, look: &Look) {
    let Some(focused) = ctx.memory(|m| m.focused()) else { return };
    // a widget that draws its own ring (the login's fields, in the login's focus color)
    if ctx.data(|d| d.get_temp::<Id>(Id::new(OWN_RING))) == Some(focused) {
        return;
    }
    let Some(rect) = ctx.read_response(focused).map(|r| r.rect) else { return };
    let painter = ctx.layer_painter(LayerId::new(Order::Foreground, Id::new("adm-focus-ring")));
    // a text field shows focus as the web's .inp:focus: its border in the field-focus color and that shadow, not the
    // focus-visible ring (egui would draw the border in the selection's ink)
    if egui::text_edit::TextEditState::load(ctx, focused).is_some() {
        // the shadow's spread as a ring outside (drawn above the field: a filled shadow would tint it)
        let mut shapes: Vec<egui::Shape> = look
            .shadows("field-focus")
            .iter()
            .filter(|l| !l.inset && l.spread > 0.0)
            .map(|l| {
                let r = rect.translate(egui::vec2(l.dx, l.dy)).expand(l.spread / 2.0);
                egui::Shape::rect_stroke(r, CornerRadius::ZERO, Stroke::new(l.spread, c32(l.color)), StrokeKind::Middle)
            })
            .collect();
        shapes.push(egui::Shape::rect_stroke(
            rect,
            CornerRadius::ZERO,
            Stroke::new(1.0, look.c("field-focus")),
            StrokeKind::Inside,
        ));
        painter.extend(shapes);
        return;
    }
    let w = look.p.focus_ring;
    painter.rect_stroke(
        rect.expand(w / 2.0 + 1.0),
        CornerRadius::ZERO,
        Stroke::new(w, look.c("focus")),
        StrokeKind::Middle,
    );
}

#[cfg(test)]
mod tests {
    /// Every color role the code asks for (`look.c("…")`) is exported by every theme: a missing one would paint
    /// transparent without a word.
    #[test]
    fn every_role_used_is_exported() {
        let src = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("src");
        let files = ["", "pages"].iter().flat_map(|d| std::fs::read_dir(src.join(d)).into_iter().flatten().flatten());
        let text: String = files
            .filter(|e| e.path().extension().is_some_and(|x| x == "rs"))
            .map(|e| std::fs::read_to_string(e.path()).unwrap_or_default())
            .collect();
        let re = regex::Regex::new(r#"\.c\("([a-z0-9-]+)"\)"#).unwrap();
        let mut missing = Vec::new();
        for id in adm_core::theme::ids() {
            let (_, p) = adm_core::theme::palette(id).unwrap();
            for c in re.captures_iter(&text) {
                if !p.has(&c[1]) && !missing.contains(&(id, c[1].to_string())) {
                    missing.push((id, c[1].to_string()));
                }
            }
        }
        assert!(missing.is_empty(), "roles the themes do not export: {missing:?}");
    }
}
