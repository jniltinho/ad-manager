//! Shares as the web's page: at the left the file servers (health, DC tag, edit and remove the registration, + new)
//! and the tasks of this session; at the right the shares of the chosen server with folder sizes and their total,
//! sessions, refresh, new share, and per-row edit and remove (typed; the files stay) — or, for a domain controller
//! without its file mode, its replication and consistency checks. Tasks are followed until they end; switching
//! servers drops the list and any late answer.

use std::collections::HashMap;
use std::time::Duration;

use adm_core::api::{Api, ApiError, esc};
use adm_core::forms::ShareForm;
use adm_core::i18n::{server_text, t};
use adm_core::logic::{Tone, human_bytes};
use adm_core::model::{Agent, Share, Task};
use adm_core::tf;
use egui::{RichText, Ui};
use reqwest::Method;
use serde_json::{Value, json};

use super::file_servers::{self, DcHealth, Health};
use crate::app::{App, Cx};
use crate::modal::{Dialog, Flow, footer};
use crate::table::{self, Cell, Column, TableState};
use crate::widgets::{self, Kind};

/// A task queued from this page, followed until it ends.
pub struct TaskItem {
    id: String,
    label: String,
    state: String,
    error: String,
}

#[derive(Default)]
pub struct Shares {
    agents: Option<Vec<Agent>>,
    /// The health of each server: online, a DC, its file mode (`None` until it answers, `Err` when down).
    health: HashMap<i64, Result<Health, ()>>,
    /// The server on screen: its agent, the client through the panel and a key bumped on every switch.
    server: Option<(Agent, Api)>,
    key: u64,
    shares: Vec<Share>,
    loading: bool,
    sizes: Option<HashMap<String, i64>>,
    measuring: bool,
    table: TableState,
    pub tasks: Vec<TaskItem>,
    dc: DcHealth,
}

/// What a click on the page asks for, done after drawing.
enum Act {
    Pick(usize),
    NewServer,
    EditServer(Agent),
    RemoveServer(Agent),
    Sessions,
    Refresh,
    NewShare,
    Edit(usize),
    Remove(usize),
    CheckDc,
}

impl Shares {
    pub fn load(&mut self, cx: &Cx) {
        let api = cx.panel.clone();
        cx.run(async move { api.get::<Vec<Agent>>("/api/agents").await }, |app, agents| {
            app.with_shell(|s, cx| s.shares.agents_loaded(cx, agents));
        });
    }

    fn agents_loaded(&mut self, cx: &Cx, agents: Vec<Agent>) {
        self.health.clear();
        for a in &agents {
            let api = cx.panel.via(a.id);
            let (id, session) = (a.id, cx.panel.id);
            cx.spawn(async move { api.get::<Health>("/api/health").await }, move |app, r| {
                if app.cx.panel.id != session {
                    return;
                }
                app.with_shell(|s, cx| {
                    let p = &mut s.shares;
                    p.health.insert(id, r.map_err(|_| ()));
                    // a DC without its file mode shows its health instead of shares
                    if p.server.as_ref().is_some_and(|(a, _)| a.id == id) {
                        if p.is_dc() {
                            p.load_dc(cx, false);
                        } else if p.shares.is_empty() && !p.loading {
                            p.reload(cx, false);
                        }
                    }
                });
            });
        }
        let first = self.server.as_ref().and_then(|(c, _)| agents.iter().position(|a| a.id == c.id)).unwrap_or(0);
        self.agents = Some(agents);
        self.server = None;
        self.switch(cx, first);
    }

    /// The server on screen is a domain controller without its file mode.
    fn is_dc(&self) -> bool {
        self.server.as_ref().is_some_and(|(a, _)| matches!(self.health.get(&a.id), Some(Ok(h)) if h.dc && !h.files))
    }

    /// Shows server `i`: the list of the previous one goes now.
    fn switch(&mut self, cx: &Cx, i: usize) {
        let Some(a) = self.agents.as_ref().and_then(|a| a.get(i)).cloned() else { return };
        self.key += 1;
        self.shares.clear();
        self.sizes = None;
        self.table.clear();
        self.dc = DcHealth::default();
        self.server = Some((a.clone(), cx.panel.via(a.id)));
        if self.is_dc() {
            self.load_dc(cx, false);
        } else {
            self.reload(cx, false);
        }
    }

