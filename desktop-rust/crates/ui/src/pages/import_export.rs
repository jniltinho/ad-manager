//! Import/export as the web's page: export the current domain's OUs, groups, users and computers as CSV (and a file
//! server's folders and permissions); import CSV files, another AD (objects, GPOs, DNS, shares, passwords; saved
//! profiles, their password kept encrypted) or folders onto a file server. Every import first previews its plan; writing asks
//! once more, and a log dialog follows it. Import only creates and updates: it never deletes nor removes members.

use std::collections::BTreeMap;
use std::path::PathBuf;

use adm_core::i18n::{server_text, t};
use adm_core::model::Agent;
use adm_core::tf;
use egui::{RichText, Ui};
use reqwest::Method;
use serde_json::json;
use zeroize::Zeroizing;

use crate::app::{App, Cx};
use crate::modal::{Dialog, Flow, footer};
use crate::widgets::{self, Kind};

/// One action of a plan, as the panel answers it.
#[derive(Debug, Clone, Default, serde::Deserialize)]
#[serde(default)]
struct Action {
    kind: String,
    target: String,
    detail: String,
    result: String,
    ok: bool,
}

#[derive(Debug, Clone, Default, serde::Deserialize)]
#[serde(default)]
struct PlanOut {
    #[serde(deserialize_with = "adm_core::model::nd")]
    plan: Vec<Action>,
    #[serde(deserialize_with = "adm_core::model::nd")]
    passwords: BTreeMap<String, String>,
    #[serde(deserialize_with = "adm_core::model::nd")]
    counts: BTreeMap<String, i64>,
}

/// A saved source AD (`GET /api/ad-sources`): never its password, only whether one is kept sealed.
#[derive(Debug, Clone, Default, serde::Deserialize)]
#[serde(default)]
struct Profile {
    id: i64,
    name: String,
    url: String,
    domain: String,
    user: String,
    insecure: bool,
    has_password: bool,
}

const EXPORTS: [(&str, &str); 4] =
    [("ous", "OUs"), ("groups", "Groups"), ("users", "Users"), ("computers", "Computers")];
const IMPORTS: [(&str, &str); 4] =
    [("ous", "ous.csv"), ("groups", "groups.csv"), ("users", "users.csv"), ("computers", "computers.csv")];

/// The name of an action kind, as the web's.
fn kind_name(k: &str) -> String {
    let n = match k {
        "ou.create" => "Create OU",
        "group.create" => "Create group",
        "group.update" => "Update group",
        "user.create" => "Create user",
        "user.update" => "Update user",
        "user.disable" => "Disable user",
        "computer.create" => "Create computer",
        "share.create" => "Create share",
        "share.update" => "Update share",
        "gpo.import" => "Import GPO",
        "password" => "Password",
        "dns.add" => "Add DNS record",
        "dns.conflict" => "DNS conflict",
        "zone.missing" => "Missing zone",
        "member.add" => "Add member",
        "skip" => "Skipped",
        _ => return k.to_string(),
    };
    t(n).to_string()
}

#[derive(Default, Clone, Copy, PartialEq)]
enum Mode {
    #[default]
    Csv,
    Ad,
    Folders,
}

/// "From another AD": the source and what it brings.
struct Source {
    url: String,
    domain: String,
    user: String,
    password: Zeroizing<String>,
    pwd_id: egui::Id,
    /// The profile used, when it keeps a password: an empty password then uses it (for that server and user only).
    profile: i64,
    insecure: bool,
    save_as: String,
    objects: bool,
    gpos: bool,
    dns: bool,
    dynamic: bool,
    zones: bool,
    shares: bool,
    passwords: bool,
    share_host: String,
    share_agent: usize,
    share_map: String,
}

impl Default for Source {
    fn default() -> Self {
        Self {
            url: "ldap://".into(),
            domain: String::new(),
            user: String::new(),
            password: Zeroizing::default(),
            pwd_id: widgets::secret_id("import-ad-password"),
            profile: 0,
            insecure: true,
            save_as: String::new(),
            objects: true,
            gpos: false,
            dns: false,
            dynamic: false,
            zones: false,
            shares: false,
            passwords: false,
            share_host: String::new(),
            share_agent: 0,
            share_map: "E:\\=/srv/shares".into(),
        }
    }
}

#[derive(Default)]
pub struct ImportExport {
    servers: Vec<Agent>,
    server: usize,
    mode: Mode,
    /// The CSV files chosen, by field.
    files: BTreeMap<&'static str, PathBuf>,
    folders_file: Option<PathBuf>,
    source: Source,
    profiles: Vec<Profile>,
    plan: Option<Vec<Action>>,
    counts: BTreeMap<String, i64>,
    applied: bool,
    /// The passwords generated for new accounts: shown once, never kept by the server.
    passwords: Option<Zeroizing<Vec<(String, String)>>>,
    busy: bool,
    kind: String,
    /// The log dialog of the import being written.
    log: Option<u64>,
}

impl ImportExport {
    /// Forgets the source password's field (its text and undo history in egui).
    pub fn forget(&mut self, ctx: &egui::Context) {
        crate::widgets::forget_field(ctx, self.source.pwd_id);
    }

