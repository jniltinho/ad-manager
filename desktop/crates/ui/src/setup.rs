//! Configure: the panel URL, the account and the language, and the graphics backend (saved at once as `[graphics]` in
//! desktop.toml: it needs no panel test and applies at the next start). Test reads the certificate (one the system does not trust
//! is pinned once the operator accepts it), the panel health and signs in; Save writes desktop.toml (no password, no
//! token) and opens the app with that session. A test whose values were edited, retested or cancelled — also one that
//! finishes after that — is signed out.

use adm_core::api::{self, Api, ApiError};
use adm_core::config::{self, Config};
use adm_core::i18n::{self, t};
use adm_core::model::Me;
use adm_core::tf;
use adm_core::tls::PanelCert;
use egui::{Frame, Margin, RichText, Ui};
use secrecy::SecretString;
use zeroize::Zeroizing;

use crate::app::{App, Cx, Go, Screen};
use crate::widgets::{self, Kind};

const LANG_CODES: [&str; 3] = ["", "pt-BR", "en"];

enum Res {
    None,
    Testing,
    Ok(String),
    Err(ApiError),
    Untrusted(Box<PanelCert>),
}

/// Where Cancel returns.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Back {
    /// First run: there is nowhere to go back to.
    None,
    Login,
    /// The shell kept aside (opened from the header: a language change alone keeps the session).
    Shell,
}

pub struct Setup {
    back: Back,
    url: String,
    user: String,
    pwd: Zeroizing<String>,
    lang: usize,
    res: Res,
    /// A successful test: its signed-in client and session, for Save.
    tested: Option<(Api, Me)>,
    save_pin: String,
    /// The certificate the operator trusts (the config's, or one accepted here).
    accepted: String,
    /// Bumped by every edit and test: a test of older values never enables Save.
    generation: u64,
    write_err: Option<String>,
    focus: bool,
    pwd_id: egui::Id,
    /// Configure › Graphics: the index in `adm_core::gpu::choices`, and the result of saving it.
    gpu: usize,
    gpu_msg: Option<Result<(), String>>,
    /// Configure › Appearance could not be saved.
    look_err: Option<String>,
}

/// Test generations are unique across Configure screens: a test of a screen that was left never counts in a new one.
fn next_generation() -> u64 {
    static G: std::sync::atomic::AtomicU64 = std::sync::atomic::AtomicU64::new(1);
    G.fetch_add(1, std::sync::atomic::Ordering::Relaxed)
}

impl Setup {
    pub fn new(cx: &Cx, back: Back) -> Self {
        let c = &cx.cfg;
        Self {
            back,
            url: c.panel.url.clone(),
            user: c.panel.user.clone(),
            pwd: Zeroizing::default(),
            lang: LANG_CODES.iter().position(|l| *l == c.lang).unwrap_or(0),
            res: Res::None,
            tested: None,
            save_pin: String::new(),
            accepted: c.panel.fingerprint.clone(),
            generation: next_generation(),
            write_err: None,
            focus: true,
            pwd_id: crate::widgets::secret_id("setup-password"),
            gpu: {
                let saved = adm_core::gpu::choice(&cx.cfg);
                adm_core::gpu::choices(adm_core::gpu::Os::current()).iter().position(|b| *b == saved).unwrap_or(0)
            },
            gpu_msg: None,
            look_err: None,
        }
    }

