//! Backup: the panel and each file server with their backups — back up now, send a backup, download, restore (per
//! kind, with its guards), log, delete (typed) and the last restoration — and the schedules of a file server.

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
}

/// The API path of a server's backups.
pub fn backup_base(s: &BackupServer) -> String {
    if s.id == 0 { "/api/backups/panel".into() } else { format!("/api/agents/{}/backups", s.id) }
}

fn server_name(s: &BackupServer) -> String {
    if s.id == 0 {
        t("This panel").into()
    } else if s.dc {
        format!("{} (DC)", s.name)
    } else {
        s.name.clone()
    }
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

    fn selected(&self) -> Option<BackupManifest> {
        self.current().and_then(|c| self.table.sel.and_then(|i| c.backups.get(i))).cloned()
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        let h = ui.available_height();
        let agent = self.current().is_some_and(|c| c.id != 0 && c.error.is_empty() && !c.kinds.is_empty());
        ui.allocate_ui(egui::vec2(ui.available_width(), if agent { h * 0.62 } else { h }), |ui| {
            widgets::card(ui, &look, t("Backup"), |ui| self.list(ui, cx, keys));
        });
        if agent {
            ui.add_space(6.0);
            widgets::card(ui, &look, t("Schedules"), |ui| self.schedules_ui(ui, cx));
        }
    }

    fn list(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        ui.horizontal(|ui| {
            let names: Vec<String> = self.servers.iter().map(server_name).collect();
            let mut i = self.cur.unwrap_or(0);
            if widgets::select(ui, "backup-server", &mut i, &names, 320.0) {
                self.cur = Some(i);
                self.show(cx);
            }
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                if widgets::icon_button(ui, &look, "⟳", t("Refresh")).clicked() {
                    self.reload(cx);
                }
                if let Some(c) = self.current() {
                    let s =
                        if c.error.is_empty() { tf!("%d backup(s)", c.backups.len()) } else { server_text(&c.error) };
                    ui.label(RichText::new(s).color(look.c("fg-muted")));
                }
            });
        });
        let cur = self.current().cloned();
        let sel = self.selected();
        ui.horizontal(|ui| {
            if widgets::button(ui, &look, "💾", t("Back up now"), Kind::Primary).clicked()
                && let Some(s) = cur.clone()
            {
                back_up_now(cx, s);
            }
            let can_send = cur.as_ref().is_some_and(|c| !c.upload_kinds.is_empty());
            if ui.add_enabled_ui(can_send, |ui| widgets::button(ui, &look, "⬆", t("Send a backup"), Kind::Secondary)).inner.clicked()
                && let Some(s) = cur.clone()
            {
                crate::files::pick_file(cx, move |app, path| {
                    app.cx.push(SendBackup { server: s, path, kind: 0 });
                });
            }
            let ok = sel.as_ref().is_some_and(|b| b.status == "ok");
            if ui.add_enabled_ui(ok, |ui| widgets::button(ui, &look, "⬇", t("Download"), Kind::Secondary)).inner.clicked()
                && let (Some(b), Some(s)) = (sel.clone(), cur.clone())
            {
                let path = format!("{}/{}/download", backup_base(&s), esc(&b.id));
                crate::files::save_download(cx, cx.panel.clone(), path, b.file.clone(), move |app, p| {
                    app.cx.set_status(tf!("Saved to %s (SHA-256 %s)", p.display(), b.sha256));
                });
            }
            if ui.add_enabled_ui(ok, |ui| widgets::button(ui, &look, "🕘", t("Restore"), Kind::Secondary)).inner.clicked()
                && let (Some(b), Some(s)) = (sel.clone(), cur.clone())
            {
                let f = RestoreForm::new(&b.kind, &b.folders);
                if f.refused() {
                    cx.show_info(t("Restore"), t("A domain controller backup is not restored over a DC in use: install ad-manager on a new server and, in the initial setup, choose \"Restore a DC\" with this backup (docs/backup.md)."));
                } else {
                    cx.push(Restore { form: f, backup: b, server: s });
                }
            }
            if ui.add_enabled_ui(sel.is_some(), |ui| widgets::button(ui, &look, "📄", t("Log"), Kind::Secondary)).inner.clicked()
                && let Some(b) = sel.clone()
            {
                let mut lines = b.log.clone();
                if !b.error.is_empty() {
                    lines.push(format!("{}: {}", t("Error"), server_text(&b.error)));
                }
                if lines.is_empty() {
                    lines.push(t("No log.").into());
                }
                cx.show_info(b.id, lines.join("\n"));
            }
            if ui.add_enabled_ui(sel.is_some(), |ui| widgets::button(ui, &look, "🗑", t("Delete"), Kind::Danger)).inner.clicked()
                && let (Some(b), Some(s)) = (sel.clone(), cur.clone())
            {
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
        });
        // the last restoration, with its log
        if let Some(r) = cur.as_ref().and_then(|c| c.restore.clone()) {
            let (st, ink) = match r.status.as_str() {
                "ok" => (t("ok").to_string(), look.c("ok")),
                "error" => (server_text(&r.error), look.c("err")),
                _ => (t("running").to_string(), look.c("warn")),
            };
            let mut txt = format!("{}: {} · {}", t("Last restoration"), backup_kind_name(&r.kind), date(r.started));
            if !r.dest.is_empty() {
                txt = format!("{txt} → {}", r.dest);
            }
            ui.horizontal(|ui| {
                ui.add(egui::Label::new(RichText::new(format!("{txt} · {st}")).color(ink)).truncate());
                if widgets::icon_button(ui, &look, "📄", t("Log")).clicked() {
                    let mut lines = r.log.clone();
                    if !r.error.is_empty() {
                        lines.push(format!("{}: {}", t("Error"), server_text(&r.error)));
                    }
                    cx.show_info(r.backup.clone(), lines.join("\n"));
                }
            });
        }
        let cols = [
            Column::new(t("Date"), 150.0),
            Column::new(t("Kind"), 230.0),
            Column::new(t("Origin"), 200.0),
            Column::new(t("Size"), 90.0).badge(),
            Column::new(t("Status"), 110.0).badge(),
        ];
        let empty = Vec::new();
        let rows = self.cur.and_then(|i| self.servers.get(i)).map_or(&empty, |c| &c.backups);
        let mut hover = None;
        table::Table { id: "backups", cols: &cols, rows: rows.len(), keys, row_h: 0.0, frame: true }.show::<()>(
            ui,
            &look,
            &mut self.table,
            &mut hover,
            |r, c| {
                let b = &rows[r];
                match c {
                    0 => Cell::new(date(b.started), Tone::Strong),
                    1 => {
                        let mut k = backup_kind_name(&b.kind);
                        if b.kind == "fileserver-data" {
                            k = format!("{k} · {}", if b.full { t("full") } else { t("incremental") });
                        }
                        Cell::plain(k)
                    }
                    2 => {
                        let mut o = backup_origin(&b.origin);
                        if !b.by.is_empty() {
                            o = format!("{o} · {}", b.by);
                        }
                        Cell::new(o, Tone::Muted)
                    }
                    3 if b.status != "ok" => Cell::new("—", Tone::Muted),
                    3 => Cell::plain(size_text(b.size)),
                    _ => match b.status.as_str() {
                        "ok" => Cell::new(t("ok").to_uppercase(), Tone::Ok),
                        "error" => Cell::new(t("error").to_uppercase(), Tone::Err),
                        _ => Cell::new(t("running").to_uppercase(), Tone::Warn),
                    },
                }
            },
        );
    }

    fn schedules_ui(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        let Some(cur) = self.current().cloned() else { return };
        ui.horizontal(|ui| {
            if widgets::button(ui, &look, "➕", t("New schedule"), Kind::Primary).clicked() {
                cx.push(NewSchedule::new(&cur, &self.schedules));
            }
            let sel = self.sched.sel.filter(|&i| i < self.schedules.len());
            if ui
                .add_enabled_ui(sel.is_some(), |ui| widgets::button(ui, &look, "⏸", t("On/off"), Kind::Secondary))
                .inner
                .clicked()
                && let Some(i) = sel
            {
                let mut list = self.schedules.clone();
                list[i].enabled = !list[i].enabled;
                save_schedules(cx, cur.id, list);
            }
            if ui
                .add_enabled_ui(sel.is_some(), |ui| widgets::button(ui, &look, "🗑", t("Remove"), Kind::Danger))
                .inner
                .clicked()
                && let Some(i) = sel
            {
                let mut list = self.schedules.clone();
                let x = list.remove(i);
                let id = cur.id;
                cx.confirm(
                    t("Remove schedule"),
                    tf!("Remove the schedule %s? Its backups stay.", x.id),
                    t("Remove"),
                    true,
                    move |cx| {
                        save_schedules(cx, id, list);
                    },
                );
            }
        });
        let cols = [
            Column::new(t("Name"), 140.0),
            Column::new(t("Kind"), 220.0),
            Column::new(t("When (cron)"), 120.0),
            Column::new(t("Keep chains"), 110.0),
            Column::new(t("Keep days"), 100.0),
            Column::new(t("On"), 60.0).badge(),
        ];
        let list = &self.schedules;
        let mut hover = None;
        table::Table { id: "schedules", cols: &cols, rows: list.len(), keys: false, row_h: 0.0, frame: true }
            .show::<()>(ui, &look, &mut self.sched, &mut hover, |r, c| {
                let x = &list[r];
                match c {
                    0 => Cell::new(&x.id, Tone::Strong),
                    1 => Cell::plain(backup_kind_name(&x.kind)),
                    2 => Cell::plain(&x.cron),
                    3 => Cell::plain(x.retention.chains.to_string()),
                    4 => Cell::plain(x.retention.days.to_string()),
                    _ if x.enabled => Cell::new(t("yes"), Tone::Ok),
                    _ => Cell::new(t("no"), Tone::Muted),
                }
            });
        ui.label(RichText::new(t("Retention deletes whole chains only (a full backup and its incrementals), never the backup just made; 0 = no limit.")).size(11.0).color(look.c("fg-muted")));
    }
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

