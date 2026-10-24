//! Sync: the file server's sync jobs (new and edit through the wizard, preview, run, quarantine, delete) and their
//! runs (details, log, cancel); the list refreshes itself every 3 s while a run is queued or running.

use std::time::Duration;

use adm_core::api::esc;
use adm_core::forms::sync_source_text;
use adm_core::i18n::{date, server_text, t};
use adm_core::logic::{Tone, run_status, size_text, sync_finished};
use adm_core::model::{Agent, SyncJob, SyncList, SyncRun};
use adm_core::tf;
use egui::{RichText, Ui};
use reqwest::Method;

use crate::app::{App, Cx};
use crate::table::{self, Cell, Column, TableState};
use crate::widgets::{self, Kind};

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
        self.reload(cx);
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

    fn selected_job(&self) -> Option<SyncJob> {
        self.jobs.sel.and_then(|i| self.data.jobs.get(i)).cloned()
    }

    fn selected_run(&self) -> Option<SyncRun> {
        self.runs.sel.and_then(|i| self.data.runs.get(i)).cloned()
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        let h = ui.available_height();
        ui.allocate_ui(egui::vec2(ui.available_width(), h * 0.45), |ui| {
            widgets::card(ui, &look, t("Sync jobs"), |ui| self.jobs_ui(ui, cx, keys));
        });
        ui.add_space(6.0);
        widgets::card(ui, &look, t("Runs"), |ui| self.runs_ui(ui, cx));
    }

    fn jobs_ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        ui.horizontal(|ui| {
            let labels: Vec<String> = self.agents.iter().map(Agent::label).collect();
            let mut i = self.pick;
            if widgets::select(ui, "sync-server", &mut i, &labels, 320.0) {
                self.switch(cx, i);
            }
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                if widgets::icon_button(ui, &look, "⟳", t("Refresh")).clicked() {
                    self.reload(cx);
                }
                ui.label(RichText::new(&self.info).color(look.c("fg-muted")));
            });
        });
        let job = self.selected_job();
        let agent = self.cur.as_ref().map(|a| a.id);
        ui.horizontal(|ui| {
            if ui
                .add_enabled_ui(agent.is_some(), |ui| widgets::button(ui, &look, "➕", t("New job"), Kind::Primary))
                .inner
                .clicked()
                && let Some(id) = agent
            {
                super::sync_wizard::open(cx, id, self.data.clone(), None);
            }
            let on = job.is_some();
            if ui.add_enabled_ui(on, |ui| widgets::button(ui, &look, "✏", t("Edit"), Kind::Secondary)).inner.clicked()
                && let (Some(id), Some(j)) = (agent, job.clone())
            {
                super::sync_wizard::open(cx, id, self.data.clone(), Some(j));
            }
            for (icon, label, preview) in [("👁", t("Preview"), true), ("▶", t("Run now"), false)] {
                if ui.add_enabled_ui(on, |ui| widgets::button(ui, &look, icon, label, Kind::Secondary)).inner.clicked()
                    && let Some(j) = job.clone()
                {
                    self.start(cx, j, preview);
                }
            }
            if ui
                .add_enabled_ui(on, |ui| widgets::button(ui, &look, "🕘", t("Quarantine"), Kind::Secondary))
                .inner
                .clicked()
                && let (Some(id), Some(j)) = (agent, job.clone())
            {
                super::quarantine::open(cx, id, j);
            }
            if ui.add_enabled_ui(on, |ui| widgets::button(ui, &look, "🗑", t("Delete"), Kind::Danger)).inner.clicked()
                && let Some(j) = job.clone()
            {
                let path = format!("{}/syncs/{}", self.base(), esc(&j.id));
                let id = j.id.clone();
                cx.confirm_typed(
                    t("Delete"),
                    tf!("Delete the sync job %s? The files already brought stay.", self.job_name(&j.id)),
                    &id,
                    move |cx| {
                        let api = cx.panel.clone();
                        cx.run(async move { api.act(Method::DELETE, &path).await }, |app, ()| reload(app));
                    },
                );
            }
        });
        let cols = [
            Column::new(t("Name"), 200.0),
            Column::new(t("Source"), 300.0),
            Column::new(t("Destination"), 220.0),
            Column::new(t("Mode"), 140.0),
            Column::new(t("When (cron)"), 110.0),
        ];
        let me = &*self;
        let names: Vec<String> = me.data.jobs.iter().map(|j| me.job_name(&j.id)).collect();
        let jobs = &self.data.jobs;
        let mut hover = None;
        table::Table { id: "sync-jobs", cols: &cols, rows: jobs.len(), keys, row_h: 27.0 }.show::<()>(
            ui,
            &look,
            &mut self.jobs,
            &mut hover,
            |r, c| {
                let j = &jobs[r];
                match c {
                    0 => Cell::new(&names[r], Tone::Strong),
                    1 => Cell::plain(sync_source_text(j)),
                    2 => Cell::plain(&j.dest),
                    3 if j.mode == "mirror" && j.approved != j.rev => {
                        Cell::new(format!("{} · {}", t("mirror"), t("preview needed")), Tone::Warn)
                    }
                    3 if j.mode == "mirror" => Cell::plain(t("mirror")),
                    3 => Cell::plain(t("copy")),
                    _ if j.enabled && !j.cron.is_empty() => Cell::plain(&j.cron),
                    _ => Cell::new("—", Tone::Muted),
                }
            },
        );
    }

    fn start(&mut self, cx: &Cx, j: SyncJob, preview: bool) {
        let path = format!("{}/syncs/{}/{}", self.base(), esc(&j.id), if preview { "preview" } else { "run" });
        let api = cx.panel.clone();
        cx.run(async move { api.act(Method::POST, &path).await }, move |app, ()| {
            app.cx.set_status(if preview { t("Preview queued.") } else { t("Sync queued.") });
            app.cx.spawn(tokio::time::sleep(Duration::from_millis(1500)), |app, ()| reload(app));
        });
    }

    fn runs_ui(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        let run = self.selected_run();
        ui.horizontal(|ui| {
            let has_result = run.as_ref().is_some_and(|r| r.result.is_some());
            if ui
                .add_enabled_ui(has_result, |ui| widgets::button(ui, &look, "ℹ", t("Details"), Kind::Secondary))
                .inner
                .clicked()
                && let Some(r) = run.clone()
            {
                cx.show_info(r.id.clone(), details(&r));
            }
            if ui
                .add_enabled_ui(run.is_some(), |ui| widgets::button(ui, &look, "📄", t("Log"), Kind::Secondary))
                .inner
                .clicked()
                && let Some(r) = run.clone()
            {
                let api = cx.panel.clone();
                let path = format!("{}/sync-runs/{}/log", self.base(), esc(&r.id));
                cx.run(async move { api.get_text(&path).await }, move |app, txt| {
                    let txt = if txt.is_empty() { t("No log.").to_string() } else { txt };
                    app.cx.push(crate::modal::Info::new(r.id, txt).wide(760.0));
                });
            }
            let running = run.as_ref().is_some_and(|r| r.status == "running");
            if ui
                .add_enabled_ui(running, |ui| widgets::button(ui, &look, "⏹", t("Cancel"), Kind::Secondary))
                .inner
                .clicked()
                && let Some(r) = run.clone()
            {
                let api = cx.panel.clone();
                let path = format!("{}/sync-runs/{}/cancel", self.base(), esc(&r.id));
                cx.run(async move { api.act(Method::POST, &path).await }, |app, ()| reload(app));
            }
        });
        let cols = [
            Column::new(t("Date"), 165.0),
            Column::new(t("Finished"), 220.0),
            Column::new(t("Job"), 160.0),
            Column::new(t("Kind"), 90.0),
            Column::new(t("Status"), 200.0),
            Column::new(t("Files"), 80.0),
            Column::new(t("Deleted"), 80.0),
        ];
        let me = &*self;
        let names: Vec<String> = me.data.runs.iter().map(|r| me.job_name(&r.job)).collect();
        let runs = &self.data.runs;
        let mut hover = None;
        table::Table { id: "sync-runs", cols: &cols, rows: runs.len(), keys: false, row_h: 27.0 }.show::<()>(
            ui,
            &look,
            &mut self.runs,
            &mut hover,
            |r, c| {
                let x = &runs[r];
                match c {
                    0 => Cell::new(date(x.started), Tone::Strong),
                    1 => match sync_finished(x) {
                        f if f == "—" => Cell::new(f, Tone::Muted),
                        f => Cell::plain(f),
                    },
                    2 => Cell::plain(&names[r]),
                    3 => Cell::new(if x.preview { t("preview") } else { t("run") }, Tone::Muted),
                    4 => {
                        let (s, tone) = run_status(&x.status);
                        let s = if x.error.is_empty() {
                            s.to_string()
                        } else {
                            format!("{s} · {}", server_text(&x.error))
                        };
                        Cell::new(s, tone)
                    }
                    5 => match &x.result {
                        None if x.status == "running" => Cell::plain(x.progress.transfers.to_string()),
                        None => Cell::new("—", Tone::Muted),
                        Some(res) if x.preview => Cell::plain((res.plan.new + res.plan.changed).to_string()),
                        Some(res) => Cell::plain(res.transfers.to_string()),
                    },
                    _ => match &x.result {
                        None => Cell::new("—", Tone::Muted),
                        Some(res) if x.preview => Cell::plain(res.plan.delete.to_string()),
                        Some(res) => Cell::plain(res.deleted.to_string()),
                    },
                }
            },
        );
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