    /// Configure › Graphics, as rustXterm's Settings › Display › Graphics: the rendering backend, saved at once as
    /// `[graphics]` in desktop.toml (only that changes: the rest stays as saved), with the same hint.
    /// Configure › Appearance: this computer's theme and flat look over the panel's, applied at once and saved into
    /// the configuration as it is on disk (on a first run, with the rest by Save).
    fn appearance_card(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        let order = adm_core::theme::order();
        let mut names = vec![t("Panel default").to_string()];
        names.extend(
            order
                .iter()
                .map(|id| adm_core::theme::palette(id).map_or_else(|| id.clone(), |(_, p)| t(&p.label).to_string())),
        );
        let a = &cx.cfg.appearance;
        let mut theme = order.iter().position(|id| *id == a.theme).map_or(0, |i| i + 1);
        let flats =
            [t("Panel default").to_string(), t("Flat (no gradients or shadows)").into(), t("With depth").into()];
        let mut flat = match a.flat.as_str() {
            "flat" => 1,
            "depth" => 2,
            _ => 0,
        };
        let mut changed = false;
        // theme and look side by side, the interface language under them (the Settings card's grid of two)
        let sw = ((ui.available_width() - 16.0) / 2.0).floor().max(1.0);
        ui.horizontal_top(|ui| {
            ui.spacing_mut().item_spacing.x = 16.0;
            changed |= widgets::labeled_select(ui, &look, t("Theme"), "setup-theme", &mut theme, &names, sw);
            changed |= widgets::labeled_select(ui, &look, t("Look"), "setup-flat", &mut flat, &flats, sw);
        });
        ui.add_space(12.0);
        let langs = [t("Panel default").to_string(), "Português".into(), "English".into()];
        widgets::labeled_select(ui, &look, t("Language"), "setup-lang", &mut self.lang, &langs, sw);
        ui.add_space(8.0);
        ui.add_space(4.0);
        let panel =
            adm_core::theme::palette(&cx.panel_theme.0).map(|(_, p)| t(&p.label).to_string()).unwrap_or_default();
        let note = if panel.is_empty() {
            t("Only on this computer; \"Panel default\" follows the panel's Settings.").to_string()
        } else {
            format!(
                "{} {}",
                t("Only on this computer; \"Panel default\" follows the panel's Settings."),
                tf!("The panel uses %s.", panel)
            )
        };
        widgets::muted(ui, &look, &note);
        if changed {
            let a = &mut cx.cfg.appearance;
            a.theme = if theme == 0 { String::new() } else { order[theme - 1].clone() };
            a.flat = ["", "flat", "depth"][flat.min(2)].to_string();
            cx.refresh_look();
            self.look_err = if cx.cfg_path.exists() {
                config::save(cx.platform.as_ref(), &cx.cfg_path, &cx.cfg).err().map(|e| e.to_string())
            } else {
                None
            };
        }
        if let Some(e) = &self.look_err {
            ui.label(RichText::new(format!("{}{e}", t("Could not write "))).color(look.c("err")));
        }
    }

    fn graphics_card(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        let os = adm_core::gpu::Os::current();
        let choices = adm_core::gpu::choices(os);
        let names: Vec<String> = choices.iter().map(|b| t(b.label(os)).to_string()).collect();
        let before = self.gpu;
        let w = ui.available_width();
        widgets::labeled_select(ui, &look, t("Rendering backend"), "setup-gpu", &mut self.gpu, &names, w);
        ui.add_space(4.0);
        if self.gpu != before
            && let Some(b) = choices.get(self.gpu)
        {
            adm_core::gpu::set_choice(&mut cx.cfg, *b);
            // saved at once into the configuration as it is on disk; on a first run, with the rest by Save
            self.gpu_msg = Some(if cx.cfg_path.exists() {
                config::save(cx.platform.as_ref(), &cx.cfg_path, &cx.cfg).map_err(|e| e.to_string())
            } else {
                Ok(())
            });
        }
        widgets::muted(ui, &look, t("Applies at the next start. After a driver crash AD Manager picks a safer one."));
        if let Some(Err(e)) = &self.gpu_msg {
            ui.label(
                RichText::new(format!("{}{e}", t("The graphics choice could not be saved: "))).color(look.c("err")),
            );
        }
    }

    /// Forgets the password field (its text and egui's undo history).
    pub fn forget(&mut self, ctx: &egui::Context) {
        self.pwd = Zeroizing::default();
        crate::widgets::forget_field(ctx, self.pwd_id);
    }

    fn base(&self) -> String {
        self.url.trim().trim_end_matches('/').to_string()
    }

