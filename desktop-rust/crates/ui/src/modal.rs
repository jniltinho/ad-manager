//! Dialogs as the web panel's Modal: the theme's frame and title bar (its gradient, the title semibold and left-aligned,
//! the bar under it — red for destructive ones), the body, and a footer band with the primary action first, over a
//! dimmed window, the top at 10% of the height. Widths are the web's: 460, or 760 for a wide dialog, at most 94% of the
//! window. A dialog is a type implementing [`Dialog`] (its own state, drawn every frame); the app keeps a stack.

use std::any::Any;

use adm_core::api::ApiError;
use adm_core::i18n::t;
use egui::{Color32, Frame, Margin, RichText, Ui};

use crate::app::Cx;
use crate::widgets::{self, Kind};

/// What a dialog wants after a frame.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Flow {
    Keep,
    Close,
}

pub trait Dialog: Any {
    fn title(&self) -> String;
    fn width(&self) -> f32 {
        480.0
    }
    /// A destructive dialog: red stripe and OK button.
    fn danger(&self) -> bool {
        false
    }
    /// The body and footer (use [`footer`]).
    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow;

    /// The dialog is gone: a dialog with password fields forgets them here (egui keeps their text and undo history).
    fn closed(&self, _ctx: &egui::Context) {}
}

/// The web's dialog width for a requested one: 460, or 760 when wide (over 520), at most 94% of the window.
pub fn web_width(requested: f32, window: f32) -> f32 {
    let w: f32 = if requested > 520.0 { 760.0 } else { 460.0 };
    w.min((window * 0.94).floor())
}

/// The body's padding, as the web's .pnl-body (px 16, py 12).
pub const BODY: (f32, f32) = (16.0, 12.0);

/// Draws a dialog in its frame (title bar, bar, body).
pub fn frame(ui: &mut Ui, d: &mut dyn Dialog, cx: &mut Cx) -> Flow {
    let look = cx.look.clone();
    let w = web_width(d.width(), ui.ctx().content_rect().width());
    let mut flow = Flow::Keep;
    let line_w = look.px("dialog-line-w", 2.0);
    let r = Frame::new().fill(look.c("surface")).show(ui, |ui| {
        ui.set_width(w);
        ui.spacing_mut().item_spacing.y = 0.0;
        // title bar: the theme's gradient, the title left-aligned, the close button right
        let (pad_y, pad_x) = head_pad(look.p.text("dialog-head-pad"));
        let under = ui.painter().add(egui::Shape::Noop);
        let head = Frame::new()
            .inner_margin(Margin {
                left: (pad_x + line_w) as i8,
                right: (pad_x + line_w) as i8,
                top: (pad_y + line_w) as i8,
                bottom: pad_y as i8,
            })
            .show(ui, |ui| {
                ui.set_width(w - 2.0 * (pad_x + line_w));
                ui.horizontal(|ui| {
                    let size = look.px("dialog-head-size", 13.0);
                    ui.label(crate::style::semibold(d.title(), size).color(look.c("on-dialog-head")));
                    ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                        let close =
                            ui.add(egui::Button::new(RichText::new("✖").color(look.c("on-dialog-head"))).frame(false));
                        if close.on_hover_text(t("Close")).clicked() {
                            flow = Flow::Close;
                        }
                    });
                });
            })
            .response;
        ui.painter().set(
            under,
            egui::Shape::Vec(crate::depth::box_shapes(
                head.rect,
                look.gradient("dialog-head"),
                look.c("dialog-head"),
                &[],
            )),
        );
        let bar = look.px("dialog-head-bar-w", 3.0);
        if bar > 0.0 {
            // the full width of the dialog, inside its frame: no notch at either end
            let color = if d.danger() { look.c("err-fill") } else { look.c("dialog-head-bar") };
            let (r, _) = ui.allocate_exact_size(egui::vec2(w, bar), egui::Sense::hover());
            ui.painter().rect_filled(r, 0.0, color);
        }
        Frame::new()
            .inner_margin(Margin {
                left: (BODY.0 + line_w) as i8,
                right: (BODY.0 + line_w) as i8,
                top: BODY.1 as i8,
                bottom: (BODY.1 + line_w) as i8,
            })
            .show(ui, |ui| {
                ui.spacing_mut().item_spacing.y = 6.0;
                ui.set_width(w - 2.0 * (BODY.0 + line_w));
                if d.ui(ui, cx) == Flow::Close {
                    flow = Flow::Close;
                }
            });
    });
    // the frame on top of everything inside, at the theme's width
    ui.painter().rect_stroke(
        r.response.rect,
        0.0,
        egui::Stroke::new(line_w, look.c("dialog-line")),
        egui::StrokeKind::Inside,
    );
    flow
}