    fn load_dc(&mut self, cx: &Cx, refresh: bool) {
        let Some((_, api)) = self.server.clone() else { return };
        file_servers::load_dc(cx, api, self.key, refresh, &mut self.dc);
    }

    pub fn dc_mut(&mut self, key: u64) -> Option<&mut DcHealth> {
        (self.key == key).then_some(&mut self.dc)
    }

    pub fn reload(&mut self, cx: &Cx, refresh_sizes: bool) {
        let Some((a, api)) = self.server.clone() else { return };
        if self.is_dc() {
            return;
        }
        let key = self.key;
        let session = cx.panel.id;
        let list = api.clone();
        self.loading = true;
        cx.spawn(async move { list.get::<Vec<Share>>("/api/shares").await }, move |app, r| {
            if app.cx.panel.id != session {
                return; // another session now
            }
            app.with_shell(|s, cx| {
                let p = &mut s.shares;
                if p.key != key {
                    return; // another server now
                }
                p.loading = false;
                match r {
                    Err(ApiError::SessionExpired) => cx.handle_error(ApiError::SessionExpired),
                    Err(e) => {
                        p.health.insert(a.id, Err(()));
                        cx.set_status(format!("{}: {e}", a.name));
                    }
                    Ok(r) => {
                        cx.set_status(tf!("%s: %d share(s)", a.name, r.len()));
                        p.shares = r;
                        p.table.clear();
                        p.load_sizes(cx, api, refresh_sizes);
                    }
                }
            });
        });
    }

    /// The folder sizes (du, cached 5 minutes by the agent; refresh forces a new measure).
    fn load_sizes(&mut self, cx: &Cx, api: Api, refresh: bool) {
        self.measuring = true;
        let key = self.key;
        let path = if refresh { "/api/sizes?refresh=1" } else { "/api/sizes" };
        #[derive(serde::Deserialize, Default)]
        struct Sizes {
            #[serde(default)]
            sizes: HashMap<String, i64>,
        }
        // du of large folders: the panel waits 2 minutes
        let api = api.with_timeout(Duration::from_secs(150));
        let session = cx.panel.id;
        cx.spawn(async move { api.get::<Sizes>(path).await }, move |app, r| {
            if app.cx.panel.id != session {
                return;
            }
            if r.as_ref().is_err_and(|e| *e == ApiError::SessionExpired) {
                app.cx.handle_error(ApiError::SessionExpired);
                return;
            }
            app.with_shell(|s, _| {
                if s.shares.key == key {
                    s.shares.measuring = false;
                    s.shares.sizes = r.ok().map(|x| x.sizes);
                }
            });
        });
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        let h = ui.available_height();
        let mut act = None;
        // shortcut: always side by side (the web stacks below 1280 px), the window's minimum still fits both
        ui.horizontal_top(|ui| {
            ui.spacing_mut().item_spacing.x = 20.0;
            ui.vertical(|ui| {
                ui.set_width(300.0);
                self.aside(ui, cx, h, &mut act);
            });
            ui.vertical(|ui| {
                ui.set_width(ui.available_width());
                if self.is_dc() {
                    let name = self.server.as_ref().map(|(a, _)| a.name.clone()).unwrap_or_default();
                    if file_servers::dc_ui(ui, &look, &name, &self.dc, h) {
                        act = Some(Act::CheckDc);
                    }
                } else {
                    self.main(ui, cx, h, keys, &mut act);
                }
            });
        });
        if let Some(a) = act {
            self.act(cx, a);
        }
    }

