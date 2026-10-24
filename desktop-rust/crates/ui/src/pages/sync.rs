//! Sync as the web's page: the servers at the left; at the right the chosen server's sync jobs (new and edit through
//! the wizard, last run, quarantine, per-row preview, run, edit and delete) and their runs (per-row details, log and
//! cancel); the list refreshes itself every 3 s while a run is queued or running.

use std::time::Duration;

use adm_core::api::esc;
use adm_core::forms::sync_unc;
use adm_core::i18n::{date, server_text, t};
use adm_core::logic::{Tone, run_status, size_text, sync_finished};
use adm_core::model::{Agent, QuarantineList, SyncJob, SyncList, SyncRun};
use adm_core::tf;
use egui::{RichText, Ui};
use reqwest::Method;

use crate::app::{App, Cx};
use crate::table::{self, Cell, Column, TableState};
use crate::widgets;

#[derive(Default)]
pub struct Sync {
    agents: Vec<Agent>,
    pick: usize,
    cur: Option<Agent>,
    /// Bumped on every server switch: an answer for another server is dropped.
    key: u64,
    pub data: SyncList,
    jobs: TableState,
    runs: TableState,
    info: String,
    /// The server (its key) whose runs are being followed: one timer per server on screen.
    polling: Option<u64>,
    /// What the runs deleted or replaced, per job, and the free space of the share roots.
    qs: QuarantineList,
}

impl Sync {
    pub fn load(&mut self, cx: &Cx) {
        let api = cx.panel.clone();
        cx.run(async move { api.get::<Vec<Agent>>("/api/agents").await }, |app, agents| {
            app.with_shell(|s, cx| {
                s.sync.agents = agents;
                s.sync.switch(cx, 0);
            });
        });
    }

    fn switch(&mut self, cx: &Cx, i: usize) {
        let Some(a) = self.agents.get(i).cloned() else { return };
        self.pick = i;
        self.key += 1;
        self.data = SyncList::default();
        self.jobs.clear();
        self.runs.clear();
        self.cur = Some(a);
        self.qs = QuarantineList::default();
        self.reload(cx);
        self.load_quarantines(cx);
    }

    /// The quarantines (not on every poll: it walks them on the server).
    fn load_quarantines(&self, cx: &Cx) {
        let (key, api, path) = (self.key, cx.panel.clone(), format!("{}/syncs/quarantines", self.base()));
        cx.spawn(async move { api.get::<QuarantineList>(&path).await }, move |app, r| {
            app.with_shell(|s, _| {
                if s.sync.key == key {
                    s.sync.qs = r.unwrap_or_default();
                }
            });
        });
    }

    fn base(&self) -> String {
        format!("/api/agents/{}", self.cur.as_ref().map_or(0, |a| a.id))
    }

    pub fn reload(&mut self, cx: &Cx) {
        let Some(a) = &self.cur else { return };
        let key = self.key;
        let api = cx.panel.clone();
        let path = format!("/api/agents/{}/syncs", a.id);
        cx.run(async move { api.get::<SyncList>(&path).await }, move |app, d| {
            app.with_shell(|s, cx| {
                let p = &mut s.sync;
                if p.key != key {
                    return;
                }
                p.info = if d.file_server { String::new() } else { t("Folders are synced into a file server.").into() };
                let busy = d.runs.iter().any(|r| r.status == "queued" || r.status == "running");
                p.data = d;
                p.jobs.clear();
                p.runs.clear();
                if busy && p.polling != Some(key) {
                    p.polling = Some(key);
                    cx.spawn(tokio::time::sleep(Duration::from_secs(3)), move |app, ()| {
                        app.with_shell(|s, cx| {
                            if s.sync.polling == Some(key) {
                                s.sync.polling = None;
                            }
                            if s.sync.key == key {
                                s.sync.reload(cx);
                            }
                        });
                    });
                }
            });
        });
    }