    /// Signed in, with the configured URL and user and no password typed: only the language may change.
    fn same_account(&self, cx: &Cx) -> bool {
        self.base() == cx.cfg.panel.url && self.user.trim() == cx.cfg.panel.user && self.pwd.is_empty()
    }

    fn can_save(&self, cx: &Cx) -> bool {
        self.tested.is_some() || (self.back == Back::Shell && self.same_account(cx))
    }

    /// Ends the session of a successful test that will not be used.
    fn abandon(&mut self, cx: &Cx) {
        if let Some((c, _)) = self.tested.take() {
            cx.spawn(async move { c.logout().await }, |_, ()| {});
        }
    }

    fn edited(&mut self, cx: &Cx) {
        self.generation = next_generation();
        self.abandon(cx);
    }

    fn test(&mut self, cx: &Cx) {
        let base = self.base();
        let account = self.user.trim().to_string();
        if !check_url(&base) {
            self.res =
                Res::Err(ApiError::Other(format!("{}{}", t("Panel URL"), t(": enter an https://server:port URL"))));
            return;
        }
        if account.is_empty() || self.pwd.is_empty() {
            self.res = Res::Err(ApiError::App(
                "Enter the user and the password to test the sign-in (the password is not saved).",
            ));
            return;
        }
        self.res = Res::Testing;
        self.generation = next_generation(); // this attempt: an earlier one still running, or any edit after, never counts
        self.abandon(cx);
        let start = self.generation;
        let secret = SecretString::from(self.pwd.as_str());
        let pin = self.accepted.clone();
        let tried = pin.clone();
        cx.spawn(async move { api::test_panel(&base, &account, &secret, &tried).await }, move |app: &mut App, out| {
            let current = match &mut app.screen {
                Screen::Setup(s) if s.generation == start => Some(s),
                _ => None,
            };
            let Some(s) = current else {
                // edited, tested again or left meanwhile: this result is for older values
                if let Ok((c, _, _)) = out.result {
                    app.cx.spawn(async move { c.logout().await }, |_, ()| {});
                }
                return;
            };
            if let Some(cert) = &out.cert
                && !cert.trusted
                && cert.pin != pin
            {
                s.res = Res::Untrusted(Box::new(cert.clone()));
                return;
            }
            match out.result {
                Err(e) => s.res = Res::Err(e),
                Ok((client, h, me)) => {
                    s.save_pin = match &out.cert {
                        Some(c) if !c.trusted => c.pin.clone(),
                        _ => String::new(),
                    };
                    s.res = Res::Ok(tf!("OK: %s signed in to %s (panel %s)", me.user, me.domain, h.version));
                    s.tested = Some((client, me));
                }
            }
        });
    }

