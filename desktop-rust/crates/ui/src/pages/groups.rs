//! Groups as the web's page: the toolbar (filter, type, count, Export CSV, New group), the full-width table (the
//! group, its system tag, type, scope, members, description, and the members / delete links of each row) and the
//! group's detail dialog (description, members added by login and removed).

use adm_core::api::esc;
use adm_core::i18n::t;
use adm_core::logic::Tone;
use adm_core::model::{Group, OrgUnit, User};
use adm_core::tf;
use egui::{RichText, Ui};
use reqwest::Method;
use serde_json::json;

use super::OuPicker;
use crate::app::Cx;
use crate::modal::{Dialog, Flow, footer};
use crate::table::{self, Cell, Column, TableState};
use crate::widgets::{self, Kind};

#[derive(Default)]
pub struct Groups {
    /// A refresh asked by the toolbar's button, in flight (it says what it read when done).
    refreshing: bool,
    groups: Vec<Group>,
    /// The logins and names of the users (the detail dialog's suggestions and names).
    logins: Vec<(String, String)>,
    search: String,
    /// 0 all, 1 security, 2 distribution.
    kind: usize,
    visible: Vec<usize>,
    table: TableState,
    generation: u64,
}

/// What a row's link does.
#[derive(Clone, Copy)]
enum GroupAction {
    Members,
    Delete,
}

fn kind_name(k: &str) -> &'static str {
    if k == "security" { t("Security") } else { t("Distribution") }
}

fn scope_name(s: &str) -> &'static str {
    match s {
        "global" => t("Global"),
        "domainlocal" => t("Domain local"),
        "universal" => t("Universal"),
        "builtin" => t("Built-in"),
        _ => "",
    }
}

impl Groups {
    pub fn load(&mut self, cx: &Cx) {
        self.reload(cx);
    }

    pub fn reload(&mut self, cx: &Cx) {
        self.generation += 1;
        let g = self.generation;
        let api = cx.panel.clone();
        cx.run(
            async move { Ok((api.get::<Vec<Group>>("/api/groups").await?, api.get::<Vec<User>>("/api/users").await?)) },
            move |app, (mut r, users)| {
                app.with_shell(|s, cx| {
                    let p = &mut s.groups;
                    if p.generation != g {
                        return;
                    }
                    r.sort_by_key(|x| x.name.to_lowercase());
                    p.groups = r;
                    p.logins = users.into_iter().map(|u| (u.sam, u.name)).collect();
                    p.apply();
                    if std::mem::take(&mut p.refreshing) {
                        cx.set_status(tf!("%d groups read from the domain.", p.groups.len()));
                    }
                });
            },
        );
    }

    /// The groups shown (tests).
    pub fn count(&self) -> usize {
        self.visible.len()
    }

