//! Backup as the web's page: the servers at the left (health, DC tag, number of backups); at the right the chosen
//! server's backups — back up now (kind, folders, full), send a backup, per-row log, download, restore (per kind, with
//! its guards) and delete (typed), the last restoration — and the schedules of a file server.

use std::time::Duration;

use adm_core::api::esc;
use adm_core::forms::RestoreForm;
use adm_core::i18n::{date, server_text, t};
use adm_core::logic::{Tone, backup_code, backup_kind_name, backup_origin, size_text};
use adm_core::model::{BackupManifest, BackupSchedule, BackupServer};
use adm_core::tf;
use egui::{RichText, Ui};
use reqwest::Method;
use serde_json::json;

use crate::app::{App, Cx};
use crate::modal::{Dialog, Flow, footer};
use crate::table::{self, Cell, Column, TableState};
use crate::widgets::{self, Kind};

#[derive(Default)]
pub struct Backup {
    servers: Vec<BackupServer>,
    cur: Option<usize>,
    table: TableState,
    schedules: Vec<BackupSchedule>,
    sched: TableState,
    generation: u64,
    now: BackupNow,
}

/// The API path of a server's backups.
pub fn backup_base(s: &BackupServer) -> String {
    if s.id == 0 { "/api/backups/panel".into() } else { format!("/api/agents/{}/backups", s.id) }
}

impl Backup {
    pub fn load(&mut self, cx: &Cx) {
        self.reload(cx);
    }

    pub fn reload(&mut self, cx: &Cx) {
        self.generation += 1;
        let g = self.generation;
        let api = cx.panel.clone();
        cx.run(async move { api.get::<Vec<BackupServer>>("/api/backups").await }, move |app, r| {
            app.with_shell(|s, cx| {
                let p = &mut s.backup;
                if p.generation != g {
                    return;
                }
                let id = p.current().map(|c| c.id);
                p.servers = r;
                p.cur = id.and_then(|id| p.servers.iter().position(|s| s.id == id)).or(if p.servers.is_empty() {
                    None
                } else {
                    Some(0)
                });
                p.show(cx);
            });
        });
    }

    fn current(&self) -> Option<&BackupServer> {
        self.cur.and_then(|i| self.servers.get(i))
    }

