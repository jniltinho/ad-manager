//! The building blocks of the pages, styled with the panel theme: buttons by kind, cards with their title bar, tags,
//! stat tiles, labeled forms and selects.

use egui::{Color32, Frame, Margin, Response, RichText, Stroke, Ui, Vec2};

use crate::style::Look;

/// The button kinds of the panel (the Fyne app's importances).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Kind {
    /// The main action of a group (theme `action`).
    Primary,
    Secondary,
    Danger,
    /// The sign-in button (`brand-action`).
    Brand,
    /// No frame, link color.
    Link,
}

fn colors(look: &Look, kind: Kind) -> (Color32, Color32, Color32) {
    match kind {
        Kind::Primary => (look.c("action"), look.c("on-action"), look.c("action-line")),
        Kind::Secondary => (look.c("action-2"), look.c("on-action-2"), look.c("action-2-line")),
        Kind::Danger => (look.c("danger"), look.c("on-danger"), look.c("danger-line")),
        Kind::Brand => (look.c("brand-action"), look.c("on-brand-action"), look.c("brand-action")),
        Kind::Link => (Color32::TRANSPARENT, look.c("link"), Color32::TRANSPARENT),
    }
}

/// The recipe parts of a button kind: background and shadow (the web's .btn, .btn-ghost, .btn-danger, brand).
fn parts(kind: Kind, hovered: bool) -> (&'static str, &'static str) {
    match kind {
        Kind::Primary if hovered => ("action-hover", "btn"),
        Kind::Primary => ("action", "btn"),
        Kind::Secondary => ("action-2", "btn-2"),
        Kind::Danger => ("danger", "btn-danger"),
        Kind::Brand => ("brand-action", "brand-action"),
        Kind::Link => ("", ""),
    }
}

/// A button with an optional icon glyph before its label, as the web's buttons: a 28 px border box with the theme's
/// gradient and shadow (none in the flat look), semibold except the secondary (ghost) one.
pub fn button(ui: &mut Ui, look: &Look, icon: &str, label: &str, kind: Kind) -> Response {
    let (fill, ink, line) = colors(look, kind);
    let text = match (icon.is_empty(), label.is_empty()) {
        (true, _) => label.to_string(),
        (false, true) => icon.to_string(),
        (false, false) => format!("{icon}  {label}"),
    };
    let size = look.px("control-text", 12.0);
    let rich = match kind {
        Kind::Primary | Kind::Danger | Kind::Brand => crate::style::semibold(text, size),
        Kind::Secondary | Kind::Link => RichText::new(text).size(size),
    }
    .color(ink);
    if kind == Kind::Link {
        let r = ui.add(egui::Button::new(rich).frame(false));
        return r.on_hover_cursor(egui::CursorIcon::PointingHand);
    }
    // the box is painted under the label: a placeholder taken before the button, filled once its rect is known
    let under = ui.painter().add(egui::Shape::Noop);
    let r = ui.add(
        egui::Button::new(rich)
            .fill(Color32::TRANSPARENT)
            .stroke(Stroke::new(1.0, line))
            .min_size(egui::vec2(0.0, crate::style::control_h())),
    );
    let (bg, shadow) = parts(kind, r.hovered());
    let mut shapes = crate::depth::box_shapes(r.rect, look.gradient(bg), fill, look.shadows(shadow));
    if !ui.is_enabled() {
        for s in &mut shapes {
            crate::depth::fade(s, 0.5);
        }
    }
    ui.painter().set(under, egui::Shape::Vec(shapes));
    r
}

/// An icon-only button with its name as tooltip.
pub fn icon_button(ui: &mut Ui, look: &Look, icon: &str, name: &str) -> Response {
    button(ui, look, icon, "", Kind::Secondary).on_hover_text(name)
}

/// A card as the web's .card: a 1 px frame, the theme's head (28 px, its gradient, the bar left of the title at the
/// theme's width, the title in the theme's size, weight and case, a line under it) and the content on the surface.
pub fn card<R>(ui: &mut Ui, look: &Look, title: &str, add: impl FnOnce(&mut Ui) -> R) -> R {
    Frame::new()
        .fill(look.c("surface"))
        .stroke(Stroke::new(1.0, look.c("line")))
        .show(ui, |ui| {
            ui.spacing_mut().item_spacing.y = 0.0;
            card_head(ui, look, title, |_| {});
            Frame::new()
                .inner_margin(Margin::same(12))
                .show(ui, |ui| {
                    ui.spacing_mut().item_spacing.y = 6.0;
                    add(ui)
                })
                .inner
        })
        .inner
}

