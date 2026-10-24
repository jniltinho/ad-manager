//! Home as the web's: six tiles (enabled, disabled, locked, inactive, groups, computers) that open their list, the
//! quick password reset / unlock by login, the domain controllers, the alerts (each locked account with its unlock)
//! and the operating systems.

use adm_core::api::esc;
use adm_core::i18n::t;
use adm_core::logic::{Tone, UserFilter, inactive};
use adm_core::model::{Summary, User};
use adm_core::tf;
use chrono::Utc;
use egui::{RichText, Ui};
use reqwest::Method;

use crate::app::Cx;
use crate::shell::{Nav, Page};
use crate::widgets::{self, Kind};

#[derive(Default)]
pub struct Home {
    data: Option<(Summary, Vec<User>)>,
    login: String,
    loaded: bool,
    generation: u64,
    /// The two card rows' heights, from the last frame.
    rows: [f32; 2],
}

/// One alert row: tag, tone role, text, and its link with what it does.
pub struct Alert {
    pub tag: String,
    pub tone: Tone,
    pub text: String,
    pub link: Option<(String, AlertGo)>,
}

/// What an alert's link does.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum AlertGo {
    Nav(Nav),
    /// Unlocks this account and reloads Home.
    Unlock(String),
}

/// A panel as the web's .card with its .card-h, the content drawn by `add` without padding (each part pads itself).
/// A card at least `min_h` high (the web's grid row stretches its cards to the tallest); returns its own height.
fn panel(ui: &mut Ui, look: &crate::style::Look, title: &str, min_h: f32, add: impl FnOnce(&mut Ui)) -> f32 {
    let under = ui.painter().add(egui::Shape::Noop);
    let mut natural = 0.0;
    let r = ui
        .vertical(|ui| {
            ui.spacing_mut().item_spacing.y = 0.0;
            widgets::card_head(ui, look, title, |_| {});
            add(ui);
            natural = ui.min_rect().height();
            if natural < min_h {
                ui.add_space(min_h - natural);
            }
        })
        .response;
    let mut shapes = crate::depth::box_shapes(r.rect, None, look.c("surface"), look.shadows("card"));
    shapes.push(egui::Shape::rect_stroke(
        r.rect,
        0.0,
        egui::Stroke::new(1.0, look.c("line")),
        egui::StrokeKind::Inside,
    ));
    ui.painter().set(under, egui::Shape::Vec(shapes));
    natural
}

/// Keeps a row's height (the taller card's, measured last frame) and asks for a frame when it changed.
fn row_height(ui: &Ui, row: &mut f32, a: f32, b: f32) {
    let h = a.max(b);
    if (h - *row).abs() > 0.5 {
        *row = h;
        ui.ctx().request_repaint();
    }
}

impl Home {
    pub fn load(&mut self, cx: &Cx) {
        self.loaded = true;
        self.generation += 1;
        let g = self.generation;
        let api = cx.panel.clone();
        // the summary has no inactive count: computed here from the users, as the web panel does
        cx.run(
            async move { Ok((api.get::<Summary>("/api/summary").await?, api.get::<Vec<User>>("/api/users").await?)) },
            move |app, d| {
                app.with_shell(|s, _| {
                    if s.home.generation == g {
                        s.home.data = Some(d);
                    }
                });
            },
        );
    }

    /// Reloads when the page was opened already (its numbers count the accounts other pages change).
    pub fn reload_if_loaded(&mut self, cx: &Cx) {
        if self.loaded {
            self.load(cx);
        }
    }