    fn job_name(&self, id: &str) -> String {
        self.data
            .jobs
            .iter()
            .find(|j| j.id == id && !j.name.is_empty())
            .map_or_else(|| id.to_string(), |j| j.name.clone())
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
                ui.set_width(260.0);
                let right = |ui: &mut Ui| refresh = widgets::act(ui, &look, "⟳", t("refresh"), None, 12.0).clicked();
                widgets::panel(ui, &look, (t("Servers"), ""), right, 0.0, |ui| {
                    for (i, a) in self.agents.iter().enumerate() {
                        let on = self.pick == i && self.cur.is_some();
                        let r = widgets::side_item(ui, &look, on, i == 0, |ui, ink| {
                            ui.vertical(|ui| {
                                ui.spacing_mut().item_spacing.y = 0.0;
                                ui.label(crate::style::semibold(&a.name, 12.0).color(ink));
                                ui.label(RichText::new(a.host()).size(11.0).color(ink));
                            });
                        });
                        if r.response.clicked() && !on {
                            pick = Some(i);
                        }
                    }
                    if self.agents.is_empty() {
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
                    RichText::new(t("The server's agent brings folders from an SMB share (Windows or Samba) with the rclone engine built into ad-manager: signed SMB, with the server's machine account (Kerberos) or a user and password kept encrypted. Files take the destination folder's Windows permissions."))
                        .size(11.0)
                        .color(look.c("fg-muted")),
                );
            });
            ui.vertical(|ui| {
                ui.set_width(ui.available_width());
                let top = ui.cursor().top();
                ui.allocate_ui(egui::vec2(ui.available_width(), (h * 0.4).floor()), |ui| self.jobs_ui(ui, cx, keys));
                ui.add_space(20.0);
                let left = h - (ui.cursor().top() - top);
                self.runs_ui(ui, cx, left);
            });
        });
        if let Some(i) = pick {
            self.switch(cx, i);
        }
        if refresh {
            self.reload(cx);
            self.load_quarantines(cx);
        }
    }

    fn jobs_ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        let agent = self.cur.as_ref().map(|a| a.id);
        let mut add = false;
        let fs = self.data.file_server;
        let right = |ui: &mut Ui| {
            if fs {
                add = widgets::act(ui, &look, "+", t("new"), None, 12.0).clicked();
            }
        };
        let mut act: Option<(JobAct, usize)> = None;
        widgets::panel(ui, &look, (&format!("🔄  {}", t("Sync jobs")), ""), right, 0.0, |ui| {
            for s in self.qs.space.iter().filter(|s| s.low) {
                egui::Frame::new().inner_margin(egui::Margin::symmetric(12, 8)).show(ui, |ui| {
                    let msg = tf!("Little free space on %s: %s of %s.", s.root, size_text(s.free), size_text(s.total));
                    ui.label(RichText::new(msg).size(12.0).color(look.c("warn")));
                });
            }
            if !self.info.is_empty() {
                egui::Frame::new().inner_margin(egui::Margin::same(12)).show(ui, |ui| {
                    ui.label(RichText::new(&self.info).size(12.0).color(look.c("fg-muted")));
                });
                return;
            }
            let cols = [
                Column::new(t("Name"), 120.0),
                Column::new(t("Source"), 200.0).fill(),
                Column::new(t("Destination"), 140.0),
                Column::new(t("Mode"), 80.0),
                Column::new(t("When (cron)"), 110.0),
                Column::new(t("Last run"), 170.0),
                Column::new(t("Quarantine"), 100.0),
                Column::new("", 110.0).right(),
            ];
            let me = &*self;
            let names: Vec<String> = me.data.jobs.iter().map(|j| me.job_name(&j.id)).collect();
            let (jobs, runs, qs) = (&self.data.jobs, &self.data.runs, &self.qs.quarantines);
            let mut hover = None;
            let out = table::Table { id: "sync-jobs", cols: &cols, rows: jobs.len(), keys, row_h: 0.0, frame: false }
                .show(ui, &look, &mut self.jobs, &mut hover, |r, c| {
                    let j = &jobs[r];
                    match c {
                        0 => Cell::plain(&names[r]).bold(),
                        1 => {
                            let auth = if j.source.auth == "ntlm" { "NTLM" } else { "Kerberos" };
                            Cell::plain(sync_unc(j)).mono().with_tag(auth, Tone::Normal)
                        }
                        2 => Cell::plain(&j.dest).mono(),
                        3 if j.mode == "mirror" && j.approved != j.rev => {
                            Cell::new(format!("{} · {}", t("mirror"), t("preview needed")), Tone::Warn)
                        }
                        3 if j.mode == "mirror" => Cell::plain(t("mirror")),
                        3 => Cell::plain(t("copy")),
                        4 if j.enabled && !j.cron.is_empty() => Cell::plain(&j.cron).mono(),
                        4 => Cell::plain("—"),
                        5 => match runs.iter().find(|x| x.job == j.id) {
                            None => Cell::plain("—"),
                            Some(x) => {
                                let (st, tone) = run_status(&x.status);
                                let phase = phase_text(x);
                                if x.status == "running" && !phase.is_empty() {
                                    Cell::new(format!("{st} · {phase}"), tone)
                                } else {
                                    Cell::new(format!("{st} · {}", date(x.started)), tone)
                                }
                            }
                        },
                        6 => {
                            let size: i64 = qs.iter().filter(|q| q.job == j.id).map(|q| q.size).sum();
                            if qs.iter().any(|q| q.job == j.id) {
                                Cell::plain(size_text(size)).link(
                                    "🗄",
                                    tf!("Quarantine of %s", names[r]),
                                    (JobAct::Quarantine, r),
                                )
                            } else {
                                Cell::plain("—")
                            }
                        }
                        _ => Cell::plain("")
                            .and_link("", Tone::Normal, "👁", t("Preview").into(), (JobAct::Preview, r))
                            .and_link("", Tone::Normal, "▶", t("Run now").into(), (JobAct::Run, r))
                            .and_link("", Tone::Normal, "✏", t("Edit").into(), (JobAct::Edit, r))
                            .and_link("", Tone::Normal, "🗑", t("Delete").into(), (JobAct::Delete, r)),
                    }
                });
            cx.hover = hover.or(cx.hover.take());
            act = out.clicked;
            if jobs.is_empty() && self.cur.is_some() {
                ui.add_space(12.0);
                ui.vertical_centered(|ui| {
                    ui.label(RichText::new(t("No sync jobs.")).size(12.0).color(look.c("fg-muted")))
                });
                ui.add_space(12.0);
            }
        });
        if add && let Some(id) = agent {
            super::sync_wizard::open(cx, id, self.data.clone(), None);
        }
        let Some((a, r)) = act else { return };
        let (Some(id), Some(j)) = (agent, self.data.jobs.get(r).cloned()) else { return };
        match a {
            JobAct::Preview => self.start(cx, j, true),
            JobAct::Run => self.start(cx, j, false),
            JobAct::Edit => super::sync_wizard::open(cx, id, self.data.clone(), Some(j)),
            JobAct::Quarantine => super::quarantine::open(cx, id, j),
            JobAct::Delete => {
                let path = format!("{}/syncs/{}", self.base(), esc(&j.id));
                let jid = j.id.clone();
                cx.confirm_typed(
                    t("Delete"),
                    tf!("Delete the sync job %s? The files already brought stay.", self.job_name(&j.id)),
                    &jid,
                    move |cx| {
                        let api = cx.panel.clone();
                        cx.run(async move { api.act(Method::DELETE, &path).await }, |app, ()| reload(app));
                    },
                );
            }
        }
    }

    fn start(&mut self, cx: &Cx, j: SyncJob, preview: bool) {
        let path = format!("{}/syncs/{}/{}", self.base(), esc(&j.id), if preview { "preview" } else { "run" });
        let api = cx.panel.clone();
        cx.run(async move { api.act(Method::POST, &path).await }, move |app, ()| {
            app.cx.set_status(if preview { t("Preview queued.") } else { t("Sync queued.") });
            app.cx.spawn(tokio::time::sleep(Duration::from_millis(1500)), |app, ()| reload(app));
            app.with_shell(|s, cx| s.sync.load_quarantines(cx));
        });
    }

    fn runs_ui(&mut self, ui: &mut Ui, cx: &mut Cx, h: f32) {
        let look = cx.look.clone();
        let mut act: Option<(RunAct, usize)> = None;
        widgets::panel(
            ui,
            &look,
            (t("Runs"), ""),
            |_| {},
            h,
            |ui| {
                let cols = [
                    Column::new(t("Date"), 160.0),
                    Column::new(t("Finished"), 150.0),
                    Column::new(t("Job"), 120.0),
                    Column::new(t("Kind"), 80.0),
                    Column::new(t("Status"), 200.0).fill(),
                    Column::new(t("Files"), 70.0).right(),
                    Column::new(t("Size"), 90.0).right(),
                    Column::new(t("Deleted"), 70.0).right(),
                    Column::new("", 80.0).right(),
                ];
                let me = &*self;
                let names: Vec<String> = me.data.runs.iter().map(|r| me.job_name(&r.job)).collect();
                let runs = &self.data.runs;
                let mut hover = None;
                let out = table::Table {
                    id: "sync-runs",
                    cols: &cols,
                    rows: runs.len(),
                    keys: false,
                    row_h: 0.0,
                    frame: false,
                }
                .show(ui, &look, &mut self.runs, &mut hover, |r, c| {
                    let x = &runs[r];
                    let plan = x.result.as_ref().map(|res| &res.plan);
                    match c {
                        0 => Cell::plain(date(x.started)),
                        1 => Cell::plain(sync_finished(x)),
                        2 => Cell::plain(&names[r]),
                        3 if x.preview => Cell::new(t("preview"), Tone::Muted),
                        3 if x.origin == "schedule" => Cell::new(t("scheduled"), Tone::Muted),
                        3 => Cell::new(t("run"), Tone::Muted),
                        4 => {
                            let (s, tone) = run_status(&x.status);
                            let phase = phase_text(x);
                            if !x.error.is_empty() {
                                Cell::new(format!("{s} · {}", server_text(&x.error)), tone)
                            } else if x.status == "running" && !phase.is_empty() {
                                Cell::new(format!("{s} · {phase}"), tone)
                            } else {
                                Cell::new(s, tone)
                            }
                        }
                        5 => match (&x.result, plan) {
                            (_, Some(p)) if x.preview => Cell::plain((p.new + p.changed).to_string()),
                            (Some(res), _) => Cell::plain(res.transfers.to_string()),
                            (None, _) if x.status == "running" => Cell::plain(x.progress.transfers.to_string()),
                            _ => Cell::plain("—"),
                        },
                        6 => match (&x.result, plan) {
                            (_, Some(p)) if x.preview => Cell::plain(size_text(p.new_bytes + p.changed_bytes)),
                            (Some(res), _) => Cell::plain(size_text(res.bytes)),
                            (None, _) if x.status == "running" => Cell::plain(size_text(x.progress.bytes)),
                            _ => Cell::plain("—"),
                        },
                        7 => match (&x.result, plan) {
                            (_, Some(p)) if x.preview => Cell::plain(p.delete.to_string()),
                            (Some(res), _) => Cell::plain(res.deleted.to_string()),
                            _ => Cell::plain("—"),
                        },
                        _ => {
                            let mut cell = Cell::plain("");
                            if x.result.is_some() {
                                cell = cell.and_link("", Tone::Normal, "👁", t("Details").into(), (RunAct::Details, r));
                            }
                            cell = cell.and_link("", Tone::Normal, "📄", t("Log").into(), (RunAct::Log, r));
                            if x.status == "running" {
                                cell = cell.and_link("", Tone::Normal, "⏹", t("Cancel").into(), (RunAct::Cancel, r));
                            }
                            cell
                        }
                    }
                });
                cx.hover = hover.or(cx.hover.take());
                act = out.clicked;
                if runs.is_empty() && self.cur.is_some() && self.info.is_empty() {
                    ui.add_space(12.0);
                    ui.vertical_centered(|ui| {
                        ui.label(RichText::new(t("No runs yet.")).size(12.0).color(look.c("fg-muted")))
                    });
                }
            },
        );
        let Some((a, i)) = act else { return };
        let Some(r) = self.data.runs.get(i).cloned() else { return };
        match a {
            RunAct::Details => cx.show_info(r.id.clone(), details(&r)),
            RunAct::Log => {
                let api = cx.panel.clone();
                let path = format!("{}/sync-runs/{}/log", self.base(), esc(&r.id));
                cx.run(async move { api.get_text(&path).await }, move |app, txt| {
                    let txt = if txt.is_empty() { t("No log.").to_string() } else { txt };
                    app.cx.push(crate::modal::Info::new(r.id, txt).wide(760.0));
                });
            }
            RunAct::Cancel => {
                let api = cx.panel.clone();
                let path = format!("{}/sync-runs/{}/cancel", self.base(), esc(&r.id));
                cx.run(async move { api.act(Method::POST, &path).await }, |app, ()| reload(app));
            }
        }
    }
}