    fn aside(&mut self, ui: &mut Ui, cx: &Cx, h: f32, act: &mut Option<Act>) {
        let look = cx.look.clone();
        let top = ui.cursor().top();
        let mut add = false;
        let new = |ui: &mut Ui| add = widgets::act(ui, &look, "+", t("new"), None, 12.0).clicked();
        let mut picked = None;
        widgets::panel(ui, &look, (t("File servers"), ""), new, 0.0, |ui| match &self.agents {
            None => {
                ui.add_space(12.0);
                ui.horizontal(|ui| {
                    ui.add_space(12.0);
                    ui.label(RichText::new(t("Loading…")).size(12.0).color(look.c("fg-muted")));
                });
                ui.add_space(12.0);
            }
            Some(agents) if agents.is_empty() => {
                ui.add_space(12.0);
                ui.horizontal_wrapped(|ui| {
                    ui.add_space(12.0);
                    ui.label(
                        RichText::new(t(
                            "No servers. Install ad-manager agent on the file server and register it here.",
                        ))
                        .size(12.0)
                        .color(look.c("fg-muted")),
                    );
                });
                ui.add_space(12.0);
            }
            Some(agents) => {
                let cur = self.server.as_ref().map(|(a, _)| a.id);
                for (i, a) in agents.iter().enumerate() {
                    let on = cur == Some(a.id);
                    let health = self.health.get(&a.id);
                    let r = widgets::side_item(ui, &look, on, i == 0, |ui, ink| {
                        let (dot, tip) = match health {
                            Some(Ok(_)) => (look.c("ok-fill"), t("online")),
                            Some(Err(())) => (look.c("err-fill"), t("unavailable")),
                            None => (look.c("neutral-fill"), ""),
                        };
                        let (r, _) = ui.allocate_exact_size(egui::vec2(8.0, 8.0), egui::Sense::hover());
                        ui.painter().rect_filled(r, 0.0, dot);
                        if !tip.is_empty() {
                            ui.interact(r, ui.id().with(("dot", a.id)), egui::Sense::hover()).on_hover_text(tip);
                        }
                        ui.label(crate::style::semibold(&a.name, 12.0).color(ink));
                        ui.add(egui::Label::new(RichText::new(a.host()).size(11.0).color(ink)).truncate());
                        if matches!(health, Some(Ok(h)) if h.dc) {
                            widgets::tag(ui, &look, "DC", Tone::Normal);
                        }
                        let link = on.then_some(ink);
                        ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                            ui.spacing_mut().item_spacing.x = 4.0;
                            if widgets::act(ui, &look, "", t("remove"), link, 11.0).clicked() {
                                return Some(Act::RemoveServer(a.clone()));
                            }
                            if widgets::act(ui, &look, "", t("edit"), link, 11.0).clicked() {
                                return Some(Act::EditServer(a.clone()));
                            }
                            None
                        })
                        .inner
                    });
                    match r.inner {
                        Some(x) => *act = Some(x),
                        None if r.response.clicked() && !on => picked = Some(i),
                        None => {}
                    }
                }
            }
        });
        if let Some(i) = picked {
            *act = Some(Act::Pick(i));
        }
        if add {
            *act = Some(Act::NewServer);
        }
        ui.add_space(20.0);
        let left = h - (ui.cursor().top() - top);
        let queue = |ui: &mut Ui| {
            if cx.me.asynqmon && widgets::act(ui, &look, "", &format!("{} ↗", t("queue")), None, 12.0).clicked() {
                let base = cx.cfg.panel.url.trim_end_matches('/').to_string();
                ui.ctx().open_url(egui::OpenUrl::new_tab(format!("{base}/asynqmon")));
            }
        };
        widgets::panel(ui, &look, (t("Tasks of this session"), ""), queue, left, |ui| {
            egui::ScrollArea::vertical().id_salt("share-tasks").auto_shrink([false, true]).show(ui, |ui| {
                if self.tasks.is_empty() {
                    ui.add_space(8.0);
                    ui.horizontal(|ui| {
                        ui.add_space(12.0);
                        ui.label(RichText::new(t("None.")).size(12.0).color(look.c("fg-muted")));
                    });
                }
                for (i, task) in self.tasks.iter().enumerate() {
                    if i > 0 {
                        let y = ui.cursor().top();
                        ui.painter().hline(ui.max_rect().x_range(), y + 0.5, egui::Stroke::new(1.0, look.c("line")));
                    }
                    egui::Frame::new().inner_margin(egui::Margin::symmetric(12, 6)).show(ui, |ui| {
                        ui.horizontal(|ui| {
                            ui.add(egui::Label::new(RichText::new(&task.label).size(12.0)).truncate())
                                .on_hover_text(&task.label);
                            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                                let (name, tone) = match task.state.as_str() {
                                    "completed" => (t("done"), Tone::Ok),
                                    "archived" => (t("failed"), Tone::Err),
                                    "active" => (t("running"), Tone::Strong),
                                    "retry" => (t("retrying"), Tone::Strong),
                                    "scheduled" => (t("scheduled"), Tone::Strong),
                                    _ => (t("queued"), Tone::Strong),
                                };
                                widgets::tag(ui, &look, name, tone);
                            });
                        });
                        if !task.error.is_empty() {
                            ui.label(RichText::new(&task.error).size(11.0).color(look.c("err")));
                        }
                    });
                }
            });
        });
    }

    fn main(&mut self, ui: &mut Ui, cx: &Cx, h: f32, keys: bool, act: &mut Option<Act>) {
        let look = cx.look.clone();
        let (title, note) = match &self.server {
            Some((a, _)) => (tf!("Shares on %s", a.name), a.url.clone()),
            None => (t("Shares").to_string(), String::new()),
        };
        let down = self.server.as_ref().is_some_and(|(a, _)| matches!(self.health.get(&a.id), Some(Err(()))));
        let on = self.server.is_some();
        let total: i64 = self.sizes.as_ref().map_or(0, |s| s.values().sum());
        let total = if self.measuring { tf!("total %s", "…") } else { tf!("total %s", human_bytes(total)) };
        let has = !self.shares.is_empty();
        let mut head = None;
        let right = |ui: &mut Ui| {
            if !on {
                return;
            }
            ui.spacing_mut().item_spacing.x = 8.0;
            if ui
                .add_enabled_ui(!down, |ui| widgets::button(ui, &look, "📁", t("New share"), Kind::Primary))
                .inner
                .clicked()
            {
                head = Some(Act::NewShare);
            }
            if widgets::button(ui, &look, "⟳", t("Refresh"), Kind::Secondary).clicked() {
                head = Some(Act::Refresh);
            }
            if ui
                .add_enabled_ui(!down, |ui| widgets::button(ui, &look, "👥", t("Sessions"), Kind::Secondary))
                .inner
                .clicked()
            {
                head = Some(Act::Sessions);
            }
            if has {
                ui.label(RichText::new(&total).size(12.0).color(look.c("fg-muted")))
                    .on_hover_text(t("Sum of the folders (nested ones count again)"));
            }
        };
        widgets::panel(ui, &look, (&title, &note), right, h, |ui| {
            let w = ui.available_width();
            let cols = [
                Column::new(t("Name"), (w * 0.16).floor()),
                Column::new(t("Path"), (w * 0.19).floor()),
                Column::new(t("Size"), 80.0).right(),
                Column::new(t("Write access"), (w * 0.20).floor()),
                Column::new(t("Read access"), (w * 0.14).floor()),
                Column::new(t("Comment"), 120.0).fill(),
                Column::new(t("Actions"), 144.0).right(),
            ];
            let mut hover = None;
            let (shares, sizes, measuring) = (&self.shares, &self.sizes, self.measuring);
            let out = table::Table { id: "shares", cols: &cols, rows: shares.len(), keys, row_h: 0.0, frame: false }
                .show(ui, &look, &mut self.table, &mut hover, |r, c| {
                    let s = &shares[r];
                    match c {
                        0 if !s.browseable => Cell::plain(&s.name).bold().with_tag(t("hidden"), Tone::Normal),
                        0 => Cell::plain(&s.name).bold(),
                        1 => Cell::plain(&s.path).mono(),
                        2 => Cell::plain(size_text(sizes.as_ref(), measuring, &s.name)),
                        3 => Cell::plain(access(s, true)),
                        4 => Cell::plain(access(s, false)),
                        5 => Cell::new(&s.comment, Tone::Muted),
                        _ => Cell::plain("")
                            .and_link(t("edit"), Tone::Normal, "✏", tf!("Edit %s", s.name), (true, r))
                            .and_link(t("remove"), Tone::Normal, "🗑", tf!("Remove %s", s.name), (false, r)),
                    }
                });
            match out.clicked {
                Some((true, r)) if !down => *act = Some(Act::Edit(r)),
                Some((false, r)) => *act = Some(Act::Remove(r)),
                _ => {}
            }
            let empty = if !on && self.agents.is_some() {
                Some(t("Choose a server."))
            } else if self.loading || self.agents.is_none() {
                Some(t("Loading…"))
            } else if on && self.shares.is_empty() {
                Some(t("No shares published by this server."))
            } else {
                None
            };
            if let Some(e) = empty {
                ui.add_space(16.0);
                ui.vertical_centered(|ui| ui.label(RichText::new(e).size(12.0).color(look.c("fg-muted"))));
            }
        });
        if head.is_some() {
            *act = head;
        }
    }

    fn act(&mut self, cx: &mut Cx, a: Act) {
        match a {
            Act::Pick(i) => self.switch(cx, i),
            Act::NewServer => file_servers::edit(cx, None),
            Act::EditServer(a) => file_servers::edit(cx, Some(a)),
            Act::RemoveServer(a) => file_servers::remove(cx, a),
            Act::Sessions => {
                if let Some((a, api)) = self.server.clone() {
                    file_servers::sessions(cx, a.name, api);
                }
            }
            Act::Refresh => self.reload(cx, true),
            Act::CheckDc => self.load_dc(cx, true),
            Act::NewShare => self.form(cx, None),
            Act::Edit(r) => {
                if let Some(s) = self.shares.get(r).cloned() {
                    self.form(cx, Some(s));
                }
            }
            Act::Remove(r) => {
                if let (Some(s), Some((a, api))) = (self.shares.get(r).cloned(), self.server.clone()) {
                    self.remove(cx, s, a, api);
                }
            }
        }
    }

    fn form(&mut self, cx: &mut Cx, s: Option<Share>) {
        let Some((a, api)) = self.server.clone() else { return };
        let form = match &s {
            Some(s) => ShareForm::edit(s),
            None => ShareForm::new_share(),
        };
        cx.push(ShareDialog { form, server: a.name, api, key: self.key, netbios: cx.me.netbios.clone() });
    }

    fn remove(&mut self, cx: &mut Cx, s: Share, a: Agent, api: Api) {
        let key = self.key; // the action stays bound to the server on screen
        let unc = format!(r"\\{}\{}", a.name, s.name);
        let msg = tf!("Unpublish %s? The folder and its files stay on the server.", unc);
        let name = s.name.clone();
        cx.confirm_typed(t("Remove share"), msg, &name, move |cx| {
            cx.defer(move |app| {
                app.with_shell(|sh, cx| {
                    if sh.shares.key != key {
                        cx.show_error_text(t("The list changed servers; refresh and try again."));
                        return;
                    }
                    enqueue(cx, api, key, "share:delete", json!({ "name": s.name }), tf!("Remove %s", unc));
                });
            });
        });
    }
}