    pub fn load(&mut self, cx: &Cx) {
        let api = cx.panel.clone();
        cx.spawn(
            async move {
                let agents = api.get::<Vec<Agent>>("/api/agents").await.unwrap_or_default();
                let profiles = api.get::<Vec<Profile>>("/api/ad-sources").await.unwrap_or_default();
                (agents, profiles)
            },
            |app, (agents, profiles)| {
                app.with_shell(|s, _| {
                    s.import.servers = agents;
                    s.import.profiles = profiles;
                });
            },
        );
    }

    fn page(app: &mut App, f: impl FnOnce(&mut ImportExport, &mut Cx)) {
        app.with_shell(|s, cx| f(&mut s.import, cx));
    }

    fn switch(&mut self, m: Mode) {
        self.mode = m;
        self.plan = None;
        self.applied = false;
        self.passwords = None;
        self.kind.clear();
    }

    /// Reads the plan (or writes it, `apply`) of the mode on screen.
    fn send(&mut self, cx: &mut Cx, apply: bool) {
        self.busy = true;
        // no total timeout: the server bounds each stage itself (GPOs and DNS up to 15 minutes, then the passwords
        // up to 33), as the web's request has none
        let api = cx.panel.without_timeout();
        let mode = self.mode;
        if apply {
            let (title, phase) = match mode {
                Mode::Csv => (t("Import CSV"), t("Writing to the domain…")),
                Mode::Ad => (t("From another AD"), t("Importing the objects…")),
                Mode::Folders => (t("Folders"), t("Sending to the server…")),
            };
            self.log = Some(cx.push(ImportLog { title: title.into(), phase: phase.into(), ..Default::default() }));
        }
        let step = if apply { "apply" } else { "plan" };
        let job: std::pin::Pin<Box<dyn std::future::Future<Output = adm_core::api::ApiResult<PlanOut>> + Send>> =
            match mode {
                Mode::Csv => {
                    let files: Vec<(String, PathBuf)> =
                        self.files.iter().map(|(k, p)| ((*k).to_string(), p.clone())).collect();
                    let path = format!("/api/import/{step}");
                    Box::pin(async move { api.post_files(&path, read_files(files).await?).await })
                }
                Mode::Folders => {
                    let Some(file) = self.folders_file.clone() else { return };
                    let id = self.servers.get(self.server).map_or(0, |a| a.id);
                    let path = format!("/api/agents/{id}/import/{step}");
                    Box::pin(
                        async move { api.post_files(&path, read_files(vec![("shares".into(), file)]).await?).await },
                    )
                }
                Mode::Ad => {
                    let body = self.ad_body();
                    let path = format!("/api/import/ad/{step}");
                    Box::pin(async move {
                        let raw: &serde_json::value::RawValue =
                            serde_json::from_slice(&body).map_err(|e| adm_core::api::ApiError::Other(e.to_string()))?;
                        api.send::<PlanOut, _>(Method::POST, &path, Some(&raw)).await
                    })
                }
            };
        let log = self.log;
        let session = cx.panel.id;
        cx.spawn(job, move |app, r| {
            // a reply of a session replaced meanwhile (signed out) reaches nothing
            if app.cx.panel.id != session {
                return;
            }
            let log_id = log.filter(|_| apply);
            match r {
                Ok(out) => {
                    let steps: Vec<Action> = out.plan.iter().filter(|a| a.kind != "skip").cloned().collect();
                    let errors = if apply { steps.iter().filter(|a| !a.ok).count() } else { 0 };
                    if let Some(id) = log_id
                        && let Some(d) = app.cx.dialog_mut::<ImportLog>(id)
                    {
                        d.done(steps, apply, errors);
                    }
                    if apply {
                        app.cx.set_status(if errors > 0 {
                            tf!("Import finished with %d error(s).", errors)
                        } else if mode == Mode::Folders {
                            t("Shares sent to the server's queue.").into()
                        } else {
                            t("Import finished.").into()
                        });
                    }
                    Self::page(app, |p, _| {
                        p.busy = false;
                        p.plan = Some(out.plan);
                        p.counts = out.counts;
                        p.applied = apply;
                        p.passwords =
                            (!out.passwords.is_empty()).then(|| Zeroizing::new(out.passwords.into_iter().collect()));
                        p.kind.clear();
                    });
                    if mode == Mode::Ad {
                        Self::page(app, |p, cx| p.load(cx)); // a profile saved with it
                    }
                }
                Err(e) => {
                    if let Some(id) = log_id
                        && let Some(d) = app.cx.dialog_mut::<ImportLog>(id)
                    {
                        d.error = Some(e.to_string());
                    }
                    Self::page(app, |p, _| p.busy = false);
                    app.cx.handle_error(e);
                }
            }
        });
    }

    /// The body of "From another AD", the password borrowed into a buffer wiped when dropped.
    fn ad_body(&self) -> Zeroizing<Vec<u8>> {
        let s = &self.source;
        let mut v = json!({
            "url": s.url.trim(), "domain": s.domain.trim(), "user": s.user.trim(), "profile": s.profile, "insecure": s.insecure,
            "save_as": s.save_as.trim(), "objects": s.objects, "gpos": s.gpos, "dns": s.dns,
            "include_dynamic": s.dynamic, "create_zones": s.zones, "passwords": s.passwords,
        });
        if s.shares {
            let agent = self.servers.get(s.share_agent).map_or(0, |a| a.id);
            v["shares"] = json!({ "host": s.share_host.trim(), "agent": agent, "map": s.share_map.trim() });
        }
        adm_core::api::json_with_secret(&v, "password", &s.password)
    }

