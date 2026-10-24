//! The sign-in screen, like the web login: the theme's box at 10% of the window height with its stripe, the logo
//! (the company's or AD Manager's), "ACTIVE DIRECTORY" with NETBIOS (domain), the fields, the message under the box
//! (at most 4 lines), the hint with the user formats, the panel URL and Configure.

use adm_core::api::ApiError;
use adm_core::i18n::{self, t};
use adm_core::tf;
use egui::{Frame, Margin, RichText, Ui};
use secrecy::SecretString;
use zeroize::Zeroizing;

use crate::app::{App, Cx, Go, Screen};
use crate::logo::{self, Logo};
use crate::widgets::{self, Kind};

const BOX_W: f32 = 480.0;
const MSG_LINES: usize = 4;

/// The panel's public brand (`/api/health`), kept across sign-in screens of the same panel.
#[derive(Default, Clone)]
pub struct PublicBrand {
    pub for_url: String,
    pub theme: String,
    pub domain: String,
    pub netbios: String,
    pub logo: Option<Logo>,
    pub setup: bool,
    pub catalog: bool,
}

enum Msg {
    Info(&'static str),
    Err(ApiError),
}

pub struct Login {
    user: String,
    pwd: Zeroizing<String>,
    pwd_id: egui::Id,
    msg: Option<Msg>,
    signing: bool,
    focus: bool,
}

impl Login {
    pub fn new(cx: &mut Cx) -> Self {
        let url = cx.cfg.panel.url.clone();
        if cx.brand.for_url != url {
            cx.brand = PublicBrand { for_url: url.clone(), ..Default::default() };
            fetch_brand(cx, url.clone());
        }
        cx.ensure_catalog();
        let theme = cx.brand.theme.clone();
        cx.set_look(if theme.is_empty() { "default" } else { &theme });
        Self {
            user: cx.cfg.panel.user.clone(),
            pwd: Zeroizing::default(),
            pwd_id: widgets::secret_id("login-password"),
            msg: None,
            signing: false,
            focus: true,
        }
    }

    /// The password field's id (tests).
    pub fn password_field_id(&self) -> egui::Id {
        self.pwd_id
    }

    /// Forgets the password field (its text and egui's undo history).
    pub fn forget(&mut self, ctx: &egui::Context) {
        self.pwd = Zeroizing::default();
        widgets::forget_field(ctx, self.pwd_id);
    }