    /// The alerts for a summary and its users.
    pub fn alerts(s: &Summary, users: &[User], days: i64) -> Vec<Alert> {
        let now = Utc::now();
        let idle = users.iter().filter(|u| inactive(u, days, now)).count();
        let mut rows = Vec::new();
        if !s.policy.complexity || s.policy.min_length < 8 {
            let msg = if s.policy.complexity {
                "Minimum password length of %d characters."
            } else {
                "Minimum password length of %d characters, no complexity."
            };
            rows.push(Alert {
                tag: t("policy").into(),
                tone: Tone::Warn,
                text: tf!(msg, s.policy.min_length),
                link: Some((t("view").into(), AlertGo::Nav(Nav::Policy))),
            });
        }
        // one row per locked account, with its unlock, as the web
        for u in users.iter().filter(|u| u.locked) {
            rows.push(Alert {
                tag: t("locked").into(),
                tone: Tone::Err,
                text: format!("{} ({})", u.name, u.sam),
                link: Some((t("unlock").into(), AlertGo::Unlock(u.sam.clone()))),
            });
        }
        if idle > 0 {
            rows.push(Alert {
                tag: t("inactive").into(),
                tone: Tone::Muted,
                text: tf!("%d enabled account(s) with no logon for more than %d days.", idle, days),
                link: Some((t("review").into(), AlertGo::Nav(Nav::Users(UserFilter::Inactive)))),
            });
        }
        rows
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Option<Nav> {
        let look = cx.look.clone();
        let days = cx.inactive_days();
        let mut nav = None;
        let (s, users) = match &self.data {
            Some((s, u)) => (Some(s), u.as_slice()),
            None => (None, &[][..]),
        };
        let idle = users.iter().filter(|u| inactive(u, days, Utc::now())).count() as i64;
        let n = |k: &str| s.map_or("…".to_string(), |s| s.users.get(k).copied().unwrap_or(0).to_string());
        let locked = s.map_or(0, |s| s.users.get("locked").copied().unwrap_or(0));
        let alerts = s.map(|s| Self::alerts(s, users, days));
        let (mut quick, mut unlock) = (false, None);
        let muted = look.c("fg-muted");
        // the six tiles, 12 points apart, each opening its list
        let tiles: [(String, String, egui::Color32, egui::Color32, &str, Nav); 6] = [
            (
                t("Enabled users").into(),
                n("enabled"),
                look.c("ok"),
                look.c("decor-ok"),
                crate::icons::USER_CHECK,
                Nav::Users(UserFilter::Enabled),
            ),
            (
                t("Disabled").into(),
                n("disabled"),
                muted,
                look.c("decor-muted"),
                crate::icons::USER_X,
                Nav::Users(UserFilter::Disabled),
            ),
            (
                t("Locked").into(),
                n("locked"),
                if locked > 0 { look.c("err") } else { muted },
                if locked > 0 { look.c("decor-err") } else { look.c("decor-muted") },
                crate::icons::LOCK,
                Nav::Users(UserFilter::Locked),
            ),
            (
                tf!("Inactive %d+ days", days),
                if s.is_some() { idle.to_string() } else { "…".into() },
                if idle > 0 { look.c("warn") } else { muted },
                if idle > 0 { look.c("decor-warn") } else { look.c("decor-muted") },
                crate::icons::CLOCK,
                Nav::Users(UserFilter::Inactive),
            ),
            (
                t("Groups").into(),
                s.map_or("…".into(), |s| s.groups.to_string()),
                look.c("fg-strong"),
                look.c("decor-strong"),
                crate::icons::USERS_ROUND,
                Nav::To(Page::Groups),
            ),
            (
                t("Computers").into(),
                s.map_or("…".into(), |s| s.computers.to_string()),
                look.c("fg-strong"),
                look.c("decor-strong"),
                crate::icons::MONITOR,
                Nav::To(Page::Computers),
            ),
        ];
        let w = ui.available_width();
        let tile_w = (w - 12.0 * 5.0) / 6.0;
        ui.horizontal(|ui| {
            ui.spacing_mut().item_spacing.x = 12.0;
            for (label, value, ink, decor, icon, to) in tiles {
                if widgets::stat_tile(ui, &look, tile_w, &label, &value, ink, decor, icon).clicked() {
                    nav = Some(to);
                }
            }
        });
        ui.add_space(20.0);
        // the reset box (a third) and the domain controllers (two thirds)
        let third = (w - 20.0 * 2.0) / 3.0;
        let [r0, r1] = self.rows;
        let (mut ha, mut hb) = (0.0, 0.0);
        ui.horizontal_top(|ui| {
            ui.spacing_mut().item_spacing.x = 20.0;
            ui.allocate_ui(egui::vec2(third, 0.0), |ui| {
                ui.set_width(third);
                ha = panel(ui, &look, t("Reset password / unlock"), r0, |ui| {
                    egui::Frame::new().inner_margin(egui::Margin::same(12)).show(ui, |ui| {
                        ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                            quick = widgets::button(ui, &look, crate::icons::KEY, t("Open"), Kind::Primary).clicked();
                            // the logins as suggestions with their names (the web's quick box)
                            let options: Vec<(String, String)> = self
                                .data
                                .as_ref()
                                .map(|(_, us)| us.iter().map(|u| (u.sam.clone(), u.name.clone())).collect())
                                .unwrap_or_default();
                            let w = ui.available_width();
                            let id = egui::Id::new("home-quick");
                            let sg = widgets::suggest(ui, &look, id, &mut self.login, &options, t("user login"), w);
                            let _ = ui.ctx().accesskit_node_builder(sg.response.id, |n| n.set_label(t("Login")));
                            quick |= sg.enter;
                        });
                    });
                });
            });
            ui.allocate_ui(egui::vec2(ui.available_width(), 0.0), |ui| {
                ui.set_width(ui.available_width());
                hb = panel(ui, &look, t("Domain controllers"), r0, |ui| {
                    let rows: Vec<[RichText; 3]> = s.map_or_else(Vec::new, |s| {
                        s.dcs
                            .iter()
                            .map(|d| {
                                let os = if d.os.is_empty() {
                                    RichText::new(t("not reported")).color(muted)
                                } else {
                                    RichText::new(format!("{} {}", d.os, d.os_version))
                                };
                                [
                                    crate::style::semibold(&d.name, 12.0),
                                    os,
                                    RichText::new(adm_core::i18n::date(d.last_logon)),
                                ]
                            })
                            .collect()
                    });
                    widgets::mini_table(ui, &look, [t("Name"), t("System"), t("Last logon")], &rows, None);
                    egui::Frame::new().inner_margin(egui::Margin::symmetric(12, 6)).show(ui, |ui| {
                        let server = s.map_or(String::new(), |s| s.server.clone());
                        ui.horizontal_wrapped(|ui| {
                            ui.spacing_mut().item_spacing.x = 3.0;
                            let small = |s: &str| RichText::new(s).size(11.0).color(muted);
                            ui.label(small(t("Connected to")));
                            ui.label(small(&server).monospace());
                            ui.label(small(&format!(". {}", t("Replication/SYSVOL are not checked here — use"))));
                            ui.label(small("samba-tool drs showrepl").monospace());
                            ui.label(small("."));
                        });
                    });
                });
            });
        });
        row_height(ui, &mut self.rows[0], ha, hb);
        ui.add_space(20.0);
        // the alerts and the operating systems, half each
        let half = (w - 20.0) / 2.0;
        let (mut hc, mut hd) = (0.0, 0.0);
        ui.horizontal_top(|ui| {
            ui.spacing_mut().item_spacing.x = 20.0;
            ui.allocate_ui(egui::vec2(half, 0.0), |ui| {
                ui.set_width(half);
                hc = panel(ui, &look, t("Alerts"), r1, |ui| match &alerts {
                    None => {
                        egui::Frame::new()
                            .inner_margin(egui::Margin::symmetric(12, 8))
                            .show(ui, |ui| ui.label(t("Loading…")));
                    }
                    Some(rows) if rows.is_empty() => {
                        egui::Frame::new().inner_margin(egui::Margin::symmetric(12, 8)).show(ui, |ui| {
                            ui.label(RichText::new(t("Nothing pending.")).size(12.0).color(muted));
                        });
                    }
                    Some(rows) => {
                        for (k, a) in rows.iter().enumerate() {
                            if k > 0 {
                                let x = ui.max_rect().x_range();
                                ui.painter().hline(x, ui.cursor().top(), egui::Stroke::new(1.0, look.c("line")));
                            }
                            egui::Frame::new().inner_margin(egui::Margin::symmetric(12, 8)).show(ui, |ui| {
                                ui.set_width(ui.available_width());
                                ui.horizontal(|ui| {
                                    widgets::tag(ui, &look, &a.tag, a.tone);
                                    ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                                        if let Some((label, go)) = &a.link {
                                            // as the web's links: unlock with its icon first, the others with a
                                            // chevron after
                                            let text = if matches!(go, AlertGo::Unlock(_)) {
                                                format!("{} {label}", crate::icons::LOCK_OPEN)
                                            } else {
                                                format!("{label} {}", crate::icons::RIGHT)
                                            };
                                            if widgets::button(ui, &look, "", &text, Kind::Link).clicked() {
                                                match go {
                                                    AlertGo::Nav(n) => nav = Some(n.clone()),
                                                    AlertGo::Unlock(sam) => unlock = Some(sam.clone()),
                                                }
                                            }
                                        }
                                        ui.with_layout(egui::Layout::left_to_right(egui::Align::Center), |ui| {
                                            // wrapped, not cut, as the web's (a monospace theme makes it longer)
                                            ui.add(egui::Label::new(RichText::new(&a.text).size(12.0)).wrap());
                                        });
                                    });
                                });
                            });
                        }
                    }
                });
            });
            ui.allocate_ui(egui::vec2(ui.available_width(), 0.0), |ui| {
                ui.set_width(ui.available_width());
                hd = panel(ui, &look, t("Operating systems"), r1, |ui| {
                    let mut systems: Vec<(String, i64)> =
                        s.map_or_else(Vec::new, |s| s.os.iter().map(|(k, v)| (k.clone(), *v)).collect());
                    systems.sort_by(|a, b| b.1.cmp(&a.1).then(a.0.cmp(&b.0)));
                    let max = systems.iter().map(|x| x.1).max().unwrap_or(1).max(1);
                    egui::Frame::new().inner_margin(egui::Margin::same(12)).show(ui, |ui| {
                        ui.spacing_mut().item_spacing.y = 4.0;
                        ui.set_width(ui.available_width());
                        for (os, n) in systems {
                            ui.horizontal(|ui| {
                                let name = if os.is_empty() { t("not reported").to_string() } else { os.clone() };
                                ui.allocate_ui(egui::vec2(192.0, 18.0), |ui| {
                                    ui.set_width(192.0);
                                    ui.add(egui::Label::new(RichText::new(&name).size(12.0)).truncate())
                                        .on_hover_text(&name);
                                });
                                let old = ["XP", "2003", "2008 Standard"].iter().any(|k| os.contains(k));
                                let bw = (ui.available_width() - 40.0).max(0.0) * 0.6 * n as f32 / max as f32;
                                let (r, _) =
                                    ui.allocate_exact_size(egui::vec2(bw.max(1.0), 12.0), egui::Sense::hover());
                                ui.painter().rect_filled(
                                    r,
                                    0.0,
                                    if old { look.c("err-fill") } else { look.c("chart-1") },
                                );
                                ui.label(RichText::new(n.to_string()).size(12.0).color(muted));
                            });
                        }
                    });
                });
            });
        });
        row_height(ui, &mut self.rows[1], hc, hd);
        if quick {
            self.quick(cx);
        }
        if let Some(sam) = unlock {
            let api = cx.panel.clone();
            let path = format!("/api/users/{}/unlock", esc(&sam));
            cx.run(async move { api.act(Method::POST, &path).await }, move |app, ()| {
                app.cx.set_status(tf!("%s unlocked.", sam));
                app.with_shell(|s, cx| s.home.load(cx));
            });
        }
        nav
    }

    /// Opens the password dialog of the typed login.
    /// Opens the password dialog of the login typed, found in the list (an unknown one only warns, as the web's).
    fn quick(&mut self, cx: &mut Cx) {
        let login = self.login.trim().to_lowercase();
        if login.is_empty() {
            return;
        }
        let Some(x) = self.data.as_ref().and_then(|(_, us)| us.iter().find(|u| u.sam.to_lowercase() == login)).cloned()
        else {
            cx.set_status(t("Login not found."));
            return;
        };
        let d = crate::pages::users::PasswordDialog::new(cx, x).after(|app| {
            app.with_shell(|s, cx| {
                s.home.login.clear();
                s.home.load(cx);
            });
        });
        cx.push(d);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use adm_core::model::PolicySummary;

    #[test]
    fn alerts() {
        let s = Summary { policy: PolicySummary { complexity: false, min_length: 7 }, ..Default::default() };
        let mut users: Vec<User> = (0..2)
            .map(|i| User {
                sam: format!("u{i}"),
                name: format!("U{i}"),
                locked: true,
                enabled: true,
                last_logon: Some(Utc::now()),
                ..Default::default()
            })
            .collect();
        users.push(User { sam: "idle".into(), enabled: true, ..Default::default() });
        let a = Home::alerts(&s, &users, 90);
        assert_eq!(a.len(), 4, "policy, one row per locked account, inactive");
        assert_eq!(a[0].link.as_ref().map(|l| l.1.clone()), Some(AlertGo::Nav(Nav::Policy)));
        assert_eq!(
            (a[1].text.as_str(), a[1].link.as_ref().map(|l| l.1.clone())),
            ("U0 (u0)", Some(AlertGo::Unlock("u0".into())))
        );
        assert_eq!(a[3].link.as_ref().map(|l| l.1.clone()), Some(AlertGo::Nav(Nav::Users(UserFilter::Inactive))));
        let ok = Summary { policy: PolicySummary { complexity: true, min_length: 8 }, ..Default::default() };
        assert!(Home::alerts(&ok, &[], 90).is_empty(), "nothing pending: the panel says so");
    }
}
