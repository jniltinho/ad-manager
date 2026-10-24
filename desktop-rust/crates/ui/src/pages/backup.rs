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
    /// The schedules of the server on screen, being edited.
    rows: Vec<SchedRow>,
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
                // another server on screen: its schedules; the same one keeps the schedules being edited
                if p.current().map(|c| c.id) != id || p.rows.is_empty() {
                    p.show(cx);
                }
                // a backup or restoration still running: look again shortly (as the web's 4 s)
                let running = p.servers.iter().any(|s| {
                    s.restore.as_ref().is_some_and(|r| r.status == "running")
                        || s.backups.iter().any(|b| b.status == "running")
                });
                if running {
                    let g = p.generation;
                    cx.spawn(tokio::time::sleep(std::time::Duration::from_secs(4)), move |app, ()| {
                        app.with_shell(|s, cx| {
                            if s.backup.generation == g {
                                s.backup.reload(cx);
                            }
                        });
                    });
                }
            });
        });
    }

    fn current(&self) -> Option<&BackupServer> {
        self.cur.and_then(|i| self.servers.get(i))
    }

    fn show(&mut self, cx: &Cx) {
        self.table.clear();
        self.rows.clear();
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
                    if let Some(c) = s.backup.current().filter(|c| c.id == id) {
                        let kinds = c.kinds.clone();
                        s.backup.rows = list.iter().map(|x| SchedRow::from(x, &kinds)).collect();
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
                let right = |ui: &mut Ui| refresh = widgets::act(ui, &look, crate::icons::REFRESH, t("refresh"), None, 12.0).clicked();
                widgets::panel(ui, &look, (t("Servers"), ""), right, 0.0, |ui| {
                    for (i, s) in self.servers.iter().enumerate() {
                        let on = self.cur == Some(i);
                        let r = widgets::side_item(ui, &look, on, i == 0, |ui, ink| {
                            let dot = if s.error.is_empty() { look.c("ok-fill") } else { look.c("err-fill") };
                            let (r, _) = widgets::status_square(ui);
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
                        .line_height(Some(widgets::LEADING * 11.0))
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
        let title = format!("{}   {name}", crate::icons::ARCHIVE);
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
                                widgets::select(ui, "backup-kind", t("Kind"), &mut self.now.kind, &names, 384.0);
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
                                crate::widgets::check(
                                    ui,
                                    &mut self.now.full,
                                    RichText::new(t("Full backup (starts a new chain)")).size(12.0),
                                );
                            });
                        }
                        if widgets::button(ui, &look, crate::icons::ARCHIVE, t("Back up now"), Kind::Primary).clicked()
                        {
                            act = Some((Act::Now, 0));
                        }
                        if !cur.upload_kinds.is_empty()
                            && widgets::button(ui, &look, crate::icons::UPLOAD, t("Send a backup"), Kind::Secondary)
                                .clicked()
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
                            ui.label(RichText::new(crate::icons::HISTORY).size(12.0).color(ink));
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
                                let log = widgets::act(ui, &look, crate::icons::SCROLL, "", None, 12.0);
                                if widgets::named(ui, log, t("Log")).clicked() {
                                    act = Some((Act::RestoreLog, 0));
                                }
                            });
                        });
                    });
                }
                let cols = [
                    Column::new(t("Date"), 160.0),
                    Column::new(t("Kind"), 160.0).fill(),
                    Column::new(t("Origin"), 180.0),
                    Column::new(t("Size"), 90.0).right(),
                    Column::new(t("Status"), 110.0),
                    Column::new("", 160.0).right(), // the web's w-40
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
                                let mut cell = Cell::plain("").and_link(
                                    "",
                                    Tone::Normal,
                                    crate::icons::SCROLL,
                                    t("Log").into(),
                                    (Act::Log, r),
                                );
                                if b.status == "ok" {
                                    cell = cell
                                        .and_link(
                                            "",
                                            Tone::Normal,
                                            crate::icons::DOWNLOAD,
                                            t("Download").into(),
                                            (Act::Download, r),
                                        )
                                        .and_link(
                                            "",
                                            Tone::Normal,
                                            crate::icons::ROTATE,
                                            t("Restore").into(),
                                            (Act::Restore, r),
                                        );
                                }
                                cell.and_link(
                                    "",
                                    Tone::Normal,
                                    crate::icons::TRASH,
                                    t("Delete").into(),
                                    (Act::Delete, r),
                                )
                            }
                        }
                    });
                cx.hover = hover.or(cx.hover.take());
                if out.clicked.is_some() {
                    act = out.clicked;
                }
                if rows.is_empty() {
                    // the web's empty row: its text centered, a border under it
                    let (row, _) = ui.allocate_exact_size(egui::vec2(ui.available_width(), 40.0), egui::Sense::hover());
                    let g = ui.painter().layout_no_wrap(
                        t("No backups yet.").to_string(),
                        egui::FontId::proportional(12.0),
                        muted,
                    );
                    ui.painter().galley(row.center() - g.size() / 2.0, g, muted);
                    ui.painter().hline(row.x_range(), row.bottom() - 0.5, egui::Stroke::new(1.0, look.c("line")));
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

    /// The schedules of a file server edited in place, as the web's: name, kind, when (cron, with presets), chains
    /// and days kept, a new full every N days (data folders), on, delete; the folders of a data schedule under its
    /// row; + new adds a row and "Save schedules" sends them all.
    fn schedules_ui(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        let Some(cur) = self.current().cloned() else { return };
        let mut add = false;
        let right = |ui: &mut Ui| add = widgets::act(ui, &look, "+", t("new"), None, 12.0).clicked();
        let mut save = false;
        let kinds: Vec<String> = cur.kinds.iter().map(|k| backup_kind_name(k)).collect();
        let roots = cur.roots.join(", ");
        widgets::panel(
            ui,
            &look,
            (&format!("{}   {}", crate::icons::CALENDAR, t("Schedules")), ""),
            right,
            0.0,
            |ui| {
                let muted = look.c("fg-muted");
                // the columns: name, kind, cron, chains, days, full every, on, delete
                // the kind takes what the others leave
                let mut widths = [136.0, 0.0, 150.0, 100.0, 90.0, 176.0, 40.0, 36.0];
                widths[1] = (ui.available_width() - widths.iter().sum::<f32>() - 12.0).max(160.0);
                let heads = [
                    t("Name"),
                    t("Kind"),
                    t("When (cron)"),
                    t("Keep chains"),
                    t("Keep days"),
                    t("New full every (days)"),
                    t("On"),
                    "",
                ];
                let (th_px, head_h) = (look.px("th-px", 8.0), 31.0);
                let (hr, _) = ui.allocate_exact_size(egui::vec2(ui.available_width(), head_h), egui::Sense::hover());
                crate::depth::paint_box(
                    ui.painter(),
                    hr,
                    look.gradient("table-head"),
                    look.c("table-head"),
                    look.shadows("table-head"),
                );
                ui.painter().hline(hr.x_range(), hr.bottom() - 0.5, egui::Stroke::new(1.0, look.c("line")));
                let mut x = hr.left();
                let font = egui::FontId::new(12.0, egui::FontFamily::Name(crate::fonts::SEMIBOLD.into()));
                for (i, h) in heads.iter().enumerate() {
                    let g = ui.painter().layout_no_wrap(h.to_string(), font.clone(), look.c("on-table-head"));
                    ui.painter().galley(
                        egui::pos2(x + th_px, hr.center().y - g.size().y / 2.0),
                        g,
                        look.c("on-table-head"),
                    );
                    x += widths[i];
                }
                let mut drop = None;
                for (r, row) in self.rows.iter_mut().enumerate() {
                    let data = cur.kinds.get(row.kind).is_some_and(|k| k == "fileserver-data");
                    egui::Frame::new().inner_margin(egui::Margin::symmetric(0, 4)).show(ui, |ui| {
                        ui.horizontal(|ui| {
                            ui.spacing_mut().item_spacing.x = 0.0;
                            let cell = |ui: &mut Ui, w: f32, add: &mut dyn FnMut(&mut Ui)| {
                                ui.allocate_ui_with_layout(
                                    egui::vec2(w, crate::style::control_h()),
                                    egui::Layout::left_to_right(egui::Align::Center),
                                    |ui| {
                                        ui.add_space(th_px);
                                        ui.set_width(w - th_px);
                                        add(ui);
                                    },
                                );
                            };
                            let fw = |w: f32| w - 2.0 * th_px;
                            cell(ui, widths[0], &mut |ui| {
                                ui.add(
                                    crate::widgets::line(&mut row.id)
                                        .font(egui::TextStyle::Monospace)
                                        .desired_width(fw(widths[0])),
                                )
                                .on_hover_text(t("Name"));
                            });
                            cell(ui, widths[1], &mut |ui| {
                                widgets::select(
                                    ui,
                                    ("sched-kind", r),
                                    t("Kind"),
                                    &mut row.kind,
                                    &kinds,
                                    fw(widths[1]) - 8.0,
                                );
                            });
                            cell(ui, widths[2], &mut |ui| {
                                ui.spacing_mut().item_spacing.x = 2.0;
                                ui.add(
                                    crate::widgets::line(&mut row.cron)
                                        .font(egui::TextStyle::Monospace)
                                        .desired_width(fw(widths[2]) - 26.0),
                                );
                                crate::widgets::combo(("sched-preset", r)).width(0.0).selected_text("").show_ui(
                                    ui,
                                    |ui| {
                                        crate::widgets::options_top(ui);
                                        for (k, (c, label)) in CRON_PRESETS.iter().enumerate() {
                                            let text = format!("{c}  ·  {}", t(label));
                                            if crate::widgets::option(ui, row.cron == *c, &text, k).clicked() {
                                                row.cron = (*c).into();
                                                ui.close();
                                            }
                                        }
                                    },
                                );
                            });
                            for (i, v) in [(3, &mut row.chains), (4, &mut row.days)] {
                                cell(ui, widths[i], &mut |ui| {
                                    ui.add(crate::widgets::line(v).desired_width(fw(widths[i])));
                                });
                            }
                            cell(ui, widths[5], &mut |ui| {
                                ui.add_enabled(data, crate::widgets::line(&mut row.every).desired_width(64.0));
                            });
                            cell(ui, widths[6], &mut |ui| {
                                crate::widgets::check(ui, &mut row.enabled, "").on_hover_text(t("On"));
                            });
                            cell(ui, widths[7], &mut |ui| {
                                let del = widgets::act(ui, &look, crate::icons::TRASH, "", None, 12.0);
                                if widgets::named(ui, del, t("Delete")).clicked() {
                                    drop = Some(r);
                                }
                            });
                        });
                        if data {
                            ui.add_space(4.0);
                            ui.horizontal(|ui| {
                                ui.add_space(th_px);
                                let hint = tf!("Folders (one per line, inside %s)", roots);
                                let w = ui.available_width() - th_px - 2.0;
                                ui.add(
                                    egui::TextEdit::multiline(&mut row.folders)
                                        .hint_text(crate::widgets::hint(hint).monospace())
                                        .font(egui::TextStyle::Monospace)
                                        .desired_rows(2)
                                        .desired_width(w),
                                );
                            });
                        }
                    });
                    let y = ui.cursor().top();
                    ui.painter().hline(ui.max_rect().x_range(), y + 0.5, egui::Stroke::new(1.0, look.c("line")));
                }
                if let Some(r) = drop {
                    self.rows.remove(r);
                }
                if self.rows.is_empty() {
                    ui.add_space(12.0);
                    ui.vertical_centered(|ui| ui.label(RichText::new(t("No schedules.")).size(12.0).color(muted)));
                    ui.add_space(12.0);
                }
                egui::Frame::new().inner_margin(egui::Margin::same(12)).show(ui, |ui| {
                ui.horizontal(|ui| {
                    ui.spacing_mut().item_spacing.x = 8.0;
                    save = widgets::button(ui, &look, crate::icons::CHECK, t("Save schedules"), Kind::Primary).clicked();
                    ui.add(
                        egui::Label::new(
                            RichText::new(t("Retention deletes whole chains only (a full backup and its incrementals), never the backup just made; 0 = no limit."))
                                .size(11.0)
                                .color(muted),
                        )
                        .wrap(),
                    );
                });
            });
            },
        );
        if add {
            let n = self.rows.len() + 1;
            self.rows.push(SchedRow {
                id: format!("backup-{n}"),
                cron: CRON_PRESETS[0].0.into(),
                chains: "4".into(),
                days: "0".into(),
                every: "7".into(),
                enabled: true,
                ..Default::default()
            });
        }
        if save {
            let num = |s: &str| s.trim().parse::<i64>().unwrap_or(0);
            let list: Vec<BackupSchedule> = self
                .rows
                .iter()
                .map(|r| {
                    let kind = cur.kinds.get(r.kind).cloned().unwrap_or_default();
                    let data = kind == "fileserver-data";
                    let mut x = BackupSchedule {
                        id: r.id.trim().into(),
                        cron: r.cron.trim().into(),
                        enabled: r.enabled,
                        folders: if data { lines(&r.folders) } else { Vec::new() },
                        full_every_days: if data { num(&r.every) } else { 0 },
                        ..Default::default()
                    };
                    x.retention.chains = num(&r.chains);
                    x.retention.days = num(&r.days);
                    x.kind = kind;
                    x
                })
                .collect();
            let (id, name) = (cur.id, cur.name.clone());
            let api = cx.panel.clone();
            cx.run(
                async move { api.act_with(Method::PUT, &format!("/api/agents/{id}/backup-schedules"), &list).await },
                move |app, ()| {
                    app.cx.set_status(tf!("Schedules saved on %s.", name));
                    app.with_shell(|s, cx| s.backup.show(cx));
                },
            );
        }
    }
}

