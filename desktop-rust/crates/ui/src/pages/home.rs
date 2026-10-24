//! Home: six tiles (enabled, disabled, locked, inactive, groups, computers), the alerts with a link to the page that
//! solves them, and the quick password reset / unlock by login.

use adm_core::api::esc;
use adm_core::i18n::t;
use adm_core::logic::{Tone, inactive};
use adm_core::model::{Summary, User};
use adm_core::tf;
use chrono::Utc;
use egui::{RichText, Ui};

use crate::app::Cx;
use crate::shell::{Nav, Page};
use crate::widgets::{self, Kind};

#[derive(Default)]
pub struct Home {
    data: Option<(Summary, Vec<User>)>,
    login: String,
    loaded: bool,
    generation: u64,
}

/// One alert row: tag, tone role, text, link and where it goes.
pub struct Alert {
    pub tag: String,
    pub tone: Tone,
    pub text: String,
    pub link: Option<(String, Nav)>,
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

    /// The alerts for a summary and its users (the first three locked names listed).
    pub fn alerts(s: &Summary, users: &[User], days: i64) -> Vec<Alert> {
        let now = Utc::now();
        let locked: Vec<String> =
            users.iter().filter(|u| u.locked).map(|u| format!("{} ({})", u.name, u.sam)).collect();
        let idle = users.iter().filter(|u| inactive(u, days, now)).count();
        let mut rows = Vec::new();
        if !s.policy.complexity || s.policy.min_length < 8 {
            let msg = if s.policy.complexity {
                "Password policy: minimum length of %d characters."
            } else {
                "Password policy: minimum length of %d characters, no complexity."
            };
            rows.push(Alert {
                tag: t("policy").into(),
                tone: Tone::Warn,
                text: tf!(msg, s.policy.min_length),
                link: Some((t("see ›").into(), Nav::Policy)),
            });
        }
        if !locked.is_empty() {
            let mut names: Vec<String> = locked.iter().take(3).cloned().collect();
            if locked.len() > 3 {
                names.push("…".into());
            }
            rows.push(Alert {
                tag: t("Locked").into(),
                tone: Tone::Err,
                text: tf!("%d locked account(s): %s.", locked.len(), names.join(", ")),
                link: Some((t("unlock ›").into(), Nav::To(Page::Users))),
            });
        }
        if idle > 0 {
            rows.push(Alert {
                tag: t("inactive").into(),
                tone: Tone::Muted,
                text: tf!("%d enabled account(s) with no logon for more than %d days.", idle, days),
                link: Some((t("review ›").into(), Nav::To(Page::Users))),
            });
        }
        if rows.is_empty() {
            rows.push(Alert { tag: "ok".into(), tone: Tone::Ok, text: t("Nothing pending.").into(), link: None });
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
        let (mut quick, mut refresh) = (false, false);
        ui.columns(6, |cols| {
            let muted = look.c("fg-muted");
            let tiles: [(String, String, egui::Color32, egui::Color32); 6] = [
                (t("Enabled users").to_uppercase(), n("enabled"), look.c("ok"), look.c("decor-ok")),
                (t("Disabled").to_uppercase(), n("disabled"), muted, look.c("decor-muted")),
                (
                    t("Locked").to_uppercase(),
                    n("locked"),
                    if locked > 0 { look.c("err") } else { muted },
                    if locked > 0 { look.c("decor-err") } else { look.c("decor-muted") },
                ),
                (
                    tf!("Inactive %d+ days", days).to_uppercase(),
                    if s.is_some() { idle.to_string() } else { "…".into() },
                    if idle > 0 { look.c("warn") } else { muted },
                    if idle > 0 { look.c("decor-warn") } else { look.c("decor-muted") },
                ),
                (
                    t("Groups").to_uppercase(),
                    s.map_or("…".into(), |s| s.groups.to_string()),
                    look.c("fg-strong"),
                    look.c("decor-strong"),
                ),
                (
                    t("Computers").to_uppercase(),
                    s.map_or("…".into(), |s| s.computers.to_string()),
                    look.c("fg-strong"),
                    look.c("decor-strong"),
                ),
            ];
            for (c, (label, value, ink, decor)) in cols.iter_mut().zip(tiles) {
                widgets::stat_tile(c, &look, &label, &value, ink, decor);
            }
        });
        ui.add_space(8.0);
        ui.columns(2, |c| {
            widgets::card(&mut c[0], &look, t("Reset password / unlock"), |ui| {
                ui.horizontal(|ui| {
                    let r = ui.add(
                        egui::TextEdit::singleline(&mut self.login)
                            .hint_text(t("user login"))
                            .desired_width(ui.available_width() - 110.0),
                    );
                    let enter = r.lost_focus() && ui.input(|i| i.key_pressed(egui::Key::Enter));
                    quick = widgets::button(ui, &look, "👤", t("Open"), Kind::Primary).clicked() || enter;
                });
                ui.label(
                    RichText::new(t("Set a password, send a reset link or unlock the account."))
                        .size(11.0)
                        .color(look.c("fg-muted")),
                );
            });
            widgets::card(&mut c[1], &look, t("Alerts"), |ui| {
                match &alerts {
                    None => {
                        ui.label(t("Loading…"));
                    }
                    Some(alerts) => {
                        // as the web: a colored tag, the text (one line, cut with "…") and a link to the page that solves it
                        for a in alerts {
                            ui.horizontal(|ui| {
                                widgets::tag(ui, &look, &a.tag.to_uppercase(), a.tone);
                                let room = (ui.available_width() - 90.0).max(40.0);
                                ui.allocate_ui(egui::vec2(room, 20.0), |ui| {
                                    ui.set_width(room);
                                    ui.add(egui::Label::new(&a.text).truncate());
                                })
                                .response
                                .on_hover_text(&a.text);
                                if let Some((label, to)) = &a.link
                                    && widgets::button(ui, &look, "", label, Kind::Link).clicked()
                                {
                                    nav = Some(*to);
                                }
                            });
                        }
                    }
                }
                ui.horizontal(|ui| {
                    refresh = widgets::icon_button(ui, &look, "⟳", t("Refresh")).clicked();
                });
            });
        });
        if quick {
            self.quick(cx);
        }
        if refresh {
            self.load(cx);
        }
        nav
    }

    /// Opens the password dialog of the typed login.
    fn quick(&mut self, cx: &mut Cx) {
        let login = self.login.trim().to_string();
        if login.is_empty() {
            return;
        }
        let api = cx.panel.clone();
        let path = format!("/api/users/{}", esc(&login));
        cx.run(async move { api.get::<User>(&path).await }, |app, x| {
            let d = crate::pages::users::PasswordDialog::new(&app.cx, x).after(|app| {
                app.with_shell(|s, cx| {
                    s.home.login.clear();
                    s.home.load(cx);
                });
            });
            app.cx.push(d);
        });
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use adm_core::model::PolicySummary;

    #[test]
    fn alerts() {
        let s = Summary { policy: PolicySummary { complexity: false, min_length: 7 }, ..Default::default() };
        let mut users: Vec<User> = (0..5)
            .map(|i| User {
                sam: format!("u{i}"),
                name: format!("U{i}"),
                locked: i < 4,
                enabled: true,
                last_logon: Some(Utc::now()),
                ..Default::default()
            })
            .collect();
        users.push(User { sam: "idle".into(), enabled: true, ..Default::default() });
        let a = Home::alerts(&s, &users, 90);
        assert_eq!(a.len(), 3);
        assert_eq!(a[0].link.as_ref().map(|l| l.1), Some(Nav::Policy));
        assert!(a[1].text.starts_with("4 locked account(s): U0 (u0), U1 (u1), U2 (u2), …"), "{}", a[1].text);
        assert_eq!(a[2].link.as_ref().map(|l| l.1), Some(Nav::To(Page::Users)));
        let ok = Summary { policy: PolicySummary { complexity: true, min_length: 8 }, ..Default::default() };
        assert_eq!(Home::alerts(&ok, &[], 90)[0].text, "Nothing pending.");
    }
}
