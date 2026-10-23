//! Dialogs styled like the web panel's: the theme's title bar, a stripe under it (red for destructive ones), the body
//! and a footer with the buttons, over a dimmed window, the top at 10% of the height. A dialog is a type implementing
//! [`Dialog`] (its own state, drawn every frame); the app keeps a stack of them.

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

/// Draws a dialog in its frame (title bar, stripe, body).
pub fn frame(ui: &mut Ui, d: &mut dyn Dialog, cx: &mut Cx) -> Flow {
    let look = cx.look.clone();
    let w = d.width();
    let border = if look.p.depthless { (2.0, look.c("dialog-line")) } else { (1.0, look.c("dialog-head")) };
    let mut flow = Flow::Keep;
    Frame::new().fill(look.c("surface")).stroke(egui::Stroke::new(border.0, border.1)).show(ui, |ui| {
        ui.set_width(w);
        ui.spacing_mut().item_spacing.y = 0.0;
        // title bar with the close button
        Frame::new().fill(look.c("dialog-head")).inner_margin(Margin::symmetric(12, 8)).show(ui, |ui| {
            ui.horizontal(|ui| {
                ui.set_width(w - 24.0);
                let x = 22.0;
                ui.add_space(x);
                ui.with_layout(egui::Layout::centered_and_justified(egui::Direction::LeftToRight), |ui| {
                    ui.set_width(w - 24.0 - 2.0 * x);
                    ui.label(RichText::new(d.title()).strong().size(14.0).color(look.c("on-dialog-head")));
                });
                let close = ui.add(egui::Button::new(RichText::new("✖").color(look.c("on-dialog-head"))).frame(false));
                if close.on_hover_text(t("Close")).clicked() {
                    flow = Flow::Close;
                }
            });
        });
        let stripe = if d.danger() { look.c("err-fill") } else { look.c("dialog-head-bar") };
        let (r, _) = ui.allocate_exact_size(egui::vec2(w, 3.0), egui::Sense::hover());
        ui.painter().rect_filled(r, 0.0, stripe);
        Frame::new().inner_margin(Margin::symmetric(16, 14)).show(ui, |ui| {
            ui.spacing_mut().item_spacing.y = 6.0;
            ui.set_width(w - 32.0);
            if d.ui(ui, cx) == Flow::Close {
                flow = Flow::Close;
            }
        });
    });
    flow
}

/// What the footer's buttons did.
#[derive(Debug, Default, Clone, Copy)]
pub struct Footer {
    pub ok: bool,
    pub cancel: bool,
}

/// The footer: Cancel and `ok` (when given), or a single `close` button; a line above it. `enabled` greys the OK.
pub fn footer(ui: &mut Ui, cx: &Cx, ok: Option<&str>, close: &str, danger: bool, enabled: bool) -> Footer {
    let mut f = Footer::default();
    ui.add_space(10.0);
    ui.separator();
    ui.horizontal(|ui| {
        ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| match ok {
            Some(label) => {
                let kind = if danger { Kind::Danger } else { Kind::Primary };
                f.ok = ui.add_enabled_ui(enabled, |ui| widgets::button(ui, &cx.look, "✔", label, kind)).inner.clicked();
                f.cancel = widgets::button(ui, &cx.look, "✖", t("Cancel"), Kind::Secondary).clicked();
            }
            None => {
                f.ok = widgets::button(ui, &cx.look, "✔", close, Kind::Primary).clicked();
            }
        });
    });
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
        let r =
            ui.add(egui::TextEdit::singleline(&mut self.typed).hint_text(&self.expected).desired_width(f32::INFINITY));
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