    /// Filters the list again (search in name and description, type).
    fn apply(&mut self) {
        let q = self.search.trim().to_lowercase();
        let want = ["", "security", "distribution"][self.kind.min(2)];
        self.visible = self
            .groups
            .iter()
            .enumerate()
            .filter(|(_, g)| want.is_empty() || g.kind == want)
            .filter(|(_, g)| {
                q.is_empty() || g.name.to_lowercase().contains(&q) || g.description.to_lowercase().contains(&q)
            })
            .map(|(i, _)| i)
            .collect();
        self.table.clear();
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        widgets::toolbar_row(ui, &look, |ui| {
            if super::search(ui, &mut self.search, t("Filter by name or description…"), 256.0).changed() {
                self.apply();
            }
            let kinds = [t("All types").to_string(), t("Security").into(), t("Distribution").into()];
            if widgets::select(ui, "groups-kind", t("Group type"), &mut self.kind, &kinds, 160.0) {
                self.apply();
            }
            ui.label(
                RichText::new(tf!("%d of %d", self.visible.len(), self.groups.len()))
                    .size(12.0)
                    .color(look.c("fg-muted")),
            );
            widgets::toolbar_end(ui, |ui| {
                if widgets::refresh_button(ui, &look, self.refreshing) {
                    self.refreshing = true;
                    self.reload(cx);
                }
                if widgets::button(ui, &look, crate::icons::PLUS, t("New group"), Kind::Primary).clicked() {
                    new_group(cx);
                }
                if widgets::button(ui, &look, crate::icons::DOWNLOAD, t("Export CSV"), Kind::Secondary).clicked() {
                    let mut rows = vec![
                        [t("Group"), t("Group type"), t("Scope"), t("Members"), t("Description")]
                            .map(String::from)
                            .to_vec(),
                    ];
                    rows.extend(self.visible.iter().filter_map(|&i| self.groups.get(i)).map(|g| {
                        vec![
                            g.name.clone(),
                            g.kind.clone(),
                            g.scope.clone(),
                            g.member_count.to_string(),
                            g.description.clone(),
                        ]
                    }));
                    crate::files::save_csv(cx, t("groups.csv"), rows);
                }
            });
        });
        widgets::toolbar_gap(ui, &look);
        // the group column as wide as the longest name shown (the web's auto layout), the description the rest
        let font = egui::FontId::new(12.0, egui::FontFamily::Name(crate::fonts::SEMIBOLD.into()));
        let longest = self
            .visible
            .iter()
            .filter_map(|&i| self.groups.get(i))
            .map(|g| ui.painter().layout_no_wrap(g.name.clone(), font.clone(), egui::Color32::PLACEHOLDER).size().x)
            .fold(0.0, f32::max);
        let cols = [
            Column::new(t("Group"), (longest + 2.0 * look.px("td-px", 8.0)).clamp(200.0, 420.0)),
            // the web's widths at 1280 (its th widths)
            Column::new("", 73.0).badge(),
            Column::new(t("Group type"), 91.0),
            Column::new(t("Scope"), 94.0),
            Column::new(t("Members"), 78.0).right(),
            Column::new(t("Description"), 200.0).fill(),
            Column::new(t("Actions"), 85.0).compact(34.0).span(2),
            Column::new("", 67.0).compact(34.0).covered(),
        ];
        let (gs, vis) = (&self.groups, &self.visible);
        let mut hover = None;
        let out = table::Table {
            id: "groups",
            cols: &cols,
            rows: vis.len(),
            keys,
            row_h: table::ROW_LINKS,
            frame: true,
            view_minus: 260.0,
        }
        .show(ui, &look, &mut self.table, &mut hover, |r, c| {
            let i = vis[r];
            let g = &gs[i];
            match c {
                0 => Cell::plain(&g.name).bold().link("", tf!("Members of %s", g.name), (GroupAction::Members, i)),
                1 if g.protected => Cell::new(t("system"), Tone::Strong),
                2 => Cell::plain(kind_name(&g.kind)),
                3 => Cell::plain(scope_name(&g.scope)),
                4 => Cell::plain(g.member_count.to_string()),
                5 => Cell::new(&g.description, Tone::Muted),
                6 => Cell::plain(t("members")).link(
                    crate::icons::USERS_ROUND,
                    tf!("Members of %s", g.name),
                    (GroupAction::Members, i),
                ),
                7 if !g.protected => Cell::new(t("delete"), Tone::Normal).link(
                    crate::icons::TRASH,
                    tf!("Delete %s", g.name),
                    (GroupAction::Delete, i),
                ),
                _ => Cell::plain(""),
            }
        });
        cx.hover = hover.or(cx.hover.take());
        let act = out.clicked.or_else(|| out.activated.and_then(|r| vis.get(r)).map(|&i| (GroupAction::Members, i)));
        if let Some((a, i)) = act
            && let Some(g) = self.groups.get(i).cloned()
        {
            match a {
                GroupAction::Members => open_detail(cx, g.name, self.logins.clone()),
                GroupAction::Delete => delete_group(cx, g),
            }
        }
    }
}

/// Deletes a group after the typed confirmation (built-in groups never: the server refuses too).
fn delete_group(cx: &mut Cx, g: Group) {
    if g.protected {
        return;
    }
    let name = g.name.clone();
    cx.confirm_typed(
        t("Delete group"),
        tf!("Delete the group %s? Its members lose the permissions it grants.", g.name),
        &name,
        move |cx| {
            let api = cx.panel.clone();
            let path = format!("/api/groups/{}", esc(&g.name));
            cx.run(async move { api.act(Method::DELETE, &path).await }, move |app, ()| {
                app.cx.set_status(tf!("Group %s deleted.", g.name));
                app.with_shell(|s, cx| s.groups.reload(cx));
            });
        },
    );
}