    fn show(&mut self, cx: &Cx) {
        self.table.clear();
        self.schedules.clear();
        self.sched.clear();
        let Some(c) = self.current() else { return };
        if c.id == 0 || !c.error.is_empty() || c.kinds.is_empty() {
            return; // schedules: agents only
        }
        let id = c.id;
        let api = cx.panel.clone();
        cx.run(
            async move { api.get::<Vec<BackupSchedule>>(&format!("/api/agents/{id}/backup-schedules")).await },
            move |app, list| {
                app.with_shell(|s, _| {
                    if s.backup.current().is_some_and(|c| c.id == id) {
                        s.backup.schedules = list;
                    }
                });
            },
        );
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        let h = ui.available_height();
        let mut pick = None;
        let mut refresh = false;
        // shortcut: always side by side (the web stacks below 1024 px), the window's minimum still fits both
        ui.horizontal_top(|ui| {
            ui.spacing_mut().item_spacing.x = 20.0;
            ui.vertical(|ui| {
                ui.set_width(300.0);
                let right = |ui: &mut Ui| refresh = widgets::act(ui, &look, "⟳", t("refresh"), None, 12.0).clicked();
                widgets::panel(ui, &look, (t("Servers"), ""), right, 0.0, |ui| {
                    for (i, s) in self.servers.iter().enumerate() {
                        let on = self.cur == Some(i);
                        let r = widgets::side_item(ui, &look, on, i == 0, |ui, ink| {
                            let dot = if s.error.is_empty() { look.c("ok-fill") } else { look.c("err-fill") };
                            let (r, _) = ui.allocate_exact_size(egui::vec2(8.0, 8.0), egui::Sense::hover());
                            ui.painter().rect_filled(r, 0.0, dot);
                            let name = if s.id == 0 { t("This panel").to_string() } else { s.name.clone() };
                            ui.label(crate::style::semibold(name, 12.0).color(ink));
                            if s.dc {
                                widgets::tag(ui, &look, "DC", Tone::Normal);
                            }
                            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                                let n = if on { ink } else { look.c("fg-muted") };
                                ui.label(RichText::new(s.backups.len().to_string()).size(12.0).color(n));
                            });
                        });
                        if !s.error.is_empty() {
                            r.response.clone().on_hover_text(server_text(&s.error));
                        }
                        if r.response.clicked() && !on {
                            pick = Some(i);
                        }
                    }
                    if self.servers.is_empty() {
                        ui.add_space(12.0);
                        ui.horizontal(|ui| {
                            ui.add_space(12.0);
                            ui.label(RichText::new(t("No servers.")).size(12.0).color(look.c("fg-muted")));
                        });
                        ui.add_space(12.0);
                    }
                });
                ui.add_space(20.0);
                ui.label(
                    RichText::new(t("Each agent makes and keeps its server's backups in its backup folder (default /var/backups/ad-manager); the schedules run on the agent, with the panel off too. A DC backup holds the domain's secrets."))
                        .size(11.0)
                        .color(look.c("fg-muted")),
                );
            });
            ui.vertical(|ui| {
                ui.set_width(ui.available_width());
                let agent = self.current().is_some_and(|c| c.id != 0 && c.error.is_empty() && !c.kinds.is_empty());
                if self.current().is_some() {
                    self.list(ui, cx, keys, if agent { (h * 0.58).floor() } else { h });
                }
                if agent {
                    ui.add_space(20.0);
                    self.schedules_ui(ui, cx);
                }
            });
        });
        if refresh {
            self.reload(cx);
        }
        if let Some(i) = pick {
            self.cur = Some(i);
            self.now = BackupNow::default();
            self.show(cx);
        }
    }

    fn list(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool, h: f32) {
        let look = cx.look.clone();
        let Some(cur) = self.current().cloned() else { return };
        let name = if cur.id == 0 { t("This panel").to_string() } else { cur.name.clone() };
        let title = format!("🗄  {name}");
        let note = tf!("%d backup(s)", cur.backups.len());
        let mut act: Option<(Act, usize)> = None;
        widgets::panel(
            ui,
            &look,
            (&title, &note),
            |_| {},
            h,
            |ui| {
                let muted = look.c("fg-muted");
                egui::Frame::new().inner_margin(egui::Margin::same(12)).show(ui, |ui| {
                    ui.set_width(ui.available_width());
                    if !cur.error.is_empty() {
                        ui.colored_label(look.c("err"), server_text(&cur.error));
                        return;
                    }
                    if cur.kinds.is_empty() {
                        ui.label(
                            RichText::new(t(
                                "This server makes no backup: the agent is read only here (role not known).",
                            ))
                            .size(12.0)
                            .color(muted),
                        );
                        return;
                    }
                    ui.horizontal_wrapped(|ui| {
                        ui.spacing_mut().item_spacing = egui::vec2(8.0, 8.0);
                        let kind = cur.kinds.get(self.now.kind).map_or("", String::as_str);
                        if cur.kinds.len() > 1 {
                            ui.vertical(|ui| {
                                ui.spacing_mut().item_spacing.y = 4.0;
                                widgets::lbl(ui, &look, t("Kind"));
                                let names: Vec<String> = cur.kinds.iter().map(|k| backup_kind_name(k)).collect();
                                widgets::select(ui, "backup-kind", &mut self.now.kind, &names, 384.0);
                            });
                        }
                        if kind == "fileserver-data" {
                            ui.vertical(|ui| {
                                ui.spacing_mut().item_spacing.y = 4.0;
                                widgets::lbl(
                                    ui,
                                    &look,
                                    &tf!("Folders (one per line, inside %s)", cur.roots.join(", ")),
                                );
                                ui.add(
                                    egui::TextEdit::multiline(&mut self.now.folders)
                                        .font(egui::TextStyle::Monospace)
                                        .desired_rows(2)
                                        .desired_width(320.0),
                                );
                                ui.checkbox(
                                    &mut self.now.full,
                                    RichText::new(t("Full backup (starts a new chain)")).size(12.0),
                                );
                            });
                        }
                        if widgets::button(ui, &look, "🗄", t("Back up now"), Kind::Primary).clicked() {
                            act = Some((Act::Now, 0));
                        }
                        if !cur.upload_kinds.is_empty()
                            && widgets::button(ui, &look, "⬆", t("Send a backup"), Kind::Secondary).clicked()
                        {
                            act = Some((Act::Send, 0));
                        }
                    });
                });
                // the last restoration, with its log
                if let Some(r) = &cur.restore {
                    let (st, ink) = match r.status.as_str() {
                        "ok" => (t("ok").to_string(), look.c("ok")),
                        "error" => (server_text(&r.error), look.c("err")),
                        _ => (t("running").to_string(), look.c("warn")),
                    };
                    let y = ui.cursor().top();
                    ui.painter().hline(ui.max_rect().x_range(), y + 0.5, egui::Stroke::new(1.0, look.c("line")));
                    egui::Frame::new().inner_margin(egui::Margin::symmetric(12, 8)).show(ui, |ui| {
                        ui.set_width(ui.available_width());
                        ui.horizontal(|ui| {
                            ui.spacing_mut().item_spacing.x = 8.0;
                            ui.label(RichText::new("🕘").size(12.0).color(ink));
                            ui.label(crate::style::semibold(format!("{}:", t("Last restoration")), 12.0).color(ink));
                            ui.label(
                                RichText::new(format!("{} · {}", backup_kind_name(&r.kind), date(r.started)))
                                    .size(12.0),
                            );
                            if !r.dest.is_empty() {
                                ui.label(RichText::new(format!("→ {}", r.dest)).monospace().size(11.0).color(muted));
                            }
                            ui.label(RichText::new(st).size(12.0).color(ink));
                            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                                if widgets::act(ui, &look, "📄", "", None, 12.0).on_hover_text(t("Log")).clicked() {
                                    act = Some((Act::RestoreLog, 0));
                                }
                            });
                        });
                    });
                }
                let cols = [
                    Column::new(t("Date"), 160.0),
                    Column::new(t("Kind"), 230.0).fill(),
                    Column::new(t("Origin"), 200.0),
                    Column::new(t("Size"), 90.0).right(),
                    Column::new(t("Status"), 110.0),
                    Column::new("", 160.0).right(),
                ];
                let rows = &cur.backups;
                let mut hover = None;
                let out = table::Table { id: "backups", cols: &cols, rows: rows.len(), keys, row_h: 0.0, frame: false }
                    .show(ui, &look, &mut self.table, &mut hover, |r, c| {
                        let b = &rows[r];
                        match c {
                            0 => Cell::plain(date(b.started)),
                            1 if b.kind == "fileserver-data" => Cell::plain(backup_kind_name(&b.kind))
                                .with_tag(if b.full { t("full") } else { t("incremental") }, Tone::Normal),
                            1 => Cell::plain(backup_kind_name(&b.kind)),
                            2 if b.by.is_empty() => Cell::new(backup_origin(&b.origin), Tone::Muted),
                            2 => Cell::new(format!("{} · {}", backup_origin(&b.origin), b.by), Tone::Muted),
                            3 if b.status != "ok" => Cell::plain("—"),
                            3 => Cell::plain(size_text(b.size)),
                            4 => match b.status.as_str() {
                                "ok" => Cell::new(t("ok"), Tone::Ok),
                                "error" => Cell::new(t("error"), Tone::Err),
                                _ => Cell::new(t("running"), Tone::Warn),
                            },
                            _ => {
                                let mut cell =
                                    Cell::plain("").and_link("", Tone::Normal, "📄", t("Log").into(), (Act::Log, r));
                                if b.status == "ok" {
                                    cell = cell
                                        .and_link("", Tone::Normal, "⬇", t("Download").into(), (Act::Download, r))
                                        .and_link("", Tone::Normal, "🕘", t("Restore").into(), (Act::Restore, r));
                                }
                                cell.and_link("", Tone::Normal, "🗑", t("Delete").into(), (Act::Delete, r))
                            }
                        }
                    });
                cx.hover = hover.or(cx.hover.take());
                if out.clicked.is_some() {
                    act = out.clicked;
                }
                if rows.is_empty() {
                    ui.add_space(12.0);
                    ui.vertical_centered(|ui| ui.label(RichText::new(t("No backups yet.")).size(12.0).color(muted)));
                }
            },
        );
        if let Some((a, r)) = act {
            self.act(cx, cur, a, r);
        }
    }

    fn act(&mut self, cx: &mut Cx, s: BackupServer, a: Act, r: usize) {
        let b = s.backups.get(r).cloned();
        match (a, b) {
            (Act::Now, _) => self.back_up_now(cx, s),
            (Act::Send, _) => crate::files::pick_file(cx, move |app, path| {
                app.cx.push(SendBackup { server: s, path, kind: 0 });
            }),
            (Act::RestoreLog, _) => {
                if let Some(r) = s.restore {
                    let mut lines = r.log.clone();
                    if !r.error.is_empty() {
                        lines.push(format!("{}: {}", t("Error"), server_text(&r.error)));
                    }
                    cx.show_info(r.backup.clone(), lines.join("\n"));
                }
            }
            (Act::Log, Some(b)) => {
                let mut lines = b.log.clone();
                if !b.error.is_empty() {
                    lines.push(format!("{}: {}", t("Error"), server_text(&b.error)));
                }
                if lines.is_empty() {
                    lines.push(t("No log.").into());
                }
                cx.show_info(b.id, lines.join("\n"));
            }
            (Act::Download, Some(b)) => {
                let path = format!("{}/{}/download", backup_base(&s), esc(&b.id));
                crate::files::save_download(cx, cx.panel.clone(), path, b.file.clone(), move |app, p| {
                    app.cx.set_status(tf!("Saved to %s (SHA-256 %s)", p.display(), b.sha256));
                });
            }
            (Act::Restore, Some(b)) => {
                let f = RestoreForm::new(&b.kind, &b.folders);
                if f.refused() {
                    cx.show_info(t("Restore"), t("A domain controller backup is not restored over a DC in use: install ad-manager on a new server and, in the initial setup, choose \"Restore a DC\" with this backup (docs/backup.md)."));
                } else {
                    cx.push(Restore { form: f, backup: b, server: s });
                }
            }
            (Act::Delete, Some(b)) => {
                let mut msg = tf!("Delete the backup %s?", b.id);
                if b.full && b.kind == "fileserver-data" {
                    msg = format!("{msg}\n{}", t("The incremental backups of this chain go too."));
                }
                let code = backup_code(&b.id).to_string();
                cx.confirm_typed(t("Delete backup"), msg, &code, move |cx| {
                    let api = cx.panel.clone();
                    let path = format!("{}/{}", backup_base(&s), esc(&b.id));
                    cx.run(async move { api.act(Method::DELETE, &path).await }, move |app, ()| {
                        app.cx.set_status(tf!("%s deleted.", b.id));
                        reload(app);
                    });
                });
            }
            _ => {}
        }
    }

    /// Backs up now: the panel at once, a file server with the kind (and the folders, full or incremental) chosen.
    fn back_up_now(&mut self, cx: &mut Cx, s: BackupServer) {
        if !s.error.is_empty() || s.kinds.is_empty() {
            return;
        }
        if s.id == 0 {
            let api = cx.panel.clone();
            cx.run(async move { api.act(Method::POST, "/api/backups/panel").await }, |app, ()| {
                app.cx.set_status(t("Backup done."));
                reload(app);
            });
            return;
        }
        let k = s.kinds.get(self.now.kind).cloned().unwrap_or_default();
        let fs = lines(&self.now.folders);
        if k == "fileserver-data" && fs.is_empty() {
            cx.show_info(t("Back up now"), tf!("Folders (one per line, inside %s)", s.roots.join(", ")));
            return;
        }
        let body = json!({ "kind": k, "folders": fs, "full": self.now.full && k == "fileserver-data" });
        let api = cx.panel.clone();
        let path = backup_base(&s);
        cx.run(async move { api.act_with(Method::POST, &path, &body).await }, move |app, ()| {
            app.cx.set_status(tf!("Backup queued on %s: it shows in the list when it ends.", s.name));
            reload(app);
        });
    }

    fn schedules_ui(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        let Some(cur) = self.current().cloned() else { return };
        let mut add = false;
        let right = |ui: &mut Ui| add = widgets::act(ui, &look, "+", t("new"), None, 12.0).clicked();
        let mut act: Option<(bool, usize)> = None;
        widgets::panel(ui, &look, (&format!("📅  {}", t("Schedules")), ""), right, 0.0, |ui| {
            let cols = [
                Column::new(t("Name"), 140.0),
                Column::new(t("Kind"), 220.0).fill(),
                Column::new(t("When (cron)"), 120.0),
                Column::new(t("Keep chains"), 100.0),
                Column::new(t("Keep days"), 90.0),
                Column::new(t("New full every (days)"), 160.0),
                Column::new(t("On"), 60.0),
                Column::new("", 40.0).right(),
            ];
            let list = &self.schedules;
            let mut hover = None;
            let out =
                table::Table { id: "schedules", cols: &cols, rows: list.len(), keys: false, row_h: 0.0, frame: false }
                    .show(ui, &look, &mut self.sched, &mut hover, |r, c| {
                        let x = &list[r];
                        match c {
                            0 => Cell::plain(&x.id).mono(),
                            1 => Cell::plain(backup_kind_name(&x.kind)),
                            2 => Cell::plain(&x.cron).mono(),
                            3 => Cell::plain(x.retention.chains.to_string()),
                            4 => Cell::plain(x.retention.days.to_string()),
                            5 if x.kind == "fileserver-data" => Cell::plain(x.full_every_days.to_string()),
                            5 => Cell::new("—", Tone::Muted),
                            6 => {
                                let (text, tone) =
                                    if x.enabled { (t("yes"), Tone::Ok) } else { (t("no"), Tone::Muted) };
                                Cell::new(text, tone).link("", tf!("Turn %s on or off", x.id), (true, r))
                            }
                            _ => Cell::plain("").and_link("", Tone::Normal, "🗑", t("Delete").into(), (false, r)),
                        }
                    });
            cx.hover = hover.or(cx.hover.take());
            act = out.clicked;
            if list.is_empty() {
                ui.add_space(12.0);
                ui.vertical_centered(|ui| {
                    ui.label(RichText::new(t("No schedules.")).size(12.0).color(look.c("fg-muted")))
                });
            }
            let y = ui.cursor().top() + 8.0;
            ui.add_space(8.0);
            ui.painter().hline(ui.max_rect().x_range(), y + 0.5, egui::Stroke::new(1.0, look.c("line")));
            egui::Frame::new().inner_margin(egui::Margin::same(12)).show(ui, |ui| {
                ui.label(
                    RichText::new(t("Retention deletes whole chains only (a full backup and its incrementals), never the backup just made; 0 = no limit."))
                        .size(11.0)
                        .color(look.c("fg-muted")),
                );
            });
        });
        if add {
            cx.push(NewSchedule::new(&cur, &self.schedules));
        }
        match act {
            Some((true, i)) => {
                let mut list = self.schedules.clone();
                list[i].enabled = !list[i].enabled;
                save_schedules(cx, cur.id, list);
            }
            Some((false, i)) => {
                let mut list = self.schedules.clone();
                let x = list.remove(i);
                let id = cur.id;
                cx.confirm(
                    t("Remove schedule"),
                    tf!("Remove the schedule %s? Its backups stay.", x.id),
                    t("Remove"),
                    true,
                    move |cx| save_schedules(cx, id, list),
                );
            }
            None => {}
        }
    }
}

