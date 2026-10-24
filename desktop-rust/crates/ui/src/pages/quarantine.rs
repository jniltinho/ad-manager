//! A sync job's quarantine (what its runs deleted or replaced): files go back to the run's destination (one already
//! there swaps places only with "replace"), one downloads, and the job's whole quarantine empties with a typed
//! confirmation.

use std::time::Duration;

use adm_core::api::{esc, q};
use adm_core::i18n::{server_text, t};
use adm_core::logic::size_text;
use adm_core::model::{Quarantine, QuarantineFile, QuarantineList, SyncJob};
use adm_core::tf;
use egui::{RichText, Ui};
use reqwest::Method;
use serde_json::json;

use crate::app::{App, Cx};
use crate::modal::{Dialog, Flow, footer};
use crate::widgets::{self, Kind};

/// Reads the quarantines of the job and opens the dialog.
pub fn open(cx: &mut Cx, agent: i64, j: SyncJob, first: &str) {
    let first = first.to_string();
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
        let d = QuarantineDialog {
            base,
            job: j,
            qs,
            warn,
            pick: 0,
            files: Vec::new(),
            sel: Vec::new(),
            truncated: false,
            replace: false,
            asked: 0,
            done: false,
        };
        let i = d.qs.iter().position(|q| q.name == first).unwrap_or(0);
        // opened first: the reply goes to this dialog by its instance id
        let id = app.cx.push(d);
        app.with_dialog::<QuarantineDialog>(id, |d, cx| d.load(cx, i));
    });
}

