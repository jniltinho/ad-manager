//! Logs: the audit trail (every change made through the panel, CLI or agent) and the Samba logs of this server and of
//! each file server (through the panel's proxy). A Samba tab loads the first time it is opened: no request goes to
//! every agent at sign-in.

use adm_core::api::{Api, esc, q};
use adm_core::i18n::{date_layout, t};
use adm_core::logic::{Tone, human_bytes};
use adm_core::model::{Agent, AuditEvent, SambaEntry, SambaFile, SambaFiles};
use adm_core::tf;
use chrono::{DateTime, Local};
use egui::{RichText, Ui};

use crate::app::Cx;
use crate::modal::Info;
use crate::table::{self, Cell, Column, TableState};
use crate::widgets::{self, Kind};

#[derive(Default)]
pub struct Logs {
    tab: usize,
    audit: Audit,
    samba: Vec<SambaView>,
}

impl Logs {
    pub fn load(&mut self, cx: &Cx) {
        self.audit.reload(cx);
        self.samba = vec![SambaView::new(t("Samba — this server").into(), cx.panel.clone())];
        let api = cx.panel.clone();
        // each file server's logs through the panel's proxy; without the list only this server's
        cx.spawn(async move { api.get::<Vec<Agent>>("/api/agents").await }, |app, r| {
            let Ok(agents) = r else { return };
            app.with_shell(|s, cx| {
                for a in agents.iter().filter(|a| !a.local()) {
                    // the agent beside the panel reads the same logs as "this server"
                    s.logs.samba.push(SambaView::new(tf!("Samba — %s", a.label()), cx.panel.via(a.id)));
                }
            });
        });
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        ui.horizontal(|ui| {
            ui.selectable_value(&mut self.tab, 0, format!("📄 {}", t("Audit")));
            for (i, v) in self.samba.iter().enumerate() {
                ui.selectable_value(&mut self.tab, i + 1, format!("🗄 {}", v.label));
            }
        });
        ui.add_space(4.0);
        match self.tab {
            0 => self.audit.ui(ui, cx, keys),
            n => {
                if let Some(v) = self.samba.get_mut(n - 1) {
                    if !v.loaded {
                        v.load(cx, n - 1);
                    }
                    v.ui(ui, cx, keys, n - 1);
                }
            }
        }
    }

    /// Whether a Samba tab has asked for its files (tests).
    pub fn samba_loaded(&self) -> Vec<bool> {
        self.samba.iter().map(|v| v.loaded).collect()
    }
}

#[derive(Default)]
struct Audit {
    all: Vec<AuditEvent>,
    visible: Vec<usize>,
    search: String,
    failures: bool,
    table: TableState,
}

impl Audit {
    fn reload(&mut self, cx: &Cx) {
        let api = cx.panel.clone();
        cx.run(async move { api.get::<Vec<AuditEvent>>("/api/audit?limit=2000").await }, |app, r| {
            app.with_shell(|s, _| {
                s.logs.audit.all = r;
                s.logs.audit.apply();
            });
        });
    }

