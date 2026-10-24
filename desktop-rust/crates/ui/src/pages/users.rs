//! Users: search, filter and count, the lean toolbar (Password policy, Export CSV, New user, refresh — behind ⋯ when
//! narrow) and the table whose rows act in place, as the web panel: the login opens the edit form, an editable
//! account's status toggles it, unlock beside a locked one, and the password, copy, move and delete columns. Every
//! action takes the account of the row it was clicked on (a copy taken at the click), never the selected row; Enter on
//! the selected row opens a menu of its actions.

use adm_core::api::{Api, esc};
use adm_core::forms::{USER_FIELDS, UserFields, template_groups};
use adm_core::i18n::{date, t};
use adm_core::logic::{
    self, Tone, UserAction, UserFilter, editable, inactive, ou_name, parent_dn, user_actions, user_status,
};
use adm_core::model::{Group, OrgUnit, User, is_set};
use adm_core::tf;
use chrono::{DateTime, Utc};
use egui::{RichText, Ui};
use reqwest::Method;
use serde_json::json;
use zeroize::Zeroizing;

use super::OuPicker;
use crate::app::{App, Cx};
use crate::modal::{Dialog, Flow, footer};
use crate::table::{self, Cell, Column, TableState};
use crate::widgets::{self, Kind};

const C_LOGIN: usize = 0;
const C_STATUS: usize = 4;
const C_TAG: usize = 5;
const C_PASSWORD: usize = 7;
const C_COPY: usize = 8;
const C_MOVE: usize = 9;
const C_DELETE: usize = 10;
const COMPACT: f32 = 34.0;

#[derive(Default)]
pub struct Users {
    all: Vec<User>,
    visible: Vec<usize>,
    search: String,
    filter: UserFilter,
    table: TableState,
    loaded: bool,
    generation: u64,
    /// The Enter menu: its account (a copy) and where it opens.
    menu: Option<(User, egui::Pos2)>,
    /// The action links showed only their icons in the last frame.
    compact: bool,
}

impl Users {
    pub fn load(&mut self, cx: &Cx) {
        self.reload(cx);
    }

    pub fn reload(&mut self, cx: &Cx) {
        self.generation += 1;
        let g = self.generation;
        let api = cx.panel.clone();
        cx.run(async move { api.get::<Vec<User>>("/api/users").await }, move |app, mut r| {
            app.with_shell(|s, cx| {
                if s.users.generation != g {
                    return;
                }
                r.sort_by_key(|u| u.sam.to_lowercase());
                s.users.all = r;
                s.users.loaded = true;
                s.users.apply(cx);
            });
        });
    }

    /// Shows the accounts of one status (Home's tiles and alerts).
    pub fn show_filter(&mut self, cx: &Cx, f: UserFilter) {
        self.filter = f;
        self.search.clear();
        if self.loaded {
            self.apply(cx);
        }
    }

    /// Shows the accounts matching the header's global search.
    pub fn search_for(&mut self, cx: &Cx, term: &str) {
        self.search = term.to_string();
        if self.loaded {
            self.apply(cx);
        }
    }

    /// Filters the list again (search, filter) and forgets the selection.
    fn apply(&mut self, cx: &Cx) {
        self.visible = logic::filter_users(&self.all, self.filter, &self.search, cx.inactive_days(), Utc::now());
        self.table.clear();
        self.menu = None;
    }

    /// Whether the action links show only their icons (tests).
    pub fn compact(&self) -> bool {
        self.compact
    }

    /// The selected row (tests).
    pub fn selected(&self) -> Option<usize> {
        self.table.sel
    }

    /// The accounts the table shows (tests).
    pub fn visible(&self) -> impl Iterator<Item = &User> {
        self.visible.iter().filter_map(|&i| self.all.get(i))
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        let days = cx.inactive_days();
        {
            // as the web's Users: the toolbar, then the table, no card around them
            self.toolbar(ui, cx, days);
            ui.add_space(look.px("toolbar-mb", 12.0));
            let now = Utc::now();
            let me = cx.me.clone();
            let cols = columns();
            let mut hover = None;
            let (all, visible) = (&self.all, &self.visible);
            let out = table::Table {
                id: "users",
                cols: &cols,
                rows: visible.len(),
                keys: keys && self.menu.is_none(),
                row_h: 29.0,
                frame: true,
            }
            .show(ui, &look, &mut self.table, &mut hover, |r, c| cell(&all[visible[r]], visible[r], c, &me, days, now));
            cx.hover = hover.or(cx.hover.take());
            self.compact = out.compact;
            if let Some((a, i)) = out.clicked
                && let Some(u) = self.all.get(i)
            {
                run(a, u.clone(), cx);
            }
            if let Some(r) = out.enter
                && let Some(&i) = self.visible.get(r)
            {
                let pos = ui.min_rect().min + egui::vec2(24.0, 70.0);
                self.menu = self.all.get(i).cloned().map(|u| (u, pos));
            }
        }
        self.menu_ui(ui, cx);
    }