/// The panel backs up at once; a file server asks the kind, the folders and full/incremental.
fn back_up_now(cx: &mut Cx, s: BackupServer) {
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
    cx.push(BackUpNow { server: s, kind: 0, folders: String::new(), full: false });
}

fn lines(s: &str) -> Vec<String> {
    s.lines().map(str::trim).filter(|l| !l.is_empty()).map(str::to_string).collect()
}

struct BackUpNow {
    server: BackupServer,
    kind: usize,
    folders: String,
    full: bool,
}

impl Dialog for BackUpNow {
    fn title(&self) -> String {
        tf!("Back up %s", self.server.name)
    }

    fn width(&self) -> f32 {
        620.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        let names: Vec<String> = self.server.kinds.iter().map(|k| backup_kind_name(k)).collect();
        widgets::form(ui, "backup-now", |ui| {
            widgets::label(ui, &look, t("Kind"));
            widgets::select(ui, "backup-kind", &mut self.kind, &names, 320.0);
            ui.end_row();
            widgets::label(ui, &look, &tf!("Folders (one per line, inside %s)", self.server.roots.join(", ")));
            ui.add(
                egui::TextEdit::multiline(&mut self.folders)
                    .hint_text(self.server.roots.join("\n"))
                    .desired_rows(3)
                    .desired_width(320.0),
            );
            ui.end_row();
            ui.label("");
            ui.checkbox(&mut self.full, t("Full backup (starts a new chain)"));
            ui.end_row();
        });
        let f = footer(ui, cx, Some(t("Back up now")), "", false, true);
        if f.ok {
            let k = self.server.kinds.get(self.kind).cloned().unwrap_or_default();
            let fs = lines(&self.folders);
            if k == "fileserver-data" && fs.is_empty() {
                return Flow::Keep;
            }
            let body = json!({ "kind": k, "folders": fs, "full": self.full && k == "fileserver-data" });
            let api = cx.panel.clone();
            let path = backup_base(&self.server);
            let name = self.server.name.clone();
            cx.run(async move { api.act_with(Method::POST, &path, &body).await }, move |app, ()| {
                app.cx.set_status(tf!("Backup queued on %s: it shows in the list when it ends.", name));
                reload(app);
            });
            return Flow::Close;
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
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
