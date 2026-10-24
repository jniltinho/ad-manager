//! Logs as the web's page: subtabs for the audit trail (every change made through the panel, CLI or agent), the
//! panel's HTTP log, and the Samba logs of this server and of each file server (through the panel's proxy). The HTTP
//! and Samba tabs load the first time they are opened: no request goes to every agent at sign-in.

use adm_core::api::{Api, esc, q};
use adm_core::i18n::{date_layout, t};
use adm_core::logic::Tone;
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
    http: Http,
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
                    s.logs.samba.push(SambaView::new(tf!("Samba — %s", a.name), cx.panel.via(a.id)));
                }
            });
        });
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let mut tabs: Vec<(&str, String)> = vec![("📄", t("Audit").into()), ("🌐", t("HTTP — panel").into())];
        tabs.extend(self.samba.iter().map(|v| ("🗄", v.label.clone())));
        let refs: Vec<(&str, &str)> = tabs.iter().map(|(i, l)| (*i, l.as_str())).collect();
        if let Some(i) = widgets::subtabs(ui, &cx.look, "logs-tabs", &refs, self.tab) {
            self.tab = i;
        }
        match self.tab {
            0 => self.audit.ui(ui, cx, keys),
            1 => {
                if !self.http.loaded {
                    self.http.reload(cx);
                }
                self.http.ui(ui, cx, keys);
            }
            n => {
                if let Some(v) = self.samba.get_mut(n - 2) {
                    if !v.loaded {
                        v.load(cx, n - 2);
                    }
                    v.ui(ui, cx, keys, n - 2);
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
    /// The actions seen, sorted, after "All actions"; 0 is every action.
    actions: Vec<String>,
    action: usize,
    failures: bool,
    table: TableState,
}

impl Audit {
    fn reload(&mut self, cx: &Cx) {
        let api = cx.panel.clone();
        cx.run(async move { api.get::<Vec<AuditEvent>>("/api/audit?limit=2000").await }, |app, r| {
            app.with_shell(|s, _| {
                let a = &mut s.logs.audit;
                let chosen = a.actions.get(a.action).cloned();
                let mut actions: Vec<String> = r.iter().map(|e| e.action.clone()).collect();
                actions.sort();
                actions.dedup();
                actions.insert(0, t("All actions").into());
                a.action = chosen.and_then(|c| actions.iter().position(|x| *x == c)).unwrap_or(0);
                a.actions = actions;
                a.all = r;
                a.apply();
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
                    && (self.action == 0 || self.actions.get(self.action).is_some_and(|a| *a == e.action))
                    && (!self.failures || e.result != "ok")
            })
            .collect();
        self.table.clear();
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        let mut export = false;
        ui.horizontal(|ui| {
            ui.spacing_mut().item_spacing.x = look.px("toolbar-gap", 8.0);
            let mut changed =
                super::search(ui, &mut self.search, t("Filter by operator, target, IP…"), 256.0).changed();
            if self.actions.is_empty() {
                self.actions.push(t("All actions").into());
            }
            changed |= widgets::select(ui, "audit-action", &mut self.action, &self.actions.clone(), 224.0);
            changed |= ui.checkbox(&mut self.failures, RichText::new(t("failures only")).size(12.0)).changed();
            if changed {
                self.apply();
            }
            let n = tf!("%d of %d (newest first)", self.visible.len(), self.all.len());
            ui.label(RichText::new(n).size(12.0).color(look.c("fg-muted")));
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                export = widgets::button(ui, &look, "⬇", t("Export CSV"), Kind::Secondary).clicked();
                if widgets::button(ui, &look, "⟳", t("Refresh"), Kind::Secondary).clicked() {
                    self.reload(cx);
                }
                if cx.me.asynqmon && widgets::button(ui, &look, "↗", t("Queues (asynqmon)"), Kind::Secondary).clicked()
                {
                    let base = cx.cfg.panel.url.trim_end_matches('/').to_string();
                    ui.ctx().open_url(egui::OpenUrl::new_tab(format!("{base}/asynqmon")));
                }
            });
        });
        ui.add_space(look.px("toolbar-mb", 12.0));
        let cols = [
            Column::new(t("Date"), 150.0),
            Column::new(t("Operator"), 130.0),
            Column::new(t("Source"), 110.0),
            Column::new(t("Action"), 150.0),
            Column::new(t("Target"), 240.0).fill(),
            Column::new(t("Result"), 200.0),
        ];
        let (all, vis) = (&self.all, &self.visible);
        let mut hover = None;
        let out = table::Table { id: "audit", cols: &cols, rows: vis.len(), keys, row_h: 0.0, frame: true }.show::<()>(
            ui,
            &look,
            &mut self.table,
            &mut hover,
            |r, c| {
                let e = &all[vis[r]];
                match c {
                    0 => Cell::plain(when(&e.time)),
                    1 => Cell::new(&e.actor, Tone::Strong).bold(),
                    2 => Cell::plain(&e.ip),
                    3 => Cell::plain(&e.action).mono(),
                    4 => Cell::plain(&e.target),
                    _ if e.result == "ok" => Cell::plain("").with_tag("ok", Tone::Ok),
                    _ => Cell::new(&e.result, Tone::Err),
                }
            },
        );
        if vis.is_empty() {
            ui.add_space(16.0);
            ui.vertical_centered(|ui| ui.label(RichText::new(t("No events.")).size(12.0).color(look.c("fg-muted"))));
        }
        // the error is usually longer than the column
        if let Some(e) = out.selected.and_then(|r| vis.get(r)).and_then(|&i| all.get(i))
            && e.result != "ok"
        {
            cx.push(Info::new(format!("{} — {}", e.action, e.target), e.result.clone()));
        }
        if export {
            let mut rows = vec![
                [t("Date"), t("Operator"), t("Source"), t("Action"), t("Target"), t("Result")]
                    .map(String::from)
                    .to_vec(),
            ];
            rows.extend(vis.iter().map(|&i| {
                let e = &all[i];
                vec![when(&e.time), e.actor.clone(), e.ip.clone(), e.action.clone(), e.target.clone(), e.result.clone()]
            }));
            crate::files::save_csv(cx, t("audit.csv"), rows);
        }
    }
}

/// A time of the panel's logs (RFC 3339) in the operator's format, as it came when it does not parse.
fn when(s: &str) -> String {
    DateTime::parse_from_rfc3339(s)
        .map(|d| d.with_timezone(&Local).format(date_layout()).to_string())
        .unwrap_or_else(|_| s.to_string())
}

/// One request of the panel's HTTP log (`GET /api/http-log`).
#[derive(Debug, Clone, Default, serde::Deserialize)]
#[serde(default)]
struct Request {
    time: String,
    ip: String,
    user: String,
    method: String,
    path: String,
    status: i64,
    ms: f64,
    error: String,
}

/// The panel's HTTP log: every API call with operator, status and duration, from the server's access.log.
#[derive(Default)]
struct Http {
    loaded: bool,
    all: Vec<Request>,
    visible: Vec<usize>,
    search: String,
    /// 0: every code, then 2xx, 3xx, 4xx, 5xx.
    class: usize,
    errors: bool,
    table: TableState,
}

impl Http {
    fn reload(&mut self, cx: &Cx) {
        self.loaded = true;
        let api = cx.panel.clone();
        cx.run(async move { api.get::<Vec<Request>>("/api/http-log?limit=2000").await }, |app, r| {
            app.with_shell(|s, _| {
                s.logs.http.all = r;
                s.logs.http.apply();
            });
        });
    }

    fn apply(&mut self) {
        let q = self.search.trim().to_lowercase();
        self.visible = (0..self.all.len())
            .filter(|&i| {
                let r = &self.all[i];
                (q.is_empty()
                    || [&r.ip, &r.user, &r.method, &r.path, &r.error].iter().any(|v| v.to_lowercase().contains(&q)))
                    && (self.class == 0 || r.status / 100 == self.class as i64 + 1)
                    && (!self.errors || r.status >= 400)
            })
            .collect();
        self.table.clear();
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        let mut export = false;
        ui.horizontal(|ui| {
            ui.spacing_mut().item_spacing.x = look.px("toolbar-gap", 8.0);
            let mut changed = super::search(ui, &mut self.search, t("Filter by path, operator, IP…"), 256.0).changed();
            let codes = [t("All codes").to_string(), "2xx".into(), "3xx".into(), "4xx".into(), "5xx".into()];
            changed |= widgets::select(ui, "http-code", &mut self.class, &codes, 160.0);
            changed |= ui.checkbox(&mut self.errors, RichText::new(t("errors only")).size(12.0)).changed();
            if changed {
                self.apply();
            }
            let n = tf!("%d of %d (newest first)", self.visible.len(), self.all.len());
            ui.label(RichText::new(n).size(12.0).color(look.c("fg-muted")));
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                export = widgets::button(ui, &look, "⬇", t("Export CSV"), Kind::Secondary).clicked();
                if widgets::button(ui, &look, "⟳", t("Refresh"), Kind::Secondary).clicked() {
                    self.reload(cx);
                }
            });
        });
        ui.add_space(look.px("toolbar-mb", 12.0));
        let cols = [
            Column::new(t("Date"), 150.0),
            Column::new(t("Source"), 112.0),
            Column::new(t("Operator"), 128.0),
            Column::new(t("Method"), 64.0),
            Column::new(t("Path"), 260.0).fill(),
            Column::new(t("Code"), 64.0).badge_w(40.0),
            Column::new("ms", 64.0).right(),
            Column::new(t("Error"), 180.0),
        ];
        let (all, vis) = (&self.all, &self.visible);
        let mut hover = None;
        table::Table { id: "http-log", cols: &cols, rows: vis.len(), keys, row_h: 0.0, frame: true }.show::<()>(
            ui,
            &look,
            &mut self.table,
            &mut hover,
            |r, c| {
                let x = &all[vis[r]];
                match c {
                    0 => Cell::plain(when(&x.time)),
                    1 => Cell::plain(&x.ip),
                    2 if x.user.is_empty() => Cell::new("—", Tone::Strong).bold(),
                    2 => Cell::new(&x.user, Tone::Strong).bold(),
                    3 => Cell::plain(&x.method).mono(),
                    4 => Cell::plain(&x.path).mono(),
                    5 => {
                        let tone = match x.status {
                            s if s >= 500 => Tone::Err,
                            s if s >= 400 => Tone::Warn,
                            s if s >= 300 => Tone::Normal,
                            _ => Tone::Ok,
                        };
                        Cell::new(x.status.to_string(), tone)
                    }
                    6 => Cell::plain(format!("{}", (x.ms * 10.0).round() / 10.0)),
                    _ => Cell::new(&x.error, Tone::Err),
                }
            },
        );
        if vis.is_empty() {
            ui.add_space(16.0);
            ui.vertical_centered(|ui| ui.label(RichText::new(t("No requests.")).size(12.0).color(look.c("fg-muted"))));
        }
        if export {
            let mut rows = vec![
                [t("Date"), t("Source"), t("Operator"), t("Method"), t("Path"), t("Code"), "ms", t("Error")]
                    .map(String::from)
                    .to_vec(),
            ];
            rows.extend(vis.iter().map(|&i| {
                let x = &all[i];
                vec![
                    when(&x.time),
                    x.ip.clone(),
                    x.user.clone(),
                    x.method.clone(),
                    x.path.clone(),
                    x.status.to_string(),
                    x.ms.to_string(),
                    x.error.clone(),
                ]
            }));
            crate::files::save_csv(cx, t("http.csv"), rows);
        }
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
    /// Only the entries of level 0 (errors).
    errors: bool,
    table: TableState,
}

const LINES: [usize; 4] = [200, 500, 2000, 5000];

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
            errors: false,
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
        let path = format!(
            "/api/samba/logs/{}?lines={}&q={}",
            esc(&name),
            LINES[self.lines.min(LINES.len() - 1)],
            q(self.search.trim())
        );
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
        let muted = look.c("fg-muted");
        ui.horizontal_top(|ui| {
            ui.spacing_mut().item_spacing.x = 16.0;
            ui.vertical(|ui| {
                ui.set_width(260.0);
                let mut chosen = None;
                widgets::panel(
                    ui,
                    &look,
                    (t("Files"), ""),
                    |_| {},
                    0.0,
                    |ui| {
                        egui::Frame::new().inner_margin(egui::Margin::symmetric(12, 4)).show(ui, |ui| {
                            ui.set_width(ui.available_width());
                            ui.add(
                                egui::Label::new(RichText::new(&self.dir).monospace().size(11.0).color(muted))
                                    .truncate(),
                            );
                        });
                        let line = |ui: &mut Ui| {
                            let y = ui.cursor().top();
                            ui.painter().hline(
                                ui.max_rect().x_range(),
                                y + 0.5,
                                egui::Stroke::new(1.0, look.c("line")),
                            );
                        };
                        line(ui);
                        if !self.list_error.is_empty() {
                            egui::Frame::new().inner_margin(egui::Margin::symmetric(12, 8)).show(ui, |ui| {
                                ui.add(
                                    egui::Label::new(RichText::new(&self.list_error).size(12.0).color(look.c("err")))
                                        .wrap(),
                                );
                            });
                        }
                        let max = (ui.available_height() - 40.0).max(120.0);
                        egui::ScrollArea::vertical().id_salt(("samba-files", i)).max_height(max).show(ui, |ui| {
                            for f in &self.files {
                                let on = f.name == self.current;
                                let under = ui.painter().add(egui::Shape::Noop);
                                let w = ui.available_width();
                                let r = ui.scope_builder(egui::UiBuilder::new().sense(egui::Sense::click()), |ui| {
                                    ui.style_mut().interaction.selectable_labels = false; // a click on the text picks it
                                    egui::Frame::new().inner_margin(egui::Margin::symmetric(12, 6)).show(ui, |ui| {
                                        ui.set_width(w - 24.0);
                                        let ink = if on {
                                            look.c("on-selection")
                                        } else if f.size == 0 {
                                            look.c("fg-subtle")
                                        } else {
                                            look.c("fg")
                                        };
                                        ui.horizontal(|ui| {
                                            ui.add(
                                                egui::Label::new(
                                                    RichText::new(&f.name).monospace().size(11.0).color(ink),
                                                )
                                                .truncate(),
                                            );
                                            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                                                ui.label(RichText::new(samba_size(f.size)).size(10.0).color(ink));
                                            });
                                        });
                                    });
                                });
                                let rect = r.response.rect;
                                let mut shapes = Vec::new();
                                if on {
                                    shapes.push(egui::Shape::rect_filled(rect, 0.0, look.c("selection")));
                                } else if r.response.hovered() {
                                    shapes.push(egui::Shape::rect_filled(rect, 0.0, look.c("row-hover")));
                                }
                                shapes.push(egui::Shape::hline(
                                    rect.x_range(),
                                    rect.bottom() - 0.5,
                                    egui::Stroke::new(1.0, look.c("line")),
                                ));
                                ui.painter().set(under, egui::Shape::Vec(shapes));
                                if r.response.on_hover_cursor(egui::CursorIcon::PointingHand).clicked() {
                                    chosen = Some(f.name.clone());
                                }
                            }
                            if self.files.is_empty() && self.list_error.is_empty() {
                                egui::Frame::new().inner_margin(egui::Margin::symmetric(12, 8)).show(ui, |ui| {
                                    ui.label(RichText::new(t("No log files.")).size(12.0).color(muted));
                                });
                            }
                        });
                    },
                );
                if let Some(n) = chosen {
                    self.current = n;
                    self.load_entries(cx, i);
                }
            });
            ui.vertical(|ui| {
                ui.set_width(ui.available_width());
                let shown: Vec<usize> =
                    (0..self.entries.len()).filter(|&k| !self.errors || self.entries[k].level == 0).collect();
                ui.horizontal(|ui| {
                    ui.spacing_mut().item_spacing.x = look.px("toolbar-gap", 8.0);
                    let r = super::search(ui, &mut self.search, t("Search in the messages…"), 288.0);
                    let mut reload = r.lost_focus() && ui.input(|x| x.key_pressed(egui::Key::Enter));
                    let labels: Vec<String> = LINES.iter().map(|n| tf!("last %d", n)).collect();
                    reload |= widgets::select(ui, ("samba-lines", i), &mut self.lines, &labels, 160.0);
                    ui.checkbox(&mut self.errors, RichText::new(t("level 0 only (errors)")).size(12.0));
                    let count = if self.current.is_empty() || self.count == t("Loading…") {
                        self.count.clone()
                    } else {
                        tf!("%d entries (newest first)", shown.len())
                    };
                    ui.label(RichText::new(count).size(12.0).color(muted));
                    ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                        if widgets::button(ui, &look, "⟳", t("Refresh"), Kind::Secondary).clicked() {
                            self.load(cx, i);
                        }
                    });
                    if reload {
                        self.load_entries(cx, i);
                    }
                });
                ui.add_space(look.px("toolbar-mb", 12.0));
                let cols = [
                    Column::new(t("Date"), 160.0),
                    Column::new(t("Level"), 56.0).badge(),
                    Column::new(t("Message"), 400.0).fill(),
                ];
                let entries = &self.entries;
                let mut hover = None;
                let room = ui.available_height() - 24.0;
                let out = ui
                    .allocate_ui(egui::vec2(ui.available_width(), room), |ui| {
                        table::Table {
                            id: &format!("samba-{i}"),
                            cols: &cols,
                            rows: shown.len(),
                            keys,
                            row_h: 0.0,
                            frame: true,
                        }
                        .show::<()>(ui, &look, &mut self.table, &mut hover, |r, c| {
                            let e = &entries[shown[r]];
                            match c {
                                0 if e.time.is_empty() => Cell::plain("—").mono(),
                                0 => Cell::plain(&e.time).mono(),
                                1 => match e.level {
                                    l if l < 0 => Cell::new("—", Tone::Normal),
                                    0 => Cell::new("0", Tone::Err),
                                    l if l <= 2 => Cell::new(l.to_string(), Tone::Warn),
                                    l => Cell::new(l.to_string(), Tone::Normal),
                                },
                                // shortcut: one line per entry (the web wraps it with its source below); the
                                // whole message opens on a click
                                _ => Cell::plain(e.message.replace('\n', " ¶ ")).mono(),
                            }
                        })
                    })
                    .inner;
                if shown.is_empty() {
                    ui.add_space(16.0);
                    let msg = if self.current.is_empty() { t("Choose a file.") } else { t("No entries.") };
                    ui.vertical_centered(|ui| ui.label(RichText::new(msg).size(12.0).color(muted)));
                }
                // the full message (cells show one truncated line)
                if let Some(e) = out.selected.and_then(|r| shown.get(r)).and_then(|&k| entries.get(k)) {
                    cx.push(Info::new(e.time.clone(), format!("{}\n\n{}", e.source, e.message)).wide(720.0));
                }
                ui.add_space(8.0);
                ui.label(
                    RichText::new(t(
                        "Times as written by Samba, in the server time zone. Read-only, never changed here.",
                    ))
                    .size(11.0)
                    .color(muted),
                );
            });
        });
    }
}

/// A log file's size as the web's: bytes, else KB with one decimal.
fn samba_size(n: i64) -> String {
    if n < 1024 { format!("{n} B") } else { format!("{:.1} KB", n as f64 / 1024.0) }
}