/// Opens a group's detail dialog with its members.
fn open_detail(cx: &mut Cx, name: String, logins: Vec<(String, String)>) {
    let api = cx.panel.clone();
    let path = format!("/api/groups/{}", esc(&name));
    cx.run(async move { api.get::<Group>(&path).await }, move |app, g| {
        let mut members = g.members.clone().unwrap_or_default();
        members.sort();
        app.cx.push(Detail {
            name: g.name,
            description: g.description,
            members,
            logins,
            add: String::new(),
            busy: false,
        });
    });
}

/// A group's detail, as the web's GroupDetailDialog: its description (saved by the dialog's button) and its members,
/// added by login and removed at once.
struct Detail {
    name: String,
    description: String,
    members: Vec<String>,
    logins: Vec<(String, String)>,
    add: String,
    busy: bool,
}

impl Detail {
    /// Adds or removes a member at once, then reloads the dialog's list and the page.
    fn member(&mut self, cx: &mut Cx, m: String, add: bool) {
        let api = cx.panel.clone();
        let (g, me) = (self.name.clone(), cx.current_dialog());
        let path = if add {
            format!("/api/groups/{}/members", esc(&g))
        } else {
            format!("/api/groups/{}/members/{}", esc(&g), esc(&m))
        };
        cx.run(
            async move {
                if add {
                    api.act_with(Method::POST, &path, &json!({ "member": m })).await?;
                } else {
                    api.act(Method::DELETE, &path).await?;
                }
                let d: Group = api.get(&format!("/api/groups/{}", esc(&g))).await?;
                Ok((m, d))
            },
            move |app, (m, d)| {
                app.cx.set_status(if add { tf!("%s added.", m) } else { tf!("%s removed.", m) });
                if let Some(x) = app.cx.dialog_mut::<Detail>(me) {
                    let mut members = d.members.unwrap_or_default();
                    members.sort();
                    x.members = members;
                    x.add.clear();
                }
                app.with_shell(|s, cx| s.groups.reload(cx));
            },
        );
    }
}

impl Dialog for Detail {
    fn title(&self) -> String {
        tf!("Group %s", self.name)
    }

