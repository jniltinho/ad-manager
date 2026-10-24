//! The domain's default password policy in a dialog (from the Users toolbar and Home's policy alert): it reads the
//! policy, validates the ranges, warns when it is weak and saves after a confirmation.

use adm_core::i18n::t;
use adm_core::model::Policy;
use adm_core::tf;
use egui::{RichText, Ui};
use reqwest::Method;

use crate::app::Cx;
use crate::modal::{Dialog, Flow, footer};
use crate::widgets;

/// The numeric fields: label, unit, range.
const FIELDS: [(&str, &str, i64, i64); 7] = [
    ("Minimum length", "characters", 0, 255),
    ("History", "passwords remembered (0-24)", 0, 24),
    ("Minimum age", "days", 0, 998),
    ("Maximum age", "days (0 = never expires)", 0, 999),
    ("Lock out after", "attempts (0 = never)", 0, 999),
    ("Lockout duration", "minutes", 0, 99999),
    ("Counting window", "minutes", 0, 99999),
];

/// Reads the policy and opens the dialog.
pub fn open(cx: &mut Cx) {
    let api = cx.panel.clone();
    cx.run(async move { api.get::<Policy>("/api/policy").await }, |app, p| {
        app.cx.push(PolicyDialog::new(&p));
    });
}

pub struct PolicyDialog {
    complexity: bool,
    values: [String; 7],
}

impl PolicyDialog {
    pub fn new(p: &Policy) -> Self {
        let v = [
            p.min_length,
            p.history,
            p.min_age_days,
            p.max_age_days,
            p.lockout_threshold,
            p.lockout_minutes,
            p.lockout_window_minutes,
        ];
        Self { complexity: p.complexity, values: v.map(|n| n.to_string()) }
    }

    /// The policy on the form, or the first field out of its range.
    pub fn read(&self) -> Result<Policy, String> {
        let mut n = [0i64; 7];
        for (i, (label, _, lo, hi)) in FIELDS.iter().enumerate() {
            match self.values[i].trim().parse::<i64>() {
                Ok(v) if (*lo..=*hi).contains(&v) => n[i] = v,
                _ => return Err(tf!("%s: enter a number from %d to %d.", t(label), lo, hi)),
            }
        }
        Ok(Policy {
            complexity: self.complexity,
            min_length: n[0],
            history: n[1],
            min_age_days: n[2],
            max_age_days: n[3],
            lockout_threshold: n[4],
            lockout_minutes: n[5],
            lockout_window_minutes: n[6],
        })
    }

    /// No complexity or fewer than 8 characters.
    pub fn weak(&self) -> bool {
        !self.complexity || self.values[0].trim().parse::<i64>().unwrap_or(0) < 8
    }
}

impl Dialog for PolicyDialog {
    fn title(&self) -> String {
        t("Domain's default password policy").into()
    }

    fn width(&self) -> f32 {
        640.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        ui.checkbox(&mut self.complexity, t("Require complexity (uppercase, lowercase, digit/symbol)"));
        widgets::form(ui, "policy", |ui| {
            for (i, (label, unit, _, _)) in FIELDS.iter().enumerate() {
                widgets::label(ui, &look, t(label));
                ui.horizontal(|ui| {
                    ui.add(egui::TextEdit::singleline(&mut self.values[i]).desired_width(100.0));
                    ui.label(RichText::new(t(unit)).color(look.c("fg-muted")));
                });
                ui.end_row();
            }
        });
        if self.weak() {
            ui.add(
                egui::Label::new(
                    RichText::new(t("Recommended: complexity on, at least 8 characters and lockout after 10 attempts — after the cutover to Samba."))
                        .color(look.c("warn")),
                )
                .wrap(),
            );
        }
        let f = footer(ui, cx, Some(t("Save policy")), "", false, true);
        if f.ok {
            match self.read() {
                Err(e) => cx.show_error_text(e),
                Ok(p) => {
                    cx.confirm(
                        t("Password policy"),
                        t("Change the domain's password policy? It applies to the next password changes.\nWhile there is a Windows DC, the Default Domain Policy may reapply the old values.").into(),
                        t("Change policy"),
                        false,
                        move |cx| {
                            let api = cx.panel.clone();
                            cx.run(async move { api.act_with(Method::PUT, "/api/policy", &p).await }, |app, ()| {
                                app.cx.set_status(t("Policy updated."));
                            });
                        },
                    );
                    return Flow::Close;
                }
            }
        }
        if f.cancel { Flow::Close } else { Flow::Keep }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn ranges_and_weak() {
        let mut d = PolicyDialog::new(&Policy { complexity: true, min_length: 8, history: 24, ..Default::default() });
        assert!(!d.weak());
        assert_eq!(d.read().map(|p| p.history), Ok(24));
        d.values[1] = "25".into();
        assert!(d.read().is_err());
        d.values[1] = "x".into();
        assert!(d.read().is_err());
        d.values[1] = "0".into();
        d.values[0] = "7".into();
        assert!(d.weak());
        d.values[0] = "8".into();
        d.complexity = false;
        assert!(d.weak());
    }
}
