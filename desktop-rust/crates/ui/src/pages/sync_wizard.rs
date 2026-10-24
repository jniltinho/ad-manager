//! The sync job wizard, as the web one, in four steps: source and access (with the connection test) → what to copy
//! (the source's shares and folders, with sizes) → where to (this server's folders, checked as they are picked) → how
//! and when, with the summary. An edit opens on the last step and keeps the stored password unless one is typed.

use std::time::Duration;

use adm_core::api::{Api, esc};
use adm_core::forms::{SyncForm, SyncFormError, new_sync_job, sync_unc};
use adm_core::i18n::{server_text, t};
use adm_core::logic::{Tone, size_text, slug};
use adm_core::model::{CheckStep, SyncJob, SyncList, SyncRun};
use adm_core::tf;
use chrono::{DateTime, Local, Utc};
use egui::{RichText, Ui};
use reqwest::Method;
use serde_json::{Value, json};
use zeroize::Zeroizing;

use crate::app::{App, Cx};
use crate::modal::{Dialog, Flow};
use crate::widgets::{self, Kind};

/// Opens the wizard for a new job (`job` None) or to edit one.
pub fn open(cx: &mut Cx, agent: i64, data: SyncList, job: Option<SyncJob>) {
    let is_new = job.is_none();
    let j = job.unwrap_or_else(|| new_sync_job(data.kerberos));
    let w = SyncWizard {
        pwd_id: crate::widgets::secret_id("sync-password"),
        base: format!("/api/agents/{agent}"),
        api: cx.panel.clone(),
        is_new,
        password: Zeroizing::default(),
        step: 0,
        host: j.source.host.clone(),
        user: j.source.user.clone(),
        domain: j.source.domain.clone(),
        kerberos: j.source.auth != "ntlm" && data.kerberos,
        test: Vec::new(),
        tested: None,
        shares: Vec::new(),
        items: Vec::new(),
        src_size: None,
        size_label: String::new(),
        item_sizes: Default::default(),
        dirs: Vec::new(),
        cur_dir: String::new(),
        dest: j.dest.clone(),
        dest_status: (String::new(), Tone::Normal),
        dest_ok: !is_new,
        dest_free: None,
        new_name: String::new(),
        form: SyncForm::from_job(&j),
        preset: 0,
        next_runs: (String::new(), Tone::Muted),
        cron_ok: true,
        done: false,
        run: None,
        saving: false,
        reached: 0,
        data,
        job: j,
    };
    let id = cx.push(w);
    if !is_new {
        cx.defer(move |app| app.with_dialog::<SyncWizard>(id, |w, cx| w.show(cx, 3)));
    }
}

const PRESETS: [(&str, &str); 4] = [
    ("", "Only by hand"),
    ("0 1 * * *", "Every day at 01:00"),
    ("0 */4 * * *", "Every 4 hours"),
    ("0 2 * * 0", "Sundays at 02:00"),
];

pub struct SyncWizard {
    pwd_id: egui::Id,
    base: String,
    api: Api,
    data: SyncList,
    is_new: bool,
    job: SyncJob,
    password: Zeroizing<String>,
    step: usize,
    // 1. source and access
    host: String,
    user: String,
    domain: String,
    kerberos: bool,
    test: Vec<(String, Tone)>,
    /// The access the last good test was for (the password compared apart, never in a key string).
    tested: Option<(String, Zeroizing<String>)>,
    shares: Vec<String>,
    // 2. what to copy
    items: Vec<String>,
    /// The size counted for a subfolder (its "size" link), by name.
    item_sizes: std::collections::HashMap<String, String>,
    src_size: Option<(i64, i64)>,
    size_label: String,
    // 3. where to
    dirs: Vec<String>,
    cur_dir: String,
    dest: String,
    dest_status: (String, Tone),
    dest_ok: bool,
    dest_free: Option<u64>,
    new_name: String,
    // 4. how and when
    form: SyncForm,
    preset: usize,
    next_runs: (String, Tone),
    cron_ok: bool,
    done: bool,
    // 5. the preview, followed here
    run: Option<SyncRun>,
    /// A save in flight: no second one meanwhile.
    saving: bool,
    /// The furthest step reached: the done ones can be opened again (not from the preview).
    reached: usize,
}

/// The wizard `id`, if still open (a reply never reaches another wizard opened later).
fn wizard(app: &mut App, id: u64) -> Option<&mut SyncWizard> {
    app.cx.dialog_mut::<SyncWizard>(id)
}

impl SyncWizard {
    /// The request body with the source's credentials (and the job, when editing: its stored password is used).
    fn creds(&self, share: &str, path: &str, extra: Value) -> Zeroizing<Vec<u8>> {
        let mut src = self.job.source.clone();
        src.share = share.into();
        src.path = path.into();
        let mut b = json!({ "source": src, "password_file": self.job.password_file });
        if !self.is_new {
            b["job"] = json!(self.job.id);
        }
        if let (Some(b), Value::Object(e)) = (b.as_object_mut(), extra) {
            b.extend(e);
        }
        adm_core::api::json_with_secret(&b, "password", &self.password)
    }

    /// Copies step 1's fields into the job ("DOMAIN\user" and "user@domain" split).
    fn read_access(&mut self) {
        self.job.source.host = self.host.trim().into();
        self.job.source.auth = if self.kerberos { "kerberos" } else { "ntlm" }.into();
        if !self.kerberos {
            let mut u = self.user.trim().to_string();
            if let Some((d, n)) = u.split_once('\\') {
                self.domain = d.into();
                u = n.into();
            } else if let Some((n, d)) = u.split_once('@') {
                self.domain = d.into();
                u = n.into();
            }
            self.user = u.clone();
            self.job.source.user = u;
            self.job.source.domain = self.domain.trim().into();
        }
    }

