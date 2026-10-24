//! Organizational units: readable path, type (OU or default container), description and DN; new OU (inside another
//! or the domain root) and delete (typed; only OUs, never Domain Controllers — the server refuses a non-empty one).

use adm_core::api::q;
use adm_core::i18n::t;
use adm_core::logic::{Tone, is_ou, ou_deletable, ou_path};
use adm_core::model::OrgUnit;
use adm_core::tf;
use egui::{RichText, Ui};
use reqwest::Method;
use serde_json::json;

use super::OuPicker;
use crate::app::Cx;
use crate::modal::{Dialog, Flow, footer};
use crate::table::{self, Cell, Column, TableState};
use crate::widgets::{self, Kind};

#[derive(Default)]
pub struct Ous {
    ous: Vec<OrgUnit>,
    table: TableState,
    generation: u64,
}

impl Ous {
    pub fn load(&mut self, cx: &Cx) {
        self.reload(cx);
    }

    pub fn reload(&mut self, cx: &Cx) {
        self.generation += 1;
        let g = self.generation;
        let api = cx.panel.clone();
        cx.run(async move { crate::pages::users::fetch_ous(&api).await }, move |app, r| {
            app.with_shell(|s, _| {
                if s.ous.generation == g {
                    s.ous.ous = r;
                    s.ous.table.clear();
                }
            });
        });
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        widgets::card(ui, &look, t("Organizational units"), |ui| {
            ui.horizontal(|ui| {
                ui.label(
                    RichText::new(tf!("Organizational units and default containers of %s", cx.me.domain))
                        .color(look.c("fg-muted")),
                );
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    if widgets::icon_button(ui, &look, "⟳", t("Refresh")).clicked() {
                        self.reload(cx);
                    }
                    ui.label(RichText::new(tf!("%d OU(s) and containers", self.ous.len())).color(look.c("fg-muted")));
                });
            });
            let sel = self.table.sel.and_then(|i| self.ous.get(i)).cloned();
            ui.horizontal(|ui| {
                if widgets::button(ui, &look, "📁", t("New OU"), Kind::Primary).clicked() {
                    let parents: Vec<OrgUnit> = self.ous.iter().filter(|o| is_ou(&o.dn)).cloned().collect();
                    cx.push(NewOu {
                        name: String::new(),
                        desc: String::new(),
                        inside: OuPicker::new(&parents, "").with_root(t("domain root")),
                        focus: true,
                    });
                }
                // containers and Domain Controllers cannot be deleted (the server refuses too)
                let deletable = sel.as_ref().is_some_and(ou_deletable);
                if ui
                    .add_enabled_ui(deletable, |ui| widgets::button(ui, &look, "🗑", t("Delete"), Kind::Danger))
                    .inner
                    .clicked()
                    && let Some(o) = sel.filter(ou_deletable)
                {
                    let name = o.name.clone();
                    cx.confirm_typed(
                        t("Delete OU"),
                        tf!("Delete the OU %s? Only possible when it is empty.", ou_path(&o.dn)),
                        &name,
                        move |cx| {
                            let api = cx.panel.clone();
                            let path = format!("/api/ous?dn={}", q(&o.dn));
                            cx.run(async move { api.act(Method::DELETE, &path).await }, move |app, ()| {
                                app.cx.set_status(tf!("OU %s deleted.", o.name));
                                app.with_shell(|s, cx| s.ous.reload(cx));
                            });
                        },
                    );
                }
            });
            let cols = [
                Column::new(t("Path"), 240.0),
                Column::new(t("Type"), 100.0).badge(),
                Column::new(t("Description"), 260.0),
                Column::new("DN", 220.0),
            ];
            let ous = &self.ous;
            let mut hover = None;
            table::Table { id: "ous", cols: &cols, rows: ous.len(), keys, row_h: 0.0 }.show::<()>(
                ui,
                &look,
                &mut self.table,
                &mut hover,
                |r, c| {
                    let o = &ous[r];
                    match c {
                        0 => Cell::new(ou_path(&o.dn), Tone::Strong),
                        1 if is_ou(&o.dn) => Cell::new("OU", Tone::Strong),
                        1 => Cell::new(t("container"), Tone::Muted),
                        2 => Cell::plain(&o.description),
                        _ => Cell::new(&o.dn, Tone::Muted),
                    }
                },
            );
        });
    }
}

struct NewOu {
    name: String,
    desc: String,
    inside: OuPicker,
    focus: bool,
}

impl Dialog for NewOu {
    fn title(&self) -> String {
        t("New OU").into()
    }

    fn width(&self) -> f32 {
        560.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        widgets::form(ui, "new-ou", |ui| {
            let r = widgets::labeled_field(ui, &look, t("Name"), &mut self.name, "", 340.0);
            if std::mem::take(&mut self.focus) {
                r.request_focus();
            }
            ui.end_row();
            widgets::labeled_field(ui, &look, t("Description"), &mut self.desc, "", 340.0);
            ui.end_row();
            widgets::label(ui, &look, t("Inside"));
            self.inside.ui(ui, "new-ou-inside", 340.0);
            ui.end_row();
        });
        let f = footer(ui, cx, Some(t("Create")), "", false, true);
        if f.ok {
            let n = self.name.trim().to_string();
            if n.is_empty() {
                return Flow::Keep;
            }
            let body = json!({ "name": n, "description": self.desc, "parent": self.inside.dn() });
            let api = cx.panel.clone();
            cx.run(async move { api.act_with(Method::POST, "/api/ous", &body).await }, move |app, ()| {
                app.cx.set_status(tf!("OU %s created.", n));
                app.with_shell(|s, cx| s.ous.reload(cx));
            });
            return Flow::Close;
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}
