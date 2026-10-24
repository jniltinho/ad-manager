//! Shares of a file server, through the panel's proxy: the server picker with its health, the table with folder
//! sizes and their total, new / edit (the same `share:create` task) and remove (typed; the files stay). Tasks are
//! polled until they end; switching servers drops the list, the selection and any late answer.

use std::collections::HashMap;
use std::time::Duration;

use adm_core::api::{Api, ApiError, esc};
use adm_core::forms::ShareForm;
use adm_core::i18n::{join_list, server_text, t};
use adm_core::logic::{Tone, human_bytes};
use adm_core::model::{Agent, Share, Task};
use adm_core::tf;
use egui::{RichText, Ui};
use reqwest::Method;
use serde_json::{Value, json};

use crate::app::{App, Cx};
use crate::modal::{Dialog, Flow, footer};
use crate::table::{self, Cell, Column, TableState};
use crate::widgets::{self, Kind};

#[derive(Default)]
pub struct Shares {
    agents: Option<Vec<Agent>>,
    pick: usize,
    /// The server on screen: its agent, the client through the panel and a key bumped on every switch.
    server: Option<(Agent, Api)>,
    key: u64,
    shares: Vec<Share>,
    sizes: Option<HashMap<String, i64>>,
    measuring: bool,
    health: Option<(String, Tone)>,
    table: TableState,
}

impl Shares {
    pub fn load(&mut self, cx: &Cx) {
        let api = cx.panel.clone();
        cx.run(async move { api.get::<Vec<Agent>>("/api/agents").await }, |app, agents| {
            app.with_shell(|s, cx| {
                let p = &mut s.shares;
                p.agents = Some(agents);
                if p.agents.as_ref().is_some_and(|a| !a.is_empty()) {
                    p.switch(cx, 0);
                } else {
                    p.health = Some((
                        t("No file server registered in the panel (web panel: Shares → File servers → + new).").into(),
                        Tone::Muted,
                    ));
                }
            });
        });
    }

    /// Shows server `i`: the list and selection of the previous one go now.
    fn switch(&mut self, cx: &Cx, i: usize) {
        let Some(a) = self.agents.as_ref().and_then(|a| a.get(i)).cloned() else { return };
        self.pick = i;
        self.key += 1;
        self.shares.clear();
        self.sizes = None;
        self.table.clear();
        self.server = Some((a.clone(), cx.panel.via(a.id)));
        self.reload(cx, false);
    }

    pub fn reload(&mut self, cx: &Cx, refresh_sizes: bool) {
        let Some((a, api)) = self.server.clone() else { return };
        let key = self.key;
        let session = cx.panel.id;
        let list = api.clone();
        cx.spawn(async move { list.get::<Vec<Share>>("/api/shares").await }, move |app, r| {
            if app.cx.panel.id != session {
                return; // another session now
            }
            app.with_shell(|s, cx| {
                let p = &mut s.shares;
                if p.key != key {
                    return; // another server now
                }
                match r {
                    Err(ApiError::SessionExpired) => cx.handle_error(ApiError::SessionExpired),
                    Err(e) => {
                        p.health = Some((format!("● {}", t("unavailable")), Tone::Err));
                        cx.set_status(format!("{}: {e}", a.name));
                    }
                    Ok(r) => {
                        p.health = Some((format!("● {}", t("online")), Tone::Ok));
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
        widgets::card(ui, &look, t("Shares"), |ui| {
            ui.horizontal(|ui| {
                ui.label(RichText::new(format!("🗄 {}", t("Server"))).strong().color(look.c("fg-strong")));
                let labels: Vec<String> = self.agents.iter().flatten().map(Agent::label).collect();
                let mut i = self.pick;
                if widgets::select(ui, "shares-server", &mut i, &labels, 360.0) {
                    self.switch(cx, i);
                }
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    if widgets::button(ui, &look, "⟳", t("Refresh"), Kind::Secondary).clicked() {
                        self.reload(cx, true);
                    }
                    let total: i64 = self.sizes.as_ref().map_or(0, |s| s.values().sum());
                    let tot = if self.measuring { tf!("total %s", "…") } else { tf!("total %s", human_bytes(total)) };
                    ui.label(RichText::new(tot).color(look.c("fg-muted")));
                    if let Some((h, tone)) = &self.health {
                        ui.add(egui::Label::new(RichText::new(h).strong().color(look.tone(*tone))).wrap());
                    }
                });
            });
            let sel = self.table.sel.and_then(|i| self.shares.get(i)).cloned();
            ui.horizontal(|ui| {
                let on = self.server.is_some();
                if ui
                    .add_enabled_ui(on, |ui| widgets::button(ui, &look, "📁", t("New share"), Kind::Primary))
                    .inner
                    .clicked()
                {
                    self.form(cx, None);
                }
                if widgets::button(ui, &look, "✏", t("Edit"), Kind::Secondary).clicked() {
                    match sel.clone() {
                        Some(s) if on => self.form(cx, Some(s)),
                        _ => cx
                            .show_info(t("Choose a share"), t("Select a share in the table before using this action.")),
                    }
                }
                if widgets::button(ui, &look, "🗑", t("Remove (keeps the files)"), Kind::Danger).clicked() {
                    match (sel.clone(), self.server.clone()) {
                        (Some(s), Some((a, api))) => self.remove(cx, s, a, api),
                        _ => cx
                            .show_info(t("Choose a share"), t("Select a share in the table before using this action.")),
                    }
                }
            });
            let cols = [
                Column::new(t("Name"), 150.0),
                Column::new(t("Path"), 200.0),
                Column::new(t("Size"), 80.0),
                Column::new(t("Write access"), 200.0),
                Column::new(t("Read access"), 190.0),
                Column::new(t("Comment"), 140.0),
            ];
            let mut hover = None;
            let (shares, sizes, measuring) = (&self.shares, &self.sizes, self.measuring);
            table::Table { id: "shares", cols: &cols, rows: shares.len(), keys, row_h: 0.0 }.show::<()>(
                ui,
                &look,
                &mut self.table,
                &mut hover,
                |r, c| {
                    let s = &shares[r];
                    match c {
                        0 if !s.browseable => Cell::new(format!("{}  ({})", s.name, t("hidden")), Tone::Strong),
                        0 => Cell::new(&s.name, Tone::Strong),
                        1 => Cell::plain(&s.path),
                        2 => Cell::plain(size_text(sizes.as_ref(), measuring, &s.name)),
                        3 => Cell::plain(join_list(&[s.rw_groups.clone(), s.rw_users.clone()].concat())),
                        4 => Cell::plain(join_list(&[s.ro_groups.clone(), s.ro_users.clone()].concat())),
                        _ => Cell::new(&s.comment, Tone::Muted),
                    }
                },
            );
        });
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