    fn access_key(&self) -> (String, Zeroizing<String>) {
        let s = &self.job.source;
        ([s.host.as_str(), &s.auth, &s.user, &s.domain].join("\0"), self.password.clone())
    }

    fn show(&mut self, cx: &Cx, n: usize) {
        self.step = n;
        match n {
            1 => {
                let (share, path) = (self.job.source.share.clone(), self.job.source.path.clone());
                self.open_dir(cx, share, path);
            }
            2 => {
                let root = self.data.roots.first().cloned().unwrap_or_else(|| "/srv/shares".into());
                let start = match self.dest.rsplit_once('/') {
                    Some((p, _)) if !self.dest.is_empty() && !p.is_empty() => p.to_string(),
                    _ => root,
                };
                self.browse(cx, start);
                let d = self.dest.trim().to_string();
                self.check_dest(cx, d);
            }
            3 => {
                if self.job.keep_days == 0 {
                    self.job.keep_days = 30; // 0 and missing mean the agent's default
                }
                if self.job.name.is_empty() {
                    let last = format!("/{}/{}", self.job.source.share, self.job.source.path);
                    let last = last.trim_end_matches('/').rsplit('/').next().unwrap_or_default().to_string();
                    self.job.name = format!("{last} · {}", self.job.source.host);
                }
                let id = if self.is_new { slug(&self.job.name) } else { self.job.id.clone() };
                self.form = SyncForm { id, ..SyncForm::from_job(&self.job) };
                self.preset = PRESETS.iter().position(|p| p.0 == self.form.cron).unwrap_or(PRESETS.len());
                let c = self.form.cron.clone();
                self.check_cron(cx, c);
            }
            _ => {}
        }
    }

    fn run_test(&mut self, cx: &mut Cx, next: bool) {
        let me = cx.current_dialog();
        self.read_access();
        cx.set_status(t("Testing the connection…"));
        let body = self.creds("", "", json!({ "check": true }));
        let api = self.api.with_timeout(Duration::from_secs(120));
        let path = format!("{}/syncs/browse", self.base);
        let key = self.access_key();
        #[derive(serde::Deserialize, Default)]
        struct Out {
            #[serde(default)]
            steps: Vec<CheckStep>,
        }
        cx.run(async move { api.send_bytes::<Out>(Method::POST, &path, &body).await }, move |app, out| {
            app.cx.set_status("");
            let Some(w) = wizard(app, me) else { return };
            let names = |n: &str| match n {
                "credentials" => t("Credentials").to_string(),
                "resolve" => t("Name resolved").into(),
                "port" => t("Port 445 answers").into(),
                "signin" => t("Signed in (signed SMB)").into(),
                _ => n.to_string(),
            };
            w.test = vec![(t("Connection test").into(), Tone::Strong)];
            let mut ok = !out.steps.is_empty();
            for st in &out.steps {
                let n = names(&st.name);
                if st.ok && st.name == "signin" {
                    w.test.push((format!("✓ {n} · {}", tf!("%d shares visible", st.items.len())), Tone::Ok));
                    w.shares = st.items.clone();
                } else if st.ok {
                    w.test.push((format!("✓ {n} {}", st.detail), Tone::Ok));
                } else {
                    ok = false;
                    let why = server_text(if st.hint.is_empty() { &st.detail } else { &st.hint });
                    w.test.push((format!("✗ {n}: {why}"), Tone::Err));
                }
            }
            ok = ok && out.steps.last().is_some_and(|s| s.name == "signin");
            if ok {
                w.tested = Some(key);
                if next {
                    app.with_dialog::<SyncWizard>(me, |w, cx| w.show(cx, 1));
                }
            }
        });
    }

    fn open_dir(&mut self, cx: &Cx, share: String, path: String) {
        let me = cx.current_dialog();
        if share.is_empty() {
            self.job.source.share.clear();
            self.job.source.path.clear();
            self.items = self.shares.clone();
            return;
        }
        let body = self.creds(&share, &path, json!({}));
        let api = self.api.clone();
        let url = format!("{}/syncs/browse", self.base);
        #[derive(serde::Deserialize, Default)]
        struct Out {
            #[serde(default)]
            dirs: Vec<String>,
        }
        cx.run(async move { api.send_bytes::<Out>(Method::POST, &url, &body).await }, move |app, out| {
            let Some(w) = wizard(app, me) else { return };
            w.job.source.share = share;
            w.job.source.path = path;
            w.items = out.dirs;
            w.size_label.clear();
            w.item_sizes.clear();
            w.src_size = None;
        });
    }

    fn count_size(&mut self, cx: &Cx) {
        let me = cx.current_dialog();
        if self.job.source.share.is_empty() {
            return;
        }
        self.size_label = t("Counting…").into();
        let body = self.creds(&self.job.source.share.clone(), &self.job.source.path.clone(), json!({ "size": true }));
        let api = self.api.with_timeout(Duration::from_secs(360));
        let url = format!("{}/syncs/browse", self.base);
        #[derive(serde::Deserialize, Default)]
        struct Out {
            #[serde(default)]
            files: i64,
            #[serde(default)]
            bytes: i64,
        }
        cx.run(async move { api.send_bytes::<Out>(Method::POST, &url, &body).await }, move |app, out| {
            if let Some(w) = wizard(app, me) {
                w.src_size = Some((out.files, out.bytes));
                w.size_label = tf!("%d files · %s", out.files, size_text(out.bytes));
            }
        });
    }