    fn toolbar(&mut self, ui: &mut Ui, cx: &mut Cx, days: i64) {
        let look = cx.look.clone();
        let wide = ui.available_width() >= 1000.0;
        ui.horizontal(|ui| {
            ui.spacing_mut().item_spacing.x = look.px("toolbar-gap", 8.0);
            if super::search(ui, &mut self.search, t("Filter by login, name, e-mail…"), 256.0).changed() {
                self.apply(cx);
            }
            let labels: Vec<String> = UserFilter::ALL.iter().map(|f| f.label(days)).collect();
            let mut i = UserFilter::ALL.iter().position(|f| *f == self.filter).unwrap_or(0);
            if widgets::select(ui, "users-filter", &mut i, &labels, 176.0) {
                self.filter = UserFilter::ALL[i];
                self.apply(cx);
            }
            ui.label(
                RichText::new(tf!("%d of %d", self.visible.len(), self.all.len())).size(12.0).color(look.c("fg-muted")),
            );
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                if wide {
                    if widgets::icon_button(ui, &look, "⟳", t("Refresh")).clicked() {
                        self.reload(cx);
                    }
                    if widgets::button(ui, &look, "➕", t("New user"), Kind::Primary).clicked() {
                        new_user(cx, None);
                    }
                    if widgets::button(ui, &look, "⬇", t("Export CSV"), Kind::Secondary).clicked() {
                        crate::files::export(cx, "users", t("users.csv"));
                    }
                    if widgets::button(ui, &look, "⚙", t("Password policy"), Kind::Secondary).clicked() {
                        super::policy::open(cx);
                    }
                } else {
                    ui.menu_button("…", |ui| {
                        if ui.button(t("New user")).clicked() {
                            new_user(cx, None);
                        }
                        if ui.button(t("Password policy")).clicked() {
                            super::policy::open(cx);
                        }
                        if ui.button(t("Export CSV")).clicked() {
                            crate::files::export(cx, "users", t("users.csv"));
                        }
                        if ui.button(t("Refresh")).clicked() {
                            self.reload(cx);
                        }
                    });
                }
            });
        });
    }

    /// The Enter menu of the selected row: the same actions as its links, with the same rules.
    fn menu_ui(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let Some((u, pos)) = self.menu.clone() else { return };
        let mut chosen = None;
        let area = egui::Area::new(egui::Id::new("users-menu")).order(egui::Order::Foreground).fixed_pos(pos).show(
            ui.ctx(),
            |ui| {
                egui::Frame::menu(ui.style()).show(ui, |ui| {
                    ui.label(RichText::new(&u.sam).strong());
                    ui.separator();
                    for (k, a) in user_actions(&u, &cx.me).into_iter().enumerate() {
                        let b = ui.button(a.label(&u));
                        if k == 0 && !ui.memory(|m| m.focused().is_some()) {
                            b.request_focus();
                        }
                        if b.clicked() {
                            chosen = Some(a);
                        }
                    }
                });
            },
        );
        let close =
            chosen.is_some() || ui.input(|i| i.key_pressed(egui::Key::Escape)) || area.response.clicked_elsewhere();
        if close {
            self.menu = None;
            // the menu's buttons go: give the keyboard back to the table (Up/Down/Enter)
            if let Some(f) = ui.memory(|m| m.focused()) {
                ui.memory_mut(|m| m.surrender_focus(f));
            }
        }
        if let Some(a) = chosen {
            run(a, u, cx);
        }
    }
}

fn columns() -> Vec<Column> {
    vec![
        Column::new(t("Login"), 130.0),
        Column::new(t("Name"), 140.0),
        Column::new(t("E-mail"), 160.0).fill(),
        Column::new(t("OU"), 100.0),
        Column::new(t("Status"), 120.0).badge_w(104.0),
        Column::new("", 130.0).badge().compact(104.0),
        Column::new(t("Last logon"), 140.0),
        Column::new(t("Actions"), 95.0).compact(COMPACT).span(4),
        Column::new("", 75.0).compact(COMPACT).covered(),
        Column::new("", 75.0).compact(COMPACT).covered(),
        Column::new("", 75.0).compact(COMPACT).covered(),
    ]
}