    fn width(&self) -> f32 {
        760.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        let l = widgets::lbl(ui, &look, t("Description"));
        widgets::field(ui, &mut self.description, "", f32::INFINITY).labelled_by(l.id);
        ui.add_space(16.0);
        let mut add = None;
        ui.horizontal(|ui| {
            ui.spacing_mut().item_spacing.x = 8.0;
            let w = ui.available_width() - 80.0;
            // the logins as suggestions with their names, members left out (the web's)
            let options: Vec<(String, String)> = self
                .logins
                .iter()
                .filter(|(sam, _)| !self.members.iter().any(|m| m.eq_ignore_ascii_case(sam)))
                .cloned()
                .collect();
            let hint = t("New member's login (computers end with $)");
            let sg =
                widgets::suggest(ui, &look, egui::Id::new(("group-add", &self.name)), &mut self.add, &options, hint, w);
            let _ = ui.ctx().accesskit_node_builder(sg.response.id, |n| n.set_label(t("New member")));
            let enter = sg.enter;
            let ready = !self.add.trim().is_empty();
            if (ui.add_enabled_ui(ready, |ui| widgets::button(ui, &look, "", t("Add"), Kind::Primary)).inner.clicked()
                || enter)
                && ready
            {
                add = Some(self.add.trim().to_string());
            }
        });
        ui.add_space(8.0);
        let names: Vec<[RichText; 3]> = self
            .members
            .iter()
            .map(|m| {
                let name = self.logins.iter().find(|(s, _)| s.eq_ignore_ascii_case(m)).map_or("", |(_, n)| n.as_str());
                [RichText::new(m).monospace(), RichText::new(name), RichText::new("")]
            })
            .collect();
        let mut remove = None;
        let head = tf!("Member (%d)", self.members.len());
        let members = self.members.clone();
        let mut link = |ui: &mut Ui, r: usize| {
            if widgets::button(ui, &look, crate::icons::TRASH, t("remove"), Kind::Link)
                .on_hover_text(tf!("Remove %s", members[r]))
                .clicked()
            {
                remove = Some(members[r].clone());
            }
        };
        widgets::mini_table(ui, &look, [head.as_str(), t("Name"), t("Action")], &names, Some(&mut link));
        if self.members.is_empty() {
            ui.label(RichText::new(t("No direct members.")).size(12.0).color(look.c("fg-muted")));
        }
        if let Some(m) = add {
            self.member(cx, m, true);
        }
        if let Some(m) = remove {
            self.member(cx, m, false);
        }
        let f = footer(ui, cx, Some(t("Save description")), "", false, !self.busy);
        if f.ok {
            let api = cx.panel.clone();
            let path = format!("/api/groups/{}", esc(&self.name));
            let (name, text) = (self.name.clone(), self.description.clone());
            cx.run(
                async move { api.act_with(Method::PUT, &path, &json!({ "description": text })).await.map(|()| text) },
                move |app, text| {
                    app.cx.set_status(t("Description saved."));
                    app.with_shell(|s, _| {
                        if let Some(g) = s.groups.groups.iter_mut().find(|g| g.name == name) {
                            g.description = text;
                        }
                    });
                },
            );
            return Flow::Close;
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}

fn new_group(cx: &mut Cx) {
    let api = cx.panel.clone();
    cx.run(async move { crate::pages::users::fetch_ous(&api).await }, |app, ous| {
        let ou = OuPicker::new(&ous, &adm_core::logic::users_container(&ous));
        app.cx.push(NewGroup {
            name: String::new(),
            desc: String::new(),
            kind: 0,
            scope: 0,
            ou,
            focus: true,
            _ous: ous,
        });
    });
}

struct NewGroup {
    name: String,
    desc: String,
    kind: usize,
    scope: usize,
    ou: OuPicker,
    focus: bool,
    _ous: Vec<OrgUnit>,
}

impl Dialog for NewGroup {
    fn title(&self) -> String {
        t("New group").into()
    }

    fn width(&self) -> f32 {
        460.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        // as the web's NewGroupDialog: labels above the fields, type and scope side by side
        let l = widgets::lbl(ui, &look, t("Name"));
        let r = widgets::field(ui, &mut self.name, "", f32::INFINITY).labelled_by(l.id);
        if std::mem::take(&mut self.focus) {
            r.request_focus();
        }
        ui.add_space(12.0);
        let l = widgets::lbl(ui, &look, t("Description"));
        widgets::field(ui, &mut self.desc, "", f32::INFINITY).labelled_by(l.id);
        ui.add_space(12.0);
        let half = (ui.available_width() - 12.0) / 2.0;
        ui.horizontal_top(|ui| {
            ui.spacing_mut().item_spacing.x = 12.0;
            ui.vertical(|ui| {
                ui.set_width(half);
                widgets::lbl(ui, &look, t("Group type"));
                widgets::select(
                    ui,
                    "group-type",
                    t("Group type"),
                    &mut self.kind,
                    &[t("Security").into(), t("Distribution").into()],
                    half,
                );
            });
            ui.vertical(|ui| {
                ui.set_width(half);
                widgets::lbl(ui, &look, t("Scope"));
                widgets::select(
                    ui,
                    "group-scope",
                    t("Scope"),
                    &mut self.scope,
                    &[t("Global").into(), t("Domain local").into(), t("Universal").into()],
                    half,
                );
            });
        });
        ui.add_space(12.0);
        widgets::lbl(ui, &look, t("OU"));
        self.ou.ui(ui, "group-ou", t("OU"), ui.available_width());
        let f = footer(ui, cx, Some(t("Create")), "", false, true);
        if f.ok {
            let name = self.name.trim().to_string();
            let kind = ["security", "distribution"][self.kind.min(1)];
            let scope = ["global", "domainlocal", "universal"][self.scope.min(2)];
            let body =
                json!({ "name": name, "description": self.desc, "type": kind, "scope": scope, "ou": self.ou.dn() });
            let api = cx.panel.clone();
            cx.run(async move { api.act_with(Method::POST, "/api/groups", &body).await }, move |app, ()| {
                app.cx.set_status(tf!("Group %s created.", name));
                app.with_shell(|s, cx| s.groups.reload(cx));
            });
            return Flow::Close;
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}