    /// Counts one subfolder of the folder on screen (its row's "size" link, as the web's).
    fn count_item(&mut self, cx: &Cx, item: String) {
        let me = cx.current_dialog();
        let s = &self.job.source;
        let (share, path) = if s.share.is_empty() {
            (item.clone(), String::new())
        } else {
            (s.share.clone(), format!("{}/{item}", s.path).trim_matches('/').to_string())
        };
        self.item_sizes.insert(item.clone(), t("Counting…").into());
        let body = self.creds(&share, &path, json!({ "size": true }));
        let api = self.api.with_timeout(Duration::from_secs(360));
        let url = format!("{}/syncs/browse", self.base);
        #[derive(serde::Deserialize, Default)]
        #[serde(default)]
        struct Out {
            files: i64,
            bytes: i64,
        }
        cx.spawn(async move { api.send_bytes::<Out>(Method::POST, &url, &body).await }, move |app, r| {
            if let Some(w) = wizard(app, me) {
                let text = match r {
                    Ok(o) => tf!("%d files · %s", o.files, size_text(o.bytes)),
                    Err(e) => e.to_string(),
                };
                w.item_sizes.insert(item, text);
            }
        });
    }

    fn browse(&mut self, cx: &Cx, p: String) {
        let me = cx.current_dialog();
        let api = self.api.clone();
        let url = format!("{}/dirs?path={}", self.base, esc(&p));
        #[derive(serde::Deserialize, Default)]
        struct Out {
            #[serde(default)]
            dirs: Vec<String>,
        }
        cx.run(async move { api.get::<Out>(&url).await }, move |app, out| {
            if let Some(w) = wizard(app, me) {
                w.cur_dir = p;
                w.dirs = out.dirs;
            }
        });
    }

    fn in_root(&self, p: &str) -> bool {
        self.data.roots.iter().any(|r| r == p)
    }

    fn check_dest(&mut self, cx: &Cx, d: String) {
        let me = cx.current_dialog();
        self.dest_ok = false;
        let api = self.api.clone();
        let url = format!("{}/syncs/validate", self.base);
        #[derive(serde::Deserialize, Default)]
        struct Dest {
            #[serde(default)]
            ok: bool,
            #[serde(default)]
            error: String,
            #[serde(default)]
            exists: bool,
            #[serde(default)]
            items: i64,
            #[serde(default)]
            free_bytes: Option<u64>,
            #[serde(default)]
            share: String,
        }
        #[derive(serde::Deserialize, Default)]
        struct Out {
            #[serde(default)]
            dest: Dest,
        }
        let body = json!({ "dest": d });
        cx.run(async move { api.send::<Out, _>(Method::POST, &url, Some(&body)).await }, move |app, out| {
            let Some(w) = wizard(app, me) else { return };
            if w.dest.trim() != d {
                return;
            }
            let r = out.dest;
            if !r.ok {
                w.dest_status = (format!("✗ {}", server_text(&r.error)), Tone::Err);
                return;
            }
            let mut msg = if r.exists { t("The folder exists").to_string() } else { t("The folder is made when the job runs").to_string() };
            if !r.share.is_empty() {
                msg = format!("{msg} · {}", tf!("shared as %s", r.share));
            }
            w.dest_free = r.free_bytes;
            if let Some(free) = r.free_bytes {
                msg = format!("{msg} · {}", tf!("%s free", size_text(free as i64)));
                if w.src_size.is_some_and(|s| s.1 > free as i64) {
                    msg = format!("{msg}\n✗ {}", t("Not enough free space for the source."));
                }
            }
            if r.items > 0 {
                msg = format!(
                    "{msg}\n⚠ {}",
                    tf!("The folder already has %d items: files of the same name are replaced (the old ones go to the quarantine).", r.items)
                );
            }
            w.dest_status = (format!("✓ {msg}"), Tone::Ok);
            w.dest_ok = true;
        });
    }

    fn check_cron(&mut self, cx: &Cx, c: String) {
        let me = cx.current_dialog();
        self.cron_ok = true;
        if c.is_empty() {
            self.next_runs.0.clear();
            return;
        }
        let api = self.api.clone();
        let url = format!("{}/syncs/validate", self.base);
        #[derive(serde::Deserialize, Default)]
        struct Cron {
            #[serde(default)]
            ok: bool,
            #[serde(default)]
            next: Vec<DateTime<Utc>>,
        }
        #[derive(serde::Deserialize, Default)]
        struct Out {
            #[serde(default)]
            cron: Cron,
        }
        let body = json!({ "cron": c });
        cx.run(async move { api.send::<Out, _>(Method::POST, &url, Some(&body)).await }, move |app, out| {
            let Some(w) = wizard(app, me) else { return };
            if w.form.cron.trim() != c {
                return;
            }
            w.cron_ok = out.cron.ok;
            w.next_runs = if out.cron.ok {
                let ts: Vec<String> = out
                    .cron
                    .next
                    .iter()
                    .map(|n| n.with_timezone(&Local).format("%a %d/%m %H:%M").to_string())
                    .collect();
                (tf!("Next: %s", ts.join(" · ")), Tone::Muted)
            } else {
                (t("Not a valid schedule (min hour day month weekday).").into(), Tone::Err)
            };
        });
    }