/// One cell of an account's row, with its link (the account index travels with the action).
pub fn cell(
    x: &User,
    i: usize,
    c: usize,
    me: &adm_core::model::Me,
    days: i64,
    now: DateTime<Utc>,
) -> Cell<(UserAction, usize)> {
    let ed = editable(x, me);
    match c {
        C_LOGIN => Cell::plain(&x.sam).bold().link("", tf!("Edit %s", x.sam), (UserAction::Edit, i)),
        // a deactivated account's text is in the subtle ink, as the web's row
        1 => Cell::new(&x.name, if x.enabled { Tone::Normal } else { Tone::Muted }),
        2 => Cell::new(&x.mail, if x.enabled { Tone::Normal } else { Tone::Muted }),
        3 => Cell::new(ou_name(&x.dn), if x.enabled { Tone::Normal } else { Tone::Muted }),
        C_STATUS => {
            let (s, tone) = user_status(x);
            let cell = Cell::new(s, tone);
            if ed {
                let hint = if x.enabled { t("Click to disable") } else { t("Click to enable") };
                cell.link("", hint.into(), (UserAction::Toggle, i))
            } else {
                cell
            }
        }
        // system and unlock are independent: a locked built-in account shows both
        C_TAG => match (x.protected, x.locked) {
            (true, true) => {
                let mut cl =
                    Cell::new(t("system"), Tone::Strong).link("🔓", tf!("Unlock %s", x.sam), (UserAction::Unlock, i));
                if let Some(l) = &mut cl.link {
                    l.tag = true;
                }
                cl
            }
            (true, false) => Cell::new(t("system"), Tone::Strong),
            (false, true) => Cell::plain(t("unlock")).link("🔓", tf!("Unlock %s", x.sam), (UserAction::Unlock, i)),
            (false, false) => Cell::plain(""),
        },
        6 => {
            if !is_set(&x.last_logon) {
                Cell::new(t("never"), Tone::Muted)
            } else if inactive(x, days, now) {
                Cell::new(date(x.last_logon), Tone::Warn)
            } else {
                Cell::plain(date(x.last_logon))
            }
        }
        C_PASSWORD => {
            Cell::plain(t("password")).link("🔑", tf!("Set a password for %s", x.sam), (UserAction::Password, i))
        }
        C_COPY => Cell::plain(t("copy")).link("📋", tf!("New user copied from %s", x.sam), (UserAction::Copy, i)),
        C_MOVE if ed => Cell::plain(t("move")).link("📁", tf!("Move %s to another OU", x.sam), (UserAction::Move, i)),
        C_DELETE if ed => Cell::new(t("delete"), Tone::Err).link("🗑", tf!("Delete %s", x.sam), (UserAction::Delete, i)),
        _ => Cell::plain(""),
    }
}

/// Runs a row action on the account `x` (a copy taken when it was clicked).
pub fn run(a: UserAction, x: User, cx: &mut Cx) {
    match a {
        UserAction::Edit => edit_user(cx, x.sam),
        UserAction::Password => {
            cx.push(PasswordDialog::new(cx, x));
        }
        UserAction::Copy => {
            let api = cx.panel.clone();
            let path = format!("/api/users/{}", esc(&x.sam));
            // the list may lack the groups: read the template itself
            cx.run(async move { api.get::<User>(&path).await }, |app, full| new_user(&mut app.cx, Some(full)));
        }
        UserAction::Unlock => act(cx, &x, "unlock", "%s unlocked."),
        UserAction::Toggle if !x.enabled => act(cx, &x, "enable", "%s enabled."),
        UserAction::Toggle => cx.confirm(
            t("Disable user"),
            tf!("Disable %s (%s)? The user will no longer be able to log on.", x.name, x.sam),
            t("Disable"),
            true,
            move |cx| act(cx, &x, "disable", "%s disabled."),
        ),
        UserAction::Move => {
            let api = cx.panel.clone();
            cx.run(async move { fetch_ous(&api).await }, move |app, ous| {
                let cur = parent_dn(&x.dn).to_string();
                app.cx.push(MoveUser { picker: OuPicker::new(&ous, &cur), user: x });
            });
        }
        UserAction::Delete => {
            let sam = x.sam.clone();
            cx.confirm_typed(
                t("Delete user"),
                tf!("Delete the account %s? Prefer disabling it: deletion cannot be undone.", x.name),
                &sam,
                move |cx| {
                    let api = cx.panel.clone();
                    let path = format!("/api/users/{}", esc(&x.sam));
                    cx.run(async move { api.act(Method::DELETE, &path).await }, move |app, ()| {
                        app.cx.set_status(tf!("%s deleted.", x.sam));
                        reload(app);
                    });
                },
            );
        }
    }
}

/// POST /api/users/{sam}/{verb}, then the status and a reload.
fn act(cx: &Cx, x: &User, verb: &'static str, msg: &'static str) {
    let api = cx.panel.clone();
    let path = format!("/api/users/{}/{verb}", esc(&x.sam));
    let sam = x.sam.clone();
    cx.run(async move { api.act(Method::POST, &path).await }, move |app, ()| {
        app.cx.set_status(tf!(msg, sam));
        reload(app);
    });
}

