//! The file servers of the Shares page: their health, the registration dialog (name, agent URL, token, self-signed
//! certificate), the sessions and open files of a server (smbstatus, with disconnect) and, for a domain controller
//! without its file mode, its replication and consistency checks (read only), as the web's.

use adm_core::api::{Api, ApiError};
use adm_core::i18n::t;
use adm_core::logic::Tone;
use adm_core::model::Agent;
use adm_core::tf;
use chrono::{DateTime, Utc};
use egui::{RichText, Ui};
use reqwest::Method;
use serde::Deserialize;
use serde_json::json;
use zeroize::Zeroizing;

use crate::app::Cx;
use crate::modal::{Dialog, Flow, footer};
use crate::style::Look;
use crate::widgets::{self, Kind};

/// What `GET /api/agents/{id}/health` says of a server.
#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Health {
    pub dc: bool,
    pub files: bool,
}

/// A date of the agent (RFC 3339) in the operator's format, as it came when it does not parse.
fn when(s: &str) -> String {
    match DateTime::parse_from_rfc3339(s) {
        Ok(d) => adm_core::i18n::date(Some(d.with_timezone(&Utc))),
        Err(_) => s.to_string(),
    }
}

// ── registration

/// Opens the registration of a new server (`None`) or of `a`.
pub fn edit(cx: &mut Cx, a: Option<Agent>) {
    let a = a.unwrap_or_else(|| Agent { url: "https://".into(), insecure: true, ..Default::default() });
    cx.push(FileServer { a, token: Zeroizing::default(), token_id: widgets::secret_id("agent-token"), busy: false });
}

/// Removes the registration of `a` (typed; nothing is deleted on the server).
pub fn remove(cx: &mut Cx, a: Agent) {
    let msg = tf!("Remove the registration of server %s? Nothing is deleted on the server.", a.name);
    let name = a.name.clone();
    cx.confirm_typed(t("Remove server"), msg, &name, move |cx| {
        let api = cx.panel.clone();
        let path = format!("/api/agents/{}", a.id);
        cx.run(async move { api.act(Method::DELETE, &path).await }, move |app, ()| {
            app.cx.set_status(tf!("Server %s removed.", a.name));
            app.with_shell(|s, cx| s.shares.load(cx));
        });
    });
}

struct FileServer {
    a: Agent,
    token: Zeroizing<String>,
    token_id: egui::Id,
    busy: bool,
}

impl Dialog for FileServer {
    fn title(&self) -> String {
        if self.a.id != 0 { tf!("Server %s", self.a.name) } else { t("New file server").into() }
    }

