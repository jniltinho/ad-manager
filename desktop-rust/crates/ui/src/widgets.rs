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
    card_head_note(ui, look, title, "", right);
}

/// A card head with a muted note after its title (the web's "Shares on fs01" and the server's URL).
pub fn card_head_note(ui: &mut Ui, look: &Look, title: &str, note: &str, right: impl FnOnce(&mut Ui)) {
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
                if !note.is_empty() {
                    ui.add_space(6.0);
                    ui.add(egui::Label::new(RichText::new(note).size(size).color(look.c("fg-muted"))).truncate());
                }
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

/// The web's .card with a head (title, muted note, `right` at its right end) and a body without padding (a list or
/// a table edge to edge), at least `min_h` high: a card that fills its column.
pub fn panel<R>(
    ui: &mut Ui,
    look: &Look,
    head: (&str, &str),
    right: impl FnOnce(&mut Ui),
    min_h: f32,
    add: impl FnOnce(&mut Ui) -> R,
) -> R {
    Frame::new()
        .fill(look.c("surface"))
        .stroke(Stroke::new(1.0, look.c("line")))
        .show(ui, |ui| {
            ui.set_min_height((min_h - 2.0).max(0.0));
            ui.spacing_mut().item_spacing.y = 0.0;
            card_head_note(ui, look, head.0, head.1, right);
            add(ui)
        })
        .inner
}

/// An item of a side list (the web's .side-nav li: px 12, py 8, 12 px text): the current one in the theme's nav-on
/// colors and shadow, the others tinted on hover, a divider between items. `add` gets the ink to draw with; the
/// response is the item's click (registered before its content, so links inside it keep their own clicks).
pub fn side_item<R>(
    ui: &mut Ui,
    look: &Look,
    on: bool,
    first: bool,
    add: impl FnOnce(&mut Ui, Color32) -> R,
) -> egui::InnerResponse<R> {
    let under = ui.painter().add(egui::Shape::Noop);
    let ink = if on { look.c("on-nav-on") } else { look.c("fg") };
    let w = ui.available_width();
    let r = ui.scope_builder(egui::UiBuilder::new().sense(egui::Sense::click()), |ui| {
        // the item's texts are not selectable: a click on them picks the item
        ui.style_mut().interaction.selectable_labels = false;
        Frame::new()
            .inner_margin(Margin::symmetric(12, 8))
            .show(ui, |ui| {
                ui.set_width(w - 24.0);
                ui.horizontal(|ui| {
                    ui.spacing_mut().item_spacing.x = 8.0;
                    add(ui, ink)
                })
                .inner
            })
            .inner
    });
    let rect = r.response.rect;
    let mut shapes = if on {
        crate::depth::box_shapes(rect, None, look.c("nav-on"), look.shadows("nav-on"))
    } else if r.response.hovered() {
        vec![egui::Shape::rect_filled(rect, 0.0, look.c("row-hover"))]
    } else {
        Vec::new()
    };
    if !first {
        shapes.push(egui::Shape::hline(rect.x_range(), rect.top() + 0.5, Stroke::new(1.0, look.c("line"))));
    }
    if on {
        shapes.push(egui::Shape::hline(rect.x_range(), rect.bottom() - 0.5, Stroke::new(1.0, look.c("nav-on-line"))));
    }
    ui.painter().set(under, egui::Shape::Vec(shapes));
    let resp = r.response.on_hover_cursor(egui::CursorIcon::PointingHand);
    egui::InnerResponse::new(r.inner, resp)
}

/// The web's `.cardtabs.fit` at the top of a dialog: a band across the dialog's whole width (over the body's
/// padding) with tabs as wide as their labels, the current one in the theme's cardtab-on colors with its bar at the
/// left. Returns the tab clicked.
pub fn cardtabs(ui: &mut Ui, look: &Look, id: &str, tabs: &[&str], sel: usize) -> Option<usize> {
    let (bx, by) = crate::modal::BODY;
    let h = 34.0;
    let (r, _) = ui.allocate_exact_size(egui::vec2(ui.available_width(), h - by), egui::Sense::hover());
    let band =
        egui::Rect::from_min_max(egui::pos2(r.left() - bx, r.top() - by), egui::pos2(r.right() + bx, r.bottom()));
    let painter = ui.painter().with_clip_rect(ui.clip_rect().union(band));
    painter.rect_filled(band, 0.0, look.c("cardtabs"));
    let upper = look.p.text("cardtab-transform") == "uppercase";
    let bar = look.px("cardtab-bar-w", 4.0);
    let font = egui::FontId::new(12.0, egui::FontFamily::Name(crate::fonts::SEMIBOLD.into()));
    let mut x = band.left() + look.px("cardtabs-pad", 0.0);
    let mut out = None;
    for (i, label) in tabs.iter().enumerate() {
        let on = i == sel;
        let text = if upper { label.to_uppercase() } else { label.to_string() };
        let ink = if on { look.c("on-cardtab-on") } else { look.c("on-cardtab") };
        let g = ui.painter().layout_no_wrap(text, font.clone(), ink);
        let w = g.size().x + 24.0 + if on { bar } else { 0.0 };
        let top = if on { band.top() } else { band.top() + look.px("cardtab-inset-top", 0.0) };
        let tab = egui::Rect::from_min_max(egui::pos2(x, top), egui::pos2(x + w, band.bottom()));
        let resp = ui
            .interact(tab.intersect(ui.clip_rect()), ui.id().with((id, i)), egui::Sense::click())
            .on_hover_cursor(egui::CursorIcon::PointingHand);
        let fill = if on {
            look.c("cardtab-on")
        } else if resp.hovered() {
            look.c("cardtab-hover")
        } else {
            look.c("cardtab")
        };
        let shadow = if on { look.shadows("cardtab-on") } else { look.shadows("cardtab") };
        painter.extend(crate::depth::box_shapes(tab, None, fill, shadow));
        if on && bar > 0.0 {
            painter.rect_filled(
                egui::Rect::from_min_size(tab.min, egui::vec2(bar, tab.height())),
                0.0,
                look.c("cardtab-bar"),
            );
        }
        let left = tab.left() + 12.0 + if on { bar } else { 0.0 };
        painter.galley(egui::pos2(left, tab.center().y - g.size().y / 2.0), g, ink);
        let _ = ui.ctx().accesskit_node_builder(resp.id, |n| {
            n.set_role(egui::accesskit::Role::Tab);
            n.set_label(label.to_string());
            if on {
                n.set_selected(true);
            }
        });
        if resp.clicked() && !on {
            out = Some(i);
        }
        x += w + look.px("cardtabs-gap", 0.0);
    }
    painter.hline(band.x_range(), band.bottom() - 0.5, Stroke::new(1.0, look.c("cardtabs-line")));
    ui.add_space(12.0);
    out
}

/// A field with its label above, `w` wide (the web's .lbl over an .inp in a grid cell).
pub fn above(
    ui: &mut Ui,
    look: &Look,
    label: &str,
    w: f32,
    add: impl FnOnce(&mut Ui, egui::Id) -> Response,
) -> Response {
    ui.vertical(|ui| {
        ui.set_width(w);
        ui.spacing_mut().item_spacing.y = 4.0;
        let l = lbl(ui, look, label);
        add(ui, l.id).labelled_by(l.id)
    })
    .inner
}

/// A removable name as the web's chip (11 px text in the third surface, a 1-point line, × to remove); true when its ×
/// was clicked.
pub fn chip(ui: &mut Ui, look: &Look, text: &str) -> bool {
    Frame::new()
        .fill(look.c("surface-3"))
        .stroke(Stroke::new(1.0, look.c("line")))
        .inner_margin(Margin::symmetric(8, 2))
        .show(ui, |ui| {
            ui.spacing_mut().item_spacing.x = 4.0;
            ui.spacing_mut().button_padding = egui::vec2(0.0, 0.0);
            ui.spacing_mut().interact_size.y = 0.0;
            ui.label(RichText::new(text).size(11.0));
            let r = act(ui, look, "", "×", None, 11.0);
            let _ = ui.ctx().accesskit_node_builder(r.id, |n| n.set_label(adm_core::tf!("remove %s", text)));
            r.clicked()
        })
        .inner
}

/// A small link inside a list item or a card head (the web's .act): `ink` when given (the current item's text color),
/// else the link color.
pub fn act(ui: &mut Ui, look: &Look, icon: &str, label: &str, ink: Option<Color32>, size: f32) -> Response {
    let text = if icon.is_empty() { label.to_string() } else { format!("{icon} {label}") };
    ui.add(egui::Button::new(RichText::new(text).size(size).color(ink.unwrap_or(look.c("link")))).frame(false))
        .on_hover_cursor(egui::CursorIcon::PointingHand)
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
    let (bg, bar, ink) = match kind {
        Note::Info => (look.c("surface-2"), look.c("accent"), look.c("fg-muted")),
        Note::Warn => (look.c("warn-soft"), look.c("warn"), look.c("on-warn-soft")),
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
                    color: ink,
                    line_height: Some(size * 1.625),
                    ..Default::default()
                },
            );
            job.wrap.max_width = ui.available_width();
            ui.label(job);
        })
        .response;
    ui.painter().rect_filled(egui::Rect::from_min_size(r.rect.min, egui::vec2(4.0, r.rect.height())), 0.0, bar);
    r
}