/// Reloads the Users list (and Home's numbers, which count the same accounts).
pub fn reload(app: &mut App) {
    app.with_shell(|s, cx| {
        s.users.reload(cx);
        s.home.reload_if_loaded(cx);
    });
}

/// The OUs and default containers, sorted by path.
pub async fn fetch_ous(api: &Api) -> adm_core::api::ApiResult<Vec<OrgUnit>> {
    let mut r: Vec<OrgUnit> = api.get("/api/ous").await?;
    logic::sort_ous(&mut r);
    Ok(r)
}

/// All group names, sorted (case-insensitive).
pub async fn group_names(api: &Api) -> adm_core::api::ApiResult<Vec<String>> {
    let gs: Vec<Group> = api.get("/api/groups").await?;
    let mut n: Vec<String> = gs.into_iter().map(|g| g.name).collect();
    n.sort_by_key(|s| s.to_lowercase());
    Ok(n)
}

/// A password of the configured length.
fn new_password(cx: &Cx) -> Zeroizing<String> {
    Zeroizing::new(logic::generate_password(logic::password_length(&cx.me)).unwrap_or_default())
}

/// The password field (shown, to be read out or copied) with its generate and copy buttons; `id` is the field's own
/// (forgotten with its undo history when the dialog closes).
fn password_field(ui: &mut Ui, cx: &mut Cx, id: egui::Id, pw: &mut Zeroizing<String>) {
    let look = cx.look.clone();
    ui.horizontal(|ui| {
        ui.add(crate::widgets::line(&mut **pw).id(id).font(egui::TextStyle::Monospace).desired_width(260.0));
        if widgets::icon_button(ui, &look, "⟳", t("Generate")).clicked() {
            *pw = new_password(cx);
        }
        if widgets::icon_button(ui, &look, "📋", t("Copy")).clicked() {
            ui.ctx().copy_text(pw.to_string());
            cx.set_status(t("Password copied."));
        }
    });
}

/// The two columns of user fields.
fn fields_form(ui: &mut Ui, cx: &Cx, f: &mut UserFields) {
    let look = cx.look.clone();
    ui.columns(2, |cols| {
        for (side, range) in [(0usize, 0..4), (1, 4..8)] {
            widgets::form(&mut cols[side], &format!("user-fields-{side}"), |ui| {
                for i in range {
                    widgets::label(ui, &look, t(USER_FIELDS[i].1));
                    ui.add(crate::widgets::line(&mut f.0[i]).desired_width(220.0));
                    ui.end_row();
                }
            });
        }
    });
}

/// A list of groups with remove buttons and a picker to add one. Returns the group added or removed.
pub enum GroupEvent {
    Add(String),
    Remove(String),
}

fn group_list(
    ui: &mut Ui,
    cx: &Cx,
    id: &str,
    names: &[String],
    groups: &[String],
    pick: &mut usize,
) -> Option<GroupEvent> {
    let look = cx.look.clone();
    let mut ev = None;
    let rest: Vec<String> = names.iter().filter(|n| !groups.contains(n)).cloned().collect();
    ui.horizontal(|ui| {
        let mut opts = vec![t("Add to a group…").to_string()];
        opts.extend(rest.iter().cloned());
        widgets::select(ui, (id, "pick"), pick, &opts, 300.0);
        if widgets::button(ui, &look, "➕", t("Add"), Kind::Secondary).clicked()
            && *pick > 0
            && let Some(g) = rest.get(*pick - 1)
        {
            ev = Some(GroupEvent::Add(g.clone()));
            *pick = 0;
        }
    });
    egui::ScrollArea::vertical().id_salt((id, "list")).max_height(150.0).min_scrolled_height(130.0).show(ui, |ui| {
        if groups.is_empty() {
            ui.label(t("Only the primary group (Domain Users)."));
        }
        for g in groups {
            ui.horizontal(|ui| {
                ui.label(g);
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    if widgets::button(ui, &look, "🗑", t("remove"), Kind::Link).clicked() {
                        ev = Some(GroupEvent::Remove(g.clone()));
                    }
                });
            });
        }
    });
    ev
}