/// A schedule being edited (numbers as typed, folders one per line).
#[derive(Default, Clone)]
struct SchedRow {
    id: String,
    kind: usize,
    cron: String,
    chains: String,
    days: String,
    every: String,
    folders: String,
    enabled: bool,
}

impl SchedRow {
    fn from(x: &BackupSchedule, kinds: &[String]) -> Self {
        Self {
            id: x.id.clone(),
            kind: kinds.iter().position(|k| *k == x.kind).unwrap_or(0),
            cron: x.cron.clone(),
            chains: x.retention.chains.to_string(),
            days: x.retention.days.to_string(),
            every: x.full_every_days.to_string(),
            folders: x.folders.join("\n"),
            enabled: x.enabled,
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
            widgets::select(ui, "send-kind", t("Kind"), &mut self.kind, &names, 320.0);
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
        // the head: kind · date (· full, or how an incremental is applied)
        ui.horizontal_wrapped(|ui| {
            ui.spacing_mut().item_spacing.x = 4.0;
            let b = &self.backup;
            ui.label(crate::style::semibold(backup_kind_name(&b.kind), 12.0).color(look.c("fg-strong")));
            ui.label(RichText::new(format!("· {}", date(b.started))).size(12.0));
            if b.kind == "fileserver-data" {
                let how = if b.full {
                    t("full")
                } else {
                    t("incremental: the full backup and the incrementals up to this one are applied in order")
                };
                ui.label(RichText::new(format!("· {how}")).size(12.0).color(look.c("fg-muted")));
            }
        });
        let f = &mut self.form;
        match f.kind.as_str() {
            "fileserver-data" => {
                widgets::label(ui, &look, t("Folders"));
                // one folder inside them typed: the folders do not apply
                let one = !f.sub.trim().is_empty();
                for (name, on) in &mut f.folders {
                    ui.add_enabled_ui(!one, |ui| {
                        crate::widgets::check(ui, on, RichText::new(name.as_str()).monospace().size(12.0))
                    });
                }
                widgets::label(ui, &look, t("Or only one folder inside them"));
                let hint = self.backup.folders.first().map(|x| format!("{x}/Financeiro")).unwrap_or_default();
                widgets::field(ui, &mut f.sub, &hint, f32::INFINITY);
                widgets::label(ui, &look, t("Where"));
                crate::widgets::radio_value(
                    ui,
                    &mut f.original,
                    false,
                    t("In a new folder: [share root]/_restored/[date]/ (nothing current is touched)"),
                );
                crate::widgets::radio_value(
                    ui,
                    &mut f.original,
                    true,
                    t(
                        "Over the original folders: they become a backup first, then go back to how this backup has them (newer files are removed)",
                    ),
                );
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
                    crate::widgets::check(ui, &mut f.parts[i], *name);
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

const CRON_PRESETS: [(&str, &str); 3] = [
    ("0 2 * * *", "Every day at 02:00"),
    ("0 3 * * 0", "Sundays at 03:00"),
    ("0 3 1 * *", "Day 1 of each month at 03:00"),
];