/// The groups and users with write access (full control marked) or read-only access, as the web's.
fn access(s: &Share, write: bool) -> String {
    let mut v: Vec<String> = Vec::new();
    if write {
        let full = s.full_groups.iter().chain(&s.full_users);
        v.extend(full.map(|n| format!("{n} ({})", t("Full control"))));
        v.extend(s.rw_groups.iter().chain(&s.rw_users).cloned());
    } else {
        v.extend(s.ro_groups.iter().chain(&s.ro_users).cloned());
    }
    v.join(", ")
}

/// A share's size: measured, being measured ("…") or unknown ("—").
fn size_text(sizes: Option<&HashMap<String, i64>>, measuring: bool, name: &str) -> String {
    match sizes.and_then(|s| s.get(name)) {
        Some(n) => human_bytes(*n),
        None if measuring => "…".into(),
        None => "—".into(),
    }
}

/// Submits a task to the agent and polls it until it ends: every second for 40 s, then every 5 s (24 h at most); a
/// task waits for the ones queued before it, and each may take up to the agent's 30-minute limit.
pub fn enqueue(cx: &Cx, api: Api, key: u64, kind: &str, payload: Value, label: String) {
    let body = json!({ "type": kind, "payload": payload });
    let post = api.clone();
    let l = label.clone();
    cx.run(async move { post.send::<Task, _>(Method::POST, "/api/tasks", Some(&body)).await }, move |app, tk| {
        app.cx.set_status(format!("{l}: {}", t("queued")));
        let id = tk.id.clone();
        app.with_shell(|s, _| {
            s.shares.tasks.insert(
                0,
                TaskItem { id: id.clone(), label: l.clone(), state: tk.state.clone(), error: String::new() },
            );
        });
        let path = format!("/api/tasks/{}", esc(&tk.id));
        let session = app.cx.panel.id;
        app.cx.spawn(
            async move {
                for i in 0..(40 + 17280) {
                    tokio::time::sleep(Duration::from_secs(if i < 40 { 1 } else { 5 })).await;
                    match api.get::<Task>(&path).await {
                        Err(ApiError::SessionExpired) => return Err(ApiError::SessionExpired),
                        Err(_) => continue,
                        Ok(st) if st.state == "completed" || st.state == "archived" => return Ok(st),
                        Ok(_) => {}
                    }
                }
                Err(ApiError::Other(format!("{label}: {}", t("timed out"))))
            },
            move |app: &mut App, r| match r {
                // a poll of a session that was replaced (signed out, another panel) says nothing to the new one
                _ if app.cx.panel.id != session => {}
                // signed out or the panel restarted: only this session's screen hears it
                Err(e) => app.cx.handle_error(e),
                Ok(st) => {
                    app.with_shell(|s, _| {
                        if let Some(x) = s.shares.tasks.iter_mut().find(|x| x.id == id) {
                            x.state = st.state.clone();
                            x.error = server_text(&st.last_error);
                        }
                    });
                    if st.state == "archived" {
                        app.cx.show_error_text(format!("{l}: {}", server_text(&st.last_error)));
                    } else {
                        app.cx.set_status(format!("{l}: {}", t("done")));
                    }
                    app.with_shell(|s, cx| {
                        if s.shares.key == key {
                            s.shares.reload(cx, false);
                        }
                    });
                }
            },
        );
    });
}

