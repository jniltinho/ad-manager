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

/// A button with an optional icon glyph before its label.
pub fn button(ui: &mut Ui, look: &Look, icon: &str, label: &str, kind: Kind) -> Response {
    let (fill, ink, line) = colors(look, kind);
    let text = match (icon.is_empty(), label.is_empty()) {
        (true, _) => label.to_string(),
        (false, true) => icon.to_string(),
        (false, false) => format!("{icon}  {label}"),
    };
    let mut b = egui::Button::new(RichText::new(text).color(ink)).fill(fill).stroke(Stroke::new(1.0, line));
    if kind == Kind::Link {
        b = b.frame(false);
    }
    let r = ui.add(b);
    if kind == Kind::Link { r.on_hover_cursor(egui::CursorIcon::PointingHand) } else { r }
}

/// An icon-only button with its name as tooltip.
pub fn icon_button(ui: &mut Ui, look: &Look, icon: &str, name: &str) -> Response {
    button(ui, look, icon, "", Kind::Secondary).on_hover_text(name)
}

/// A card: the theme's head with a bar left of the title, the content on the surface, a 1 px frame.
pub fn card<R>(ui: &mut Ui, look: &Look, title: &str, add: impl FnOnce(&mut Ui) -> R) -> R {
    let (head, on_head, bar) = (look.c("card-head"), look.c("on-card-head"), look.c("card-head-bar"));
    Frame::new()
        .fill(look.c("surface"))
        .stroke(Stroke::new(1.0, look.c("line")))
        .show(ui, |ui| {
            ui.spacing_mut().item_spacing.y = 0.0;
            let w = ui.available_width();
            Frame::new().fill(head).inner_margin(Margin { left: 0, right: 10, top: 6, bottom: 6 }).show(ui, |ui| {
                ui.set_width(w - 10.0);
                ui.horizontal(|ui| {
                    let (r, _) = ui.allocate_exact_size(egui::vec2(4.0, 18.0), egui::Sense::hover());
                    ui.painter().rect_filled(r, 0.0, bar);
                    ui.add_space(6.0);
                    ui.label(RichText::new(title).strong().size(14.0).color(on_head));
                });
            });
            Frame::new()
                .inner_margin(Margin::same(10))
                .show(ui, |ui| {
                    ui.spacing_mut().item_spacing.y = 6.0;
                    add(ui)
                })
                .inner
        })
        .inner
}

/// A tag (badge), like the web .tag: the state's fill with its own ink (ok, err, warn, neutral; strong otherwise). Drawn,
/// not framed: a hover-only widget, so a click on it reaches what is under it (the table row).
pub fn tag(ui: &mut Ui, look: &Look, text: &str, tone: adm_core::logic::Tone) -> Response {
    use adm_core::logic::Tone;
    let (fill, ink) = match tone {
        Tone::Ok => (look.c("ok-fill"), look.c("on-ok-fill")),
        Tone::Err => (look.c("err-fill"), look.c("on-err-fill")),
        Tone::Warn => (look.c("warn-fill"), look.c("on-warn-fill")),
        Tone::Muted => (look.c("neutral-fill"), look.c("on-neutral-fill")),
        _ => (look.c("strong-fill"), look.c("on-strong-fill")),
    };
    let galley = ui.painter().layout_no_wrap(text.to_string(), egui::FontId::proportional(11.0), ink);
    let size = egui::vec2((galley.size().x + 12.0).max(64.0), 18.0);
    let (rect, resp) = ui.allocate_exact_size(size, egui::Sense::hover());
    ui.painter().rect_filled(rect, 0.0, fill);
    ui.painter().galley(rect.center() - galley.size() / 2.0, galley, ink);
    resp.widget_info(|| egui::WidgetInfo::labeled(egui::WidgetType::Label, true, text));
    resp
}

/// A tag that is a link (an editable account's status): a button drawn as the tag.
pub fn tag_button(ui: &mut Ui, look: &Look, text: &str, tone: adm_core::logic::Tone) -> Response {
    use adm_core::logic::Tone;
    let (fill, ink) = match tone {
        Tone::Ok => (look.c("ok-fill"), look.c("on-ok-fill")),
        Tone::Err => (look.c("err-fill"), look.c("on-err-fill")),
        Tone::Warn => (look.c("warn-fill"), look.c("on-warn-fill")),
        Tone::Muted => (look.c("neutral-fill"), look.c("on-neutral-fill")),
        _ => (look.c("strong-fill"), look.c("on-strong-fill")),
    };
    ui.add(
        egui::Button::new(RichText::new(text).size(11.0).color(ink))
            .fill(fill)
            .stroke(Stroke::NONE)
            .min_size(egui::vec2(64.0, 18.0)),
    )
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

/// A single-line text field of `width` (INFINITY: the rest of the row).
pub fn field(ui: &mut Ui, text: &mut String, hint: &str, width: f32) -> Response {
    ui.add(egui::TextEdit::singleline(text).hint_text(hint).desired_width(width))
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
    ui.add(egui::TextEdit::singleline(text).id(id).password(true).hint_text(hint).desired_width(width))
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
    ui.add(egui::TextEdit::singleline(&mut v).desired_width(f32::INFINITY).interactive(true))
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