    fn sign_in(&mut self, cx: &mut Cx) {
        if self.signing {
            return;
        }
        self.signing = true;
        self.msg = Some(Msg::Info("Signing in…"));
        let client = cx.panel.clone();
        let user = self.user.trim().to_string();
        let secret = SecretString::from(self.pwd.as_str());
        cx.spawn(async move { client.login(&user, &secret).await }, |app: &mut App, r| {
            let Screen::Login(l) = &mut app.screen else { return };
            l.signing = false;
            match r {
                Ok(me) => {
                    l.forget(&app.cx.ctx); // never keep the password around
                    let api = app.cx.panel.clone();
                    app.cx.go = Some(Go::Shell(Box::new(api), Box::new(me)));
                }
                Err(e) => {
                    l.msg = Some(Msg::Err(e));
                    l.focus = true;
                }
            }
        });
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, modal: bool) {
        let look = cx.look.clone();
        egui::CentralPanel::default().frame(Frame::new().fill(look.c("login-page"))).show(ui, |ui| {
            let h = ui.available_height();
            ui.vertical_centered(|ui| {
                ui.add_space((h * 0.1 - 26.0).max(0.0));
                ui.set_max_width(BOX_W);
                // the box
                Frame::new().fill(look.c("login-box")).stroke(egui::Stroke::new(1.0, look.c("login-box-line"))).show(
                    ui,
                    |ui| {
                        ui.set_width(BOX_W);
                        ui.spacing_mut().item_spacing.y = 0.0;
                        let (r, _) = ui.allocate_exact_size(egui::vec2(BOX_W, 4.0), egui::Sense::hover());
                        ui.painter().rect_filled(r, 0.0, look.c("login-box-bar"));
                        Frame::new().inner_margin(Margin::same(18)).show(ui, |ui| {
                            ui.spacing_mut().item_spacing.y = 8.0;
                            ui.horizontal(|ui| {
                                let fallback = if adm_core::theme::is_dark(adm_core::theme::Rgba(
                                    look.c("login-box").to_srgba_unmultiplied(),
                                )) {
                                    logo::product_white()
                                } else {
                                    logo::product_navy()
                                };
                                let img = cx.brand.logo.as_ref().map_or(fallback, Logo::image);
                                ui.add(img.fit_to_exact_size(egui::vec2(195.0, 65.0)));
                                ui.with_layout(egui::Layout::top_down(egui::Align::Max), |ui| {
                                    ui.add_space(12.0);
                                    ui.label(
                                        RichText::new("ACTIVE DIRECTORY")
                                            .strong()
                                            .size(12.0)
                                            .color(look.c("login-title")),
                                    );
                                    if !cx.brand.domain.is_empty() {
                                        let nb = if cx.brand.netbios.is_empty() {
                                            netbios_of(&cx.brand.domain)
                                        } else {
                                            cx.brand.netbios.clone()
                                        };
                                        ui.add(
                                            egui::Label::new(
                                                RichText::new(format!("{nb} ({})", cx.brand.domain))
                                                    .monospace()
                                                    .size(11.0)
                                                    .color(look.c("on-login-box-muted")),
                                            )
                                            .truncate(),
                                        );
                                    }
                                });
                            });
                            ui.add_space(10.0);
                            let ink = look.c("on-login-box");
                            egui::Grid::new("login-form").num_columns(2).spacing([10.0, 8.0]).show(ui, |ui| {
                                let l = ui.label(RichText::new(t("User")).size(13.0).color(ink));
                                ui.add(egui::TextEdit::singleline(&mut self.user).desired_width(320.0))
                                    .labelled_by(l.id);
                                ui.end_row();
                                let l = ui.label(RichText::new(t("Password")).size(13.0).color(ink));
                                let r =
                                    widgets::secret_field(ui, self.pwd_id, &mut self.pwd, "", 320.0).labelled_by(l.id);
                                if std::mem::take(&mut self.focus) && !modal {
                                    r.request_focus();
                                }
                                if r.lost_focus() && ui.input(|i| i.key_pressed(egui::Key::Enter)) && !modal {
                                    self.sign_in(cx);
                                }
                                ui.end_row();
                            });
                            ui.vertical_centered(|ui| {
                                let b = ui
                                    .add_enabled_ui(!self.signing, |ui| {
                                        widgets::button(ui, &look, "🔑", t("Sign in"), Kind::Brand)
                                    })
                                    .inner;
                                if b.clicked() {
                                    self.sign_in(cx);
                                }
                            });
                        });
                    },
                );
                // the message under the box: "signing in" or the error, with a bar in the state's color
                if let Some(m) = &self.msg {
                    let (text, bar) = match m {
                        Msg::Info(s) => (t(s).to_string(), look.c("accent")),
                        Msg::Err(e) => (e.to_string(), look.c("err")),
                    };
                    ui.add_space(4.0);
                    Frame::new().fill(look.c("surface")).inner_margin(Margin::symmetric(10, 6)).show(ui, |ui| {
                        ui.set_width(BOX_W - 20.0);
                        ui.horizontal(|ui| {
                            let (r, _) = ui.allocate_exact_size(egui::vec2(4.0, 18.0), egui::Sense::hover());
                            ui.painter().rect_filled(r, 0.0, bar);
                            let mut job = egui::text::LayoutJob::simple(
                                text,
                                egui::FontId::proportional(12.0),
                                look.c("fg"),
                                BOX_W - 40.0,
                            );
                            job.wrap.max_rows = MSG_LINES;
                            ui.label(job);
                        });
                    });
                }
                // the graphics start fell back: said before anyone signs in
                if let Some(n) = &cx.gpu_notice {
                    ui.add_space(4.0);
                    Frame::new().fill(look.c("surface")).inner_margin(Margin::symmetric(10, 6)).show(ui, |ui| {
                        ui.set_width(BOX_W - 20.0);
                        ui.add(egui::Label::new(RichText::new(n).size(12.0).color(look.c("warn"))).wrap());
                    });
                }
                ui.add_space(8.0);
                let dom = if cx.brand.domain.is_empty() { t("domain").to_string() } else { cx.brand.domain.clone() };
                let nb = if cx.brand.netbios.is_empty() { netbios_of(&dom) } else { cx.brand.netbios.clone() };
                let muted = look.c("fg-muted");
                for s in [
                    tf!("Use %s, %s@%s or %s\\%s.", t("user"), t("user"), dom, nb, t("user")),
                    t("Domain administrators only; every action is audited.").to_string(),
                ] {
                    let mut job = egui::text::LayoutJob::simple(s, egui::FontId::proportional(12.0), muted, BOX_W);
                    job.wrap.max_rows = 2;
                    job.halign = egui::Align::Center;
                    ui.label(job);
                }
                ui.add(egui::Label::new(RichText::new(&cx.cfg.panel.url).size(11.0).color(muted)).truncate());
                if widgets::button(ui, &look, "⚙", t("Configure"), Kind::Link).clicked() {
                    cx.go = Some(Go::Setup);
                }
            });
        });
        cx.title = t("AD Manager — sign in").into();
    }
}

/// The NETBIOS name a domain would have: its first label in capitals.
pub fn netbios_of(domain: &str) -> String {
    domain.split('.').next().unwrap_or_default().to_uppercase()
}

/// Reads the panel's public brand (theme, domain, logo, default language); a late answer only changes a sign-in
/// screen of the same panel still showing.
fn fetch_brand(cx: &Cx, url: String) {
    let api = cx.panel.clone();
    cx.spawn(
        async move {
            let h = api.health().await.ok()?;
            let logo = if h.brand.logo { api.image("/api/brand/logo").await.ok() } else { None };
            Some((h, logo))
        },
        move |app, r| {
            let Some((h, logo)) = r else { return };
            if app.cx.cfg.panel.url != url {
                return;
            }
            let b = &mut app.cx.brand;
            b.theme = h.brand.theme.clone();
            b.domain = h.domain.clone();
            b.netbios = h.netbios.clone();
            b.logo = logo.and_then(|(bytes, ct)| Logo::decode(bytes, &ct));
            let was_setup = std::mem::replace(&mut b.setup, h.setup);
            if app.cx.cfg.lang.is_empty() && !h.brand.lang.is_empty() {
                i18n::set_lang(&h.brand.lang);
            }
            if matches!(app.screen, Screen::Login(_)) {
                app.cx.set_look(&h.brand.theme);
                if h.setup && !was_setup {
                    app.cx.show_info(
                        t("Panel being set up"),
                        tf!("The panel at %s is being set up: finish its setup wizard in a browser, with the token from the server (sudo cat /opt/ad-manager/data/setup-token). Then sign in here.", url),
                    );
                }
            }
        },
    );
}

#[cfg(test)]
mod tests {
    #[test]
    fn netbios() {
        assert_eq!(super::netbios_of("domain.local"), "DOMAIN");
    }
}