    fn save(&mut self, cx: &mut Cx, preview: bool) {
        let me = cx.current_dialog();
        if self.saving {
            return;
        }
        if !self.cron_ok || (self.is_new && !self.dest_ok) {
            cx.show_info(t("Sync"), t("Not a valid schedule (min hour day month weekday)."));
            return;
        }
        let job = match self.form.to_job(&self.job, t("MIRROR"), &self.password) {
            Ok(j) => j,
            Err(SyncFormError::Mirror) => {
                cx.show_info(t("Mirror"), tf!("A mirror deletes in the destination. Type %s to confirm", t("MIRROR")));
                return;
            }
        };
        let api = self.api.clone();
        let path = format!("{}/syncs/{}", self.base, esc(&job.id));
        self.saving = true;
        let session = cx.panel.id;
        cx.spawn(
            async move {
                api.act_with(Method::PUT, &path, &job).await?;
                if preview {
                    return Ok(Some(
                        api.send::<SyncRun, _>(Method::POST, &format!("{path}/preview"), None::<&()>).await?,
                    ));
                }
                Ok(None)
            },
            move |app, r: adm_core::api::ApiResult<Option<SyncRun>>| {
                if let Some(w) = wizard(app, me) {
                    w.saving = false;
                }
                if app.cx.panel.id != session {
                    return;
                }
                let run = match r {
                    Ok(run) => run,
                    Err(e) => return app.cx.handle_error(e),
                };
                app.with_shell(|s, cx| s.sync.reload(cx));
                let Some(r) = run else {
                    app.cx.set_status(t("Sync job saved."));
                    if let Some(w) = wizard(app, me) {
                        w.done = true;
                    }
                    return;
                };
                // the preview is followed in the wizard (step 5); the wizard closed meanwhile: only a note
                if let Some(w) = wizard(app, me) {
                    w.run = Some(r);
                    w.step = 4;
                    poll(&app.cx, me);
                } else {
                    app.cx.set_status(t("Sync job saved; preview queued."));
                }
            },
        );
    }

    /// Step 5: the preview, followed here (nothing written yet), with Run now once it is done; true to close.
    fn step5(&mut self, ui: &mut Ui, cx: &mut Cx) -> bool {
        let look = cx.look.clone();
        let Some(r) = self.run.clone() else { return true };
        let running = r.status == "queued" || r.status == "running";
        let (icon, text, ink) = if running {
            ("⟳", t("Preview running: listing the source and the destination…"), look.c("fg-strong"))
        } else if r.status == "ok" {
            ("✔", t("Preview done: nothing was written yet."), look.c("ok"))
        } else {
            ("✖", t("The preview failed."), look.c("err"))
        };
        ui.horizontal(|ui| {
            ui.label(RichText::new(icon).color(ink));
            ui.label(crate::style::semibold(text, 12.0).color(look.c("fg-strong")));
        });
        if running {
            ui.ctx().request_repaint_after(std::time::Duration::from_millis(500));
        }
        if !r.error.is_empty() {
            ui.colored_label(look.c("err"), adm_core::i18n::server_text(&r.error));
        }
        if let Some(p) = r.result.as_ref().map(|x| &x.plan) {
            ui.add_space(8.0);
            let mut rows = vec![
                [RichText::new(t("New")).strong(), RichText::new(format!("{} ({})", p.new, size_text(p.new_bytes)))],
                [
                    RichText::new(t("Changed")).strong(),
                    RichText::new(format!("{} ({})", p.changed, size_text(p.changed_bytes))),
                ],
            ];
            if self.form.mirror {
                let del = RichText::new(format!("{} ({})", p.delete, size_text(p.delete_bytes)));
                rows.push([
                    RichText::new(t("To delete")).strong(),
                    if p.delete > 0 { del.color(look.c("warn")) } else { del },
                ]);
            }
            rows.push([RichText::new(t("Unchanged")).strong(), RichText::new(p.same.to_string())]);
            widgets::mini_table(ui, &look, ["", ""], &rows, None);
            for (k, label) in [("new", "New"), ("changed", "Changed"), ("delete", "To delete")] {
                if let Some(list) = p.sample.get(k).filter(|l| !l.is_empty()) {
                    egui::CollapsingHeader::new(format!("{} ({})", t(label), tf!("first %d", list.len())))
                        .id_salt(("preview-sample", k))
                        .show(ui, |ui| {
                            egui::Frame::new().fill(look.c("surface-2")).inner_margin(egui::Margin::same(8)).show(
                                ui,
                                |ui| {
                                    ui.set_width(ui.available_width());
                                    egui::ScrollArea::vertical().max_height(128.0).show(ui, |ui| {
                                        ui.label(RichText::new(list.join("\n")).monospace().size(11.0));
                                    });
                                },
                            );
                        });
                }
            }
        }
        let (mut close, mut run_now) = (false, false);
        let ok = r.status == "ok";
        Self::nav(
            ui,
            &look,
            |_| {},
            |ui| {
                let label = t("Run now");
                run_now =
                    ui.add_enabled_ui(ok, |ui| widgets::button(ui, &look, "▶", label, Kind::Primary)).inner.clicked();
                close = widgets::button(ui, &look, "", t("Close (the job is saved)"), Kind::Secondary).clicked();
            },
        );
        if run_now {
            let api = self.api.clone();
            // the previewed job (the run's), not the form's id, editable meanwhile
            let path = format!("{}/syncs/{}/run", self.base, esc(&r.job));
            cx.run(async move { api.act(Method::POST, &path).await }, |app, ()| {
                app.cx.set_status(t("Sync queued."));
                app.with_shell(|s, cx| s.sync.reload(cx));
            });
            return true;
        }
        close
    }

