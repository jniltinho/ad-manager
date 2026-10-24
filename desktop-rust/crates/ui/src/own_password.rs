//! "my password" in the header, as the web's ChangeOwnPasswordDialog: the current password, the new one (generate,
//! show or hide, copy) and its confirmation, sent to `POST /api/me/password`; it closes on success.

use adm_core::i18n::t;
use egui::Ui;
use reqwest::Method;
use serde_json::json;
use zeroize::Zeroizing;

use crate::app::Cx;
use crate::modal::{Dialog, Flow, footer};
use crate::widgets::{self, Kind};

pub struct OwnPassword {
    current: Zeroizing<String>,
    next: Zeroizing<String>,
    confirm: Zeroizing<String>,
    ids: [egui::Id; 3],
    visible: bool,
    busy: bool,
    done: bool,
    err: Option<String>,
    focus: bool,
}

impl OwnPassword {
    pub fn new() -> Self {
        Self {
            current: Zeroizing::default(),
            next: Zeroizing::default(),
            confirm: Zeroizing::default(),
            ids: ["own-current", "own-next", "own-confirm"].map(widgets::secret_id),
            visible: false,
            busy: false,
            done: false,
            err: None,
            focus: true,
        }
    }

    fn change(&mut self, cx: &mut Cx) {
        if *self.next != *self.confirm {
            self.err = Some(t("The confirmation does not match.").into());
            return;
        }
        self.busy = true;
        self.err = None;
        let body = json!({ "old": &*self.current, "new": &*self.next });
        let api = cx.panel.clone();
        let session = cx.panel.id;
        let me = cx.current_dialog();
        cx.spawn(async move { api.act_with(Method::POST, "/api/me/password", &body).await }, move |app, r| {
            if app.cx.panel.id != session {
                return;
            }
            match r {
                Ok(()) => {
                    if let Some(d) = app.cx.dialog_mut::<OwnPassword>(me) {
                        d.done = true;
                    }
                    app.cx.set_status(t("Your password was changed."));
                }
                Err(e) => {
                    if let Some(d) = app.cx.dialog_mut::<OwnPassword>(me) {
                        d.busy = false;
                        d.err = Some(e.to_string());
                    }
                }
            }
        });
    }
}

impl Default for OwnPassword {
    fn default() -> Self {
        Self::new()
    }
}

impl Dialog for OwnPassword {
    fn title(&self) -> String {
        t("Change my password").into()
    }

    fn closed(&self, ctx: &egui::Context) {
        for id in self.ids {
            widgets::forget_field(ctx, id);
        }
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        if self.done {
            return Flow::Close;
        }
        let look = cx.look.clone();
        let l = widgets::lbl(ui, &look, t("Current password"));
        let r = widgets::secret_field(ui, self.ids[0], &mut self.current, "", f32::INFINITY).labelled_by(l.id);
        if std::mem::take(&mut self.focus) {
            r.request_focus();
        }
        ui.add_space(12.0);
        let l = widgets::lbl(ui, &look, t("New password"));
        ui.horizontal(|ui| {
            ui.spacing_mut().item_spacing.x = 0.0;
            let w = ui.available_width() - 250.0;
            let edit = widgets::line(&mut *self.next).id(self.ids[1]).password(!self.visible).desired_width(w);
            let edit = if self.visible { edit.font(egui::TextStyle::Monospace) } else { edit };
            ui.add(edit).labelled_by(l.id);
            if widgets::button(ui, &look, "✨", t("generate"), Kind::Secondary)
                .on_hover_text(t("Generate a strong password"))
                .clicked()
                && let Ok(p) = adm_core::logic::generate_password(cx.me.settings.password_length.max(16) as usize)
            {
                *self.next = p.clone();
                *self.confirm = p;
                self.visible = true;
            }
            let (icon, label) = if self.visible { ("🙈", t("hide")) } else { ("👁", t("show")) };
            if widgets::button(ui, &look, icon, label, Kind::Secondary).clicked() {
                self.visible = !self.visible;
            }
            if ui
                .add_enabled_ui(!self.next.is_empty(), |ui| {
                    widgets::button(ui, &look, "📋", t("copy"), Kind::Secondary)
                })
                .inner
                .clicked()
            {
                ui.ctx().copy_text(self.next.to_string());
                cx.set_status(t("Password copied."));
            }
        });
        ui.add_space(12.0);
        let l = widgets::lbl(ui, &look, t("Confirm"));
        widgets::secret_field(ui, self.ids[2], &mut self.confirm, "", f32::INFINITY).labelled_by(l.id);
        ui.add_space(6.0);
        let note = t("Copy the generated password before changing: it is not shown again.");
        ui.label(egui::RichText::new(note).size(11.0).color(look.c("fg-muted")));
        if let Some(e) = &self.err {
            ui.add_space(6.0);
            ui.colored_label(look.c("err"), e);
        }
        let ready = !self.busy && !self.current.is_empty() && !self.next.is_empty();
        let f = footer(ui, cx, Some(t("Change")), "", false, ready);
        if f.ok && ready {
            self.change(cx);
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}
