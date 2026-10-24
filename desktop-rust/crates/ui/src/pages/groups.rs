//! Groups: the list (system tag, type, members, description), new group, edit description, delete (typed; built-in
//! groups never), and the members of the selected group (add by login, remove with a confirmation).

use adm_core::api::esc;
use adm_core::i18n::t;
use adm_core::logic::Tone;
use adm_core::model::{Group, OrgUnit};
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
    groups: Vec<Group>,
    table: TableState,
    /// The members of the group open on the right: its name and logins.
    open: Option<(String, Vec<String>)>,
    members: TableState,
    new_member: String,
    generation: u64,
}

impl Groups {
    pub fn load(&mut self, cx: &Cx) {
        self.reload(cx);
    }

    pub fn reload(&mut self, cx: &Cx) {
        self.generation += 1;
        let g = self.generation;
        let api = cx.panel.clone();
        cx.run(async move { api.get::<Vec<Group>>("/api/groups").await }, move |app, mut r| {
            app.with_shell(|s, _| {
                let p = &mut s.groups;
                if p.generation != g {
                    return;
                }
                r.sort_by_key(|x| x.name.to_lowercase());
                p.groups = r;
                p.table.clear();
                p.open = None;
                p.members.clear();
            });
        });
    }

    fn current(&self) -> Option<&Group> {
        self.table.sel.and_then(|i| self.groups.get(i))
    }

    /// Shows the members of the selected group and refreshes its row.
    fn open_group(&mut self, cx: &Cx) {
        let Some(g) = self.current().map(|g| g.name.clone()) else { return };
        let api = cx.panel.clone();
        let path = format!("/api/groups/{}", esc(&g));
        cx.run(async move { api.get::<Group>(&path).await }, move |app, d| {
            app.with_shell(|s, _| {
                let p = &mut s.groups;
                let mut members = d.members.clone().unwrap_or_default();
                members.sort();
                if let Some(row) = p.groups.iter_mut().find(|x| x.name == d.name) {
                    row.member_count = members.len() as i64;
                    row.description = d.description.clone();
                }
                if p.current().is_some_and(|x| x.name == d.name) {
                    p.open = Some((d.name, members));
                    p.members.clear();
                }
            });
        });
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        let w = ui.available_width();
        ui.horizontal_top(|ui| {
            ui.vertical(|ui| {
                ui.set_width(w * 0.62 - 6.0);
                widgets::card(ui, &look, t("Groups"), |ui| self.list(ui, cx, keys));
            });
            ui.vertical(|ui| {
                ui.set_width(w * 0.38 - 6.0);
                widgets::card(ui, &look, t("Members"), |ui| self.members_ui(ui, cx));
            });
        });
    }