/// Opens the new-user dialog (a copy of `tpl`: OU, job title, department, description and groups).
pub fn new_user(cx: &mut Cx, tpl: Option<User>) {
    let api = cx.panel.clone();
    cx.run(async move { Ok((fetch_ous(&api).await?, group_names(&api).await?)) }, move |app, (ous, names)| {
        let cx = &mut app.cx;
        let mut ou = cx.me.settings.default_ou.clone();
        if ou.is_empty() {
            ou = logic::users_container(&ous);
        }
        let (title, fields, groups) = match &tpl {
            Some(t0) => {
                ou = parent_dn(&t0.dn).to_string();
                (tf!("New user from %s", t0.sam), UserFields::from_template(t0), template_groups(t0))
            }
            None => (t("New user").to_string(), UserFields::default(), Vec::new()),
        };
        let pw = new_password(cx);
        cx.push(NewUser {
            pw_id: widgets::secret_id("new-user-password"),
            title,
            sam: String::new(),
            fields,
            ou: OuPicker::new(&ous, &ou),
            pw,
            must_change: true,
            names,
            groups,
            pick: 0,
            busy: false,
            done: false,
            focus: true,
        });
    });
}

pub struct NewUser {
    title: String,
    sam: String,
    fields: UserFields,
    ou: OuPicker,
    pw: Zeroizing<String>,
    pw_id: egui::Id,
    must_change: bool,
    names: Vec<String>,
    groups: Vec<String>,
    pick: usize,
    busy: bool,
    done: bool,
    focus: bool,
}

impl NewUser {
    fn create(&mut self, cx: &Cx) {
        self.busy = true;
        /// The body, the password borrowed (no copy outside the wiped buffers).
        #[derive(serde::Serialize)]
        struct Body<'a> {
            #[serde(flatten)]
            fields: serde_json::Map<String, serde_json::Value>,
            sam: &'a str,
            ou: String,
            password: &'a str,
            must_change: bool,
        }
        let body = Body {
            fields: self.fields.body(),
            sam: self.sam.trim(),
            ou: self.ou.dn(),
            password: &self.pw,
            must_change: self.must_change,
        };
        let body = Zeroizing::new(serde_json::to_vec(&body).unwrap_or_default());
        let groups = self.groups.clone();
        let api = cx.panel.clone();
        let session = cx.panel.id;
        let me = cx.current_dialog();
        cx.spawn(
            async move {
                let raw: &serde_json::value::RawValue =
                    serde_json::from_slice(&body).map_err(|e| adm_core::api::ApiError::Other(e.to_string()))?;
                let created: User = api.send(Method::POST, "/api/users", Some(&raw)).await?;
                let mut failed = Vec::new();
                for g in groups {
                    let path = format!("/api/groups/{}/members", esc(&g));
                    if let Err(e) = api.act_with(Method::POST, &path, &json!({ "member": created.sam })).await {
                        failed.push(format!("{g}: {e}"));
                    }
                }
                Ok::<_, adm_core::api::ApiError>((created, failed))
            },
            move |app, r| {
                if app.cx.panel.id != session {
                    return;
                }
                let d = app.cx.dialog_mut::<NewUser>(me);
                match r {
                    Ok((created, failed)) => {
                        if let Some(d) = d {
                            d.done = true;
                        }
                        app.cx.set_status(tf!("User %s created.", created.sam));
                        reload(app);
                        if !failed.is_empty() {
                            app.cx.show_error_text(failed.join("\n"));
                        }
                    }
                    Err(e) => {
                        if let Some(d) = d {
                            d.busy = false;
                        }
                        app.cx.handle_error(e);
                    }
                }
            },
        );
    }
}

impl Dialog for NewUser {
    fn title(&self) -> String {
        self.title.clone()
    }

    fn closed(&self, ctx: &egui::Context) {
        widgets::forget_field(ctx, self.pw_id);
    }

