//! Computers as the web's page: filter, system select, "no logon for N+ days", count, DNS and Export CSV; the table
//! (name, system — red when unsupported —, version, DC / deactivated tags, last logon) with per-row sessions (servers),
//! enable/disable (confirmed), move (a desktop extra) and delete (typed) — never on a domain controller.

use adm_core::api::esc;
use adm_core::i18n::t;
use adm_core::logic::{Tone, computer_inactive, legacy_os, os_label};
use adm_core::model::Computer;
use adm_core::tf;
use chrono::Utc;
use egui::{RichText, Ui};
use reqwest::Method;

use crate::app::Cx;
use crate::table::{self, Cell, Column, TableState};
use crate::widgets::{self, Kind};

#[derive(Default)]
pub struct Computers {
    /// A refresh asked by the toolbar's button, in flight (it says what it read when done).
    refreshing: bool,
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
                if std::mem::take(&mut p.refreshing) {
                    cx.set_status(tf!("%d computers read from the domain.", p.all.len()));
                }
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
        widgets::toolbar_row(ui, &look, |ui| {
            let mut changed = super::search(ui, &mut self.search, t("Filter by name…"), 224.0).changed();
            if self.systems.is_empty() {
                self.systems.push(t("All systems").into());
            }
            changed |= widgets::select(ui, "computers-os", t("System"), &mut self.system, &self.systems.clone(), 256.0);
            changed |= widgets::check(
                ui,
                &mut self.only_inactive,
                RichText::new(tf!("no logon for %d+ days", days)).size(12.0),
            )
            .changed();
            if changed {
                self.apply(cx);
            }
            ui.label(
                RichText::new(tf!("%d of %d", self.visible.len(), self.all.len())).size(12.0).color(look.c("fg-muted")),
            );
            widgets::toolbar_end(ui, |ui| {
                if widgets::reload_keys(ui, self.refreshing) {
                    self.refreshing = true;
                    self.reload(cx);
                }
                if widgets::button(ui, &look, crate::icons::DOWNLOAD, t("Export CSV"), Kind::Secondary).clicked() {
                    let yes = |v: bool| if v { t("yes") } else { t("no") }.to_string();
                    let mut rows = vec![
                        [t("Name"), "DNS", t("System"), t("Version"), t("Active"), "DC", t("Last logon")]
                            .map(String::from)
                            .to_vec(),
                    ];
                    rows.extend(self.visible.iter().filter_map(|&i| self.all.get(i)).map(|c| {
                        vec![
                            c.name.clone(),
                            c.dns_name.clone(),
                            c.os.clone(),
                            c.os_version.clone(),
                            yes(c.enabled),
                            yes(c.is_dc),
                            adm_core::i18n::date(c.last_logon),
                        ]
                    }));
                    crate::files::save_csv(cx, t("computers.csv"), rows);
                }
                if widgets::button(ui, &look, crate::icons::GLOBE, "DNS", Kind::Secondary).clicked() {
                    crate::pages::dns::open(cx);
                }
            });
        });
        widgets::toolbar_gap(ui, &look);
        // the web's auto layout at 1280 (its th widths); the links' column grows when a row has all three
        let three = self
            .visible
            .iter()
            .filter_map(|&i| self.all.get(i))
            .any(|x| x.enabled && !x.is_dc && x.os.to_lowercase().contains("server"));
        let cols = [
            Column::new(t("Name"), 156.0),
            Column::new(t("System"), 250.0).fill(),
            Column::new(t("Version"), 108.0),
            Column::new(t("Status"), 144.0).badge(),
            Column::new(t("Last logon (approx.)"), 206.0),
            Column::new(t("Actions"), if three { 260.0 } else { 183.0 }).compact(96.0).right(),
        ];
        let now = Utc::now();
        let (all, vis) = (&self.all, &self.visible);
        let mut hover = None;
        let out = table::Table {
            id: "computers",
            cols: &cols,
            rows: vis.len(),
            keys,
            row_h: table::ROW_LINKS,
            frame: true,
            view_minus: 260.0,
            heights: None,
        }
        .show(ui, &look, &mut self.table, &mut hover, |r, c| {
            let i = vis[r];
            let x = &all[i];
            let ink = if x.enabled { Tone::Normal } else { Tone::Muted };
            let server = x.is_dc || x.os.to_lowercase().contains("server");
            match c {
                // a deactivated computer's whole row in the subtle ink, as the web's
                0 => Cell::new(&x.name, ink).bold(),
                1 if legacy_os(&x.os) => Cell::new(&x.os, Tone::Err),
                1 => Cell::new(&x.os, ink),
                2 => Cell::new(&x.os_version, ink),
                3 if x.is_dc => Cell::new("DC", Tone::Strong),
                3 if !x.enabled => Cell::new(t("deactivated"), Tone::Muted),
                3 => Cell::plain(""),
                // never logged on counts as inactive, as the web's (its dash in the warning ink)
                4 if computer_inactive(x, days, now) => Cell::new(adm_core::i18n::date(x.last_logon), Tone::Warn),
                4 => Cell::new(adm_core::i18n::date(x.last_logon), ink),
                5 => {
                    let mut cell = Cell::plain("");
                    if x.enabled && server {
                        cell = cell.and_link(
                            t("sessions"),
                            Tone::Normal,
                            crate::icons::USERS,
                            tf!("SMB sessions on %s", x.name),
                            (Act::Sessions, i),
                        );
                    }
                    if !x.is_dc {
                        let (icon, label, hint) = if x.enabled {
                            (crate::icons::POWER, t("disable"), tf!("Disable %s", x.name))
                        } else {
                            (crate::icons::POWER, t("enable"), tf!("Enable %s", x.name))
                        };
                        cell = cell
                            // the web's actions: enable/disable and delete (no move here, as the web)
                            .and_link(label, Tone::Normal, icon, hint, (Act::Toggle, i))
                            .and_link(
                                t("delete"),
                                Tone::Normal,
                                crate::icons::TRASH,
                                tf!("Delete %s", x.name),
                                (Act::Delete, i),
                            );
                    }
                    cell
                }
                _ => Cell::plain(""),
            }
        });
        cx.hover = hover.or(cx.hover.take());
        if let Some((a, i)) = out.clicked
            && let Some(x) = self.all.get(i).cloned()
        {
            match a {
                Act::Sessions => crate::pages::dns::sessions(
                    cx,
                    if x.dns_name.is_empty() { x.name.clone() } else { x.dns_name.clone() },
                ),
                // DCs are never disabled or deleted here (the server refuses too)
                _ if x.is_dc => {}
                Act::Toggle => toggle_computer(cx, x),
                Act::Delete => {
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
            }
        }
    }
}

/// What a row's link does.
#[derive(Clone, Copy)]
enum Act {
    Sessions,
    Toggle,
    Delete,
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
