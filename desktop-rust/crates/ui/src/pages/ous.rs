//! Organizational units as the web's page: the line naming the domain with GPOs and New OU, and the tree (the name
//! indented by level with its marker and a "container" tag, the description, the DN in monospace, and delete for an
//! OU — never a container nor Domain Controllers; the server refuses a non-empty one).

use adm_core::api::q;
use adm_core::i18n::t;
use adm_core::logic::{Tone, is_ou, ou_deletable, ou_path};
use adm_core::model::OrgUnit;
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
pub struct Ous {
    ous: Vec<OrgUnit>,
    /// The tree order: index in `ous` and level.
    tree: Vec<(usize, usize)>,
    table: TableState,
    generation: u64,
}

/// The web's tree: each DN's path from the root (the domain's DC parts dropped), sorted, indented by its depth.
pub fn tree(ous: &[OrgUnit]) -> Vec<(usize, usize)> {
    let mut v: Vec<(String, usize, usize)> = ous
        .iter()
        .enumerate()
        .map(|(i, o)| {
            let parts: Vec<String> =
                o.dn.split(',')
                    .filter(|p| !p.trim().to_uppercase().starts_with("DC="))
                    .map(|p| p.trim().to_lowercase())
                    .collect();
            let key = parts.iter().rev().cloned().collect::<Vec<_>>().join("/");
            (key, i, parts.len().saturating_sub(1))
        })
        .collect();
    v.sort();
    v.into_iter().map(|(_, i, l)| (i, l)).collect()
}

impl Ous {
    pub fn load(&mut self, cx: &Cx) {
        self.reload(cx);
    }

    pub fn reload(&mut self, cx: &Cx) {
        self.generation += 1;
        let g = self.generation;
        let api = cx.panel.clone();
        cx.run(async move { crate::pages::users::fetch_ous(&api).await }, move |app, r| {
            app.with_shell(|s, _| {
                if s.ous.generation == g {
                    s.ous.tree = tree(&r);
                    s.ous.ous = r;
                    s.ous.table.clear();
                }
            });
        });
    }

    /// The rows shown (tests).
    pub fn count(&self) -> usize {
        self.tree.len()
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) {
        let look = cx.look.clone();
        ui.horizontal(|ui| {
            ui.spacing_mut().item_spacing.x = 3.0;
            let muted = look.c("fg-muted");
            ui.label(RichText::new(t("Organizational units and default containers of")).size(12.0).color(muted));
            ui.label(crate::style::semibold(&cx.me.domain, 12.0).color(muted));
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                ui.spacing_mut().item_spacing.x = look.px("toolbar-gap", 8.0);
                if widgets::icon_button(ui, &look, crate::icons::REFRESH, t("Refresh")).clicked() {
                    self.reload(cx);
                }
                if widgets::button(ui, &look, crate::icons::FOLDER_PLUS, t("New OU"), Kind::Primary).clicked() {
                    let parents: Vec<OrgUnit> = self.ous.iter().filter(|o| is_ou(&o.dn)).cloned().collect();
                    cx.push(NewOu {
                        name: String::new(),
                        desc: String::new(),
                        inside: OuPicker::new(&parents, "").with_root(t("domain root")),
                        focus: true,
                    });
                }
                if widgets::button(ui, &look, crate::icons::FILE_COG, "GPOs", Kind::Secondary).clicked() {
                    crate::pages::gpos::open(cx);
                }
            });
        });
        ui.add_space(look.px("toolbar-mb", 12.0));
        let cols = [
            Column::new(t("Name"), 280.0),
            Column::new(t("Description"), 240.0),
            Column::new("DN", 360.0).fill(),
            Column::new(t("Actions"), 90.0).right(),
        ];
        let (ous, tr) = (&self.ous, &self.tree);
        let mut hover = None;
        let out = table::Table { id: "ous", cols: &cols, rows: tr.len(), keys, row_h: 0.0, frame: true }.show(
            ui,
            &look,
            &mut self.table,
            &mut hover,
            |r, c| {
                let (i, level) = tr[r];
                let o = &ous[i];
                match c {
                    0 => {
                        let cell = Cell::plain(&o.name).bold().tree(level);
                        if is_ou(&o.dn) { cell } else { cell.with_tag(t("container"), Tone::Normal) }
                    }
                    1 => Cell::plain(&o.description),
                    2 => Cell::new(&o.dn, Tone::Muted).mono(),
                    _ if ou_deletable(o) => {
                        Cell::new(t("delete"), Tone::Normal).link(crate::icons::TRASH, tf!("Delete %s", o.name), i)
                    }
                    _ => Cell::plain(""),
                }
            },
        );
        cx.hover = hover.or(cx.hover.take());
        if let Some(i) = out.clicked
            && let Some(o) = self.ous.get(i).cloned().filter(ou_deletable)
        {
            // a plain confirmation, as the web's (the server refuses an OU that is not empty)
            cx.confirm(
                t("Delete OU"),
                tf!("Delete the OU %s? Only possible when it is empty.", ou_path(&o.dn)),
                t("Delete"),
                true,
                move |cx| {
                    let api = cx.panel.clone();
                    let path = format!("/api/ous?dn={}", q(&o.dn));
                    cx.run(async move { api.act(Method::DELETE, &path).await }, move |app, ()| {
                        app.cx.set_status(tf!("OU %s deleted.", o.name));
                        app.with_shell(|s, cx| s.ous.reload(cx));
                    });
                },
            );
        }
    }
}

struct NewOu {
    name: String,
    desc: String,
    inside: OuPicker,
    focus: bool,
}

impl Dialog for NewOu {
    fn title(&self) -> String {
        t("New OU").into()
    }

    fn width(&self) -> f32 {
        460.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        // as the web's NewOrgUnitDialog: labels above the fields
        let l = widgets::lbl(ui, &look, t("Name"));
        let r = widgets::field(ui, &mut self.name, "", f32::INFINITY).labelled_by(l.id);
        if std::mem::take(&mut self.focus) {
            r.request_focus();
        }
        ui.add_space(12.0);
        let l = widgets::lbl(ui, &look, t("Description"));
        widgets::field(ui, &mut self.desc, "", f32::INFINITY).labelled_by(l.id);
        ui.add_space(12.0);
        widgets::lbl(ui, &look, t("Inside"));
        self.inside.ui(ui, "new-ou-inside", t("Inside"), ui.available_width());
        let f = footer(ui, cx, Some(t("Create")), "", false, true);
        if f.ok {
            let n = self.name.trim().to_string();
            if n.is_empty() {
                return Flow::Keep;
            }
            let body = json!({ "name": n, "description": self.desc, "parent": self.inside.dn() });
            let api = cx.panel.clone();
            cx.run(async move { api.act_with(Method::POST, "/api/ous", &body).await }, move |app, ()| {
                app.cx.set_status(tf!("OU %s created.", n));
                app.with_shell(|s, cx| s.ous.reload(cx));
            });
            return Flow::Close;
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn tree_as_the_web() {
        let o = |dn: &str| OrgUnit {
            dn: dn.into(),
            name: dn.split(',').next().unwrap_or("").split('=').nth(1).unwrap_or("").into(),
            ..Default::default()
        };
        let ous = [o("OU=SP,OU=Example,DC=d,DC=l"), o("CN=Users,DC=d,DC=l"), o("OU=Example,DC=d,DC=l")];
        assert_eq!(tree(&ous), [(1, 0), (2, 0), (0, 1)], "sorted by path; a child one level in");
    }
}