/// The title bar's padding from the theme's `dialog-head-pad` ("0.5rem 0.75rem": 8 and 12).
fn head_pad(v: &str) -> (f32, f32) {
    let px = |s: &str| {
        let s = s.trim();
        if let Some(r) = s.strip_suffix("rem") {
            r.parse::<f32>().ok().map(|x| x * 16.0)
        } else {
            s.trim_end_matches("px").parse().ok()
        }
    };
    let mut it = v.split_whitespace().filter_map(px);
    let y = it.next().unwrap_or(8.0);
    (y, it.next().unwrap_or(y.max(12.0)))
}

/// What the footer's buttons did.
#[derive(Debug, Default, Clone, Copy)]
pub struct Footer {
    pub ok: bool,
    pub cancel: bool,
}

/// The footer as the web's: a band across the dialog (the theme's dialog-foot recipe, a line on top) with the buttons
/// at the right, the primary one (`ok`, or the single `close`) first, then Cancel. `enabled` greys the OK.
pub fn footer(ui: &mut Ui, cx: &Cx, ok: Option<&str>, close: &str, danger: bool, enabled: bool) -> Footer {
    let mut f = Footer::default();
    let look = &cx.look;
    ui.add_space(BODY.1);
    let under = ui.painter().add(egui::Shape::Noop);
    let r = ui
        .horizontal(|ui| {
            ui.set_min_height(crate::style::control_h() + 16.0);
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| match ok {
                Some(label) => {
                    // right to left: Cancel at the right end, the primary action before it
                    f.cancel = widgets::button(ui, look, "", t("Cancel"), Kind::Secondary).clicked();
                    let kind = if danger { Kind::Danger } else { Kind::Primary };
                    f.ok = ui.add_enabled_ui(enabled, |ui| widgets::button(ui, look, "✔", label, kind)).inner.clicked();
                }
                // only a way out: the web's ghost button
                None => f.ok = widgets::button(ui, look, "", close, Kind::Secondary).clicked(),
            });
        })
        .response;
    // the band reaches the dialog's sides and bottom: past the body's padding
    let band = r.rect.expand2(egui::vec2(BODY.0, 0.0)).with_max_y(r.rect.max.y + BODY.1);
    let mut shapes = crate::depth::box_shapes(band, look.gradient("dialog-foot"), look.c("surface-2"), &[]);
    shapes.push(egui::Shape::hline(band.x_range(), band.top() + 0.5, egui::Stroke::new(1.0, look.c("line"))));
    let clip = ui.clip_rect().expand2(egui::vec2(BODY.0, BODY.1));
    ui.painter().with_clip_rect(clip).set(under, egui::Shape::Vec(shapes));
    f
}

/// A message with a single OK button.
pub struct Info {
    title: String,
    msg: String,
    width: f32,
}

impl Info {
    pub fn new(title: impl Into<String>, msg: impl Into<String>) -> Self {
        Self { title: title.into(), msg: msg.into(), width: 460.0 }
    }

    pub fn wide(mut self, w: f32) -> Self {
        self.width = w;
        self
    }
}

impl Dialog for Info {
    fn title(&self) -> String {
        self.title.clone()
    }

    fn width(&self) -> f32 {
        self.width
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        egui::ScrollArea::vertical().max_height(420.0).show(ui, |ui| {
            ui.add(egui::Label::new(&self.msg).wrap().selectable(true));
        });
        if footer(ui, cx, None, "OK", false, true).ok { Flow::Close } else { Flow::Keep }
    }
}