    /// The web's stepper: numbered boxes (the current one filled), a line under them, then "Step n of 4: name".
    fn header(&mut self, ui: &mut Ui, cx: &Cx) {
        let look = cx.look.clone();
        let look = &look;
        let names =
            [t("Source and access"), t("What to copy"), t("Where to"), t("How and when"), t("Preview of the copy")];
        let mut go = None;
        ui.horizontal_wrapped(|ui| {
            ui.spacing_mut().item_spacing.x = 8.0;
            for (i, s) in names.iter().enumerate() {
                let on = i == self.step;
                let (r, _) = ui.allocate_exact_size(egui::vec2(20.0, 20.0), egui::Sense::hover());
                if on {
                    ui.painter().rect_filled(r, 0.0, look.c("action"));
                } else {
                    ui.painter().rect_stroke(r, 0.0, egui::Stroke::new(1.0, look.c("line")), egui::StrokeKind::Inside);
                }
                let ink = if on { look.c("on-action") } else { look.c("fg-muted") };
                let g = ui.painter().layout_no_wrap(
                    (i + 1).to_string(),
                    egui::FontId::new(11.0, egui::FontFamily::Name(crate::fonts::SEMIBOLD.into())),
                    ink,
                );
                ui.painter().galley(r.center() - g.size() / 2.0, g, ink);
                let label = if on {
                    crate::style::semibold(*s, 12.0).color(look.c("fg-strong"))
                } else {
                    RichText::new(*s).size(12.0).color(look.c("fg-muted"))
                };
                // a done step opens again (not from the preview, nor the preview itself)
                let open = !on && i < 4 && i <= self.reached && self.step < 4;
                let r = ui.add(egui::Label::new(label).sense(if open {
                    egui::Sense::click()
                } else {
                    egui::Sense::hover()
                }));
                if open && r.on_hover_cursor(egui::CursorIcon::PointingHand).clicked() {
                    go = Some(i);
                }
                ui.add_space(8.0);
            }
        });
        ui.add_space(6.0);
        let (r, _) = ui.allocate_exact_size(egui::vec2(ui.available_width(), 3.0), egui::Sense::hover());
        ui.painter().rect_filled(r, 0.0, look.c("surface-2"));
        ui.add_space(12.0);
        let head = tf!("Step %d of %d: %s", self.step + 1, names.len(), names[self.step.min(4)]);
        ui.label(crate::style::semibold(head, 13.0).color(look.c("fg-strong")));
        ui.add_space(4.0);
        if let Some(i) = go {
            self.show(cx, i);
        }
    }

