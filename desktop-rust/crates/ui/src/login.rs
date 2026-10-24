//! The sign-in screen, like the web login: the page's pattern, the theme's box at 10% of the window height with its
//! stripe and shadow, the logo (the company's or AD Manager's), "ACTIVE DIRECTORY" with NETBIOS (domain), the fields,
//! the elliptic shadow under the box, the error (at most 4 lines), the hint with the user formats, the languages, and
//! the desktop's panel URL and Configure.

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
    pub flat: bool,
    pub domain: String,
    pub netbios: String,
    pub logo: Option<Logo>,
    pub setup: bool,
    pub catalog: bool,
}

enum Msg {
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
        cx.panel_look(&theme, cx.brand.flat);
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
        self.msg = None;
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
            look.page_pattern(ui.painter(), ui.max_rect());
            let h = ui.available_height();
            ui.vertical_centered(|ui| {
                ui.spacing_mut().item_spacing.y = 0.0;
                ui.add_space((h * 0.1).floor());
                ui.set_max_width(BOX_W);
                self.login_box(ui, cx, &look, modal);
                // the elliptic shadow under the box (a light glow on dark pages), 3 points over its bottom
                let shadow = match look.p.text("login-shadow") {
                    _ if look.flat => None,
                    "dark" => Some(("bytes://login-shadow.png", adm_core::assets::LOGIN_SHADOW)),
                    "light" => Some(("bytes://login-shadow-light.png", adm_core::assets::LOGIN_SHADOW_LIGHT)),
                    _ => None,
                };
                let top = ui.cursor().top() - 3.0;
                if let Some((uri, bytes)) = shadow {
                    let r = egui::Rect::from_center_size(
                        egui::pos2(ui.max_rect().center().x, top + 5.0),
                        egui::vec2(550.0, 10.0),
                    );
                    egui::Image::from_bytes(uri, bytes).paint_at(ui, r);
                }
                ui.add_space(7.0);
                self.under_box(ui, cx, &look);
            });
        });
        cx.title = t("AD Manager — sign in").into();
    }

    /// The web's .box-inner: the stripe, the logo with "ACTIVE DIRECTORY" and NETBIOS (domain), the user and password
    /// rows (an 83-point label column, 326 × 25 fields) and the sign-in button.
    fn login_box(&mut self, ui: &mut Ui, cx: &mut Cx, look: &crate::style::Look, modal: bool) {
        let under = ui.painter().add(egui::Shape::Noop);
        let r = Frame::new()
            .stroke(egui::Stroke::new(1.0, look.c("login-box-line")))
            .inner_margin(Margin { left: 24, right: 24, top: 14, bottom: 24 })
            .show(ui, |ui| {
                ui.set_width(BOX_W - 50.0);
                ui.set_min_height(267.0 - 40.0);
                ui.spacing_mut().item_spacing.y = 0.0;
                ui.horizontal(|ui| {
                    let fallback =
                        if adm_core::theme::is_dark(adm_core::theme::Rgba(look.c("login-box").to_srgba_unmultiplied()))
                        {
                            logo::product_white(ui, egui::vec2(195.0, 65.0))
                        } else {
                            logo::product_navy(ui, egui::vec2(195.0, 65.0))
                        };
                    let img = cx.brand.logo.as_ref().map_or(fallback, |l| l.image(ui, egui::vec2(195.0, 65.0)));
                    ui.add(img.fit_to_exact_size(egui::vec2(195.0, 65.0)));
                    ui.with_layout(egui::Layout::bottom_up(egui::Align::Max), |ui| {
                        ui.add_space(6.0);
                        if !cx.brand.domain.is_empty() {
                            let nb = if cx.brand.netbios.is_empty() {
                                netbios_of(&cx.brand.domain)
                            } else {
                                cx.brand.netbios.clone()
                            };
                            let mut job = egui::text::LayoutJob::default();
                            let mono = egui::FontId::monospace(11.0);
                            job.append(&nb, 0.0, egui::TextFormat::simple(mono.clone(), look.c("on-login-box-strong")));
                            job.append(
                                &format!(" ({})", cx.brand.domain),
                                0.0,
                                egui::TextFormat::simple(mono, look.c("on-login-box-faint")),
                            );
                            ui.add(egui::Label::new(job).truncate());
                            ui.add_space(2.0);
                        }
                        // 11 px bold capitals spaced 0.06 em, as the web's .ad-title
                        let mut job = egui::text::LayoutJob::default();
                        let mut fmt = egui::TextFormat::simple(
                            egui::FontId::new(11.0, egui::FontFamily::Name(crate::fonts::SEMIBOLD.into())),
                            look.c("login-title"),
                        );
                        fmt.extra_letter_spacing = 0.66;
                        job.append("ACTIVE DIRECTORY", 0.0, fmt);
                        ui.label(job);
                    });
                });
                ui.add_space(23.0);
                let ink = look.c("on-login-box");
                let row = |ui: &mut Ui,
                           label: &str,
                           add: &mut dyn FnMut(&mut Ui, egui::Id) -> egui::Response|
                 -> egui::Response {
                    ui.add_space(2.0);
                    ui.horizontal(|ui| {
                        ui.spacing_mut().item_spacing.x = 0.0;
                        ui.add_space(2.0);
                        let (cell, _) = ui.allocate_exact_size(egui::vec2(83.0, 41.0), egui::Sense::hover());
                        let g = ui.painter().layout_no_wrap(label.into(), egui::FontId::proportional(12.0), ink);
                        let at = egui::pos2(cell.right() - 12.0 - g.size().x, cell.center().y - g.size().y / 2.0);
                        let id = ui.id().with(("login-label", label));
                        ui.painter().galley(at, g, ink);
                        ui.add_space(2.0 + 8.0);
                        let r = add(ui, id);
                        // the label is painted: the field carries its name for screen readers
                        let _ = ui.ctx().accesskit_node_builder(r.id, |n| n.set_label(label.to_string()));
                        r
                    })
                    .inner
                };
                let user = &mut self.user;
                row(ui, t("User"), &mut |ui, _| field(ui, look, egui::TextEdit::singleline(user)));
                let pwd_id = self.pwd_id;
                let pwd = &mut self.pwd;
                let r = row(ui, t("Password"), &mut |ui, _| {
                    field(ui, look, egui::TextEdit::singleline(&mut **pwd).id(pwd_id).password(true))
                });
                if std::mem::take(&mut self.focus) && !modal {
                    r.request_focus();
                }
                let enter = r.lost_focus() && ui.input(|i| i.key_pressed(egui::Key::Enter)) && !modal;
                ui.add_space(24.0);
                let signing = self.signing;
                let b = ui
                    .vertical_centered(|ui| {
                        ui.add_enabled_ui(!signing, |ui| {
                            widgets::busy_button(ui, look, t("Sign in"), t("Signing in…"), Kind::Brand, signing)
                        })
                        .inner
                    })
                    .inner;
                ui.add_space(12.0);
                if b.clicked() || enter {
                    self.sign_in(cx);
                }
            })
            .response;
        // the box: its fill, the shadow, and the 4-point stripe on top
        let rect = r.rect;
        let mut shapes = crate::depth::box_shapes(rect, None, look.c("login-box"), look.shadows("login-box"));
        shapes.push(egui::Shape::rect_filled(
            egui::Rect::from_min_size(rect.min, egui::vec2(rect.width(), 4.0)),
            0.0,
            look.c("login-box-bar"),
        ));
        ui.painter().set(under, egui::Shape::Vec(shapes));
    }

    /// The web's .box-bottom: the error, the hint with the user formats, the languages; then the desktop's panel
    /// address and Configure.
    fn under_box(&mut self, ui: &mut Ui, cx: &mut Cx, look: &crate::style::Look) {
        if let Some(Msg::Err(e)) = &self.msg {
            ui.add_space(6.0);
            let r = Frame::new()
                .fill(look.c("err-soft"))
                .inner_margin(Margin { left: 14, right: 10, top: 6, bottom: 6 })
                .show(ui, |ui| {
                    ui.set_width(BOX_W - 24.0);
                    let mut job = egui::text::LayoutJob::simple(
                        e.to_string(),
                        egui::FontId::proportional(12.0),
                        look.c("on-err-soft"),
                        BOX_W - 24.0,
                    );
                    job.wrap.max_rows = MSG_LINES;
                    ui.label(job);
                })
                .response;
            ui.painter().rect_filled(
                egui::Rect::from_min_size(r.rect.min, egui::vec2(4.0, r.rect.height())),
                0.0,
                look.c("toast-err"),
            );
        }
        for n in cx.gpu_notes.iter().filter(|n| matches!(n, crate::app::GpuNote::Fallback(..))) {
            ui.add_space(6.0);
            Frame::new().fill(look.c("surface")).inner_margin(Margin::symmetric(10, 6)).show(ui, |ui| {
                ui.set_width(BOX_W - 20.0);
                ui.add(egui::Label::new(RichText::new(n.text()).size(12.0).color(look.c("fg"))).wrap());
            });
        }
        // Use user, user@domain or NETBIOS\user — the formats in monospace, as the web's <code>
        ui.add_space(4.0);
        let dom = if cx.brand.domain.is_empty() { t("domain").to_string() } else { cx.brand.domain.clone() };
        let nb = if cx.brand.netbios.is_empty() { netbios_of(&dom) } else { cx.brand.netbios.clone() };
        let (muted, strong) = (look.c("fg-muted"), look.c("fg-strong"));
        let mut job = egui::text::LayoutJob::default();
        let text = egui::TextFormat {
            line_height: Some(16.5),
            ..egui::TextFormat::simple(egui::FontId::proportional(11.0), muted)
        };
        let code = egui::TextFormat {
            line_height: Some(16.5),
            ..egui::TextFormat::simple(egui::FontId::monospace(10.5), strong)
        };
        let u = t("user");
        for (s, f) in [
            (format!("{} ", t("Use")), &text),
            (u.to_string(), &code),
            (", ".into(), &text),
            (format!("{u}@{dom}"), &code),
            (format!(" {} ", t("or")), &text),
            (format!("{nb}\\{u}"), &code),
            (".".into(), &text),
        ] {
            job.append(&s, 0.0, f.clone());
        }
        job.halign = egui::Align::Center;
        ui.label(job);
        let mut job = egui::text::LayoutJob::simple(
            t("Domain administrators only; every action is audited.").into(),
            egui::FontId::proportional(11.0),
            muted,
            BOX_W,
        );
        job.halign = egui::Align::Center;
        ui.label(job);
        // the languages, the current one underlined
        ui.add_space(10.0);
        ui.horizontal(|ui| {
            let w = 140.0;
            ui.add_space(((ui.available_width() - w) / 2.0).max(0.0));
            ui.spacing_mut().item_spacing.x = 12.0;
            for (code, name) in [("pt-BR", "Português"), ("en", "English")] {
                let on = i18n::lang() == code;
                let rich = if on {
                    crate::style::semibold(name, 11.0).color(strong).underline()
                } else {
                    RichText::new(name).size(11.0).color(muted)
                };
                if ui
                    .add(egui::Button::new(rich).frame(false))
                    .on_hover_cursor(egui::CursorIcon::PointingHand)
                    .clicked()
                    && !on
                {
                    i18n::set_lang(code);
                    cx.cfg.lang = code.into();
                    if cx.cfg_path.exists()
                        && let Err(e) = adm_core::config::save(cx.platform.as_ref(), &cx.cfg_path, &cx.cfg)
                    {
                        cx.set_status(e.to_string());
                    }
                }
            }
        });
        ui.add_space(12.0);
        ui.add(egui::Label::new(RichText::new(&cx.cfg.panel.url).size(11.0).color(muted)).truncate());
        ui.add_space(4.0);
        if widgets::button(ui, look, crate::icons::SETTINGS, t("Configure"), Kind::Link).clicked() {
            cx.go = Some(Go::Setup);
        }
    }
}

