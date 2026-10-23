//! Computers: search, OS filter, "no logon for N+ days", legacy systems in red; enable/disable (confirmed), move and
//! delete (typed) — never a domain controller — and CSV export.

use adm_core::api::esc;
use adm_core::i18n::t;
use adm_core::logic::{Tone, computer_inactive, legacy_os, os_label, ou_path, parent_dn};
use adm_core::model::{Computer, is_set};
use adm_core::tf;
use chrono::Utc;
use egui::{RichText, Ui};
use reqwest::Method;
use serde_json::json;

use super::OuPicker;
use crate::app::Cx;
use crate::modal::{Dialog, Flow, footer};
use crate::table::{self, Cell, Column, TableState};
use crate::widgets::{self, Kind};

#[derive(Default)]
pub struct Computers {
    all: Vec<Computer>,
    visible: Vec<usize>,
    search: String,
    systems: Vec<String>,
    system: usize,
    only_inactive: bool,
    table: TableState,
    generation: u64,
}

impl Computers {
    pub fn load(&mut self, cx: &Cx) {
        self.reload(cx);
    }

    pub fn reload(&mut self, cx: &Cx) {
        self.generation += 1;
        let g = self.generation;
        let api = cx.panel.clone();
        cx.run(async move { api.get::<Vec<Computer>>("/api/computers").await }, move |app, mut r| {
            app.with_shell(|s, cx| {
                let p = &mut s.computers;
                if p.generation != g {
                    return;
                }
                r.sort_by_key(|c| c.name.to_lowercase());
                let chosen = p.systems.get(p.system).cloned();
                let mut systems: Vec<String> = Vec::new();
                for c in &r {
                    let l = os_label(&c.os);
                    if !systems.contains(&l) {
                        systems.push(l);
                    }
                }
                systems.sort();
                systems.insert(0, t("All systems").into());
                p.system = chosen.and_then(|c| systems.iter().position(|s| *s == c)).unwrap_or(0);
                p.systems = systems;
                p.all = r;
                p.apply(cx);
            });
        });
    }

    fn apply(&mut self, cx: &Cx) {
        let q = self.search.trim().to_lowercase();
        let days = cx.inactive_days();
        let now = Utc::now();
        self.visible = (0..self.all.len())
            .filter(|&i| {
                let x = &self.all[i];
                (q.is_empty() || format!("{} {}", x.name, x.dns_name).to_lowercase().contains(&q))
                    && (self.system == 0 || self.systems.get(self.system).is_some_and(|s| *s == os_label(&x.os)))
                    && (!self.only_inactive || computer_inactive(x, days, now))
            })
            .collect();
        self.table.clear();
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        let days = cx.inactive_days();
        widgets::card(ui, &look, t("Domain computers"), |ui| {
            ui.horizontal(|ui| {
                let w = (ui.available_width() - 640.0).max(200.0);
                let mut changed = super::search(ui, &mut self.search, t("Filter by name…"), w).changed();
                if self.systems.is_empty() {
                    self.systems.push(t("All systems").into());
                }
                changed |= widgets::select(ui, "computers-os", &mut self.system, &self.systems.clone(), 240.0);
                changed |= ui.checkbox(&mut self.only_inactive, tf!("no logon for %d+ days", days)).changed();
                if changed {
                    self.apply(cx);
                }
                ui.label(RichText::new(tf!("%d of %d", self.visible.len(), self.all.len())).color(look.c("fg-muted")));
                if widgets::button(ui, &look, "⬇", t("Export CSV"), Kind::Secondary).clicked() {
                    crate::files::export(cx, "computers", t("computers.csv"));
                }
                if widgets::icon_button(ui, &look, "⟳", t("Refresh")).clicked() {
                    self.reload(cx);
                }
            });
            let sel = self.table.sel.and_then(|r| self.visible.get(r)).and_then(|&i| self.all.get(i)).cloned();
            // DCs are never disabled, moved or deleted here
            let ok = sel.as_ref().is_some_and(|x| !x.is_dc);
            ui.horizontal(|ui| {
                let toggle = if sel.as_ref().is_some_and(|x| !x.enabled) { t("Enable") } else { t("Disable") };
                let x = sel.clone().filter(|x| !x.is_dc);
                if ui.add_enabled_ui(ok, |ui| widgets::button(ui, &look, "⏸", toggle, Kind::Secondary)).inner.clicked()
                    && let Some(x) = x.clone()
                {
                    toggle_computer(cx, x);
                }
                if ui
                    .add_enabled_ui(ok, |ui| widgets::button(ui, &look, "📁", t("Move"), Kind::Secondary))
                    .inner
                    .clicked()
                    && let Some(x) = x.clone()
                {
                    let api = cx.panel.clone();
                    cx.run(async move { crate::pages::users::fetch_ous(&api).await }, move |app, ous| {
                        let cur = parent_dn(&x.dn).to_string();
                        app.cx.push(MoveComputer { picker: OuPicker::new(&ous, &cur), x });
                    });
                }
                if ui
                    .add_enabled_ui(ok, |ui| widgets::button(ui, &look, "🗑", t("Delete"), Kind::Danger))
                    .inner
                    .clicked()
                    && let Some(x) = x
                {
                    let name = x.name.clone();
                    cx.confirm_typed(
                        t("Delete computer"),
                        tf!("Delete the computer account %s? It leaves the domain and will have to rejoin.", x.name),
                        &name,
                        move |cx| {
                            let api = cx.panel.clone();
                            let path = format!("/api/computers/{}", esc(&x.sam));
                            cx.run(async move { api.act(Method::DELETE, &path).await }, move |app, ()| {
                                app.cx.set_status(tf!("%s deleted.", x.name));
                                app.with_shell(|s, cx| s.computers.reload(cx));
                            });
                        },
                    );
                }
            });
            let cols = [
                Column::new(t("Name"), 180.0),
                Column::new(t("System"), 260.0),
                Column::new(t("Version"), 120.0),
                Column::new(t("Status"), 110.0).badge(),
                Column::new("DC", 50.0).badge(),
                Column::new(t("Last logon (approx.)"), 160.0),
            ];
            let now = Utc::now();
            let (all, vis) = (&self.all, &self.visible);
            let mut hover = None;
            table::Table { id: "computers", cols: &cols, rows: vis.len(), keys }.show::<()>(
                ui,
                &look,
                &mut self.table,
                &mut hover,
                |r, c| {
                    let x = &all[vis[r]];
                    match c {
                        0 => Cell::new(&x.name, Tone::Strong),
                        1 if legacy_os(&x.os) => Cell::new(&x.os, Tone::Err),
                        1 => Cell::plain(&x.os),
                        2 => Cell::plain(&x.os_version),
                        3 if !x.enabled => Cell::new(t("deactivated").to_uppercase(), Tone::Muted),
                        3 => Cell::new(t("active").to_uppercase(), Tone::Ok),
                        4 if x.is_dc => Cell::new("DC", Tone::Strong),
                        4 => Cell::plain(""),
                        _ if !is_set(&x.last_logon) => Cell::new(t("never"), Tone::Warn),
                        _ if computer_inactive(x, days, now) => {
                            Cell::new(adm_core::i18n::date(x.last_logon), Tone::Warn)
                        }
                        _ => Cell::plain(adm_core::i18n::date(x.last_logon)),
                    }
                },
            );
        });
    }
}