    fn apply(&mut self, cx: &mut Cx) {
        let pending = self.plan.as_ref().map_or(0, |p| p.iter().filter(|a| a.kind != "skip").count());
        if self.mode == Mode::Folders {
            let name = self.servers.get(self.server).map(|a| a.name.clone()).unwrap_or_default();
            let msg = tf!(
                "Create/update %d share(s) on %s?\nFolders are created empty, with the permissions only.",
                pending,
                name
            );
            cx.confirm(t("Folders"), msg, t("Send"), false, |cx| {
                cx.defer(|app| Self::page(app, |p, cx| p.send(cx, true)))
            });
            return;
        }
        let msg = tf!(
            "Write %d change(s) to the domain?\nNew accounts get a generated password and must change it at first logon.",
            pending
        );
        cx.confirm(t("Import"), msg, t("Apply"), false, |cx| cx.defer(|app| Self::page(app, |p, cx| p.send(cx, true))));
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let h = ui.available_height();
        let mut preview = false;
        let mut apply = false;
        // as the web's: from its lg breakpoint (1024) the left column scrolls alone beside the plan; narrower, the page
        // scrolls as a whole
        if ui.ctx().content_rect().width() >= 1024.0 {
            ui.horizontal_top(|ui| {
                ui.spacing_mut().item_spacing.x = 20.0;
                ui.vertical(|ui| {
                    // the web's 360-point column with its scroll bar inside: the cards 349.6 wide, the plan at 20
                    // points from the column (egui draws its bar beside the content: the content is the 350)
                    ui.set_width(354.5); // egui's bar takes 6 inside and 6.5 beside: cards ≈349, the column ≈361
                    egui::ScrollArea::vertical()
                        .id_salt("import-left")
                        .max_height(h)
                        .auto_shrink([false, true])
                        .scroll_bar_visibility(egui::scroll_area::ScrollBarVisibility::AlwaysVisible)
                        .show(ui, |ui| {
                            // the cards' frames as wide as their content (not over the bar's room)
                            ui.set_width(ui.available_width());
                            self.export_card(ui, cx);
                            ui.add_space(20.0 - ui.spacing().item_spacing.y); // the web's space-y-5
                            preview = self.import_card(ui, cx);
                        });
                });
                ui.vertical(|ui| {
                    ui.set_width(ui.available_width());
                    apply = self.plan_card(ui, cx, h);
                });
            });
        } else {
            egui::ScrollArea::vertical().id_salt("import-page").auto_shrink([false, false]).show(ui, |ui| {
                ui.horizontal_top(|ui| {
                    ui.spacing_mut().item_spacing.x = 20.0;
                    ui.vertical(|ui| {
                        ui.set_width(360.0);
                        self.export_card(ui, cx);
                        ui.add_space(20.0 - ui.spacing().item_spacing.y); // the web's space-y-5
                        preview = self.import_card(ui, cx);
                    });
                    ui.vertical(|ui| {
                        ui.set_width(ui.available_width());
                        apply = self.plan_card(ui, cx, h);
                    });
                });
            });
        }
        if preview {
            self.plan = None;
            self.send(cx, false);
        }
        if apply {
            self.apply(cx);
        }
    }

    fn export_card(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        widgets::panel(
            ui,
            &look,
            (t("Export from the current domain"), ""),
            |_| {},
            0.0,
            |ui| {
                egui::Frame::new().inner_margin(egui::Margin::same(12)).show(ui, |ui| {
                    ui.spacing_mut().item_spacing = egui::vec2(8.0, 8.0);
                    let w = ((ui.available_width() - 8.0) / 2.0).floor();
                    for pair in EXPORTS.chunks(2) {
                        ui.horizontal(|ui| {
                            for (f, label) in pair {
                                let r = widgets::button_end(
                                    ui,
                                    &look,
                                    t(label),
                                    crate::icons::DOWNLOAD,
                                    Kind::Secondary,
                                    w,
                                );
                                if r.on_hover_text(format!("{f}.csv")).clicked() {
                                    let path = format!("/api/export/{f}");
                                    crate::files::save_download(
                                        cx,
                                        cx.panel.clone(),
                                        path,
                                        format!("{f}.csv"),
                                        |app, p| {
                                            app.cx.set_status(tf!("Exported to %s", p.display()));
                                        },
                                    );
                                }
                            }
                        });
                    }
                    if !self.servers.is_empty() {
                        // the web's border-t pt-2 over the server row, its gap-1 between the select and the button
                        let y = ui.cursor().top();
                        ui.painter().hline(ui.max_rect().x_range(), y + 0.5, egui::Stroke::new(1.0, look.c("line")));
                        ui.add_space(9.0); // pt-2 under the 1 px line (egui puts its spacing after an item, not before)
                        ui.horizontal(|ui| {
                            ui.spacing_mut().item_spacing.x = 4.0;
                            let names: Vec<String> = self.servers.iter().map(|a| a.name.clone()).collect();
                            widgets::select(
                                ui,
                                "export-server",
                                t("File server"),
                                &mut self.server,
                                &names,
                                ui.available_width() - 105.0, // the select takes the flex share, the button its 101
                            );
                            let r = widgets::button_end(
                                ui,
                                &look,
                                &format!("{}  {}", crate::icons::FOLDER_OPEN, t("Folders")),
                                crate::icons::DOWNLOAD,
                                Kind::Secondary,
                                101.0, // the web's natural width of the button
                            );
                            if r.on_hover_text(t("Folders and permissions (no files)")).clicked()
                                && let Some(a) = self.servers.get(self.server)
                            {
                                let path = format!("/api/agents/{}/export", a.id);
                                crate::files::save_download(
                                    cx,
                                    cx.panel.clone(),
                                    path,
                                    "shares.csv".into(),
                                    |app, p| {
                                        app.cx.set_status(tf!("Exported to %s", p.display()));
                                    },
                                );
                            }
                        });
                    }
                    let muted = look.c("fg-muted");
                    let mut job = egui::text::LayoutJob::default();
                    let mut text = egui::TextFormat::simple(egui::FontId::proportional(12.0), muted);
                    text.line_height = Some(widgets::LEADING * 12.0);
                    // the web's <code> at 1em of the text around it
                    let mut code = egui::TextFormat::simple(egui::FontId::monospace(12.0), muted);
                    code.line_height = text.line_height;
                    for (s, f) in [
                        (format!("{} ", t("No passwords or files. From the CLI:")), &text),
                        (format!("ad-manager export -d {}", t("folder")), &code),
                        (format!(" {} ", t("(also from the 2008 AD) and")), &text),
                        ("ad-manager migrate shares --csv".to_string(), &code),
                        (format!(" {}", t("(folders and permissions of a Windows server).")), &text),
                    ] {
                        job.append(&s, 0.0, f.clone());
                    }
                    job.wrap.max_width = ui.available_width();
                    ui.label(job);
                });
            },
        );
    }