    fn list(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        let cur = self.current().cloned();
        ui.horizontal(|ui| {
            if widgets::button(ui, &look, "➕", t("New group"), Kind::Primary).clicked() {
                new_group(cx);
            }
            if ui
                .add_enabled_ui(cur.is_some(), |ui| {
                    widgets::button(ui, &look, "✏", t("Edit description"), Kind::Secondary)
                })
                .inner
                .clicked()
                && let Some(g) = &cur
            {
                cx.push(Description { name: g.name.clone(), text: g.description.clone() });
            }
            // built-in groups cannot be deleted (the server refuses too)
            let deletable = cur.as_ref().is_some_and(|g| !g.protected);
            if ui
                .add_enabled_ui(deletable, |ui| widgets::button(ui, &look, "🗑", t("Delete"), Kind::Danger))
                .inner
                .clicked()
                && let Some(g) = cur.clone().filter(|g| !g.protected)
            {
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
            if widgets::icon_button(ui, &look, "⟳", t("Refresh")).clicked() {
                self.reload(cx);
            }
        });
        let kind = |k: &str| match k {
            "security" => t("Security"),
            "distribution" => t("Distribution"),
            _ => "",
        };
        let cols = [
            Column::new(t("Group"), 200.0),
            Column::new("", 75.0).badge(),
            Column::new(t("Group type"), 100.0),
            Column::new(t("Members"), 75.0),
            Column::new(t("Description"), 160.0),
        ];
        let gs = &self.groups;
        let mut hover = None;
        let out = table::Table { id: "groups", cols: &cols, rows: gs.len(), keys }.show::<()>(
            ui,
            &look,
            &mut self.table,
            &mut hover,
            |r, c| {
                let g = &gs[r];
                match c {
                    0 => Cell::new(&g.name, Tone::Strong),
                    1 if g.protected => Cell::new(t("system"), Tone::Strong),
                    1 => Cell::plain(""),
                    2 => Cell::plain(kind(&g.kind)),
                    3 => Cell::plain(g.member_count.to_string()),
                    _ => Cell::new(&g.description, Tone::Muted),
                }
            },
        );
        if out.selected.is_some() {
            self.open = None;
            self.open_group(cx);
        }
    }

    fn members_ui(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        let title = match &self.open {
            Some((g, m)) => tf!("%s — %d member(s)", g, m.len()),
            None => t("Choose a group").into(),
        };
        ui.label(RichText::new(title).strong().size(13.0).color(look.c("fg-strong")));
        let group = self.open.as_ref().map(|o| o.0.clone());
        ui.horizontal(|ui| {
            let r = ui.add(
                egui::TextEdit::singleline(&mut self.new_member)
                    .hint_text(t("New member's login (computers end with $)"))
                    .desired_width(ui.available_width() - 200.0),
            );
            let enter = r.lost_focus() && ui.input(|i| i.key_pressed(egui::Key::Enter));
            let add = ui
                .add_enabled_ui(group.is_some(), |ui| widgets::button(ui, &look, "➕", t("Add"), Kind::Primary))
                .inner
                .clicked();
            let member = self.members.sel.and_then(|i| self.open.as_ref().and_then(|o| o.1.get(i).cloned()));
            let remove = ui
                .add_enabled_ui(member.is_some(), |ui| widgets::button(ui, &look, "➖", t("Remove"), Kind::Danger))
                .inner
                .clicked();
            if let Some(g) = group.clone() {
                let m = self.new_member.trim().to_string();
                if (add || enter) && !m.is_empty() {
                    let api = cx.panel.clone();
                    let path = format!("/api/groups/{}/members", esc(&g));
                    cx.run(
                        async move { api.act_with(Method::POST, &path, &json!({ "member": m })).await.map(|()| m) },
                        |app, m| {
                            app.cx.set_status(tf!("%s added.", m));
                            app.with_shell(|s, cx| {
                                s.groups.new_member.clear();
                                s.groups.open_group(cx);
                            });
                        },
                    );
                }
                if remove && let Some(m) = member {
                    cx.confirm(
                        t("Remove member"),
                        tf!("Remove %s from the group %s?", m, g),
                        t("Remove"),
                        true,
                        move |cx| {
                            let api = cx.panel.clone();
                            let path = format!("/api/groups/{}/members/{}", esc(&g), esc(&m));
                            cx.run(async move { api.act(Method::DELETE, &path).await }, move |app, ()| {
                                app.cx.set_status(tf!("%s removed.", m));
                                app.with_shell(|s, cx| s.groups.open_group(cx));
                            });
                        },
                    );
                }
            }
        });
        let empty = Vec::new();
        let members = self.open.as_ref().map_or(&empty, |o| &o.1);
        let mut hover = None;
        table::Table {
            id: "members",
            cols: &[Column::new(t("Member (login)"), 320.0)],
            rows: members.len(),
            keys: false,
        }
        .show::<()>(ui, &look, &mut self.members, &mut hover, |r, _| Cell::plain(&members[r]));
    }
}

/// Edits a group's description.
struct Description {
    name: String,
    text: String,
}

impl Dialog for Description {
    fn title(&self) -> String {
        tf!("Group %s", self.name)
    }

    fn width(&self) -> f32 {
        560.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        widgets::form(ui, "group-desc", |ui| {
            widgets::labeled_field(ui, &look, t("Description"), &mut self.text, "", 380.0);
            ui.end_row();
        });
        let f = footer(ui, cx, Some(t("Save description")), "", false, true);
        if f.ok {
            let api = cx.panel.clone();
            let path = format!("/api/groups/{}", esc(&self.name));
            let (name, text) = (self.name.clone(), self.text.clone());
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
        600.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        widgets::form(ui, "new-group", |ui| {
            let r = widgets::labeled_field(ui, &look, t("Name"), &mut self.name, "", 360.0);
            if std::mem::take(&mut self.focus) {
                r.request_focus();
            }
            ui.end_row();
            widgets::labeled_field(ui, &look, t("Description"), &mut self.desc, "", 360.0);
            ui.end_row();
            widgets::label(ui, &look, t("Group type"));
            widgets::select(ui, "group-type", &mut self.kind, &[t("Security").into(), t("Distribution").into()], 200.0);
            ui.end_row();
            widgets::label(ui, &look, t("Scope"));
            widgets::select(
                ui,
                "group-scope",
                &mut self.scope,
                &[t("Global").into(), t("Domain local").into(), t("Universal").into()],
                200.0,
            );
            ui.end_row();
            widgets::label(ui, &look, t("OU"));
            self.ou.ui(ui, "group-ou", 360.0);
            ui.end_row();
        });
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
