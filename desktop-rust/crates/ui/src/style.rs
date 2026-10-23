//! The panel theme on egui: a [`Look`] (theme id, palette, flat) turned into egui visuals, the colors pages use for
//! their text tones and the focus ring. Every theme is drawn flat (solid fills and 1 px frames): the flat look the
//! panel offers for every theme.

use adm_core::logic::Tone;
use adm_core::theme::{self, Palette, Rgba};
use egui::{Color32, Context, CornerRadius, Id, LayerId, Order, Stroke, StrokeKind, Visuals};

/// The active theme: the panel's Settings choice, Default until the panel answers.
#[derive(Clone)]
pub struct Look {
    pub id: &'static str,
    pub p: &'static Palette,
}

impl Look {
    /// Theme `id` (unknown: Default).
    pub fn new(id: &str) -> Option<Self> {
        let (id, p) = theme::palette(id)?;
        Some(Self { id, p })
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
        v.faint_bg_color = self.c("row-alt");
        v.code_bg_color = self.c("surface-2");
        v.hyperlink_color = self.c("link");
        v.warn_fg_color = self.c("warn");
        v.error_fg_color = self.c("err");
        v.selection.bg_fill = self.c("selection");
        // egui draws selected text (and a focused field's border) in this color: the ink that reads on the
        // selection; the theme's focus color is the ring drawn outside (focus_ring)
        v.selection.stroke = Stroke::new(1.0, self.c("on-selection"));
        v.window_corner_radius = CornerRadius::ZERO;
        v.menu_corner_radius = CornerRadius::ZERO;
        v.window_shadow = egui::Shadow::NONE;
        v.popup_shadow = egui::Shadow::NONE;
        let line = Stroke::new(1.0, self.c("field-line"));
        let w = &mut v.widgets;
        for (state, fill) in [
            (&mut w.noninteractive, self.c("surface")),
            (&mut w.inactive, self.c("action-2")),
            (&mut w.hovered, self.c("row-hover")),
            (&mut w.active, self.c("selection")),
            (&mut w.open, self.c("action-2")),
        ] {
            state.corner_radius = CornerRadius::ZERO;
            state.bg_fill = fill;
            state.weak_bg_fill = fill;
            state.bg_stroke = line;
            state.fg_stroke = Stroke::new(1.0, self.c("on-action-2"));
            state.expansion = 0.0;
        }
        w.noninteractive.bg_stroke = Stroke::new(1.0, self.c("line"));
        w.noninteractive.fg_stroke = Stroke::new(1.0, fg);
        w.hovered.bg_stroke = Stroke::new(1.0, self.c("focus"));
        ctx.set_visuals(v);
        ctx.all_styles_mut(|s| {
            s.spacing.item_spacing = egui::vec2(8.0, 6.0);
            s.spacing.button_padding = egui::vec2(10.0, 4.0);
            s.spacing.interact_size.y = 26.0;
            // solid scroll bars: floating ones fade in and out over several frames after each scroll
            s.spacing.scroll = egui::style::ScrollStyle::solid();
            // no fades: every frame is a full redraw, an animation costs one per frame
            s.animation_time = 0.0;
        });
    }
}

pub fn c32(c: Rgba) -> Color32 {
    let [r, g, b, a] = c.0;
    Color32::from_rgba_unmultiplied(r, g, b, a)
}

/// Draws the theme's focus ring outside the focused widget (fields, selects, checks, buttons), at the theme's width.
pub fn focus_ring(ctx: &Context, look: &Look) {
    let Some(rect) = ctx.memory(|m| m.focused()).and_then(|f| ctx.read_response(f)).map(|r| r.rect) else { return };
    let w = look.p.focus_ring;
    let painter = ctx.layer_painter(LayerId::new(Order::Foreground, Id::new("adm-focus-ring")));
    painter.rect_stroke(
        rect.expand(w / 2.0 + 1.0),
        CornerRadius::ZERO,
        Stroke::new(w, look.c("focus")),
        StrokeKind::Middle,
    );
}