    /// The import card with its three tabs; true when Preview was clicked.
    fn import_card(&mut self, ui: &mut Ui, cx: &mut Cx) -> bool {
        let look = cx.look.clone();
        let muted = look.c("fg-muted");
        let mut preview = false;
        widgets::panel(
            ui,
            &look,
            ("", ""),
            |_| {},
            0.0,
            |ui| {
                // the web's .cardtabs across the card (not the dialog's: no body padding here)
                let tabs = [t("Import CSV"), t("From another AD"), t("Folders")];
                let sel = match self.mode {
                    Mode::Csv => 0,
                    Mode::Ad => 1,
                    Mode::Folders => 2,
                };
                if let Some(i) = widgets::cardtabs_at(ui, &look, "import-tabs", &tabs, sel, 0.0, 0.0, false) {
                    self.switch([Mode::Csv, Mode::Ad, Mode::Folders][i]);
                }
                egui::Frame::new().inner_margin(egui::Margin::same(12)).show(ui, |ui| {
                ui.set_width(ui.available_width());
                ui.spacing_mut().item_spacing.y = 4.0;
                let busy = self.busy;
                let preview_label = |busy: bool, ad: bool| {
                    if busy {
                        if ad { t("Reading the source AD…") } else { t("Analyzing…") }
                    } else if ad {
                        t("Connect and preview")
                    } else {
                        t("Preview")
                    }
                };
                match self.mode {
                    Mode::Csv => {
                        for (f, name) in IMPORTS {
                            widgets::lbl(ui, &look, name);
                            let chosen = self.files.get(f).and_then(|p| p.file_name()).map(|n| n.to_string_lossy().into_owned());
                            ui.horizontal(|ui| {
                                ui.spacing_mut().item_spacing.x = 8.0;
                                if widgets::button(ui, &look, crate::icons::FOLDER_OPEN, t("Choose a file…"), Kind::Secondary).clicked() {
                                    crate::files::pick_file_of(cx, Some(("CSV", &["csv"])), move |app, p| {
                                        Self::page(app, |s, _| {
                                            s.files.insert(f, p);
                                            s.plan = None;
                                            s.applied = false;
                                        });
                                    });
                                }
                                match chosen {
                                    Some(n) => {
                                        ui.label(RichText::new(n).size(12.0));
                                        if widgets::act(ui, &look, "", "×", None, 12.0).on_hover_text(t("Remove")).clicked() {
                                            self.files.remove(f);
                                        }
                                    }
                                    None => {
                                        ui.label(RichText::new(t("No file chosen")).size(12.0).color(muted));
                                    }
                                }
                            });
                            // the web's space-y-3 (egui's 4 already after the row); before Preview, full_button's 4 too
                            ui.add_space(if f == IMPORTS[IMPORTS.len() - 1].0 { 4.0 } else { 8.0 });
                        }
                        let ready = !busy && !self.files.is_empty();
                        preview = full_button(ui, &look, ready, crate::icons::EYE, preview_label(busy, false));
                        note(ui, &look, t("Only creates and updates — never deletes objects nor removes members. Key: login / group name / OU path."));
                    }
                    Mode::Folders => {
                        if self.servers.is_empty() {
                            note(ui, &look, t("Register a file server under Shares."));
                        } else {
                            widgets::lbl(ui, &look, t("Target server"));
                            let names: Vec<String> = self.servers.iter().map(|a| a.name.clone()).collect();
                            if widgets::select(ui, "import-target", t("Target server"), &mut self.server, &names, ui.available_width() - 8.0) {
                                self.plan = None;
                            }
                            ui.add_space(8.0);
                            widgets::lbl(ui, &look, "shares.csv");
                            let chosen = self.folders_file.as_ref().and_then(|p| p.file_name()).map(|n| n.to_string_lossy().into_owned());
                            ui.horizontal(|ui| {
                                ui.spacing_mut().item_spacing.x = 8.0;
                                if widgets::button(ui, &look, crate::icons::FOLDER_OPEN, t("Choose a file…"), Kind::Secondary).clicked() {
                                    crate::files::pick_file_of(cx, Some(("CSV", &["csv"])), |app, p| {
                                        Self::page(app, |s, _| {
                                            s.folders_file = Some(p);
                                            s.plan = None;
                                        });
                                    });
                                }
                                ui.label(RichText::new(chosen.unwrap_or_else(|| t("No file chosen").into())).size(12.0).color(muted));
                            });
                            ui.add_space(8.0);
                            let ready = !busy && self.folders_file.is_some();
                            preview = full_button(ui, &look, ready, crate::icons::EYE, preview_label(busy, false));
                            note(ui, &look, t("Structure and permissions only: each share becomes an empty folder with the ACLs of the groups and users, published on Samba by the agent. It does not copy files and never removes shares."));
                        }
                    }
                    Mode::Ad => {
                        preview = self.ad_form(ui, cx);
                    }
                }
            });
            },
        );
        preview
    }