/// The web's .card-h: the head of a card or a panel, with `right` drawn at its right end (a link, a count).
pub fn card_head(ui: &mut Ui, look: &Look, title: &str, right: impl FnOnce(&mut Ui)) {
    let w = ui.available_width();
    let bar = look.px("card-head-bar-w", 4.0);
    let size = look.px("card-head-size", 12.0);
    let title =
        if look.p.text("card-head-transform") == "uppercase" { title.to_uppercase() } else { title.to_string() };
    let rich = if look.px("card-head-weight", 600.0) >= 600.0 {
        crate::style::semibold(title, size)
    } else {
        RichText::new(title).size(size)
    }
    .color(look.c("on-card-head"));
    let under = ui.painter().add(egui::Shape::Noop);
    let r = Frame::new()
        .inner_margin(Margin { left: (12.0 + bar) as i8, right: 12, top: 6, bottom: 6 })
        .show(ui, |ui| {
            ui.set_width(w - 24.0 - bar);
            ui.horizontal(|ui| {
                ui.label(rich);
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), right);
            });
        })
        .response;
    let mut shapes = crate::depth::box_shapes(r.rect, look.gradient("card-head"), look.c("card-head"), &[]);
    if bar > 0.0 {
        shapes.push(egui::Shape::rect_filled(
            egui::Rect::from_min_size(r.rect.min, egui::vec2(bar, r.rect.height())),
            0.0,
            look.c("card-head-bar"),
        ));
    }
    shapes.push(egui::Shape::hline(r.rect.x_range(), r.rect.bottom() - 0.5, Stroke::new(1.0, look.c("line"))));
    ui.painter().set(under, egui::Shape::Vec(shapes));
}

/// The fill and ink of a tag tone, as the web's tag classes: soft green for ok (`bg-ok-soft`), solid for errors,
/// warnings, neutral and strong, the second surface with muted ink for a plain tag.
fn tag_colors(look: &Look, tone: adm_core::logic::Tone) -> (Color32, Color32) {
    use adm_core::logic::Tone;
    match tone {
        Tone::Ok => (look.c("ok-soft"), look.c("on-ok-soft")),
        Tone::Err => (look.c("err-fill"), look.c("on-err-fill")),
        Tone::Warn => (look.c("warn-fill"), look.c("on-warn-fill")),
        Tone::Muted => (look.c("neutral-fill"), look.c("on-neutral-fill")),
        Tone::Strong => (look.c("strong-fill"), look.c("on-strong-fill")),
        Tone::Normal => (look.c("surface-2"), look.c("fg-muted")),
    }
}

/// The galley and box size of a tag: the web's .tag (semibold uppercase at the tag size, 6/1 padding), at least
/// `min_w` wide (the Users status is 104).
fn tag_layout(ui: &Ui, look: &Look, text: &str, ink: Color32, min_w: f32) -> (std::sync::Arc<egui::Galley>, Vec2) {
    let size = look.px("tag-size", 10.0);
    let font = egui::FontId::new(size, egui::FontFamily::Name(crate::fonts::SEMIBOLD.into()));
    let galley = ui.painter().layout_no_wrap(text.to_uppercase(), font, ink);
    let (px, py) = (look.px("tag-px", 6.0), look.px("tag-py", 1.0));
    let w = (galley.size().x + 2.0 * px).max(min_w).ceil();
    let h = (galley.size().y + 2.0 * py).ceil().max(17.0);
    (galley, egui::vec2(w, h))
}

/// A tag (badge) as the web's .tag, as wide as its text. Drawn, not framed: a hover-only widget, so a click on it
/// reaches what is under it (the table row).
pub fn tag(ui: &mut Ui, look: &Look, text: &str, tone: adm_core::logic::Tone) -> Response {
    tag_w(ui, look, text, tone, 0.0)
}

/// A tag at least `min_w` wide, its text centered.
pub fn tag_w(ui: &mut Ui, look: &Look, text: &str, tone: adm_core::logic::Tone, min_w: f32) -> Response {
    let (fill, ink) = tag_colors(look, tone);
    let (galley, size) = tag_layout(ui, look, text, ink, min_w);
    let (rect, resp) = ui.allocate_exact_size(size, egui::Sense::hover());
    ui.painter().rect_filled(rect, 0.0, fill);
    ui.painter().galley(rect.center() - galley.size() / 2.0, galley, ink);
    resp.widget_info(|| egui::WidgetInfo::labeled(egui::WidgetType::Label, true, text));
    resp
}