/// An error (red stripe); the text is translated when drawn, so a catalog arriving later still translates it.
pub struct ErrorBox(pub ApiError);

impl Dialog for ErrorBox {
    fn title(&self) -> String {
        t("Could not complete").into()
    }

    fn danger(&self) -> bool {
        true
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        ui.horizontal(|ui| {
            ui.label(RichText::new("⚠").size(22.0).color(cx.look.c("err")));
            ui.add(egui::Label::new(self.0.to_string()).wrap().selectable(true));
        });
        if footer(ui, cx, None, "OK", true, true).ok { Flow::Close } else { Flow::Keep }
    }
}

type Action = Box<dyn FnOnce(&mut Cx)>;

/// Asks before running an action.
pub struct Confirm {
    title: String,
    msg: String,
    button: String,
    danger: bool,
    action: Option<Action>,
}

impl Confirm {
    pub fn new(title: &str, msg: String, button: &str, danger: bool, action: impl FnOnce(&mut Cx) + 'static) -> Self {
        Self { title: title.into(), msg, button: button.into(), danger, action: Some(Box::new(action)) }
    }
}

impl Dialog for Confirm {
    fn title(&self) -> String {
        self.title.clone()
    }

    fn danger(&self) -> bool {
        self.danger
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        ui.add(egui::Label::new(&self.msg).wrap());
        let f = footer(ui, cx, Some(&self.button), "", self.danger, true);
        if f.ok {
            if let Some(a) = self.action.take() {
                a(cx);
            }
            return Flow::Close;
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}

/// Runs a destructive action only once the expected text is typed (case-insensitive), as the web panel.
pub struct ConfirmTyped {
    title: String,
    msg: String,
    expected: String,
    typed: String,
    mismatch: bool,
    action: Option<Action>,
    focus: bool,
}

impl ConfirmTyped {
    pub fn new(title: &str, msg: String, expected: &str, action: impl FnOnce(&mut Cx) + 'static) -> Self {
        Self {
            title: title.into(),
            msg,
            expected: expected.into(),
            typed: String::new(),
            mismatch: false,
            action: Some(Box::new(action)),
            focus: true,
        }
    }

    /// Whether `typed` confirms (trimmed, case-insensitive).
    pub fn matches(typed: &str, expected: &str) -> bool {
        typed.trim().to_lowercase() == expected.to_lowercase()
    }
}

impl Dialog for ConfirmTyped {
    fn title(&self) -> String {
        self.title.clone()
    }

    fn width(&self) -> f32 {
        500.0
    }

    fn danger(&self) -> bool {
        true
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        ui.add(egui::Label::new(&self.msg).wrap());
        ui.add_space(4.0);
        ui.label(
            RichText::new(format!("{}{}{}", t("Type "), self.expected, t(" to confirm:")))
                .strong()
                .color(cx.look.c("fg-strong")),
        );
        let r = ui.add(crate::widgets::line(&mut self.typed).hint_text(&self.expected).desired_width(f32::INFINITY));
        if std::mem::take(&mut self.focus) {
            r.request_focus();
        }
        let enter = r.lost_focus() && ui.input(|i| i.key_pressed(egui::Key::Enter));
        if self.mismatch {
            ui.label(RichText::new(t("The text does not match.")).color(cx.look.c("err")));
        }
        let f = footer(ui, cx, Some(t("Confirm")), "", true, true);
        if f.ok || enter {
            if !Self::matches(&self.typed, &self.expected) {
                self.mismatch = true;
                return Flow::Keep;
            }
            if let Some(a) = self.action.take() {
                a(cx);
            }
            return Flow::Close;
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}

/// A dimmed color (for disabled text).
pub fn faded(c: Color32) -> Color32 {
    c.gamma_multiply(0.6)
}

#[cfg(test)]
mod tests {
    use super::ConfirmTyped;

    #[test]
    fn typed_confirmation() {
        assert!(ConfirmTyped::matches(" Ana ", "ana"));
        assert!(!ConfirmTyped::matches("an", "ana"));
        assert!(!ConfirmTyped::matches("", "ana"));
    }
}