/// A Home tile as the web's: a card `w` wide with the decor stripe on top (the theme's width), the label (11 point
/// semibold uppercase, muted), the number (30 point semibold in its color) and a faint icon at the bottom right; the
/// whole tile is a button.
#[allow(clippy::too_many_arguments)]
pub fn stat_tile(
    ui: &mut Ui,
    look: &Look,
    w: f32,
    label: &str,
    value: &str,
    ink: Color32,
    decor: Color32,
    icon: &str,
) -> Response {
    let h = 12.0 + 16.5 + 4.0 + 36.0 + 12.0 + look.p.card_stripe;
    let (rect, resp) = ui.allocate_exact_size(egui::vec2(w, h), egui::Sense::click());
    let resp = resp.on_hover_cursor(egui::CursorIcon::PointingHand);
    let p = ui.painter();
    let line = if resp.hovered() { look.c("card-hover-line") } else { look.c("line") };
    let shadow = if resp.hovered() { "card-hover" } else { "card" };
    crate::depth::paint_box(p, rect, None, look.c("surface"), look.shadows(shadow));
    p.rect_stroke(rect, 0.0, Stroke::new(1.0, line), egui::StrokeKind::Inside);
    if look.p.card_stripe > 0.0 {
        p.rect_filled(egui::Rect::from_min_size(rect.min, egui::vec2(w, look.p.card_stripe)), 0.0, decor);
    }
    p.text(
        rect.right_bottom() - egui::vec2(8.0, 6.0),
        egui::Align2::RIGHT_BOTTOM,
        icon,
        egui::FontId::proportional(36.0),
        ink.gamma_multiply(0.15),
    );
    let top = rect.top() + look.p.card_stripe + 12.0;
    let lg = p.layout_no_wrap(
        label.to_uppercase(),
        egui::FontId::new(11.0, egui::FontFamily::Name(crate::fonts::SEMIBOLD.into())),
        look.c("fg-muted"),
    );
    p.galley(egui::pos2(rect.left() + 12.0, top), lg, look.c("fg-muted"));
    let vg = p.layout_no_wrap(
        value.to_string(),
        egui::FontId::new(30.0, egui::FontFamily::Name(crate::fonts::SEMIBOLD.into())),
        ink,
    );
    p.galley(egui::pos2(rect.left() + 12.0, top + 16.5 + 4.0), vg, ink);
    resp.widget_info(|| egui::WidgetInfo::labeled(egui::WidgetType::Button, true, format!("{label}: {value}")));
    resp
}