    /// The chosen profile keeps a password and the form still names its server and user (as the server checks).
    fn kept_password(&self) -> bool {
        let s = &self.source;
        self.profiles.iter().any(|p| {
            p.id == s.profile
                && p.has_password
                && p.url == s.url.trim()
                && p.domain.eq_ignore_ascii_case(s.domain.trim())
                && p.user.eq_ignore_ascii_case(s.user.trim())
        })
    }

    /// "From another AD"; true when Connect and preview was clicked.
    fn ad_form(&mut self, ui: &mut Ui, cx: &mut Cx) -> bool {
        let look = cx.look.clone();
        let w = ui.available_width();
        let mut use_profile = None;
        let mut drop_profile = None;
        if !self.profiles.is_empty() {
            widgets::lbl(ui, &look, t("Saved profiles"));
            ui.horizontal_wrapped(|ui| {
                ui.spacing_mut().item_spacing = egui::vec2(4.0, 4.0);
                for p in &self.profiles {
                    egui::Frame::new()
                        .fill(look.c("surface-3"))
                        .stroke(egui::Stroke::new(1.0, look.c("line")))
                        .inner_margin(egui::Margin::symmetric(8, 2))
                        .show(ui, |ui| {
                            ui.spacing_mut().item_spacing.x = 4.0;
                            if widgets::act(ui, &look, "", &p.name, Some(look.c("fg-strong")), 11.0).clicked() {
                                use_profile = Some(p.clone());
                            }
                            let r = widgets::act(ui, &look, crate::icons::TRASH, "", Some(look.c("fg-muted")), 11.0);
                            if widgets::named(ui, r, &tf!("Delete the profile %s?", p.name)).clicked() {
                                drop_profile = Some(p.clone());
                            }
                        });
                }
            });
            ui.add_space(8.0);
        }
        let kept = self.kept_password();
        let s = &mut self.source;
        widgets::above(ui, &look, t("Source DC URL"), w, |ui, _| {
            ui.add(widgets::line(&mut s.url).font(egui::TextStyle::Monospace).desired_width(w))
        });
        ui.add_space(8.0);
        let cw = ((w - 8.0) / 2.0).floor();
        ui.horizontal_top(|ui| {
            ui.spacing_mut().item_spacing.x = 8.0;
            widgets::above(ui, &look, t("Domain"), cw, |ui, _| ui.add(widgets::line(&mut s.domain).desired_width(cw)));
            widgets::above(ui, &look, t("User"), cw, |ui, _| ui.add(widgets::line(&mut s.user).desired_width(cw)));
        });
        ui.add_space(8.0);
        let (pwd_id, pwd) = (s.pwd_id, &mut s.password);
        widgets::above(ui, &look, t("Password"), w, |ui, _| {
            widgets::secret_field(ui, pwd_id, pwd, if kept { t("Kept password (encrypted)") } else { "" }, w)
        });
        ui.add_space(4.0);
        crate::widgets::check(ui, &mut s.insecure, RichText::new(t("Accept self-signed certificate")).size(12.0));
        ui.add_space(8.0);
        let y = ui.cursor().top();
        ui.painter().hline(ui.max_rect().x_range(), y + 0.5, egui::Stroke::new(1.0, look.c("line")));
        ui.add_space(6.0);
        widgets::lbl(ui, &look, t("Import"));
        ui.horizontal_wrapped(|ui| {
            ui.spacing_mut().item_spacing.x = 12.0;
            crate::widgets::check(ui, &mut s.objects, RichText::new(t("Objects")).size(12.0));
            crate::widgets::check(ui, &mut s.gpos, RichText::new("GPOs").size(12.0));
            crate::widgets::check(ui, &mut s.dns, RichText::new("DNS").size(12.0));
            crate::widgets::check(ui, &mut s.shares, RichText::new(t("Shares")).size(12.0));
            crate::widgets::check(ui, &mut s.passwords, RichText::new(t("Passwords")).size(12.0));
        });
        if s.passwords {
            note(
                ui,
                &look,
                t(
                    "New-domain cutover only: the DC agent reads the source password hashes over a read-only DRSUAPI and sets them on matching users, using the login and password entered above. Needs the agent password-migration mode on; the source is left unchanged.",
                ),
            );
        }
        if s.gpos {
            note(
                ui,
                &look,
                t(
                    "New GPOs, not linked, created by the agent of a DC in its GPO restore mode; names already in use are skipped.",
                ),
            );
        }
        if s.dns {
            crate::widgets::check(
                ui,
                &mut s.dynamic,
                RichText::new(t("Also records registered by clients (dynamic)")).size(12.0),
            );
            crate::widgets::check(
                ui,
                &mut s.zones,
                RichText::new(t("Create the zones missing on this DC (primary, integrated with the directory)"))
                    .size(12.0),
            );
            note(ui, &look, t("Only A, CNAME and PTR; never records of domain controllers, SOA, NS or SRV."));
        }
        if s.shares {
            ui.add_space(4.0);
            ui.horizontal_top(|ui| {
                ui.spacing_mut().item_spacing.x = 8.0;
                widgets::above(ui, &look, t("Source server"), cw, |ui, _| {
                    ui.add(
                        widgets::line(&mut s.share_host)
                            .hint_text(crate::widgets::hint("fs01.example.local").monospace())
                            .font(egui::TextStyle::Monospace)
                            .desired_width(cw),
                    )
                });
                ui.vertical(|ui| {
                    ui.set_width(cw);
                    let names: Vec<String> = self.servers.iter().map(|a| a.name.clone()).collect();
                    widgets::labeled_select(
                        ui,
                        &look,
                        t("Target server"),
                        "import-share-agent",
                        &mut s.share_agent,
                        &names,
                        cw - 8.0,
                    );
                });
            });
            ui.add_space(4.0);
            widgets::above(ui, &look, t("Path map (Windows=Linux)"), w, |ui, _| {
                ui.add(widgets::line(&mut s.share_map).font(egui::TextStyle::Monospace).desired_width(w))
            });
        }
        ui.add_space(8.0);
        widgets::above(ui, &look, t("Save as profile (the password is kept encrypted)"), w, |ui, _| {
            ui.add(
                widgets::line(&mut s.save_as).hint_text(crate::widgets::hint(t("e.g. legacy-2008"))).desired_width(w),
            )
        });
        ui.add_space(8.0);
        let some = s.objects || s.gpos || s.dns || s.shares || s.passwords;
        let ready = !self.busy && some;
        let label = if self.busy { t("Reading the source AD…") } else { t("Connect and preview") };
        let go = full_button(ui, &look, ready, crate::icons::PLUG, label);
        let mut job = egui::text::LayoutJob::default();
        let text = egui::TextFormat::simple(egui::FontId::proportional(11.0), look.c("fg-muted"));
        let code = egui::TextFormat::simple(egui::FontId::monospace(11.0), look.c("fg-strong"));
        for (st, f) in [
            (format!("{} (", t("Read-only on the source")), &text),
            ("ldap://".to_string(), &code),
            (format!(" {} {}", t("uses NTLM)."), t("Writes to the current domain only after confirmation.")), &text),
        ] {
            job.append(&st, 0.0, f.clone());
        }
        job.wrap.max_width = w;
        ui.label(job);
        if let Some(p) = use_profile {
            let s = &mut self.source;
            s.url = p.url;
            s.domain = p.domain;
            s.user = p.user;
            s.insecure = p.insecure;
            s.save_as = p.name;
            s.profile = if p.has_password { p.id } else { 0 };
            s.password = Zeroizing::default();
        }
        if let Some(p) = drop_profile {
            cx.confirm(t("Saved profiles"), tf!("Delete the profile %s?", p.name), t("Delete"), true, move |cx| {
                let api = cx.panel.clone();
                let path = format!("/api/ad-sources/{}", p.id);
                cx.run(async move { api.act(Method::DELETE, &path).await }, move |app, ()| {
                    Self::page(app, |s, _| s.profiles.retain(|x| x.id != p.id));
                });
            });
        }
        go
    }