/// What a click on the page asks for, done after drawing.
#[derive(Clone, Copy)]
enum Act {
    Now,
    Send,
    RestoreLog,
    Log,
    Download,
    Restore,
    Delete,
}

/// The "Back up now" form of a file server: the kind, and for data the folders and full or incremental.
#[derive(Default)]
struct BackupNow {
    kind: usize,
    folders: String,
    full: bool,
}

fn reload(app: &mut App) {
    app.with_shell(|s, cx| s.backup.reload(cx));
}

fn save_schedules(cx: &Cx, id: i64, list: Vec<BackupSchedule>) {
    let api = cx.panel.clone();
    cx.run(
        async move { api.act_with(Method::PUT, &format!("/api/agents/{id}/backup-schedules"), &list).await },
        |app, ()| {
            app.cx.set_status(t("Schedules saved."));
            app.with_shell(|s, cx| s.backup.show(cx));
        },
    );
}

fn lines(s: &str) -> Vec<String> {
    s.lines().map(str::trim).filter(|l| !l.is_empty()).map(str::to_string).collect()
}

struct SendBackup {
    server: BackupServer,
    path: std::path::PathBuf,
    kind: usize,
}

impl Dialog for SendBackup {
    fn title(&self) -> String {
        t("Send a backup").into()
    }