/// A tag that is a link (an editable account's status, 104 wide as the web's): a button drawn as the tag.
pub fn tag_button(ui: &mut Ui, look: &Look, text: &str, tone: adm_core::logic::Tone) -> Response {
    let (fill, ink) = tag_colors(look, tone);
    let (galley, size) = tag_layout(ui, look, text, ink, 104.0);
    let (rect, resp) = ui.allocate_exact_size(size, egui::Sense::click());
    ui.painter().rect_filled(rect, 0.0, fill);
    ui.painter().galley(rect.center() - galley.size() / 2.0, galley, ink);
    resp.widget_info(|| egui::WidgetInfo::labeled(egui::WidgetType::Button, true, text));
    resp
}

/// The web's page tabs (.subtab: Logs, Backup | Sync): the theme's line under the strip, the other tabs lower by the
/// theme's inset in their recipe, the current one in the subtab-on recipe covering the line; returns a tab clicked.
pub fn subtabs(ui: &mut Ui, look: &Look, id: &str, tabs: &[(&str, &str)], sel: usize) -> Option<usize> {
    let mut out = None;
    let inset = look.px("subtab-inset-top", 0.0);
    let line_w = look.px("subtabs-line-w", 2.0);
    let overlap = look.px("subtab-on-overlap", 0.0);
    let r = ui
        .push_id(id, |ui| {
            ui.horizontal(|ui| {
                ui.spacing_mut().item_spacing.x = look.px("subtabs-gap", 0.0);
                ui.add_space(look.px("subtabs-pad", 0.0));
                for (i, (icon, label)) in tabs.iter().enumerate() {
                    let on = i == sel;
                    let font = egui::FontId::new(12.0, egui::FontFamily::Name(crate::fonts::SEMIBOLD.into()));
                    let g = ui.painter().layout_no_wrap(label.to_string(), font, Color32::PLACEHOLDER);
                    let ig = ui.painter().layout_no_wrap(
                        icon.to_string(),
                        egui::FontId::proportional(13.0),
                        Color32::PLACEHOLDER,
                    );
                    let w = 16.0 + ig.size().x + 6.0 + g.size().x + 16.0;
                    let h = 6.0 + g.size().y.max(ig.size().y) + 6.0;
                    let (rect, resp) = ui.allocate_exact_size(egui::vec2(w, h + inset), egui::Sense::click());
                    let resp = resp.on_hover_cursor(egui::CursorIcon::PointingHand);
                    let top = if on { 0.0 } else { inset };
                    let bottom = if on { overlap } else { 0.0 };
                    let b = egui::Rect::from_min_max(
                        egui::pos2(rect.left(), rect.top() + top),
                        egui::pos2(rect.right(), rect.bottom() + bottom),
                    );
                    let (fill, ink, shadow) = if on {
                        (look.c("subtab-on"), look.c("on-subtab-on"), "subtab-on")
                    } else if resp.hovered() {
                        (look.c("subtab-hover"), look.c("on-subtab"), "subtab")
                    } else {
                        (look.c("subtab"), look.c("on-subtab"), "subtab")
                    };
                    look.paint(ui.painter(), b, "", shadow, fill);
                    let y = rect.top() + top + (h - top.min(0.0)) / 2.0;
                    ui.painter().galley(egui::pos2(b.left() + 16.0, y - ig.size().y / 2.0), ig, ink);
                    ui.painter().galley(egui::pos2(b.left() + 16.0 + 6.0 + 13.0, y - g.size().y / 2.0), g, ink);
                    resp.widget_info(|| egui::WidgetInfo::selected(egui::WidgetType::Button, true, on, *label));
                    if resp.clicked() && !on {
                        out = Some(i);
                    }
                }
            })
        })
        .inner
        .response;
    // the line under the strip; the current tab covers it by its overlap
    let y = r.rect.bottom() + line_w / 2.0;
    ui.painter().hline(ui.max_rect().x_range(), y, Stroke::new(line_w, look.c("subtab-line")));
    ui.add_space(line_w + 12.0);
    out
}