    /// The plan (or the result); true when Write was clicked.
    fn plan_card(&mut self, ui: &mut Ui, cx: &mut Cx, h: f32) -> bool {
        let look = cx.look.clone();
        let muted = look.c("fg-muted");
        let title = if self.applied { t("Result") } else { t("Plan preview") };
        let mut note_txt = String::new();
        if let Some(p) = &self.plan {
            let pending = p.iter().filter(|a| a.kind != "skip").count();
            note_txt = tf!("%d actions, %d change(s)", p.len(), pending);
            if self.mode == Mode::Ad && !self.counts.is_empty() {
                let parts: Vec<String> = [
                    ("users", "%d users"),
                    ("groups", "%d groups"),
                    ("ous", "%d OUs"),
                    ("computers", "%d computers"),
                    ("gpos", "%d GPOs"),
                    ("shares", "%d shares"),
                    ("dns", "%d DNS records"),
                ]
                .iter()
                .filter_map(|(k, l)| self.counts.get(*k).map(|n| tf!(l, n)))
                .collect();
                note_txt = format!("{note_txt} · {} {}", t("source:"), parts.join(", "));
            }
        }
        let mut apply = false;
        let mut download = false;
        widgets::panel(
            ui,
            &look,
            (title, &note_txt),
            |_| {},
            h,
            |ui| {
                let Some(plan) = &self.plan else {
                    ui.add_space(24.0);
                    ui.vertical_centered(|ui| {
                        if self.busy {
                            let msg =
                                if self.mode == Mode::Ad { t("Reading the source AD…") } else { t("Analyzing…") };
                            ui.label(RichText::new(msg).size(13.0).color(muted));
                            ui.add_space(8.0);
                            indeterminate(ui, &look, 160.0);
                        } else {
                            ui.label(RichText::new(t("Choose the files and click preview.")).size(11.0).color(muted));
                        }
                    });
                    return;
                };
                // the kinds as filters, and the actions
                let mut counts: BTreeMap<String, usize> = BTreeMap::new();
                for a in plan {
                    *counts.entry(a.kind.clone()).or_default() += 1;
                }
                egui::Frame::new().inner_margin(egui::Margin::same(12)).show(ui, |ui| {
                    ui.set_width(ui.available_width());
                    ui.horizontal_wrapped(|ui| {
                        ui.spacing_mut().item_spacing = egui::vec2(8.0, 6.0);
                        for (k, n) in &counts {
                            let on = self.kind == *k;
                            let (fill, ink) = if on {
                                (look.c("selection"), look.c("on-selection"))
                            } else {
                                (look.c("surface-3"), look.c("fg-strong"))
                            };
                            let r = egui::Frame::new()
                                .fill(fill)
                                .stroke(egui::Stroke::new(1.0, look.c("line")))
                                .inner_margin(egui::Margin::symmetric(
                                    look.px("tag-px", 6.0) as i8,
                                    look.px("tag-py", 2.0) as i8,
                                ))
                                .show(ui, |ui| {
                                    ui.add(
                                        egui::Label::new(
                                            crate::style::semibold(format!("{}: {n}", kind_name(k)), 11.0).color(ink),
                                        )
                                        .selectable(false),
                                    )
                                })
                                .response
                                .interact(egui::Sense::click())
                                .on_hover_cursor(egui::CursorIcon::PointingHand);
                            if r.clicked() {
                                self.kind = if on { String::new() } else { k.clone() };
                            }
                        }
                        ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                            ui.spacing_mut().item_spacing.x = 8.0;
                            if !self.applied {
                                let pending = plan.iter().any(|a| a.kind != "skip");
                                let label = if self.busy {
                                    t("Saving…")
                                } else if self.mode == Mode::Folders {
                                    t("Send to the server")
                                } else {
                                    t("Write to the domain")
                                };
                                apply = ui
                                    .add_enabled_ui(!self.busy && pending, |ui| {
                                        widgets::button(ui, &look, crate::icons::CHECK, label, Kind::Primary)
                                    })
                                    .inner
                                    .clicked();
                            }
                            if let Some(pw) = &self.passwords {
                                download = widgets::button(
                                    ui,
                                    &look,
                                    crate::icons::KEY,
                                    &tf!("generated passwords (%d)", pw.len()),
                                    Kind::Secondary,
                                )
                                .clicked();
                            }
                        });
                    });
                });
                let shown: Vec<&Action> = plan.iter().filter(|a| self.kind.is_empty() || a.kind == self.kind).collect();
                let subtle = look.c("fg-subtle");
                let rows4: Vec<[RichText; 4]> = shown
                    .iter()
                    .map(|a| {
                        let skip = a.kind == "skip";
                        let c = |r: RichText| if skip { r.color(subtle) } else { r };
                        let res = RichText::new(server_text(&a.result)).color(if a.ok || skip {
                            look.c("ok")
                        } else {
                            look.c("err")
                        });
                        [
                            c(RichText::new(kind_name(&a.kind))),
                            c(crate::style::semibold(&a.target, 12.0)),
                            c(RichText::new(server_text(&a.detail))),
                            res,
                        ]
                    })
                    .collect();
                let room = (ui.available_height() - 40.0).max(120.0);
                egui::ScrollArea::vertical().id_salt("import-plan").max_height(room).show(ui, |ui| {
                    if self.applied {
                        widgets::mini_table(
                            ui,
                            &look,
                            [t("Action"), t("Target"), t("Detail"), t("Result")],
                            &rows4,
                            None,
                        );
                    } else {
                        let rows3: Vec<[RichText; 3]> = rows4.into_iter().map(|[a, b, c, _]| [a, b, c]).collect();
                        widgets::mini_table(ui, &look, [t("Action"), t("Target"), t("Detail")], &rows3, None);
                    }
                });
                if self.passwords.is_some() {
                    egui::Frame::new().fill(look.c("warn-soft")).inner_margin(egui::Margin::symmetric(12, 8)).show(ui, |ui| {
                    ui.set_width(ui.available_width());
                    let msg = t("Download the generated passwords now: they are not kept on the server. Each user changes the password at first logon.");
                    ui.add(egui::Label::new(RichText::new(msg).size(12.0).color(look.c("on-warn-soft"))).wrap());
                });
                }
            },
        );
        if download && let Some(pw) = &self.passwords {
            let mut rows = vec![vec!["login".to_string(), t("initial_password").to_string()]];
            rows.extend(pw.iter().map(|(l, p)| vec![l.clone(), p.clone()]));
            crate::files::save_csv(cx, t("generated-passwords.csv"), rows);
        }
        apply
    }
}

