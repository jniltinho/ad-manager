//! A sync job's quarantine (what its runs deleted or replaced): files go back to the run's destination (one already
//! there swaps places only with "replace"), one downloads, and the job's whole quarantine empties with a typed
//! confirmation.

use std::time::Duration;

use adm_core::api::{esc, q};
use adm_core::i18n::{server_text, t};
use adm_core::logic::size_text;
use adm_core::model::{Quarantine, QuarantineFile, QuarantineList, SyncJob};
use adm_core::tf;
use chrono::Local;
use egui::{RichText, Ui};
use reqwest::Method;
use serde_json::json;

use crate::app::{App, Cx};
use crate::modal::{Dialog, Flow, footer};
use crate::widgets::{self, Kind};

/// Reads the quarantines of the job and opens the dialog.
pub fn open(cx: &mut Cx, agent: i64, j: SyncJob) {
    let base = format!("/api/agents/{agent}/syncs");
    let api = cx.panel.clone();
    let path = format!("{base}/quarantines");
    cx.run(async move { api.get::<QuarantineList>(&path).await }, move |app, all| {
        let qs: Vec<Quarantine> = all.quarantines.into_iter().filter(|q| q.job == j.id).collect();
        let warn = all
            .space
            .iter()
            .filter(|s| s.low)
            .map(|s| tf!("Little free space on %s: %s of %s.", s.root, size_text(s.free), size_text(s.total)))
            .collect();
        let mut d = QuarantineDialog {
            base,
            job: j,
            qs,
            warn,
            pick: 0,
            files: Vec::new(),
            sel: None,
            replace: false,
            asked: 0,
            done: false,
        };
        d.load(&app.cx, 0);
        app.cx.push(d);
    });
}

pub struct QuarantineDialog {
    base: String,
    job: SyncJob,
    qs: Vec<Quarantine>,
    warn: Vec<String>,
    pick: usize,
    files: Vec<QuarantineFile>,
    sel: Option<usize>,
    replace: bool,
    /// The newest list asked for: an older answer that arrives late is dropped.
    asked: u64,
    done: bool,
}

fn dialog(app: &mut App, id: u64) -> Option<&mut QuarantineDialog> {
    app.cx.dialog_mut::<QuarantineDialog>(id)
}

impl QuarantineDialog {
    fn name(&self) -> String {
        if self.job.name.is_empty() { self.job.id.clone() } else { self.job.name.clone() }
    }

    /// Lists the files of quarantine `i` (nothing of the one before stays actionable while this loads).
    fn load(&mut self, cx: &Cx, i: usize) {
        let me = cx.current_dialog();
        let Some(qr) = self.qs.get(i).cloned() else { return };
        self.pick = i;
        self.asked += 1;
        let n = self.asked;
        self.files.clear();
        self.sel = None;
        #[derive(serde::Deserialize, Default)]
        struct Out {
            #[serde(default)]
            files: Vec<QuarantineFile>,
        }
        let api = cx.panel.clone();
        let path = format!("{}/quarantines/{}/files", self.base, esc(&qr.name));
        cx.run(async move { api.get::<Out>(&path).await }, move |app, out| {
            if let Some(d) = dialog(app, me)
                && d.asked == n
            {
                d.files = out.files;
            }
        });
    }

    fn give_back(&mut self, cx: &Cx, all: bool) {
        let me = cx.current_dialog();
        let paths: Vec<String> = if all {
            Vec::new()
        } else {
            match self.sel.and_then(|i| self.files.get(i)) {
                Some(f) => vec![f.path.clone()],
                None => return,
            }
        };
        let Some(qr) = self.qs.get(self.pick).cloned() else { return };
        #[derive(serde::Deserialize, Default)]
        #[serde(default)]
        struct Out {
            restored: i64,
            swapped: i64,
            conflicts: Vec<String>,
            conflict_count: i64,
            errors: Vec<String>,
        }
        let api = cx.panel.with_timeout(Duration::from_secs(1800));
        let path = format!("{}/quarantines/{}/restore", self.base, esc(&qr.name));
        let body = json!({ "paths": paths, "replace": self.replace });
        cx.run(async move { api.send::<Out, _>(Method::POST, &path, Some(&body)).await }, move |app, r| {
            let mut lines = vec![tf!("%d files given back.", r.restored + r.swapped)];
            if r.swapped > 0 {
                lines.push(tf!("%d files of the destination went into the quarantine in their place.", r.swapped));
            }
            if r.conflict_count > 0 {
                lines.push(tf!(
                    "%d files are in the destination already (check \"replace\" to swap them):",
                    r.conflict_count
                ));
                lines.extend(r.conflicts.into_iter().take(20));
            }
            lines.extend(r.errors.iter().take(20).map(|e| server_text(e)));
            app.cx.show_info(t("Give back"), lines.join("\n"));
            // the answer belongs to this quarantine, whatever is picked meanwhile
            if let Some(i) =
                dialog(app, me).and_then(|d| d.qs.iter().position(|x| x.name == qr.name).filter(|&i| i == d.pick))
            {
                app.with_dialog::<QuarantineDialog>(me, |d, cx| d.load(cx, i));
            }
        });
    }
}