/// The kind of a callout.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Note {
    Info,
    Warn,
}

/// A callout as the web's notes: a tinted box with a bar on the left (accent for information, warning for a
/// recommendation), the text at a 1.5 line height.
pub fn callout(ui: &mut Ui, look: &Look, kind: Note, text: &str) -> Response {
    let (bg, bar) = match kind {
        Note::Info => (look.c("surface-2"), look.c("accent")),
        Note::Warn => (look.c("warn-fill").gamma_multiply(0.12), look.c("warn")),
    };
    let r = Frame::new()
        .fill(bg)
        .inner_margin(Margin { left: 12, right: 10, top: 8, bottom: 8 })
        .show(ui, |ui| {
            ui.set_width(ui.available_width());
            let size = look.px("control-text", 12.0);
            let mut job = egui::text::LayoutJob::single_section(
                text.to_string(),
                egui::TextFormat {
                    font_id: egui::FontId::proportional(size),
                    color: look.c("fg"),
                    line_height: Some(size * 1.6),
                    ..Default::default()
                },
            );
            job.wrap.max_width = ui.available_width();
            ui.label(job);
        })
        .response;
    ui.painter().rect_filled(egui::Rect::from_min_size(r.rect.min, egui::vec2(3.0, r.rect.height())), 0.0, bar);
    r
}

/// A Home tile: the label, a big number in its color, a stripe in the decor color on top (the theme's width).
pub fn stat_tile(ui: &mut Ui, look: &Look, label: &str, value: &str, ink: Color32, decor: Color32) {
    Frame::new().fill(look.c("surface")).stroke(Stroke::new(1.0, look.c("line"))).show(ui, |ui| {
        let w = ui.available_width();
        ui.vertical(|ui| {
            ui.spacing_mut().item_spacing.y = 2.0;
            if look.p.card_stripe > 0.0 {
                let (r, _) = ui.allocate_exact_size(egui::vec2(w, look.p.card_stripe), egui::Sense::hover());
                ui.painter().rect_filled(r, 0.0, decor);
            }
            Frame::new().inner_margin(Margin::symmetric(10, 8)).show(ui, |ui| {
                ui.set_width(w - 20.0);
                ui.add(egui::Label::new(RichText::new(label).size(11.0).strong().color(look.c("fg-muted"))).truncate());
                ui.label(RichText::new(value).size(34.0).strong().color(ink));
            });
        });
    });
}

/// A labeled form (two columns: bold label, field) laid out by egui's grid.
pub fn form<R>(ui: &mut Ui, id: &str, add: impl FnOnce(&mut Ui) -> R) -> R {
    egui::Grid::new(id).num_columns(2).spacing(Vec2::new(10.0, 6.0)).show(ui, add).inner
}

/// The label cell of a form row.
pub fn label(ui: &mut Ui, look: &Look, s: &str) -> Response {
    ui.label(RichText::new(s).strong().color(look.c("fg-strong")))
}

/// A single-line text edit as the web's .inp: a 28 px border box with 8 px side padding, the text vertically centered.
pub fn line(text: &mut dyn egui::TextBuffer) -> egui::TextEdit<'_> {
    egui::TextEdit::singleline(text)
        .margin(Margin::symmetric(8, 0))
        .min_size(egui::vec2(0.0, crate::style::control_h()))
        .vertical_align(egui::Align::Center)
}

/// A single-line text field of `width` (INFINITY: the rest of the row).
pub fn field(ui: &mut Ui, text: &mut String, hint: &str, width: f32) -> Response {
    ui.add(line(text).hint_text(hint).desired_width(width))
}

/// A label above its field, as the web's .lbl: small semibold uppercase in the muted ink.
pub fn lbl(ui: &mut Ui, look: &Look, s: &str) -> Response {
    ui.label(crate::style::semibold(s.to_uppercase(), 11.0).color(look.c("fg-muted")))
}

/// A select of `options` by index.
pub fn select(
    ui: &mut Ui,
    id: impl std::hash::Hash + std::fmt::Debug,
    sel: &mut usize,
    options: &[String],
    width: f32,
) -> bool {
    let before = *sel;
    let shown = options.get(*sel).cloned().unwrap_or_default();
    egui::ComboBox::from_id_salt(id).width(width).selected_text(shown).show_ui(ui, |ui| {
        for (i, o) in options.iter().enumerate() {
            ui.selectable_value(sel, i, o);
        }
    });
    *sel != before
}