/// Reads the chosen files for a multipart body (field, file name, bytes).
async fn read_files(files: Vec<(String, PathBuf)>) -> adm_core::api::ApiResult<Vec<(String, String, Vec<u8>)>> {
    let mut out = Vec::new();
    for (field, p) in files {
        let bytes =
            tokio::fs::read(&p).await.map_err(|e| adm_core::api::ApiError::Other(format!("{}: {e}", p.display())))?;
        let name = p.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default();
        out.push((field, name, bytes));
    }
    Ok(out)
}

/// A button across the card's width (the web's `btn w-full justify-center`); true when clicked.
fn full_button(ui: &mut Ui, look: &crate::style::Look, enabled: bool, icon: &str, label: &str) -> bool {
    ui.add_space(4.0);
    let w = ui.available_width();
    ui.add_enabled_ui(enabled, |ui| {
        ui.allocate_ui_with_layout(
            egui::vec2(w, crate::style::control_h()),
            egui::Layout::centered_and_justified(egui::Direction::LeftToRight),
            |ui| widgets::button(ui, look, icon, label, Kind::Primary),
        )
        .inner
    })
    .inner
    .clicked()
}

fn note(ui: &mut Ui, look: &crate::style::Look, s: &str) {
    ui.add_space(4.0);
    ui.add(egui::Label::new(RichText::new(s).size(11.0).color(look.c("fg-muted"))).wrap());
}