fn toggle_computer(cx: &mut Cx, x: Computer) {
    let run = move |cx: &mut Cx, verb: &'static str, msg: &'static str, x: Computer| {
        let api = cx.panel.clone();
        let path = format!("/api/computers/{}/{verb}", esc(&x.sam));
        cx.run(async move { api.act(Method::POST, &path).await }, move |app, ()| {
            app.cx.set_status(tf!(msg, x.name));
            app.with_shell(|s, cx| s.computers.reload(cx));
        });
    };
    if !x.enabled {
        run(cx, "enable", "%s enabled.", x);
        return;
    }
    cx.confirm(
        t("Disable computer"),
        tf!("Disable the computer %s? Users will not be able to log on to it with a domain account.", x.name),
        t("Disable"),
        true,
        move |cx| run(cx, "disable", "%s disabled.", x),
    );
}

struct MoveComputer {
    x: Computer,
    picker: OuPicker,
}

impl Dialog for MoveComputer {
    fn title(&self) -> String {
        tf!("Move %s", self.x.name)
    }

    fn width(&self) -> f32 {
        560.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        widgets::form(ui, "move-computer", |ui| {
            widgets::label(ui, &look, t("Computer"));
            ui.label(&self.x.name);
            ui.end_row();
            widgets::label(ui, &look, t("Move to"));
            self.picker.ui(ui, "move-computer-ou", 380.0);
            ui.end_row();
        });
        let f = footer(ui, cx, Some(t("Move")), "", false, self.picker.chosen());
        if f.ok && self.picker.chosen() {
            let target = self.picker.dn();
            let api = cx.panel.clone();
            let path = format!("/api/computers/{}/move", esc(&self.x.sam));
            let name = self.x.name.clone();
            let body = json!({ "ou": target });
            cx.run(async move { api.act_with(Method::POST, &path, &body).await }, move |app, ()| {
                app.cx.set_status(tf!("%s moved to %s.", name, ou_path(&target)));
                app.with_shell(|s, cx| s.computers.reload(cx));
            });
            return Flow::Close;
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}