    fn width(&self) -> f32 {
        780.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        if self.done {
            return Flow::Close;
        }
        let look = cx.look.clone();
        widgets::form(ui, "new-user-top", |ui| {
            let r = widgets::labeled_field(ui, &look, t("Login (sAMAccountName)"), &mut self.sam, "", 300.0);
            if std::mem::take(&mut self.focus) {
                r.request_focus();
            }
            ui.end_row();
            widgets::label(ui, &look, t("OU"));
            self.ou.ui(ui, "new-user-ou", 400.0);
            ui.end_row();
        });
        fields_form(ui, cx, &mut self.fields);
        widgets::form(ui, "new-user-pw", |ui| {
            widgets::label(ui, &look, t("Initial password"));
            password_field(ui, cx, self.pw_id, &mut self.pw);
            ui.end_row();
            ui.label("");
            ui.checkbox(&mut self.must_change, t("Require change at next logon"));
            ui.end_row();
        });
        widgets::label(ui, &look, t("Groups"));
        // the new user's groups are only collected here; they are added after the account is created
        match group_list(ui, cx, "new-user-groups", &self.names, &self.groups, &mut self.pick) {
            Some(GroupEvent::Add(g)) => {
                self.groups.push(g);
                self.groups.sort();
            }
            Some(GroupEvent::Remove(g)) => self.groups.retain(|x| *x != g),
            None => {}
        }
        let f = footer(ui, cx, Some(t("Create")), "", false, !self.busy);
        if f.ok && !self.busy {
            self.create(cx);
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}

/// Opens the edit form of `sam`: the Account tab and the Groups tab (applied right away, as in the web panel).
pub fn edit_user(cx: &mut Cx, sam: String) {
    let api = cx.panel.clone();
    let path = format!("/api/users/{}", esc(&sam));
    cx.run(async move { Ok((api.get::<User>(&path).await?, group_names(&api).await?)) }, |app, (x, names)| {
        let mut groups = x.groups.clone();
        groups.sort();
        app.cx.push(EditUser {
            fields: UserFields::from_user(&x),
            user: x,
            names,
            groups,
            pick: 0,
            tab: 0,
            busy: false,
            done: false,
        });
    });
}

pub struct EditUser {
    user: User,
    fields: UserFields,
    names: Vec<String>,
    groups: Vec<String>,
    pick: usize,
    tab: usize,
    busy: bool,
    done: bool,
}

impl EditUser {
    /// The label of the Groups tab: how many groups the account is in, kept current.
    pub fn groups_label(n: usize) -> String {
        tf!("Groups (%d)", n)
    }

    fn member(&mut self, cx: &Cx, g: String, add: bool) {
        let me = cx.current_dialog();
        let api = cx.panel.clone();
        let sam = self.user.sam.clone();
        cx.run(
            async move {
                if add {
                    api.act_with(Method::POST, &format!("/api/groups/{}/members", esc(&g)), &json!({ "member": sam }))
                        .await?;
                } else {
                    api.act(Method::DELETE, &format!("/api/groups/{}/members/{}", esc(&g), esc(&sam))).await?;
                }
                Ok(g)
            },
            move |app, g| {
                app.cx.set_status(if add { tf!("Added to %s.", g) } else { tf!("Removed from %s.", g) });
                if let Some(d) = app.cx.dialog_mut::<EditUser>(me) {
                    if add {
                        if !d.groups.contains(&g) {
                            d.groups.push(g);
                            d.groups.sort();
                        }
                    } else {
                        d.groups.retain(|x| *x != g);
                    }
                }
            },
        );
    }
}

impl Dialog for EditUser {
    fn title(&self) -> String {
        format!("{} ({})", self.user.name, self.user.sam)
    }

    fn width(&self) -> f32 {
        780.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        if self.done {
            return Flow::Close;
        }
        let look = cx.look.clone();
        ui.horizontal(|ui| {
            ui.selectable_value(&mut self.tab, 0, format!("👤 {}", t("Account")));
            ui.selectable_value(&mut self.tab, 1, format!("☰ {}", Self::groups_label(self.groups.len())));
        });
        ui.separator();
        if self.tab == 0 {
            widgets::form(ui, "edit-user-login", |ui| {
                widgets::label(ui, &look, t("Login"));
                ui.label(format!("{} · {}", self.user.sam, self.user.upn));
                ui.end_row();
            });
            fields_form(ui, cx, &mut self.fields);
            ui.separator();
            ui.columns(3, |c| {
                let small = |s: String| RichText::new(s).size(11.0);
                c[0].label(small(tf!("Created: %s", date(self.user.created))));
                c[1].label(small(tf!("Last logon: %s", date(self.user.last_logon))));
                c[2].label(small(tf!("Password changed: %s", date(self.user.pwd_last_set))));
            });
            ui.add(egui::Label::new(RichText::new(format!("{} · {}", self.user.dn, self.user.sid)).size(11.0)).wrap());
        } else {
            match group_list(ui, cx, "edit-user-groups", &self.names, &self.groups, &mut self.pick) {
                Some(GroupEvent::Add(g)) => self.member(cx, g, true),
                Some(GroupEvent::Remove(g)) => self.member(cx, g, false),
                None => {}
            }
        }
        let f = footer(ui, cx, Some(t("Save")), "", false, !self.busy);
        if f.ok && !self.busy {
            self.busy = true;
            let api = cx.panel.clone();
            let path = format!("/api/users/{}", esc(&self.user.sam));
            let body = self.fields.body();
            let session = cx.panel.id;
            let me = cx.current_dialog();
            cx.spawn(async move { api.act_with(Method::PUT, &path, &body).await }, move |app, r| {
                if app.cx.panel.id != session {
                    return;
                }
                match r {
                    Ok(()) => {
                        if let Some(d) = app.cx.dialog_mut::<EditUser>(me) {
                            d.done = true;
                        }
                        app.cx.set_status(t("Details saved."));
                        reload(app);
                    }
                    Err(e) => {
                        if let Some(d) = app.cx.dialog_mut::<EditUser>(me) {
                            d.busy = false;
                        }
                        app.cx.handle_error(e);
                    }
                }
            });
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}

pub struct MoveUser {
    user: User,
    picker: OuPicker,
}

impl Dialog for MoveUser {
    fn title(&self) -> String {
        tf!("Move %s", self.user.sam)
    }

    fn width(&self) -> f32 {
        560.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        widgets::form(ui, "move-user", |ui| {
            widgets::label(ui, &look, t("Destination"));
            self.picker.ui(ui, "move-user-ou", 400.0);
            ui.end_row();
        });
        let f = footer(ui, cx, Some(t("Move")), "", false, self.picker.chosen());
        if f.ok && self.picker.chosen() {
            let api = cx.panel.clone();
            let path = format!("/api/users/{}/move", esc(&self.user.sam));
            let ou = self.picker.dn();
            cx.run(async move { api.act_with(Method::POST, &path, &json!({ "ou": ou })).await }, |app, ()| {
                app.cx.set_status(t("User moved."));
                reload(app);
            });
            return Flow::Close;
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}

const VALIDITY: [(&str, i64); 4] = [("1 hour", 1), ("24 hours", 24), ("3 days", 72), ("7 days", 168)];

/// Resets a password ADUC style (generator, copy, change at next logon, optional unlock), or sends the user a
/// single-use link to choose the new one.
pub struct PasswordDialog {
    user: User,
    pw: Zeroizing<String>,
    pw_id: egui::Id,
    must_change: bool,
    unlock: bool,
    tab: usize,
    hours: usize,
    after: Option<crate::app::Job>,
}

impl PasswordDialog {
    pub fn new(cx: &Cx, user: User) -> Self {
        let unlock = user.locked;
        Self {
            user,
            pw: new_password(cx),
            pw_id: widgets::secret_id("reset-password"),
            must_change: true,
            unlock,
            tab: 0,
            hours: 1,
            after: None,
        }
    }

    /// What runs after a success besides reloading Users (Home's quick reset clears its field).
    pub fn after(mut self, f: impl FnOnce(&mut App) + Send + 'static) -> Self {
        self.after = Some(Box::new(f));
        self
    }
}

impl Dialog for PasswordDialog {
    fn title(&self) -> String {
        format!("{} — {} ({})", t("Reset password"), self.user.name, self.user.sam)
    }

    fn closed(&self, ctx: &egui::Context) {
        widgets::forget_field(ctx, self.pw_id);
    }

    fn width(&self) -> f32 {
        600.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        ui.horizontal(|ui| {
            ui.selectable_value(&mut self.tab, 0, format!("👤 {}", t("Set password")));
            ui.selectable_value(&mut self.tab, 1, format!("✉ {}", t("Send link to the user")));
        });
        ui.separator();
        if self.tab == 0 {
            widgets::form(ui, "password-set", |ui| {
                widgets::label(ui, &look, t("New password"));
                password_field(ui, cx, self.pw_id, &mut self.pw);
                ui.end_row();
                ui.label("");
                ui.checkbox(&mut self.must_change, t("Require change at next logon"));
                ui.end_row();
                ui.label("");
                ui.checkbox(&mut self.unlock, t("Unlock the account too"));
                ui.end_row();
            });
        } else {
            widgets::form(ui, "password-link", |ui| {
                widgets::label(ui, &look, t("Valid for"));
                let labels: Vec<String> = VALIDITY.iter().map(|v| t(v.0).to_string()).collect();
                widgets::select(ui, "password-hours", &mut self.hours, &labels, 160.0);
                ui.end_row();
            });
            widgets::wrapped(
                ui,
                &format!(
                    "{} {}",
                    t(
                        "The current password stops working now and the account is unlocked. The user sets the new one through the link."
                    ),
                    t("Only the first browser that opens the link can use it, within 30 minutes; it works once.")
                ),
            );
        }
        let f = footer(ui, cx, Some(t("OK")), "", false, true);
        if f.ok {
            let api = cx.panel.clone();
            let x = self.user.clone();
            let after = self.after.take();
            if self.tab == 1 {
                #[derive(serde::Deserialize, Default)]
                struct Link {
                    #[serde(default)]
                    token: String,
                    #[serde(default)]
                    expires: Option<DateTime<Utc>>,
                }
                let hours = VALIDITY[self.hours.min(3)].1;
                let path = format!("/api/users/{}/password-link", esc(&x.sam));
                cx.run(
                    async move { api.send::<Link, _>(Method::POST, &path, Some(&json!({ "hours": hours }))).await },
                    move |app, r| {
                        let url = format!("{}/password/{}", app.cx.panel.base.trim_end_matches('/'), r.token);
                        app.cx.set_status(tf!("Password link for %s generated.", x.sam));
                        app.cx.push(ShowLink { sam: x.sam, url, expires: r.expires });
                        reload(app);
                        if let Some(a) = after {
                            a(app);
                        }
                    },
                );
            } else {
                #[derive(serde::Serialize)]
                struct Body<'a> {
                    password: &'a str,
                    must_change: bool,
                }
                // serialized here, from the borrowed password, into a buffer wiped when dropped
                let body = Zeroizing::new(
                    serde_json::to_vec(&Body { password: &self.pw, must_change: self.must_change }).unwrap_or_default(),
                );
                let unlock = self.unlock;
                let base = format!("/api/users/{}", esc(&x.sam));
                cx.run(
                    async move {
                        let raw: &serde_json::value::RawValue =
                            serde_json::from_slice(&body).map_err(|e| adm_core::api::ApiError::Other(e.to_string()))?;
                        api.act_with(Method::POST, &format!("{base}/password"), &raw).await?;
                        if unlock {
                            api.act(Method::POST, &format!("{base}/unlock")).await?;
                        }
                        Ok(())
                    },
                    move |app, ()| {
                        app.cx.set_status(tf!("Password of %s reset.", x.sam));
                        reload(app);
                        if let Some(a) = after {
                            a(app);
                        }
                    },
                );
            }
            return Flow::Close;
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}

/// A generated password link with a copy button.
pub struct ShowLink {
    sam: String,
    url: String,
    expires: Option<DateTime<Utc>>,
}

impl Dialog for ShowLink {
    fn title(&self) -> String {
        tf!("Link for %s", self.sam)
    }

    fn width(&self) -> f32 {
        640.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        ui.horizontal(|ui| {
            let mut v = self.url.clone();
            ui.add(crate::widgets::line(&mut v).font(egui::TextStyle::Monospace).desired_width(470.0));
            if widgets::button(ui, &look, "📋", t("copy"), Kind::Secondary).clicked() {
                ui.ctx().copy_text(self.url.clone());
                cx.set_status(t("Link copied."));
            }
        });
        widgets::wrapped(
            ui,
            &tf!("Valid until %s. Send it to the user through a trusted channel.", date(self.expires)),
        );
        if footer(ui, cx, None, t("Close"), false, true).ok { Flow::Close } else { Flow::Keep }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use adm_core::model::Me;

    fn me() -> Me {
        Me { user: "ana".into(), ..Default::default() }
    }

    fn links(x: &User) -> Vec<Option<UserAction>> {
        (0..11).map(|c| cell(x, 7, c, &me(), 90, Utc::now()).link.map(|l| l.action.0)).collect()
    }

    #[test]
    fn links_per_account_kind() {
        let bob = User { sam: "bob".into(), enabled: true, ..Default::default() };
        let l = links(&bob);
        assert_eq!(l[C_LOGIN], Some(UserAction::Edit));
        assert_eq!(l[C_STATUS], Some(UserAction::Toggle));
        assert_eq!(l[C_TAG], None);
        assert_eq!(l[C_MOVE], Some(UserAction::Move));
        assert_eq!(l[C_DELETE], Some(UserAction::Delete));
        // the operator's own login (any case) and a built-in account: no toggle, move or delete
        for x in [
            User { sam: "ANA".into(), enabled: true, ..Default::default() },
            User { sam: "krbtgt".into(), protected: true, ..Default::default() },
        ] {
            let l = links(&x);
            assert_eq!((l[C_STATUS], l[C_MOVE], l[C_DELETE]), (None, None, None), "{}", x.sam);
            assert_eq!((l[C_PASSWORD], l[C_COPY]), (Some(UserAction::Password), Some(UserAction::Copy)));
            assert!(cell(&x, 0, C_MOVE, &me(), 90, Utc::now()).text.is_empty());
        }
        // a locked built-in account: unlock and the system tag, together
        let admin =
            User { sam: "Administrator".into(), protected: true, locked: true, enabled: true, ..Default::default() };
        let c = cell(&admin, 0, C_TAG, &me(), 90, Utc::now());
        assert_eq!(c.link.as_ref().map(|l| (l.action.0, l.tag)), Some((UserAction::Unlock, true)));
        assert_eq!(c.text, "system");
        // every link carries the index of its own row
        assert!(links(&bob).iter().flatten().count() == 6);
        assert!((0..11).filter_map(|c| cell(&bob, 42, c, &me(), 90, Utc::now()).link).all(|l| l.action.1 == 42));
    }

    #[test]
    fn groups_tab_label() {
        assert_eq!(EditUser::groups_label(3), "Groups (3)");
    }
}
