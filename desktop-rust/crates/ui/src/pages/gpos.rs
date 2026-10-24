//! The Group Policy Objects dialog of the OUs page, as the web's GpoListDialog: each GPO with its status, versions,
//! links (struck through when disabled, "enforced" said), extensions and a backup by the DC's agent; the containers
//! that block inheritance; and the restore of a backup as a new, unlinked GPO (typed confirmation of its name).

use std::path::PathBuf;

use adm_core::api::esc;
use adm_core::i18n::t;
use adm_core::model::Gpos;
use adm_core::tf;
use egui::{RichText, Ui};
use reqwest::Method;
use serde::Deserialize;
use serde_json::json;

use crate::app::Cx;
use crate::modal::{Dialog, Flow, footer};
use crate::widgets::{self, Kind};

/// Opens the dialog and loads the GPOs into it.
pub fn open(cx: &mut Cx) {
    let id = cx.push(GpoList { data: None, error: None, file: None, name: String::new(), busy: false });
    load(cx, id);
}

fn load(cx: &Cx, id: u64) {
    let api = cx.panel.clone();
    let session = cx.panel.id;
    cx.spawn(async move { api.get::<Gpos>("/api/gpos").await }, move |app, r| {
        if app.cx.panel.id != session {
            return;
        }
        if let Some(d) = app.cx.dialog_mut::<GpoList>(id) {
            match r {
                Ok(g) => d.data = Some(g),
                Err(e) => d.error = Some(e.to_string()),
            }
        }
    });
}

struct GpoList {
    data: Option<Gpos>,
    error: Option<String>,
    file: Option<PathBuf>,
    name: String,
    busy: bool,
}

#[derive(Default, Deserialize)]
#[serde(default)]
struct Restored {
    guid: String,
    dc: String,
    sync: Vec<String>,
}

/// The first RDN's value, or the domain for the root.
fn short_dn(dn: &str, domain: &str) -> String {
    if dn.to_uppercase().starts_with("DC=") {
        return domain.to_string();
    }
    dn.split(',').next().and_then(|r| r.split_once('=')).map_or(dn.to_string(), |(_, v)| v.to_string())
}

impl Dialog for GpoList {
    fn title(&self) -> String {
        t("Group Policy Objects (GPOs)").into()
    }