/// Draws one row's cell (its index) in a custom way.
pub type RowCell<'a> = dyn FnMut(&mut Ui, usize) + 'a;

/// A small table inside a panel, as the web's .grid-t without its frame (Home's domain controllers): the theme's
/// header band and the rows with their dividers and stripes.
/// `last`, when given, draws the last column of each row (a remove link) instead of its text.
pub fn mini_table<const N: usize>(
    ui: &mut Ui,
    look: &Look,
    heads: [&str; N],
    rows: &[[RichText; N]],
    mut last: Option<&mut RowCell<'_>>,
) {
    let w = ui.available_width();
    let (th_px, th_py, td_py) = (look.px("th-px", 8.0), look.px("th-py", 6.0), look.px("td-py", 4.0));
    let head_h = (12.0 * 1.5 + 2.0 * th_py + 1.0).round();
    let row_h = (12.0 * 1.5 + 2.0 * td_py + 1.0).round();
    // the web's auto layout: each column as wide as its widest cell, the spare width shared in proportion
    let measure = |t: RichText, semibold: bool| {
        let font = egui::FontId::new(
            12.0,
            if semibold {
                egui::FontFamily::Name(crate::fonts::SEMIBOLD.into())
            } else {
                egui::FontFamily::Proportional
            },
        );
        egui::WidgetText::from(t).into_galley(ui, Some(egui::TextWrapMode::Extend), f32::INFINITY, font).size().x
    };
    let natural: Vec<f32> = (0..N)
        .map(|i| {
            let cells = rows.iter().map(|r| measure(r[i].clone().size(12.0), false));
            let widest = cells.fold(measure(RichText::new(heads[i]), true), f32::max);
            // the last column of links (`last`) has no text to measure: room for an icon link
            let widest = if i + 1 == N && last.is_some() { widest.max(24.0) } else { widest };
            widest + 2.0 * th_px
        })
        .collect();
    let sum: f32 = natural.iter().sum::<f32>().max(1.0);
    let col_w: Vec<f32> = natural.iter().map(|n| n * w / sum).collect();
    // rows touch each other, as the web's
    let spacing = std::mem::replace(&mut ui.spacing_mut().item_spacing.y, 0.0);
    let (rect, _) = ui.allocate_exact_size(egui::vec2(w, head_h), egui::Sense::hover());
    crate::depth::paint_box(
        ui.painter(),
        rect,
        look.gradient("table-head"),
        look.c("table-head"),
        look.shadows("table-head"),
    );
    ui.painter().hline(rect.x_range(), rect.bottom() - 0.5, Stroke::new(1.0, look.c("line")));
    let mut x = rect.left();
    for (i, h) in heads.iter().enumerate() {
        let g = ui.painter().layout_no_wrap(
            h.to_string(),
            egui::FontId::new(12.0, egui::FontFamily::Name(crate::fonts::SEMIBOLD.into())),
            look.c("on-table-head"),
        );
        ui.painter().galley(egui::pos2(x + th_px, rect.center().y - g.size().y / 2.0), g, look.c("on-table-head"));
        x += col_w[i];
    }
    for (r, row) in rows.iter().enumerate() {
        let (rect, _) = ui.allocate_exact_size(egui::vec2(w, row_h), egui::Sense::hover());
        if r % 2 == 1 {
            ui.painter().rect_filled(rect, 0.0, look.c("row-alt"));
        }
        if r > 0 {
            ui.painter().hline(rect.x_range(), rect.top() + 0.5, Stroke::new(1.0, look.c("line")));
        }
        let mut x = rect.left();
        for (i, cell) in row.iter().enumerate() {
            let cell_rect =
                egui::Rect::from_min_size(egui::pos2(x + th_px, rect.top()), egui::vec2(col_w[i] - 2.0 * th_px, row_h));
            match (&mut last, i + 1 == N) {
                (Some(f), true) => {
                    let layout = egui::Layout::right_to_left(egui::Align::Center);
                    let mut child = ui.new_child(egui::UiBuilder::new().max_rect(cell_rect).layout(layout));
                    f(&mut child, r);
                }
                _ => {
                    let layout = egui::Layout::left_to_right(egui::Align::Center);
                    let mut child = ui.new_child(egui::UiBuilder::new().max_rect(cell_rect).layout(layout));
                    child.add(egui::Label::new(cell.clone().size(12.0)).truncate());
                }
            }
            x += col_w[i];
        }
    }
    ui.spacing_mut().item_spacing.y = spacing;
}