pub struct QuarantineDialog {
    base: String,
    job: SyncJob,
    qs: Vec<Quarantine>,
    warn: Vec<String>,
    pick: usize,
    files: Vec<QuarantineFile>,
    /// The files checked for "Give back selected".
    sel: Vec<bool>,
    /// The agent listed only the first 2000 files.
    truncated: bool,
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
        self.sel.clear();
        #[derive(serde::Deserialize, Default)]
        #[serde(default)]
        struct Out {
            files: Vec<QuarantineFile>,
            truncated: bool,
        }
        let api = cx.panel.clone();
        let path = format!("{}/quarantines/{}/files", self.base, esc(&qr.name));
        cx.run(async move { api.get::<Out>(&path).await }, move |app, out| {
            if let Some(d) = dialog(app, me)
                && d.asked == n
            {
                d.sel = vec![false; out.files.len()];
                d.files = out.files;
                d.truncated = out.truncated;
            }
        });
    }

    fn give_back(&mut self, cx: &Cx, all: bool) {
        let me = cx.current_dialog();
        let paths: Vec<String> = if all {
            Vec::new()
        } else {
            let p: Vec<String> =
                self.files.iter().zip(&self.sel).filter(|(_, on)| **on).map(|(f, _)| f.path.clone()).collect();
            if p.is_empty() {
                return;
            }
            p
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
            error_count: i64,
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
            if r.error_count > 0 {
                lines.push(format!("{} {}", t("Errors:"), r.error_count));
                lines.extend(r.errors.iter().take(20).map(|e| server_text(e)));
            }
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
        let muted = look.c("fg-muted");
        let days = if self.job.keep_days == 0 { 30 } else { self.job.keep_days };
        let gib = if self.job.keep_gib > 0 { tf!(" and up to %d GiB", self.job.keep_gib) } else { String::new() };
        let intro = tf!(
            "What the runs deleted or replaced, kept %s days%s. Files go back to where they were in the destination of that run.",
            days,
            gib
        );
        ui.add(egui::Label::new(RichText::new(intro).size(12.0).color(muted)).wrap());
        for w in &self.warn {
            ui.label(RichText::new(w).strong().size(11.0).color(look.c("warn")));
        }
        ui.add_space(8.0);
        let (mut give, mut give_all, mut empty) = (false, false, false);
        if self.qs.is_empty() {
            ui.add_space(8.0);
            ui.vertical_centered(|ui| ui.label(RichText::new(t("The quarantine is empty.")).size(12.0).color(muted)));
        } else {
            // the quarantines (one per run), the one shown in bold
            let rows: Vec<[RichText; 4]> = self
                .qs
                .iter()
                .enumerate()
                .map(|(i, q)| {
                    let b = |s: String| if i == self.pick { crate::style::semibold(s, 12.0) } else { RichText::new(s) };
                    [
                        b(adm_core::i18n::date(q.created)),
                        RichText::new(&q.dest).monospace().size(11.0),
                        b(q.files.to_string()),
                        b(size_text(q.size)),
                    ]
                })
                .collect();
            let mut pick = None;
            let mut cell = |ui: &mut Ui, k: usize| {
                if widgets::act(ui, &look, "", t("open ›"), None, 12.0).clicked() {
                    pick = Some(k);
                }
            };
            let rows5: Vec<[RichText; 5]> =
                rows.into_iter().map(|[a, b, c, d]| [a, b, c, d, RichText::new("")]).collect();
            widgets::mini_table(
                ui,
                &look,
                [t("Run"), t("Destination"), t("Files"), t("Size"), ""],
                &rows5,
                Some(&mut cell),
            );
            if let Some(k) = pick {
                self.load(cx, k);
            }
            ui.add_space(12.0);
            // the files of the one shown: checked ones go back with "Give back selected"
            let mut download = None;
            egui::Frame::new().stroke(egui::Stroke::new(1.0, look.c("line"))).show(ui, |ui| {
                egui::ScrollArea::vertical().id_salt("quarantine-files").max_height(256.0).show(ui, |ui| {
                    let rows: Vec<[RichText; 4]> = self
                        .files
                        .iter()
                        .map(|f| {
                            let name = if f.in_dest {
                                format!("{}  ({})", f.path, t("in the destination"))
                            } else {
                                f.path.clone()
                            };
                            [
                                RichText::new(name).monospace().size(11.0),
                                RichText::new(size_text(f.size)),
                                RichText::new(adm_core::i18n::date(f.modified)),
                                RichText::new(""),
                            ]
                        })
                        .collect();
                    let sel = &mut self.sel;
                    let files = &self.files;
                    let mut first = |ui: &mut Ui, k: usize| {
                        if widgets::act(ui, &look, crate::icons::DOWNLOAD, "", None, 12.0)
                            .on_hover_text(t("Download"))
                            .clicked()
                        {
                            download = Some(k);
                        }
                        ui.add_space(8.0);
                        if let Some(on) = sel.get_mut(k) {
                            let r = crate::widgets::check(ui, on, "");
                            let _ = ui.ctx().accesskit_node_builder(r.id, |n| n.set_label(files[k].path.clone()));
                        }
                    };
                    widgets::mini_table(ui, &look, [t("File"), t("Size"), t("Modified"), ""], &rows, Some(&mut first));
                    if self.files.is_empty() {
                        ui.add_space(8.0);
                        ui.vertical_centered(|ui| ui.label(RichText::new(t("No files.")).size(12.0).color(muted)));
                        ui.add_space(8.0);
                    }
                });
            });
            if let (Some(k), Some(qr)) = (download, self.qs.get(self.pick))
                && let Some(f) = self.files.get(k)
            {
                let path = format!("{}/quarantines/{}/file?path={}", self.base, esc(&qr.name), q(&f.path));
                let name = f.path.rsplit('/').next().unwrap_or(&f.path).to_string();
                crate::files::save_download(cx, cx.panel.clone(), path, name, |app, p| {
                    app.cx.set_status(tf!("Saved to %s", p.display()))
                });
            }
            if self.truncated {
                ui.label(
                    RichText::new(t("Only the first 2000 files are listed; \"give back all\" takes every one."))
                        .size(12.0)
                        .color(muted),
                );
            }
            crate::widgets::check(
                ui,
                &mut self.replace,
                RichText::new(t(
                    "Replace files in the destination (the current ones go into the quarantine in their place)",
                ))
                .size(12.0),
            );
            ui.add_space(12.0);
            ui.horizontal(|ui| {
                ui.spacing_mut().item_spacing.x = 8.0;
                empty =
                    widgets::button(ui, &look, crate::icons::TRASH, t("Empty the quarantine"), Kind::Danger).clicked();
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    let some = !self.files.is_empty();
                    give_all = ui
                        .add_enabled_ui(some, |ui| {
                            widgets::button(ui, &look, crate::icons::ROTATE, t("Give back all"), Kind::Primary)
                        })
                        .inner
                        .clicked();
                    let picked = self.sel.iter().any(|x| *x);
                    give = ui
                        .add_enabled_ui(picked, |ui| {
                            widgets::button(ui, &look, crate::icons::ROTATE, t("Give back selected"), Kind::Secondary)
                        })
                        .inner
                        .clicked();
                });
            });
        }
        if give {
            self.give_back(cx, false);
        }
        if give_all {
            self.give_back(cx, true);
        }
        if empty {
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
                    cx.run(async move { api.send::<Out, _>(Method::POST, &path, Some(&body)).await }, move |app, r| {
                        app.cx.set_status(tf!("%d quarantines removed (%s).", r.removed, size_text(r.freed)));
                        if let Some(d) = dialog(app, me) {
                            d.done = true;
                        }
                    });
                },
            );
        }
        if footer(ui, cx, None, t("Close"), false, true).ok { Flow::Close } else { Flow::Keep }
    }
}