    fn closed(&self, ctx: &egui::Context) {
        widgets::forget_field(ctx, self.token_id);
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        let l = widgets::lbl(ui, &look, t("Name"));
        ui.add(widgets::line(&mut self.a.name).hint_text("fs01").desired_width(f32::INFINITY)).labelled_by(l.id);
        ui.add_space(12.0);
        let l = widgets::lbl(ui, &look, t("Agent URL"));
        let url = widgets::line(&mut self.a.url)
            .hint_text("https://fs01.example.local:9443")
            .font(egui::TextStyle::Monospace);
        ui.add(url.desired_width(f32::INFINITY)).labelled_by(l.id);
        ui.add_space(12.0);
        let editing = self.a.id != 0;
        let label = if editing { format!("Token {}", t("(empty = keep the current one)")) } else { "Token".into() };
        let l = widgets::lbl(ui, &look, &label);
        widgets::secret_field(ui, self.token_id, &mut self.token, "", f32::INFINITY).labelled_by(l.id);
        ui.horizontal(|ui| {
            ui.spacing_mut().item_spacing.x = 4.0;
            let muted = look.c("fg-muted");
            ui.label(RichText::new(t("Generated on the server with")).size(11.0).color(muted));
            ui.label(RichText::new("sudo ad-manager agent token").monospace().size(11.0).color(muted));
        });
        ui.add_space(12.0);
        crate::widgets::check(ui, &mut self.a.insecure, t("Accept self-signed certificate"));
        let ready = !self.busy
            && !self.a.name.trim().is_empty()
            && self.a.url.trim().len() > "https://".len()
            && (editing || !self.token.is_empty());
        let f = footer(ui, cx, Some(t("Save")), "", false, ready);
        if f.ok && ready {
            self.busy = true;
            // the token straight into a wiped buffer, not a Value kept while the request runs
            let body = adm_core::api::json_with_secret(
                &json!({ "id": self.a.id, "name": self.a.name.trim(), "url": self.a.url.trim(), "insecure": self.a.insecure }),
                "token",
                &self.token,
            );
            let (method, path) = if editing {
                (Method::PUT, format!("/api/agents/{}", self.a.id))
            } else {
                (Method::POST, "/api/agents".into())
            };
            let api = cx.panel.clone();
            let name = self.a.name.trim().to_string();
            cx.run(async move { api.act_bytes(method, &path, &body).await }, move |app, ()| {
                app.cx.set_status(tf!("Server %s saved.", name));
                app.with_shell(|s, cx| s.shares.load(cx));
            });
            return Flow::Close;
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}

// ── sessions and open files

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
struct Session {
    id: String,
    pid: i64,
    user: String,
    machine: String,
    dialect: String,
    encryption: String,
    signing: String,
    since: String,
    shares: Vec<String>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
struct OpenFile {
    path: String,
    pid: i64,
    user: String,
    machine: String,
    access: String,
    opened_at: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
struct SessionList {
    sessions: Vec<Session>,
    open_files: Vec<OpenFile>,
}

/// Opens the sessions and open files of server `name`, read through its agent `api`.
pub fn sessions(cx: &mut Cx, name: String, api: Api) {
    let id = cx.push(Sessions { name, api, list: None, error: None, busy: false });
    load_sessions(cx, id);
}

fn load_sessions(cx: &mut Cx, id: u64) {
    let Some(d) = cx.dialog_mut::<Sessions>(id) else { return };
    d.busy = true;
    let api = d.api.clone();
    cx.spawn(async move { api.get::<SessionList>("/api/sessions").await }, move |app, r: Result<_, ApiError>| {
        if let Some(d) = app.cx.dialog_mut::<Sessions>(id) {
            d.busy = false;
            match r {
                Ok(l) => (d.list, d.error) = (Some(l), None),
                Err(e) => d.error = Some(e.to_string()),
            }
        }
    });
}

struct Sessions {
    name: String,
    api: Api,
    list: Option<SessionList>,
    error: Option<String>,
    busy: bool,
}

impl Sessions {
    fn disconnect(&self, cx: &mut Cx, s: &Session) {
        let Some(l) = &self.list else { return };
        let files = l.open_files.iter().filter(|f| f.pid == s.pid).count();
        let mut msg = tf!(
            "Disconnect %s (%s)? Their open files (%d) are closed and unsaved changes in them are lost.",
            s.user,
            s.machine,
            files
        );
        // the server process may serve other sessions too: they end with it
        let others: Vec<String> = l
            .sessions
            .iter()
            .filter(|x| x.pid == s.pid && x.id != s.id)
            .map(|x| format!("{} ({})", x.user, x.machine))
            .collect();
        if !others.is_empty() {
            msg = format!("{msg} {}", tf!("Also disconnected (same connection): %s.", others.join(", ")));
        }
        let (api, me, body) = (self.api.clone(), cx.current_dialog(), json!({ "pid": s.pid, "session_id": s.id }));
        cx.confirm(t("Disconnect"), msg, t("Disconnect"), true, move |cx| {
            #[derive(Deserialize, Default)]
            #[serde(default)]
            struct Closed {
                closed: Vec<Session>,
            }
            cx.run(
                async move { api.send::<Closed, _>(Method::POST, "/api/sessions/close", Some(&body)).await },
                move |app, r| {
                    let who: Vec<String> = r.closed.into_iter().map(|x| x.user).collect();
                    app.cx.set_status(tf!("%s disconnected.", who.join(", ")));
                    load_sessions(&mut app.cx, me);
                },
            );
        });
    }
}

impl Dialog for Sessions {
    fn title(&self) -> String {
        tf!("Sessions and open files on %s", self.name)
    }

    fn width(&self) -> f32 {
        760.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        let muted = look.c("fg-muted");
        let mut close = None;
        match (&self.list, &self.error) {
            (_, Some(e)) => {
                ui.colored_label(look.c("err"), e);
            }
            (None, None) => {
                ui.label(t("Loading…"));
            }
            (Some(l), None) => {
                let rows: Vec<[RichText; 6]> = l
                    .sessions
                    .iter()
                    .map(|s| {
                        [
                            crate::style::semibold(&s.user, 12.0),
                            RichText::new(&s.machine).monospace().size(11.0),
                            RichText::new(s.shares.join(", ")),
                            RichText::new(&s.dialect),
                            RichText::new(when(&s.since)),
                            RichText::new(""),
                        ]
                    })
                    .collect();
                let mut cell = |ui: &mut Ui, k: usize| {
                    if widgets::act(ui, &look, "", t("Disconnect"), None, 12.0)
                        .on_hover_text(tf!(
                            "Signing: %s · encryption: %s",
                            l.sessions[k].signing,
                            l.sessions[k].encryption
                        ))
                        .clicked()
                    {
                        close = Some(l.sessions[k].clone());
                    }
                };
                let heads = [t("User"), t("Machine"), t("Shares"), t("Protocol"), t("Since"), ""];
                widgets::mini_table(ui, &look, heads, &rows, Some(&mut cell));
                if l.sessions.is_empty() {
                    ui.add_space(8.0);
                    ui.vertical_centered(|ui| ui.label(RichText::new(t("Nobody connected.")).size(12.0).color(muted)));
                }
                ui.add_space(16.0);
                widgets::lbl(ui, &look, t("Open files"));
                ui.add_space(4.0);
                let rows: Vec<[RichText; 5]> = l
                    .open_files
                    .iter()
                    .map(|f| {
                        [
                            RichText::new(&f.path).monospace().size(11.0),
                            RichText::new(&f.user),
                            RichText::new(&f.machine).monospace().size(11.0),
                            RichText::new(&f.access),
                            RichText::new(when(&f.opened_at)),
                        ]
                    })
                    .collect();
                widgets::mini_table(
                    ui,
                    &look,
                    [t("File"), t("User"), t("Machine"), t("Access"), t("Opened at")],
                    &rows,
                    None,
                );
                if l.open_files.is_empty() {
                    ui.add_space(8.0);
                    ui.vertical_centered(|ui| ui.label(RichText::new(t("No open files.")).size(12.0).color(muted)));
                }
                ui.add_space(8.0);
                ui.label(
                    RichText::new(t(
                        "Samba cannot close a single file: disconnecting the user closes all of their files.",
                    ))
                    .size(11.0)
                    .color(muted),
                );
            }
        }
        if let Some(s) = close {
            self.disconnect(cx, &s);
        }
        let label = if self.busy { t("Please wait…") } else { t("Refresh") };
        let f = footer(ui, cx, Some(label), "", false, !self.busy);
        if f.ok && !self.busy {
            load_sessions(cx, cx.current_dialog());
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}

// ── domain controller health

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Link {
    direction: String,
    nc: String,
    dsa: String,
    message: String,
    failures: i64,
    last_success: String,
    deleted: bool,
    ok: bool,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Replication {
    links: Vec<Link>,
    ok: bool,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Check {
    name: String,
    ok: bool,
    objects: i64,
    errors: i64,
    output: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Checks {
    at: Option<DateTime<Utc>>,
    checks: Vec<Check>,
    ok: bool,
}

/// The replication and the database checks of a DC: each loads on its own (a slow dbcheck does not hold the
/// replication back, a failure of one does not hide the other).
#[derive(Default)]
pub struct DcHealth {
    repl: Option<Result<Replication, String>>,
    checks: Option<Result<Checks, String>>,
}

impl DcHealth {
    fn loading(&self) -> bool {
        self.repl.is_none() || self.checks.is_none()
    }
}

/// Loads the health of the DC behind `api` for the screen `key`; a refresh runs the checks again.
pub fn load_dc(cx: &Cx, api: Api, key: u64, refresh: bool, dc: &mut DcHealth) {
    *dc = DcHealth::default();
    let session = cx.panel.id;
    let a = api.clone();
    cx.spawn(async move { a.get::<Replication>("/api/dc/replication").await }, move |app, r| {
        if app.cx.panel.id == session {
            app.with_shell(|s, _| {
                if let Some(d) = s.shares.dc_mut(key) {
                    d.repl = Some(r.map_err(|e| e.to_string()));
                }
            });
        }
    });
    let path = if refresh { "/api/dc/checks?refresh=1" } else { "/api/dc/checks" };
    let api = api.with_timeout(std::time::Duration::from_secs(300));
    cx.spawn(async move { api.get::<Checks>(path).await }, move |app, r| {
        if app.cx.panel.id == session {
            app.with_shell(|s, _| {
                if let Some(d) = s.shares.dc_mut(key) {
                    d.checks = Some(r.map_err(|e| e.to_string()));
                }
            });
        }
    });
}

/// The DC's card (replication links, consistency checks); true when "Check again" was clicked.
pub fn dc_ui(ui: &mut Ui, look: &Look, name: &str, dc: &DcHealth, h: f32) -> bool {
    let mut again = false;
    let busy = dc.loading();
    let right = |ui: &mut Ui| {
        let label = if busy { t("Checking…") } else { t("Check again") };
        if ui
            .add_enabled_ui(!busy, |ui| widgets::button(ui, look, crate::icons::REFRESH, label, Kind::Secondary))
            .inner
            .clicked()
        {
            again = true;
        }
    };
    let title = tf!("Domain controller %s", name);
    widgets::panel(ui, look, (&title, t("read only")), right, h, |ui| {
        egui::Frame::new().inner_margin(egui::Margin::same(12)).show(ui, |ui| {
            ui.spacing_mut().item_spacing.y = 4.0;
            let muted = look.c("fg-muted");
            let state = |ui: &mut Ui, head: String, ok: bool, good: &str, bad: &str| {
                ui.horizontal(|ui| {
                    ui.spacing_mut().item_spacing.x = 0.0;
                    widgets::lbl(ui, look, &format!("{head} · "));
                    let tone = if ok { look.c("ok") } else { look.c("err") };
                    ui.label(crate::style::semibold((if ok { good } else { bad }).to_uppercase(), 11.0).color(tone));
                });
            };
            match &dc.repl {
                Some(Err(e)) => {
                    ui.colored_label(look.c("err"), e);
                }
                None => {
                    ui.label(RichText::new(t("Loading…")).size(12.0).color(muted));
                }
                Some(Ok(r)) => {
                    state(ui, t("Replication").into(), r.ok, t("healthy"), t("with failures"));
                    let rows: Vec<[RichText; 6]> = r
                        .links
                        .iter()
                        .map(|l| {
                            let dsa = if l.deleted { format!("{} ({})", l.dsa, t("deleted")) } else { l.dsa.clone() };
                            let fail = RichText::new(l.failures.to_string());
                            let msg = RichText::new(&l.message);
                            [
                                RichText::new(if l.direction == "inbound" { t("receives") } else { t("sends") }),
                                RichText::new(&l.nc).monospace().size(11.0),
                                RichText::new(dsa),
                                RichText::new(if l.last_success == "NTTIME(0)" { t("never") } else { &l.last_success }),
                                if l.failures > 0 { fail.color(look.c("err")) } else { fail },
                                if l.ok { msg } else { msg.color(look.c("err")) },
                            ]
                        })
                        .collect();
                    let heads = [
                        t("Direction"),
                        t("Partition"),
                        t("Partner DC"),
                        t("Last success"),
                        t("Failures"),
                        t("Last attempt"),
                    ];
                    widgets::mini_table(ui, look, heads, &rows, None);
                    if r.links.is_empty() {
                        ui.add_space(8.0);
                        ui.vertical_centered(|ui| {
                            ui.label(
                                RichText::new(t("No other domain controller replicates with this one."))
                                    .size(12.0)
                                    .color(muted),
                            )
                        });
                    }
                }
            }
            ui.add_space(16.0);
            match &dc.checks {
                Some(Err(e)) => {
                    ui.colored_label(look.c("err"), e);
                }
                None => {
                    ui.label(RichText::new(t("Checking the directory database…")).size(12.0).color(muted));
                }
                Some(Ok(c)) => {
                    let head = format!("{} · {}", t("Consistency"), adm_core::i18n::date(c.at));
                    state(ui, head, c.ok, t("no problems"), t("problems found"));
                    for k in &c.checks {
                        egui::Frame::new()
                            .fill(look.c("surface-3"))
                            .stroke(egui::Stroke::new(1.0, look.c("line")))
                            .inner_margin(egui::Margin::symmetric(12, 8))
                            .show(ui, |ui| {
                                ui.set_width(ui.available_width());
                                ui.horizontal(|ui| {
                                    let name = if k.name == "dbcheck" {
                                        t("Directory database (dbcheck)")
                                    } else {
                                        t("Sysvol permissions (sysvolcheck)")
                                    };
                                    ui.label(crate::style::semibold(name, 12.0));
                                    let (tag, tone) = if k.ok { ("OK", Tone::Ok) } else { (t("problems"), Tone::Err) };
                                    widgets::tag(ui, look, tag, tone);
                                    let n = tf!("%d objects, %d errors", k.objects, k.errors);
                                    ui.label(RichText::new(n).size(12.0).color(muted));
                                });
                                if !k.ok && !k.output.is_empty() {
                                    ui.label(RichText::new(&k.output).monospace().size(11.0));
                                }
                            });
                        ui.add_space(8.0);
                    }
                    ui.label(
                        RichText::new(t("Read only: nothing is fixed here (samba-tool dbcheck without --fix)."))
                            .size(11.0)
                            .color(muted),
                    );
                }
            }
        });
    });
    again
}