/// A labeled form (two columns: bold label, field) laid out by egui's grid.
pub fn form<R>(ui: &mut Ui, id: &str, add: impl FnOnce(&mut Ui) -> R) -> R {
    // the web's forms stack each label over its field; `end_row` does nothing outside a grid
    ui.push_id(id, |ui| {
        ui.vertical(|ui| {
            ui.spacing_mut().item_spacing.y = 4.0;
            add(ui)
        })
        .inner
    })
    .inner
}

/// The label of a form field: the web's .lbl, 8 points under the field before it.
pub fn label(ui: &mut Ui, look: &Look, s: &str) -> Response {
    ui.add_space(8.0);
    lbl(ui, look, s)
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
    let r = ui.label(crate::style::semibold(s.to_uppercase(), 11.0).color(look.c("fg-muted")));
    // drawn in capitals, named as written (the web's text-transform leaves the accessible name alone)
    let _ = ui.ctx().accesskit_node_builder(r.id, |n| n.set_value(s.to_string()));
    r
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

/// A select with its label above (the web's .lbl over a select), named by it for screen readers and tests; true
/// when the choice changed.
pub fn labeled_select(
    ui: &mut Ui,
    look: &Look,
    label: &str,
    id: impl std::hash::Hash + std::fmt::Debug,
    sel: &mut usize,
    options: &[String],
    width: f32,
) -> bool {
    let before = *sel;
    ui.vertical(|ui| {
        ui.spacing_mut().item_spacing.y = 4.0;
        let l = lbl(ui, look, label);
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
    });
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