    fn save(&mut self, cx: &mut Cx) {
        let mut n = Config::default();
        n.panel.url = self.base();
        n.panel.user = self.user.trim().to_string();
        n.lang = LANG_CODES[self.lang.min(2)].to_string();
        n.graphics = cx.cfg.graphics.clone();
        n.appearance = cx.cfg.appearance.clone();
        if self.tested.is_none() {
            // signed in, same account: only the language changes, the session and the trust stay
            n.panel.fingerprint = cx.cfg.panel.fingerprint.clone();
            n.panel.insecure = cx.cfg.panel.insecure;
        } else {
            n.panel.fingerprint = self.save_pin.clone();
        }
        if let Err(e) = config::save(cx.platform.as_ref(), &cx.cfg_path, &n) {
            self.write_err = Some(format!("{}{}: {e}", t("Could not write "), cx.cfg_path.display()));
            return;
        }
        cx.cfg = n;
        // "Panel default": the session's default language (a new session sets it when the shell opens)
        let lang = if cx.cfg.lang.is_empty() { cx.me.settings.lang.clone() } else { cx.cfg.lang.clone() };
        i18n::set_lang(&lang);
        match self.tested.take() {
            None => cx.go = Some(Go::KeepShell),
            Some((client, me)) => {
                self.pwd = Zeroizing::default();
                cx.brand.catalog = false; // a panel's catalog: ask the one now configured
                cx.brand.for_url.clear(); // the next sign-in screen asks this panel for its brand
                cx.go = Some(Go::Shell(Box::new(client), Box::new(me)));
            }
        }
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        cx.title = t("AD Manager — configure").into();
        egui::CentralPanel::default().frame(Frame::new().fill(look.c("page"))).show(ui, |ui| {
            // the other screens' frame: app box, header, tab strip and footer; the cards in two columns as Settings
            let file = format!(
                "{}{}{}",
                t("File: "),
                cx.cfg_path.display(),
                t("  (another file: ad-manager-desktop --config path)")
            );
            let area = crate::shell::app_frame(
                ui,
                cx,
                t("Configure AD Manager desktop"),
                (crate::icons::SETTINGS, t("Configure")),
                &file,
            );
            let mut page =
                ui.new_child(egui::UiBuilder::new().max_rect(area).layout(egui::Layout::top_down(egui::Align::Min)));
            page.set_clip_rect(area.expand(20.0));
            let ui = &mut page;
            // only a very short window (or the certificate's details) scrolls
            egui::ScrollArea::vertical().max_height(area.height() - 48.0).auto_shrink([false, true]).show(ui, |ui| {
                let total = ui.available_width();
                // the web's Settings grid: the main column and a 520-point one at its right; stacked when narrow (the
                // web's xl breakpoint)
                if total < 900.0 {
                    ui.spacing_mut().item_spacing.y = 20.0;
                    widgets::card(ui, &look, t("Panel"), |ui| self.panel_card(ui, cx));
                    widgets::card(ui, &look, t("Appearance"), |ui| self.appearance_card(ui, cx));
                    widgets::card(ui, &look, t("Graphics"), |ui| self.graphics_card(ui, cx));
                    return;
                }
                let right_w = 520.0_f32.min((total * 0.45).floor()).max(1.0);
                ui.horizontal_top(|ui| {
                    ui.spacing_mut().item_spacing.x = 20.0;
                    let col = egui::Layout::top_down(egui::Align::Min);
                    ui.allocate_ui_with_layout(egui::vec2((total - right_w - 20.0).max(1.0), 0.0), col, |ui| {
                        ui.set_width((total - right_w - 20.0).max(1.0));
                        widgets::card(ui, &look, t("Panel"), |ui| self.panel_card(ui, cx));
                    });
                    ui.allocate_ui_with_layout(egui::vec2(right_w, 0.0), col, |ui| {
                        ui.set_width(right_w);
                        ui.spacing_mut().item_spacing.y = 20.0;
                        widgets::card(ui, &look, t("Appearance"), |ui| self.appearance_card(ui, cx));
                        widgets::card(ui, &look, t("Graphics"), |ui| self.graphics_card(ui, cx));
                    });
                });
            });
            if let Some(e) = &self.write_err {
                ui.label(RichText::new(e).color(look.c("err")));
            }
            ui.add_space(12.0);
            // the buttons at the right (the file is in the footer)
            ui.horizontal(|ui| {
                ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                    if self.back != Back::None && widgets::button(ui, &look, "", t("Cancel"), Kind::Secondary).clicked()
                    {
                        self.forget(ui.ctx());
                        self.abandon(cx);
                        self.generation = next_generation(); // a test still running is signed out when it ends
                        cx.go = Some(if self.back == Back::Shell { Go::KeepShell } else { Go::Login });
                    }
                    let can = self.can_save(cx);
                    if ui
                        .add_enabled_ui(can, |ui| {
                            widgets::button(ui, &look, crate::icons::CHECK, t("Save"), Kind::Primary)
                        })
                        .inner
                        .clicked()
                    {
                        self.save(cx);
                    }
                });
            });
        });
    }

    fn panel_card(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        let mut changed = false;
        // the web's form: labels above, 12 points between fields; the URL on its row, user and password side by side
        let w = ui.available_width();
        let half = ((w - 16.0) / 2.0).floor().max(1.0);
        let url = widgets::above(ui, &look, t("Panel URL"), w, |ui, _| {
            ui.add(
                crate::widgets::line(&mut self.url)
                    .hint_text(crate::widgets::hint("https://panel.example.local:8443"))
                    .desired_width(w),
            )
        });
        changed |= url.changed();
        ui.add_space(12.0);
        let (pwd_id, focus) = (self.pwd_id, std::mem::take(&mut self.focus));
        ui.horizontal_top(|ui| {
            ui.spacing_mut().item_spacing.x = 16.0;
            let user = widgets::above(ui, &look, t("User"), half, |ui, _| {
                ui.add(
                    crate::widgets::line(&mut self.user)
                        .hint_text(crate::widgets::hint("administrator"))
                        .desired_width(half),
                )
            });
            changed |= user.changed();
            let pwd = &mut self.pwd;
            let r = widgets::above(ui, &look, t("Password"), half, |ui, _| {
                crate::widgets::secret_field(ui, pwd_id, pwd, "", half)
            });
            if focus {
                r.request_focus();
            }
            changed |= r.changed();
        });
        ui.add_space(12.0);
        if changed {
            self.edited(cx);
        }
        widgets::muted(
            ui,
            &look,
            t(
                "Test signs in with the account (Domain Admins). The password is used only for the test and the session; it is never saved.",
            ),
        );
        ui.add_space(4.0);
        if widgets::button(ui, &look, crate::icons::SEARCH, t("Test connection"), Kind::Secondary).clicked() {
            self.test(cx);
        }
        let mut trust = None;
        match &self.res {
            Res::None => {}
            Res::Testing => {
                ui.label(RichText::new(t("Testing…")).color(look.c("fg-muted")));
            }
            Res::Ok(s) => {
                ui.label(RichText::new(s).color(look.c("ok")));
            }
            Res::Err(e) => {
                ui.add(egui::Label::new(RichText::new(format!("{}{e}", t("Failed: "))).color(look.c("err"))).wrap());
            }
            Res::Untrusted(c) => {
                ui.label(RichText::new(t("Certificate not trusted")).color(look.c("err")));
                widgets::wrapped(
                    ui,
                    t(
                        "This computer does not trust the panel's certificate (self-signed). Check its fingerprint with the panel's administrator before trusting it:",
                    ),
                );
                cert_details(ui, &look, c);
                if widgets::button(ui, &look, crate::icons::CHECK, t("Trust this certificate"), Kind::Secondary)
                    .clicked()
                {
                    trust = Some(c.pin.clone());
                }
            }
        }
        if let Some(pin) = trust {
            self.accepted = pin;
            self.test(cx);
        }
    }
}

/// A certificate for the operator to compare: fingerprint, subject, issuer and validity.
fn cert_details(ui: &mut Ui, look: &crate::style::Look, c: &PanelCert) {
    let date = |d| adm_core::i18n::date(d);
    Frame::new().inner_margin(Margin::same(4)).show(ui, |ui| {
        widgets::form(ui, "cert", |ui| {
            for (k, v) in [
                (t("Fingerprint"), c.pin.trim_start_matches("sha256:").to_string()),
                (t("Subject"), c.subject.clone()),
                (t("Issuer"), c.issuer.clone()),
                (t("Valid"), format!("{} – {}", date(c.not_before), date(c.not_after))),
            ] {
                widgets::label(ui, look, k);
                widgets::selectable(ui, &v);
                ui.end_row();
            }
        });
    });
}

/// Only https:// URLs with a host (the panel refuses plain HTTP outside loopback anyway).
pub fn check_url(s: &str) -> bool {
    adm_core::tls::https_host(s).is_ok()
}

#[cfg(test)]
mod tests {
    #[test]
    fn urls() {
        assert!(super::check_url("https://panel.domain.local:8443"));
        assert!(!super::check_url("http://panel:8443"));
        assert!(!super::check_url("panel"));
    }
}