    fn apply(&mut self) {
        let q = self.search.trim().to_lowercase();
        self.visible = (0..self.all.len())
            .filter(|&i| {
                let e = &self.all[i];
                (q.is_empty()
                    || [&e.actor, &e.ip, &e.target, &e.result, &e.action]
                        .map(|s| s.as_str())
                        .join(" ")
                        .to_lowercase()
                        .contains(&q))
                    && (!self.failures || e.result != "ok")
            })
            .collect();
        self.table.clear();
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        widgets::card(ui, &look, t("Audit"), |ui| {
            ui.horizontal(|ui| {
                let w = (ui.available_width() - 420.0).max(200.0);
                let mut changed =
                    super::search(ui, &mut self.search, t("Filter by operator, target, IP…"), w).changed();
                changed |= ui.checkbox(&mut self.failures, t("failures only")).changed();
                if changed {
                    self.apply();
                }
                ui.label(
                    RichText::new(tf!("%d of %d (newest first)", self.visible.len(), self.all.len()))
                        .color(look.c("fg-muted")),
                );
                if widgets::button(ui, &look, "⟳", t("Refresh"), Kind::Secondary).clicked() {
                    self.reload(cx);
                }
            });
            let cols = [
                Column::new(t("Date"), 140.0),
                Column::new(t("Operator"), 140.0),
                Column::new(t("Source"), 120.0),
                Column::new(t("Action"), 150.0),
                Column::new(t("Target"), 240.0),
                Column::new(t("Result"), 110.0).badge(),
            ];
            let (all, vis) = (&self.all, &self.visible);
            let mut hover = None;
            let out = table::Table { id: "audit", cols: &cols, rows: vis.len(), keys, row_h: 28.0, frame: true }
                .show::<()>(ui, &look, &mut self.table, &mut hover, |r, c| {
                    let e = &all[vis[r]];
                    match c {
                        0 => Cell::plain(
                            DateTime::parse_from_rfc3339(&e.time)
                                .map(|d| d.with_timezone(&Local).format(date_layout()).to_string())
                                .unwrap_or_else(|_| e.time.clone()),
                        ),
                        1 => Cell::new(&e.actor, Tone::Strong),
                        2 => Cell::plain(&e.ip),
                        3 => Cell::plain(&e.action),
                        4 => Cell::plain(&e.target),
                        _ if e.result == "ok" => Cell::new("ok", Tone::Ok),
                        _ => Cell::new(&e.result, Tone::Err),
                    }
                });
            // the error is usually longer than the column
            if let Some(e) = out.selected.and_then(|r| vis.get(r)).and_then(|&i| all.get(i))
                && e.result != "ok"
            {
                cx.push(Info::new(format!("{} — {}", e.action, e.target), e.result.clone()));
            }
        });
    }
}

/// The Samba logs of one host: files on the left, the newest entries of the chosen file on the right.
struct SambaView {
    label: String,
    api: Api,
    loaded: bool,
    dir: String,
    list_error: String,
    files: Vec<SambaFile>,
    current: String,
    entries: Vec<SambaEntry>,
    count: String,
    search: String,
    lines: usize,
    table: TableState,
}

const LINES: [usize; 3] = [200, 500, 2000];

impl SambaView {
    fn new(label: String, api: Api) -> Self {
        Self {
            label,
            api,
            loaded: false,
            dir: String::new(),
            list_error: String::new(),
            files: Vec::new(),
            current: String::new(),
            entries: Vec::new(),
            count: String::new(),
            search: String::new(),
            lines: 1,
            table: TableState::default(),
        }
    }

    fn view(app: &mut crate::app::App, i: usize, f: impl FnOnce(&mut SambaView, &mut Cx)) {
        app.with_shell(|s, cx| {
            if let Some(v) = s.logs.samba.get_mut(i) {
                f(v, cx);
            }
        });
    }

    fn load(&mut self, cx: &Cx, i: usize) {
        self.loaded = true;
        let api = self.api.clone();
        cx.run(async move { api.get::<SambaFiles>("/api/samba/logs").await }, move |app, r| {
            Self::view(app, i, |v, cx| {
                v.files = r.files;
                v.dir = r.dir;
                v.list_error = r.error;
                // the file on screen, else the first one with content
                let pick = v
                    .files
                    .iter()
                    .position(|f| f.name == v.current)
                    .or_else(|| if v.current.is_empty() { v.files.iter().position(|f| f.size > 0) } else { None });
                v.current = pick.map(|p| v.files[p].name.clone()).unwrap_or_default();
                v.load_entries(cx, i);
            });
        });
    }