/// The new/edit share form.
struct ShareDialog {
    form: ShareForm,
    server: String,
    api: Api,
    key: u64,
    netbios: String,
}

impl Dialog for ShareDialog {
    fn title(&self) -> String {
        if self.form.editing {
            tf!("Edit share %s on %s", self.form.name, self.server)
        } else {
            tf!("New share on %s", self.server)
        }
    }

    fn width(&self) -> f32 {
        720.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        let groups = format!("{}\\{}, …", self.netbios, t("group"));
        let users = format!("{}\\{}, …", self.netbios, t("user"));
        let f = &mut self.form;
        widgets::form(ui, "share-form", |ui| {
            widgets::label(ui, &look, t("Share name"));
            if ui.add_enabled(!f.editing, crate::widgets::line(&mut f.name).desired_width(420.0)).changed() {
                f.name_changed();
            }
            ui.end_row();
            widgets::label(ui, &look, t("Folder"));
            ui.add_enabled(!f.editing, crate::widgets::line(&mut f.folder).desired_width(420.0));
            ui.end_row();
            for (label, v, hint) in [
                (t("Comment"), &mut f.comment, ""),
                (t("Groups with full control"), &mut f.full, groups.as_str()),
                (t("Groups with write access"), &mut f.rw, groups.as_str()),
                (t("Read-only groups"), &mut f.ro, groups.as_str()),
                (t("Users with full control"), &mut f.full_users, users.as_str()),
                (t("Users with write access"), &mut f.rw_users, users.as_str()),
                (t("Read-only users"), &mut f.ro_users, users.as_str()),
            ] {
                widgets::labeled_field(ui, &look, label, v, hint, 420.0);
                ui.end_row();
            }
        });
        ui.checkbox(&mut f.hidden, t("Hidden on the network (not shown when browsing)"));
        ui.checkbox(&mut f.recycle, t("Recycle bin (deleted files go to .recycle, per user)"));
        ui.checkbox(&mut f.abe, t("Hide what the user cannot open (access-based enumeration)"));
        ui.checkbox(&mut f.audit, t("Audit file changes (renamed and deleted files, new folders → syslog)"));
        if f.editing {
            widgets::wrapped(
                ui,
                t(
                    "Saving republishes the share and reapplies the folder permissions, also on the files and subfolders that inherit them; their content is not changed.",
                ),
            );
        } else {
            ui.checkbox(
                &mut f.create_path,
                t("Create the folder (owner group = 1st with write access, inherited ACL)"),
            );
        }
        let button = if f.editing { t("Save") } else { t("Create") };
        let foot = footer(ui, cx, Some(button), "", false, f.ready());
        if foot.ok && f.ready() {
            let unc = f.unc(&self.server);
            let label = if f.editing { tf!("Edit %s", unc) } else { tf!("Create %s", unc) };
            enqueue(cx, self.api.clone(), self.key, "share:create", f.payload(), label);
            return Flow::Close;
        }
        if foot.cancel { Flow::Close } else { Flow::Keep }
    }
}