/// A field of the login box as the web's: 326 × 25, 4 points of padding, the theme's field line, and the login's
/// focus ring around it when focused.
fn field(ui: &mut Ui, look: &crate::style::Look, edit: egui::TextEdit<'_>) -> egui::Response {
    let line = egui::Stroke::new(1.0, look.c("login-field-line"));
    let v = ui.visuals_mut();
    v.widgets.inactive.bg_stroke = line;
    v.widgets.hovered.bg_stroke = line;
    v.selection.stroke = egui::Stroke::new(1.0, look.c("field-focus"));
    let r = ui.add(
        edit.margin(Margin::symmetric(4, 0))
            .min_size(egui::vec2(326.0, 25.0))
            .desired_width(326.0 - 8.0)
            .vertical_align(egui::Align::Center)
            .font(egui::FontId::proportional(12.0)),
    );
    if r.has_focus() {
        ui.ctx().data_mut(|d| d.insert_temp(egui::Id::new(crate::style::OWN_RING), r.id));
        let w = look.p.focus_ring.max(1.0);
        // right outside the field's border, as the web's ring (no gap between them)
        ui.painter().rect_stroke(r.rect, 0.0, egui::Stroke::new(w, look.c("login-focus")), egui::StrokeKind::Outside);
    }
    r
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
            b.flat = h.brand.flat;
            b.domain = h.domain.clone();
            b.netbios = h.netbios.clone();
            b.logo = logo.and_then(|(bytes, ct)| Logo::decode(bytes, &ct));
            let was_setup = std::mem::replace(&mut b.setup, h.setup);
            if app.cx.cfg.lang.is_empty() && !h.brand.lang.is_empty() {
                i18n::set_lang(&h.brand.lang);
            }
            if matches!(app.screen, Screen::Login(_)) {
                app.cx.panel_look(&h.brand.theme, h.brand.flat);
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