    fn width(&self) -> f32 {
        760.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        let muted = look.c("fg-muted");
        let me = cx.current_dialog();
        match (&self.data, &self.error) {
            (_, Some(e)) => {
                ui.colored_label(look.c("err"), e);
            }
            (None, None) => {
                ui.label(t("Loading…"));
            }
            (Some(d), None) => {
                let domain = cx.me.domain.clone();
                let rows: Vec<[RichText; 5]> = d
                    .gpos
                    .iter()
                    .map(|g| {
                        let status = match (g.machine_enabled, g.user_enabled) {
                            (true, true) => RichText::new(t("in use")),
                            (false, false) => RichText::new(t("turned off")).color(look.c("err")),
                            (true, false) => RichText::new(t("user part disabled")).color(look.c("warn")),
                            (false, true) => RichText::new(t("computer part disabled")).color(look.c("warn")),
                        };
                        let links = if g.links.is_empty() {
                            RichText::new(t("not linked")).color(muted)
                        } else {
                            RichText::new(
                                g.links
                                    .iter()
                                    .map(|l| {
                                        let s = short_dn(&l.target, &domain);
                                        let s = if l.enforced { format!("{s} ({})", t("enforced")) } else { s };
                                        if l.disabled { format!("{s} ({})", t("disabled")) } else { s }
                                    })
                                    .collect::<Vec<_>>()
                                    .join(", "),
                            )
                        };
                        let ext = if g.extensions.is_empty() { "—".to_string() } else { g.extensions.join(", ") };
                        [
                            crate::style::semibold(if g.name.is_empty() { &g.guid } else { &g.name }, 12.0),
                            status,
                            RichText::new(format!("{} / {}", g.machine_version, g.user_version)),
                            links,
                            RichText::new(ext).color(muted),
                        ]
                    })
                    .collect();
                let guids: Vec<(String, String)> = d.gpos.iter().map(|g| (g.guid.clone(), g.name.clone())).collect();
                let mut backup = None;
                let mut cell = |ui: &mut Ui, r: usize| {
                    let hint = t("Back up this GPO (needs the agent installed on a domain controller)");
                    if widgets::button(ui, &look, "⬇", t("backup"), Kind::Link).on_hover_text(hint).clicked() {
                        backup = guids.get(r).cloned();
                    }
                };
                // the backup link takes the last column, after the extensions
                let rows6: Vec<[RichText; 6]> =
                    rows.into_iter().map(|[a, b, c, d, e]| [a, b, c, d, e, RichText::new("")]).collect();
                widgets::mini_table(
                    ui,
                    &look,
                    [t("Name"), t("Status"), t("Version (computer / user)"), t("Linked to"), t("Extensions"), ""],
                    &rows6,
                    Some(&mut cell),
                );
                if d.gpos.is_empty() {
                    ui.label(RichText::new(t("No GPOs.")).size(12.0).color(muted));
                }
                let blocked: Vec<String> =
                    d.containers.iter().filter(|c| c.block_inheritance).map(|c| short_dn(&c.dn, &domain)).collect();
                if !blocked.is_empty() {
                    ui.add_space(8.0);
                    ui.horizontal_wrapped(|ui| {
                        ui.label(RichText::new(format!("{}:", t("Inheritance blocked in"))).size(12.0));
                        ui.label(crate::style::semibold(blocked.join(", "), 12.0));
                    });
                }
                ui.add_space(8.0);
                ui.label(
                    RichText::new(t(
                        "From the directory (LDAP). A struck-through link is disabled; in a container, link order 1 wins.",
                    ))
                    .size(11.0)
                    .color(muted),
                );
                if let Some((guid, name)) = backup {
                    let file =
                        format!("{}.tar.gz", if name.is_empty() { guid.trim_matches(['{', '}']) } else { &name });
                    crate::files::save_download(
                        cx,
                        cx.panel.clone(),
                        format!("/api/gpos/{}/backup", esc(&guid)),
                        file,
                        |app, p| {
                            app.cx.set_status(tf!("Saved to %s", p.display()));
                        },
                    );
                }
            }
        }
        // restore a backup as a new GPO
        ui.add_space(12.0);
        let x = ui.max_rect().x_range();
        ui.painter().hline(x, ui.cursor().top(), egui::Stroke::new(1.0, look.c("line")));
        ui.add_space(12.0);
        ui.horizontal_wrapped(|ui| {
            ui.label(crate::style::semibold(t("Restore a backup as a new GPO"), 12.0));
            let file = self
                .file
                .as_ref()
                .and_then(|p| p.file_name())
                .map_or(t("Choose a file…").to_string(), |n| n.to_string_lossy().into());
            if widgets::button(ui, &look, "📂", &file, Kind::Secondary).clicked() {
                crate::files::pick_file_of(cx, Some((t("GPO backup"), &["gz", "tgz"])), move |app, p| {
                    if let Some(d) = app.cx.dialog_mut::<GpoList>(me) {
                        d.file = Some(p);
                    }
                });
            }
            let r = ui.add(
                widgets::line(&mut self.name).hint_text(t("Name of the new GPO")).desired_width(256.0).char_limit(128),
            );
            let _ = ui.ctx().accesskit_node_builder(r.id, |n| n.set_label(t("Name of the new GPO")));
            let ready = !self.busy && self.file.is_some() && !self.name.trim().is_empty();
            if ui
                .add_enabled_ui(ready, |ui| widgets::button(ui, &look, "⬆", t("Restore"), Kind::Primary))
                .inner
                .clicked()
                && let Some(path) = self.file.clone()
            {
                let name = self.name.trim().to_string();
                let file = path.file_name().map(|n| n.to_string_lossy().to_string()).unwrap_or_default();
                cx.confirm_typed(
                    t("Restore"),
                    tf!("Create the new GPO %s from %s? It is created unlinked, with a new GUID.", name, file),
                    &name.clone(),
                    move |cx| restore(cx, me, path, name),
                );
            }
        });
        let f = footer(ui, cx, None, t("Close"), false, true);
        if f.ok { Flow::Close } else { Flow::Keep }
    }
}

/// Sends the archive (base64 in JSON, as the web) and says where to copy the new GPO's folder.
fn restore(cx: &mut Cx, me: u64, path: PathBuf, name: String) {
    if let Some(d) = cx.dialog_mut::<GpoList>(me) {
        d.busy = true;
    }
    let api = cx.panel.clone();
    cx.run(
        async move {
            let bytes = tokio::fs::read(&path).await.map_err(|e| adm_core::api::ApiError::Other(e.to_string()))?;
            let archive = base64::Engine::encode(&base64::engine::general_purpose::STANDARD, bytes);
            api.send::<Restored, _>(Method::POST, "/api/gpos/restore", Some(&json!({ "name": name, "archive": archive })))
                .await
                .map(|r| (name, r))
        },
        move |app, (name, r)| {
            app.cx.set_status(tf!("GPO %s created (%s).", name, r.guid));
            if let Some(d) = app.cx.dialog_mut::<GpoList>(me) {
                d.busy = false;
                d.file = None;
                d.name.clear();
            }
            if !r.sync.is_empty() {
                app.cx.show_info(
                    t("GPO restored"),
                    tf!(
                        "Samba does not replicate SYSVOL: copy the folder Policies/%s from %s to %s before linking the GPO.",
                        r.guid,
                        r.dc,
                        r.sync.join(", ")
                    ),
                );
            }
            load(&app.cx, me);
        },
    );
}