/// An indeterminate progress bar (a segment sliding across its track), as the web's pm-bar.
fn indeterminate(ui: &mut Ui, look: &crate::style::Look, w: f32) {
    let (r, _) = ui.allocate_exact_size(egui::vec2(w, 4.0), egui::Sense::hover());
    ui.painter().rect_filled(r, 0.0, look.c("surface-3"));
    let t = ui.input(|i| i.time) as f32;
    let phase = (t / 1.1).fract();
    let seg = r.width() * 0.4;
    let x = r.left() - seg + phase * (r.width() + seg);
    let bar = egui::Rect::from_min_max(
        egui::pos2(x.max(r.left()), r.top()),
        egui::pos2((x + seg).min(r.right()), r.bottom()),
    );
    ui.painter().rect_filled(bar, 0.0, look.c("warn"));
    ui.ctx().request_repaint();
}

/// The log of an import being written: its phase with a moving bar, then each action's result and the status.
#[derive(Default)]
struct ImportLog {
    title: String,
    phase: String,
    steps: Option<Vec<Action>>,
    applied: bool,
    errors: usize,
    error: Option<String>,
}

impl ImportLog {
    fn done(&mut self, steps: Vec<Action>, applied: bool, errors: usize) {
        self.steps = Some(steps);
        self.applied = applied;
        self.errors = errors;
    }
}

impl Dialog for ImportLog {
    fn title(&self) -> String {
        format!("{} — {}", t("Import"), self.title)
    }

    fn width(&self) -> f32 {
        680.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        let running = self.steps.is_none() && self.error.is_none();
        if running {
            ui.label(RichText::new(&self.phase).size(13.0).color(look.c("fg-muted")));
            ui.add_space(6.0);
            indeterminate(ui, &look, ui.available_width());
        } else if let Some(e) = &self.error {
            ui.colored_label(look.c("err"), e);
        } else if self.errors > 0 {
            ui.colored_label(look.c("err"), tf!("Finished with %d error(s).", self.errors));
        } else {
            let n = self.steps.as_ref().map_or(0, Vec::len);
            ui.colored_label(look.c("ok"), tf!("Finished: %d action(s).", n));
        }
        if let Some(steps) = &self.steps
            && !steps.is_empty()
        {
            ui.add_space(8.0);
            egui::Frame::new().stroke(egui::Stroke::new(1.0, look.c("line"))).show(ui, |ui| {
                egui::ScrollArea::vertical().max_height(ui.ctx().content_rect().height() * 0.46).show(ui, |ui| {
                    let rows: Vec<[RichText; 4]> =
                        steps
                            .iter()
                            .map(|a| {
                                let ok = !self.applied || a.ok;
                                let detail = if a.kind == "password" || !self.applied { &a.detail } else { &a.result };
                                [
                                    RichText::new(if ok { crate::icons::CHECK } else { "✖" })
                                        .monospace()
                                        .color(if ok { look.c("ok") } else { look.c("err") }),
                                    RichText::new(kind_name(&a.kind)),
                                    RichText::new(&a.target).monospace().size(11.0),
                                    RichText::new(server_text(detail)).color(look.c("fg-muted")),
                                ]
                            })
                            .collect();
                    widgets::mini_table(ui, &look, ["", "", "", ""], &rows, None);
                });
            });
        }
        // closes only once it ended (the web's Close is disabled while it runs)
        let f = footer(ui, cx, None, t("Close"), false, true);
        if f.ok && !running { Flow::Close } else { Flow::Keep }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn ad_body_carries_the_password_last() {
        let mut p = ImportExport::default();
        p.source.domain = "old.local".into();
        *p.source.password = "S3cr\"et".into();
        let v: serde_json::Value = serde_json::from_slice(&p.ad_body()).unwrap();
        assert_eq!(v["password"], "S3cr\"et");
        assert_eq!(v["domain"], "old.local");
        assert!(v.get("shares").is_none());
        assert_eq!(kind_name("user.create"), "Create user");
    }
}