/// A form row: a label and a select named by it (for screen readers and tests); true when the choice changed.
pub fn labeled_select(
    ui: &mut Ui,
    look: &Look,
    label: &str,
    id: impl std::hash::Hash + std::fmt::Debug,
    sel: &mut usize,
    options: &[String],
    width: f32,
) -> bool {
    let l = self::label(ui, look, label);
    let before = *sel;
    let shown = options.get(*sel).cloned().unwrap_or_default();
    egui::ComboBox::from_id_salt(id)
        .width(width)
        .selected_text(shown)
        .show_ui(ui, |ui| {
            for (i, o) in options.iter().enumerate() {
                ui.selectable_value(sel, i, o);
            }
        })
        .response
        .labelled_by(l.id);
    *sel != before
}

/// Muted small text.
pub fn muted(ui: &mut Ui, look: &Look, s: &str) -> Response {
    ui.add(egui::Label::new(RichText::new(s).size(12.0).color(look.c("fg-muted"))).wrap())
}

/// A unique id for a password field: each screen or dialog instance gets its own, so nothing typed in one is in
/// another's text or undo history.
pub fn secret_id(name: &str) -> egui::Id {
    static N: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(0);
    egui::Id::new((name, N.fetch_add(1, std::sync::atomic::Ordering::Relaxed)))
}

/// A password field of its own id (hidden text).
pub fn secret_field(ui: &mut Ui, id: egui::Id, text: &mut String, hint: &str, width: f32) -> Response {
    ui.add(line(text).id(id).password(true).hint_text(hint).desired_width(width))
}

/// Forgets a text field's egui state: its text history (undo) is gone with it.
pub fn forget_field(ctx: &egui::Context, id: egui::Id) {
    ctx.data_mut(|d| d.remove::<egui::text_edit::TextEditState>(id));
}

/// A bold label and its field, the field named by the label for screen readers.
pub fn labeled_field(ui: &mut Ui, look: &Look, name: &str, text: &mut String, hint: &str, width: f32) -> Response {
    let l = label(ui, look, name);
    field(ui, text, hint, width).labelled_by(l.id)
}

/// A wrapped label.
pub fn wrapped(ui: &mut Ui, s: &str) -> Response {
    ui.add(egui::Label::new(s).wrap())
}

/// A text that can be selected and copied (a fingerprint to compare).
pub fn selectable(ui: &mut Ui, s: &str) -> Response {
    let mut v = s.to_string();
    ui.add(line(&mut v).desired_width(f32::INFINITY).interactive(true))
}

#[cfg(test)]
mod tests {
    /// Every icon glyph the pages draw is in egui's bundled fonts (a missing one draws an empty box).
    #[test]
    fn icons_have_glyphs() {
        let ctx = egui::Context::default();
        for _ in 0..2 {
            // fonts are set up by the first pass
            let mut out = ctx.run_ui(egui::RawInput::default(), |_| {});
            out.textures_delta.clear();
        }
        let src: String = ["../src", "../src/pages"]
            .iter()
            .flat_map(|d| {
                std::fs::read_dir(std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join(&d[1..]))
                    .into_iter()
                    .flatten()
                    .flatten()
            })
            .filter(|e| e.path().extension().is_some_and(|x| x == "rs"))
            .map(|e| std::fs::read_to_string(e.path()).unwrap_or_default())
            .collect();
        let re = regex::Regex::new(r#""([^"a-zA-Z0-9 \\{}%:/(),.\-]{1,2})""#).unwrap();
        // a glyph the fonts lack is drawn with the replacement glyph: the same atlas cell as a surely missing one
        let font = egui::FontId::proportional(14.0);
        let cell = |s: &str| {
            ctx.fonts_mut(|f| f.layout_no_wrap(s.into(), font.clone(), egui::Color32::WHITE))
                .rows
                .first()
                .and_then(|r| r.row.glyphs.first())
                .map(|g| g.uv_rect)
        };
        let replacement = cell("\u{4E2D}");
        let mut missing = Vec::new();
        for c in re.captures_iter(&src) {
            let g = c[1].to_string();
            let first: String = g.chars().take(1).collect();
            if cell(&first) == replacement && !missing.contains(&g) {
                missing.push(g);
            }
        }
        assert!(missing.is_empty(), "no glyph for {missing:?}");
    }
}