    /// The row under a step: the line, then `left` (back, test) at the left and `right` (the step's action) at the right.
    fn nav(ui: &mut Ui, look: &crate::style::Look, left: impl FnOnce(&mut Ui), right: impl FnOnce(&mut Ui)) {
        ui.add_space(12.0);
        let y = ui.cursor().top();
        ui.painter().hline(ui.max_rect().x_range(), y + 0.5, egui::Stroke::new(1.0, look.c("line")));
        ui.add_space(12.0);
        ui.horizontal(|ui| {
            ui.spacing_mut().item_spacing.x = 8.0;
            left(ui);
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), right);
        });
    }

    /// A choice as the web's card: an icon, a semibold title and its description; the chosen one stands out.
    fn choice(ui: &mut Ui, look: &crate::style::Look, w: f32, on: bool, icon: &str, title: &str, text: &str) -> bool {
        Self::choice_if(ui, look, w, on, true, icon, title, text)
    }

    /// A choice card, greyed and not clickable when `enabled` is false (Kerberos without a domain).
    #[allow(clippy::too_many_arguments)]
    fn choice_if(
        ui: &mut Ui,
        look: &crate::style::Look,
        w: f32,
        on: bool,
        enabled: bool,
        icon: &str,
        title: &str,
        text: &str,
    ) -> bool {
        if !enabled {
            ui.add_enabled_ui(false, |ui| Self::choice_if(ui, look, w, false, true, icon, title, text));
            return false;
        }
        let fill = if on { look.c("row-hover") } else { look.c("surface") };
        let line = if on { look.c("fg-strong") } else { look.c("line") };
        let r = ui.scope_builder(egui::UiBuilder::new().sense(egui::Sense::click()), |ui| {
            ui.style_mut().interaction.selectable_labels = false; // a click on the text picks it
            egui::Frame::new()
                .fill(fill)
                .stroke(egui::Stroke::new(1.0, line))
                .inner_margin(egui::Margin::symmetric(16, 12))
                .show(ui, |ui| {
                    ui.set_width(w - 34.0);
                    ui.vertical(|ui| {
                        ui.spacing_mut().item_spacing.y = 4.0;
                        let title = crate::style::semibold(format!("{icon}  {title}"), 12.0).color(look.c("fg-strong"));
                        ui.label(title);
                        ui.add(egui::Label::new(RichText::new(text).size(11.0).color(look.c("fg-muted"))).wrap());
                    });
                });
        });
        let resp = r.response.on_hover_cursor(egui::CursorIcon::PointingHand);
        let _ = ui.ctx().accesskit_node_builder(resp.id, |n| {
            n.set_role(egui::accesskit::Role::RadioButton);
            n.set_label(title.to_string());
            n.set_toggled(if on { egui::accesskit::Toggled::True } else { egui::accesskit::Toggled::False });
        });
        resp.clicked()
    }

    fn step1(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        let w = ui.available_width();
        widgets::above(ui, &look, t("Source server"), w, |ui, _| {
            ui.add(
                crate::widgets::line(&mut self.host)
                    .hint_text("old-fs.example.local")
                    .font(egui::TextStyle::Monospace)
                    .desired_width(w),
            )
        });
        let ip = self.host.trim().split('.').count() == 4
            && self
                .host
                .trim()
                .split('.')
                .all(|p| !p.is_empty() && p.len() <= 3 && p.chars().all(|c| c.is_ascii_digit()));
        if self.kerberos && ip {
            ui.label(
                RichText::new(t(
                    "Kerberos needs the full name (old-fs.example.local); with an IP use user and password.",
                ))
                .size(11.0)
                .color(look.c("warn")),
            );
        }
        // how to sign in to the source: two cards, as the web's
        ui.add_space(12.0);
        let machine = if self.data.machine.is_empty() { "FS01$".to_string() } else { self.data.machine.clone() };
        let cw = ((w - 12.0) / 2.0).floor();
        ui.horizontal_top(|ui| {
            ui.spacing_mut().item_spacing.x = 12.0;
            // shown also when not joined to a domain, greyed with why (as the web's)
            let text = if self.data.kerberos {
                tf!(
                    "Same domain, no password kept. Give %s (or Domain Computers) read permission on the share.",
                    machine
                )
            } else {
                t("Not available: this server is not joined to a domain.").to_string()
            };
            let title = t("This server's machine account (Kerberos)");
            if Self::choice_if(ui, &look, cw, self.kerberos, self.data.kerberos, "🗄", title, &text) {
                self.kerberos = true;
            }
            let text = t("Another domain or a standalone server; the password is kept encrypted.");
            if Self::choice(ui, &look, cw, !self.kerberos, "🔑", t("User and password"), text) {
                self.kerberos = false;
            }
        });
        if !self.kerberos {
            let kept = !self.is_new && (self.job.password_set || !self.job.password_file.is_empty());
            ui.add_space(12.0);
            ui.horizontal_top(|ui| {
                ui.spacing_mut().item_spacing.x = 16.0;
                let fw = ((w - 16.0) / 2.0).floor();
                widgets::above(ui, &look, t("User"), fw, |ui, _| {
                    ui.add(crate::widgets::line(&mut self.user).hint_text("DOMAIN\\user").desired_width(fw))
                });
                widgets::above(ui, &look, t("Domain (or the server name)"), fw, |ui, _| {
                    ui.add(crate::widgets::line(&mut self.domain).desired_width(fw))
                });
            });
            ui.add_space(12.0);
            let hint = if kept { t("kept (type to change)") } else { "" };
            let (pwd_id, pwd) = (self.pwd_id, &mut self.password);
            widgets::above(ui, &look, t("Password"), w, |ui, _| crate::widgets::secret_field(ui, pwd_id, pwd, hint, w));
        }
        if !self.test.is_empty() {
            ui.add_space(12.0);
            egui::Frame::new()
                .stroke(egui::Stroke::new(1.0, look.c("line")))
                .inner_margin(egui::Margin::symmetric(12, 8))
                .show(ui, |ui| {
                    ui.set_width(ui.available_width());
                    ui.spacing_mut().item_spacing.y = 4.0;
                    widgets::lbl(ui, &look, t("Connection test"));
                    for (line, tone) in &self.test {
                        ui.add(egui::Label::new(RichText::new(line).size(12.0).color(look.tone(*tone))).wrap());
                    }
                });
        }
        let (mut test, mut next) = (false, false);
        Self::nav(
            ui,
            &look,
            |ui| test = widgets::button(ui, &look, "", t("Test connection"), Kind::Secondary).clicked(),
            |ui| next = widgets::button(ui, &look, "", &format!("{} ›", t("Next")), Kind::Primary).clicked(),
        );
        if test {
            self.run_test(cx, false);
        }
        if next {
            self.read_access();
            if self.tested.as_ref() == Some(&self.access_key()) {
                self.show(cx, 1);
            } else {
                self.run_test(cx, true);
            }
        }
    }

    fn step2(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        ui.label(RichText::new(t("Open the share and the folder to copy; \"size\" counts what is inside (it can take a while on large folders).")).size(11.0).color(look.c("fg-muted")));
        let mut go: Option<(String, String)> = None;
        let mut count: Option<String> = None;
        ui.horizontal(|ui| {
            if widgets::button(ui, &look, "⬅", t("Up"), Kind::Secondary).clicked() {
                let s = &self.job.source;
                go = Some(if !s.path.is_empty() {
                    (s.share.clone(), s.path.rsplit_once('/').map(|(p, _)| p.to_string()).unwrap_or_default())
                } else {
                    (String::new(), String::new())
                });
            }
            ui.label(RichText::new(sync_unc(&self.job)).strong());
        });
        egui::ScrollArea::vertical().id_salt("sync-items").max_height(300.0).show(ui, |ui| {
            for it in &self.items {
                ui.horizontal(|ui| {
                    if ui.selectable_label(false, format!("📁 {it}")).clicked() {
                        let s = &self.job.source;
                        go = Some(if s.share.is_empty() {
                            (it.clone(), String::new())
                        } else {
                            (s.share.clone(), format!("{}/{it}", s.path).trim_matches('/').to_string())
                        });
                    }
                    ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                        match self.item_sizes.get(it) {
                            Some(sz) => {
                                ui.label(RichText::new(sz).size(11.0).color(look.c("fg-muted")));
                            }
                            None => {
                                if widgets::act(ui, &look, "", t("size"), None, 11.0).clicked() {
                                    count = Some(it.clone());
                                }
                            }
                        }
                    });
                });
            }
        });
        if let Some(it) = count {
            self.count_item(cx, it);
        }
        if let Some((share, path)) = go {
            self.open_dir(cx, share, path);
        }
        ui.label(RichText::new(&self.size_label).size(11.0).color(look.c("fg-muted")));
        let (mut back, mut size, mut copy) = (false, false, false);
        Self::nav(
            ui,
            &look,
            |ui| {
                back = widgets::button(ui, &look, "", &format!("‹ {}", t("Back")), Kind::Secondary).clicked();
                size = widgets::button(ui, &look, "", t("Size"), Kind::Secondary).clicked();
            },
            |ui| {
                copy = widgets::button(ui, &look, "", &format!("{} ›", t("Copy this folder")), Kind::Primary).clicked()
            },
        );
        if back {
            self.step = 0;
        }
        if size {
            self.count_size(cx);
        }
        {
            if copy && !self.job.source.share.is_empty() {
                if self.dest.is_empty() {
                    let root = self.data.roots.first().cloned().unwrap_or_else(|| "/srv/shares".into());
                    let src = format!("/{}/{}", self.job.source.share, self.job.source.path);
                    let last = src.trim_end_matches('/').rsplit('/').next().unwrap_or_default();
                    self.dest = format!("{root}/{last}");
                }
                self.show(cx, 2);
            }
        }
    }

    fn step3(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        ui.label(
            RichText::new(t("Open a folder of this server and use it, or make a new one."))
                .size(11.0)
                .color(look.c("fg-muted")),
        );
        let mut go = None;
        ui.horizontal(|ui| {
            if widgets::button(ui, &look, "⬅", t("Up"), Kind::Secondary).clicked()
                && !self.cur_dir.is_empty()
                && !self.in_root(&self.cur_dir)
            {
                go = self
                    .cur_dir
                    .rsplit_once('/')
                    .map(|(p, _)| if p.is_empty() { "/".to_string() } else { p.to_string() });
            }
            ui.label(RichText::new(&self.cur_dir).strong());
        });
        egui::ScrollArea::vertical().id_salt("sync-dirs").max_height(220.0).show(ui, |ui| {
            for d in &self.dirs {
                let name = d.rsplit('/').next().unwrap_or(d);
                if ui.selectable_label(false, format!("📁 {name}")).clicked() {
                    go = Some(d.clone());
                }
            }
        });
        if let Some(p) = go {
            self.browse(cx, p);
        }
        let mut dest_changed = false;
        ui.horizontal(|ui| {
            if widgets::button(ui, &look, "✔", t("Use this folder"), Kind::Secondary).clicked()
                && !self.cur_dir.is_empty()
                && !self.in_root(&self.cur_dir)
            {
                self.dest = self.cur_dir.clone();
                dest_changed = true;
            }
            widgets::field(ui, &mut self.new_name, t("name of the new folder"), 220.0);
            if widgets::button(ui, &look, "📁", t("New folder…"), Kind::Secondary).clicked() {
                let n = self.new_name.trim().trim_matches(['/', '\\']).to_string();
                if !n.is_empty() && !self.cur_dir.is_empty() {
                    self.dest = format!("{}/{n}", self.cur_dir);
                    dest_changed = true;
                }
            }
        });
        widgets::form(ui, "sync-3", |ui| {
            dest_changed |=
                widgets::labeled_field(ui, &look, t("Destination folder"), &mut self.dest, "", 420.0).changed();
            ui.end_row();
        });
        if dest_changed {
            let d = self.dest.trim().to_string();
            self.check_dest(cx, d);
        }
        ui.add(egui::Label::new(RichText::new(&self.dest_status.0).color(look.tone(self.dest_status.1))).wrap());
        let (mut back, mut next) = (false, false);
        let ok = self.dest_ok;
        Self::nav(
            ui,
            &look,
            |ui| back = widgets::button(ui, &look, "", &format!("‹ {}", t("Back")), Kind::Secondary).clicked(),
            |ui| {
                let label = format!("{} ›", t("Next"));
                next =
                    ui.add_enabled_ui(ok, |ui| widgets::button(ui, &look, "", &label, Kind::Primary)).inner.clicked();
            },
        );
        if back {
            self.show(cx, 1);
        }
        if next {
            self.job.dest = self.dest.trim().into();
            self.show(cx, 3);
        }
    }

    fn step4(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        crate::widgets::radio_value(ui, &mut self.form.mirror, false, t("Copy (recommended)"));
        crate::widgets::radio_value(ui, &mut self.form.mirror, true, t("Mirror"));
        let root = self
            .data
            .roots
            .iter()
            .find(|r| self.job.dest.starts_with(&format!("{r}/")))
            .cloned()
            .unwrap_or_else(|| "/srv/shares".into());
        ui.label(
            RichText::new(tf!(
                "Deleted and replaced files go to %s (root only), where they can be given back.",
                format!("{root}/_sync/")
            ))
            .size(11.0)
            .color(look.c("fg-muted")),
        );
        let mut cron_changed = false;
        widgets::form(ui, "sync-4", |ui| {
            widgets::labeled_field(ui, &look, t("Days kept in the quarantine"), &mut self.form.keep_days, "", 100.0);
            ui.end_row();
            widgets::labeled_field(
                ui,
                &look,
                t("GiB kept in the quarantine (0: no limit)"),
                &mut self.form.keep_gib,
                "",
                100.0,
            );
            ui.end_row();
            if self.form.mirror {
                widgets::labeled_field(
                    ui,
                    &look,
                    t("Most files deleted per run"),
                    &mut self.form.max_delete,
                    "",
                    100.0,
                );
                ui.end_row();
                widgets::labeled_field(
                    ui,
                    &look,
                    t("Most deleted, % of the destination"),
                    &mut self.form.max_delete_pct,
                    "",
                    100.0,
                );
                ui.end_row();
            }
            widgets::label(ui, &look, t("When"));
            let mut labels: Vec<String> = PRESETS.iter().map(|p| t(p.1).to_string()).collect();
            labels.push(t("Custom (cron)…").into());
            if widgets::select(ui, "sync-when", t("When"), &mut self.preset, &labels, 240.0)
                && let Some(p) = PRESETS.get(self.preset)
            {
                self.form.cron = p.0.into();
                cron_changed = true;
            }
            ui.end_row();
            if self.preset >= PRESETS.len() {
                cron_changed |= widgets::field(ui, &mut self.form.cron, "30 22 * * 1-5", 200.0).changed();
                ui.end_row();
            }
            widgets::label(ui, &look, t("Name"));
            if widgets::field(ui, &mut self.form.name, "", 360.0).changed() && self.is_new {
                self.form.id = slug(&self.form.name);
            }
            ui.end_row();
        });
        if self.form.mirror {
            ui.label(
                RichText::new(t("A mirror runs by hand only after a preview of this version (saving makes one)."))
                    .size(11.0)
                    .color(look.c("fg-muted")),
            );
        }
        if cron_changed {
            let c = self.form.cron.trim().to_string();
            self.check_cron(cx, c);
        }
        ui.label(RichText::new(&self.next_runs.0).size(11.0).color(look.tone(self.next_runs.1)));
        let who = if self.job.source.auth == "ntlm" {
            if self.job.source.domain.is_empty() {
                self.job.source.user.clone()
            } else {
                format!("{}@{}", self.job.source.user, self.job.source.domain)
            }
        } else {
            t("machine account").into()
        };
        let mut src = format!("{} · {who}", sync_unc(&self.job));
        if let Some((f, b)) = self.src_size {
            src = format!("{src} · {}", tf!("%d files · %s", f, size_text(b)));
        }
        let mut dst = self.job.dest.clone();
        if let Some(free) = self.dest_free {
            dst = format!("{dst} · {}", tf!("%s free", size_text(free as i64)));
        }
        widgets::form(ui, "sync-summary", |ui| {
            widgets::label(ui, &look, t("Source"));
            ui.label(src);
            ui.end_row();
            widgets::label(ui, &look, t("Destination"));
            ui.label(dst);
            ui.end_row();
        });
        egui::CollapsingHeader::new(t("Advanced")).id_salt("sync-advanced").show(ui, |ui| {
            widgets::form(ui, "sync-adv", |ui| {
                widgets::label(ui, &look, t("Leave out (one pattern per line)"));
                ui.add(egui::TextEdit::multiline(&mut self.form.exclude).desired_rows(2).desired_width(300.0));
                ui.end_row();
                widgets::labeled_field(
                    ui,
                    &look,
                    t("Bandwidth limit (e.g. 10M, or 08:00,2M 19:00,off)"),
                    &mut self.form.bwlimit,
                    "",
                    200.0,
                );
                ui.end_row();
                widgets::labeled_field(ui, &look, t("Simultaneous transfers"), &mut self.form.transfers, "", 80.0);
                ui.end_row();
                crate::widgets::check(
                    ui,
                    &mut self.form.verify,
                    t("Compare the content after the run (reads the source again)"),
                );
                ui.end_row();
                crate::widgets::check(
                    ui,
                    &mut self.form.reinherit,
                    t("Apply the destination folder's Windows permissions to what came"),
                );
                ui.end_row();
                widgets::label(ui, &look, t("Id"));
                ui.add_enabled(self.is_new, crate::widgets::line(&mut self.form.id).desired_width(240.0));
                ui.end_row();
            });
        });
        if self.form.mirror {
            ui.label(
                RichText::new(tf!("A mirror deletes in the destination. Type %s to confirm", t("MIRROR")))
                    .size(11.0)
                    .color(look.c("warn")),
            );
            widgets::field(ui, &mut self.form.confirm, "", 200.0);
        }
        let (mut back, mut preview, mut only) = (false, false, false);
        Self::nav(
            ui,
            &look,
            |ui| back = widgets::button(ui, &look, "", &format!("‹ {}", t("Back")), Kind::Secondary).clicked(),
            |ui| {
                preview = widgets::button(ui, &look, "", t("Save and preview"), Kind::Primary).clicked();
                only = widgets::button(ui, &look, "", t("Only save"), Kind::Secondary).clicked();
            },
        );
        if back {
            self.show(cx, 2);
        }
        if preview {
            self.save(cx, true);
        }
        if only {
            self.save(cx, false);
        }
    }
}