/// What a job's link does.
#[derive(Clone, Copy)]
enum JobAct {
    Preview,
    Run,
    Edit,
    Quarantine,
    Delete,
}

/// What a run's link does.
#[derive(Clone, Copy)]
enum RunAct {
    Details,
    Log,
    Cancel,
}

/// What a running run is doing right now (so the panel never shows an opaque "running").
fn phase_text(r: &SyncRun) -> String {
    let p = &r.progress;
    match p.phase.as_str() {
        "preparing" => t("preparing").into(),
        "copying" if p.total > 0 => tf!("copying %d of %d", p.transfers, p.total),
        "copying" => t("copying").into(),
        "acl" if p.acl_items > 0 => format!("{} ({})", t("applying Windows permissions"), p.acl_items),
        "acl" => t("applying Windows permissions").into(),
        "finalizing" => t("finalizing").into(),
        _ => String::new(),
    }
}

fn reload(app: &mut App) {
    app.with_shell(|s, cx| s.sync.reload(cx));
}

/// A run's details: the plan, what was transferred and how it was checked, the quarantine, samples and notes.
pub fn details(r: &SyncRun) -> String {
    let Some(res) = &r.result else { return String::new() };
    let p = &res.plan;
    let mut lines = vec![
        format!("{}: {} ({})", t("New"), p.new, size_text(p.new_bytes)),
        format!("{}: {} ({})", t("Changed"), p.changed, size_text(p.changed_bytes)),
        format!("{}: {} ({})", t("To delete"), p.delete, size_text(p.delete_bytes)),
        format!("{}: {}", t("Unchanged"), p.same),
    ];
    if !r.preview {
        let v = if res.content_verified {
            t("content compared")
        } else if res.checked {
            t("sizes and dates compared")
        } else {
            t("not compared")
        };
        lines.push(format!("{}: {} ({}) · {v}", t("Transferred"), res.transfers, size_text(res.bytes)));
    }
    if !r.quarantine.is_empty() {
        lines.push(format!("{}: {}", t("Quarantine"), r.quarantine));
    }
    for (k, label) in [("new", "New"), ("changed", "Changed"), ("delete", "To delete"), ("error", "Errors")] {
        if let Some(s) = p.sample.get(k).filter(|s| !s.is_empty()) {
            lines.push(String::new());
            lines.push(format!("{}:", t(label)));
            lines.push(s.join("\n"));
        }
    }
    for n in &r.notes {
        lines.push(format!("· {}", server_text(n)));
    }
    lines.join("\n")
}