    fn width(&self) -> f32 {
        620.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        let names: Vec<String> = self.server.upload_kinds.iter().map(|k| backup_kind_name(k)).collect();
        widgets::form(ui, "send-backup", |ui| {
            widgets::label(ui, &look, t("File"));
            ui.label(self.path.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default());
            ui.end_row();
            widgets::label(ui, &look, t("Kind"));
            widgets::select(ui, "send-kind", &mut self.kind, &names, 320.0);
            ui.end_row();
        });
        widgets::wrapped(
            ui,
            &tf!(
                "The server checks the content for the kind and lists it as sent; then it restores like the others. Up to %d MiB.",
                self.server.upload_max_mib
            ),
        );
        let f = footer(ui, cx, Some(t("Send")), "", false, true);
        if f.ok {
            let k = self.server.upload_kinds.get(self.kind).cloned().unwrap_or_default();
            let path = format!("{}/upload?kind={}", backup_base(&self.server), esc(&k));
            let api = cx.panel.clone();
            let file = self.path.clone();
            cx.set_status(t("Sending…"));
            cx.run(async move { api.upload::<BackupManifest>(&path, &file).await }, |app, m| {
                app.cx.set_status(tf!("Backup sent: %s", m.id));
                reload(app);
            });
            return Flow::Close;
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}

/// A restoration: per kind, what to restore and where; what replaces data in use asks for the typed code.
struct Restore {
    form: RestoreForm,
    backup: BackupManifest,
    server: BackupServer,
}

impl Dialog for Restore {
    fn title(&self) -> String {
        tf!("Restore %s", self.backup.id)
    }

    fn width(&self) -> f32 {
        680.0
    }

    fn danger(&self) -> bool {
        true
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        let code = backup_code(&self.backup.id).to_string();
        let f = &mut self.form;
        match f.kind.as_str() {
            "fileserver-data" => {
                widgets::label(ui, &look, t("Folders"));
                for (name, on) in &mut f.folders {
                    ui.checkbox(on, name.as_str());
                }
                widgets::label(ui, &look, t("Or only one folder inside them"));
                let hint = self.backup.folders.first().map(|x| format!("{x}/Financeiro")).unwrap_or_default();
                widgets::field(ui, &mut f.sub, &hint, f32::INFINITY);
                widgets::label(ui, &look, t("Where"));
                ui.radio_value(
                    &mut f.original,
                    false,
                    t("In a new folder: [share root]/_restored/[date]/ (nothing current is touched)"),
                );
                ui.radio_value(&mut f.original, true, t("Over the original folders: they become a backup first, then go back to how this backup has them (newer files are removed)"));
            }
            "fileserver-config" => {
                widgets::label(ui, &look, t("What to restore (the current configuration becomes a backup first)"));
                for (i, name) in [
                    t("Shares (registry)"),
                    t("Permissions of the share roots"),
                    t("smb.conf (checked with testparm; the current one is kept)"),
                ]
                .iter()
                .enumerate()
                {
                    ui.checkbox(&mut f.parts[i], *name);
                }
            }
            "samba-files" => {
                widgets::wrapped(
                    ui,
                    t(
                        "Samba stops; /etc/samba, /var/lib/samba and krb5.conf are moved aside (…before-restore-[time]) and come back as this backup has them; Samba starts again. On a DC this undoes everything done to the domain since.",
                    ),
                );
            }
            "panel" => {
                widgets::wrapped(
                    ui,
                    t(
                        "The panel's database (settings, registered servers, sources, links) goes back to this backup; the current one becomes a backup first. The panel restarts. The configuration file and the audit log are not restored.",
                    ),
                );
            }
            _ => {}
        }
        if f.needs_code() {
            widgets::labeled_field(
                ui,
                &look,
                &tf!("This replaces what is in use. Type %s to confirm", code),
                &mut f.confirm,
                &code,
                200.0,
            );
        }
        let foot = footer(ui, cx, Some(t("Restore")), "", true, true);
        if foot.ok {
            #[derive(serde::Deserialize, Default)]
            struct Out {
                #[serde(default)]
                restart: bool,
            }
            let body = f.body();
            let api = cx.panel.clone();
            let path = format!("{}/{}/restore", backup_base(&self.server), esc(&self.backup.id));
            let name = self.server.name.clone();
            cx.run(async move { api.send::<Out, _>(Method::POST, &path, Some(&body)).await }, move |app, out| {
                if out.restart {
                    app.cx.show_info(
                        t("Restore"),
                        t("The panel restarts with the restored database: sign in again in a few seconds."),
                    );
                    return;
                }
                app.cx.set_status(tf!("Restoration queued on %s: follow it under Last restoration.", name));
                app.cx.spawn(tokio::time::sleep(Duration::from_secs(2)), |app, ()| reload(app));
            });
            return Flow::Close;
        }
        if foot.cancel { Flow::Close } else { Flow::Keep }
    }
}

const CRON_PRESETS: [&str; 3] = ["0 2 * * *", "0 3 * * 0", "0 3 1 * *"];

struct NewSchedule {
    server: BackupServer,
    existing: Vec<BackupSchedule>,
    id: String,
    kind: usize,
    cron: String,
    chains: String,
    days: String,
    every: String,
    folders: String,
}

impl NewSchedule {
    fn new(s: &BackupServer, existing: &[BackupSchedule]) -> Self {
        Self {
            server: s.clone(),
            existing: existing.to_vec(),
            id: format!("backup-{}", existing.len() + 1),
            kind: 0,
            cron: CRON_PRESETS[0].into(),
            chains: "4".into(),
            days: "0".into(),
            every: "7".into(),
            folders: String::new(),
        }
    }
}

impl Dialog for NewSchedule {
    fn title(&self) -> String {
        t("New schedule").into()
    }

    fn width(&self) -> f32 {
        620.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        let names: Vec<String> = self.server.kinds.iter().map(|k| backup_kind_name(k)).collect();
        widgets::form(ui, "new-schedule", |ui| {
            widgets::labeled_field(ui, &look, t("Name"), &mut self.id, "", 260.0);
            ui.end_row();
            widgets::label(ui, &look, t("Kind"));
            widgets::select(ui, "schedule-kind", &mut self.kind, &names, 320.0);
            ui.end_row();
            widgets::label(ui, &look, t("When (cron)"));
            ui.horizontal(|ui| {
                widgets::field(ui, &mut self.cron, "", 160.0);
                egui::ComboBox::from_id_salt("cron-presets").selected_text("…").show_ui(ui, |ui| {
                    for p in CRON_PRESETS {
                        ui.selectable_value(&mut self.cron, p.to_string(), p);
                    }
                });
            });
            ui.end_row();
            for (label, v) in [
                (t("Keep chains"), &mut self.chains),
                (t("Keep days"), &mut self.days),
                (t("New full every (days)"), &mut self.every),
            ] {
                widgets::labeled_field(ui, &look, label, v, "", 100.0);
                ui.end_row();
            }
            widgets::label(ui, &look, &tf!("Folders (one per line, inside %s)", self.server.roots.join(", ")));
            ui.add(egui::TextEdit::multiline(&mut self.folders).desired_rows(2).desired_width(320.0));
            ui.end_row();
        });
        let f = footer(ui, cx, Some(t("Save")), "", false, true);
        if f.ok {
            let num = |s: &str| s.trim().parse::<i64>().unwrap_or(0);
            let kind = self.server.kinds.get(self.kind).cloned().unwrap_or_default();
            let mut x = BackupSchedule {
                id: self.id.trim().into(),
                cron: self.cron.trim().into(),
                enabled: true,
                ..Default::default()
            };
            x.retention.chains = num(&self.chains);
            x.retention.days = num(&self.days);
            x.full_every_days = num(&self.every);
            if kind == "fileserver-data" {
                x.folders = lines(&self.folders);
            }
            x.kind = kind;
            let mut list = self.existing.clone();
            list.push(x);
            save_schedules(cx, self.server.id, list);
            return Flow::Close;
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}