/// Follows the wizard's preview every 2 s until it ends (the list of runs, as the web's).
fn poll(cx: &Cx, id: u64) {
    let Some(w) = cx
        .dialogs
        .iter()
        .find(|d| d.0 == id)
        .and_then(|d| (d.1.as_ref() as &dyn std::any::Any).downcast_ref::<SyncWizard>())
    else {
        return;
    };
    let Some(run) = w.run.clone() else { return };
    let (api, path) = (w.api.clone(), format!("{}/syncs", w.base));
    cx.spawn(
        async move {
            tokio::time::sleep(std::time::Duration::from_secs(2)).await;
            api.get::<SyncList>(&path).await
        },
        move |app, r| {
            let Some(w) = wizard(app, id) else { return };
            if let Ok(d) = r
                && let Some(x) = d.runs.into_iter().find(|x| x.id == run.id)
            {
                w.run = Some(x);
            }
            if w.run.as_ref().is_some_and(|x| x.status == "queued" || x.status == "running") {
                poll(&app.cx, id);
            }
        },
    );
}

impl Dialog for SyncWizard {
    fn title(&self) -> String {
        if self.is_new { t("New sync job").into() } else { tf!("Edit %s", self.job.name) }
    }

    fn width(&self) -> f32 {
        760.0
    }

    fn closed(&self, ctx: &egui::Context) {
        crate::widgets::forget_field(ctx, self.pwd_id);
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        if self.done {
            return Flow::Close;
        }
        self.header(ui, cx);
        self.reached = self.reached.max(self.step);
        match self.step {
            0 => self.step1(ui, cx),
            1 => self.step2(ui, cx),
            2 => self.step3(ui, cx),
            3 => self.step4(ui, cx),
            _ => {
                if self.step5(ui, cx) {
                    return Flow::Close;
                }
            }
        }
        Flow::Keep
    }
}