    fn load_entries(&mut self, cx: &Cx, i: usize) {
        self.entries.clear();
        self.table.clear();
        if self.current.is_empty() {
            self.count = t("Choose a file.").into();
            return;
        }
        self.count = t("Loading…").into();
        let name = self.current.clone();
        let path =
            format!("/api/samba/logs/{}?lines={}&q={}", esc(&name), LINES[self.lines.min(2)], q(self.search.trim()));
        let api = self.api.clone();
        cx.run(async move { api.get::<Vec<SambaEntry>>(&path).await }, move |app, r| {
            Self::view(app, i, |v, _| {
                if v.current != name {
                    return; // another file was chosen meanwhile
                }
                v.count = tf!("%d entries (newest first)", r.len());
                v.entries = r;
            });
        });
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool, i: usize) {
        let look = cx.look.clone();
        let w = ui.available_width();
        ui.horizontal_top(|ui| {
            ui.vertical(|ui| {
                ui.set_width(w * 0.22 - 6.0);
                widgets::card(ui, &look, t("Files"), |ui| {
                    ui.label(RichText::new(&self.dir).size(11.0).color(look.c("fg-muted")));
                    if !self.list_error.is_empty() {
                        ui.add(egui::Label::new(RichText::new(&self.list_error).color(look.c("err"))).wrap());
                    }
                    let mut chosen = None;
                    egui::ScrollArea::vertical().id_salt(("samba-files", i)).show(ui, |ui| {
                        for f in &self.files {
                            let ink = if f.size == 0 { look.c("fg-muted") } else { look.c("fg-strong") };
                            let r = ui.horizontal(|ui| {
                                let r = ui.selectable_label(f.name == self.current, RichText::new(&f.name).color(ink));
                                ui.label(RichText::new(human_bytes(f.size)).size(10.0).color(look.c("fg-muted")));
                                r
                            });
                            if r.inner.clicked() {
                                chosen = Some(f.name.clone());
                            }
                        }
                    });
                    if let Some(n) = chosen {
                        self.current = n;
                        self.load_entries(cx, i);
                    }
                });
            });
            ui.vertical(|ui| {
                widgets::card(ui, &look, t("Entries"), |ui| {
                    ui.horizontal(|ui| {
                        let r = super::search(
                            ui,
                            &mut self.search,
                            t("Search in the messages…"),
                            (ui.available_width() - 330.0).max(160.0),
                        );
                        let mut reload = r.lost_focus() && ui.input(|x| x.key_pressed(egui::Key::Enter));
                        let labels: Vec<String> = LINES.iter().map(|n| tf!("last %d", n)).collect();
                        reload |= widgets::select(ui, ("samba-lines", i), &mut self.lines, &labels, 120.0);
                        ui.label(RichText::new(&self.count).color(look.c("fg-muted")));
                        if widgets::button(ui, &look, "⟳", t("Refresh"), Kind::Secondary).clicked() {
                            self.load(cx, i);
                        }
                        if reload {
                            self.load_entries(cx, i);
                        }
                    });
                    let cols = [
                        Column::new(t("Date"), 190.0),
                        Column::new(t("Level"), 60.0).badge(),
                        Column::new(t("Message"), 400.0),
                    ];
                    let entries = &self.entries;
                    let mut hover = None;
                    let out = table::Table {
                        id: &format!("samba-{i}"),
                        cols: &cols,
                        rows: entries.len(),
                        keys,
                        row_h: 0.0,
                        frame: true,
                    }
                    .show::<()>(ui, &look, &mut self.table, &mut hover, |r, c| {
                        let e = &entries[r];
                        match c {
                            0 if e.time.is_empty() => Cell::new("—", Tone::Muted),
                            0 => Cell::plain(&e.time),
                            1 => match e.level {
                                l if l < 0 => Cell::new("—", Tone::Muted),
                                0 => Cell::new("0", Tone::Err),
                                l if l <= 2 => Cell::new(l.to_string(), Tone::Warn),
                                l => Cell::new(l.to_string(), Tone::Strong),
                            },
                            _ => Cell::plain(e.message.replace('\n', " ⏎ ")),
                        }
                    });
                    // the full message (cells show one truncated line)
                    if let Some(e) = out.selected.and_then(|r| entries.get(r)) {
                        cx.push(Info::new(e.time.clone(), format!("{}\n\n{}", e.source, e.message)).wide(720.0));
                    }
                    ui.label(
                        RichText::new(t(
                            "Times as written by Samba, in the server time zone. Read-only, never changed here.",
                        ))
                        .size(11.0)
                        .color(look.c("fg-muted")),
                    );
                });
            });
        });
    }
}