impl Dialog for QuarantineDialog {
    fn title(&self) -> String {
        tf!("Quarantine of %s", self.name())
    }

    fn width(&self) -> f32 {
        760.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        if self.done {
            return Flow::Close;
        }
        let look = cx.look.clone();
        let days = if self.job.keep_days == 0 { 30 } else { self.job.keep_days };
        ui.label(RichText::new(tf!("What the runs deleted or replaced, kept %d days. Files go back to where they were in the destination of that run.", days)).size(11.0).color(look.c("fg-muted")));
        for w in &self.warn {
            ui.label(RichText::new(w).strong().size(11.0).color(look.c("warn")));
        }
        if self.qs.is_empty() {
            ui.label(RichText::new(t("The quarantine is empty.")).color(look.c("fg-muted")));
            return if footer(ui, cx, None, "OK", false, true).ok { Flow::Close } else { Flow::Keep };
        }
        let labels: Vec<String> = self
            .qs
            .iter()
            .map(|q| {
                let when =
                    q.created.map(|c| c.with_timezone(&Local).format("%d/%m/%Y %H:%M").to_string()).unwrap_or_default();
                format!("{when}  ·  {} {}  ·  {}", q.files, t("files"), size_text(q.size))
            })
            .collect();
        let mut i = self.pick;
        if widgets::select(ui, "quarantine-pick", &mut i, &labels, 520.0) {
            self.load(cx, i);
        }
        egui::ScrollArea::vertical().id_salt("quarantine-files").max_height(300.0).min_scrolled_height(300.0).show(
            ui,
            |ui| {
                for (k, f) in self.files.iter().enumerate() {
                    let mut txt = format!("{}  ·  {}", f.path, size_text(f.size));
                    if f.in_dest {
                        txt = format!("{txt}  ·  {}", t("in the destination"));
                    }
                    if ui.selectable_label(self.sel == Some(k), txt).clicked() {
                        self.sel = Some(k);
                    }
                }
            },
        );
        ui.checkbox(
            &mut self.replace,
            t("Replace files in the destination (the current ones go into the quarantine in their place)"),
        );
        ui.horizontal(|ui| {
            if widgets::button(ui, &look, "↩", t("Give back selected"), Kind::Secondary).clicked() {
                self.give_back(cx, false);
            }
            if widgets::button(ui, &look, "↩", t("Give back all"), Kind::Primary).clicked() {
                self.give_back(cx, true);
            }
            if widgets::button(ui, &look, "⬇", t("Download"), Kind::Secondary).clicked()
                && let (Some(f), Some(qr)) = (self.sel.and_then(|i| self.files.get(i)), self.qs.get(self.pick))
            {
                let path = format!("{}/quarantines/{}/file?path={}", self.base, esc(&qr.name), q(&f.path));
                let name = f.path.rsplit('/').next().unwrap_or(&f.path).to_string();
                crate::files::save_download(cx, cx.panel.clone(), path, name, |app, p| {
                    app.cx.set_status(tf!("Saved to %s", p.display()))
                });
            }
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                if widgets::button(ui, &look, "🗑", t("Empty the quarantine"), Kind::Danger).clicked() {
                    let me = cx.current_dialog();
                    let total: i64 = self.qs.iter().map(|q| q.size).sum();
                    let (id, base) = (self.job.id.clone(), self.base.clone());
                    cx.confirm_typed(
                        t("Empty the quarantine"),
                        tf!(
                            "Remove every quarantine of %s (%s)? Nothing there can be given back afterwards.",
                            self.name(),
                            size_text(total)
                        ),
                        &id.clone(),
                        move |cx| {
                            #[derive(serde::Deserialize, Default)]
                            #[serde(default)]
                            struct Out {
                                removed: i64,
                                freed: i64,
                            }
                            let api = cx.panel.with_timeout(Duration::from_secs(1800));
                            let path = format!("{base}/{}/quarantine/empty", esc(&id));
                            let body = json!({ "confirm": id });
                            cx.run(
                                async move { api.send::<Out, _>(Method::POST, &path, Some(&body)).await },
                                move |app, r| {
                                    app.cx.set_status(tf!(
                                        "%d quarantines removed (%s).",
                                        r.removed,
                                        size_text(r.freed)
                                    ));
                                    if let Some(d) = dialog(app, me) {
                                        d.done = true;
                                    }
                                },
                            );
                        },
                    );
                }
            });
        });
        if footer(ui, cx, None, t("Close"), false, true).ok { Flow::Close } else { Flow::Keep }
    }
}
