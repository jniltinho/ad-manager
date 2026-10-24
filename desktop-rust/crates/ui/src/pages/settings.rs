//! Settings as the web's page: the panel's runtime settings (language, theme and flat look, session timeout,
//! Swagger; inactive days, password length, default OU; company, support link, logo) saved with
//! `PUT /api/settings`, the values from config.toml (read only) and a new file server agent token. The theme saved
//! here is every operator's; Configure › Appearance can still override it on this computer.

use adm_core::i18n::{self, t};
use adm_core::model::{Me, OrgUnit};
use adm_core::tf;
use egui::{RichText, Ui};
use reqwest::Method;
use serde_json::{Map, Value, json};

use crate::app::Cx;
use crate::widgets::{self, Kind};

#[derive(Debug, Clone, Default, serde::Deserialize)]
#[serde(default)]
struct Loaded {
    settings: Map<String, Value>,
    readonly: Map<String, Value>,
    stored: bool,
    logo: bool,
}

#[derive(Debug, Clone, Default, serde::Deserialize)]
#[serde(default)]
struct AgentToken {
    token: String,
    command: String,
}

const LANGS: [(&str, &str); 2] = [("pt-BR", "Português"), ("en", "English")];

/// The form: the settings as the server sent them (unknown keys kept), and the fields being edited.
#[derive(Default)]
struct Form {
    raw: Map<String, Value>,
    lang: usize,
    theme: usize,
    flat: bool,
    session: String,
    swagger: bool,
    inactive: String,
    pwlen: String,
    ou: usize,
    company: String,
    support: String,
}

impl Form {
    fn from(raw: Map<String, Value>, ous: &[OrgUnit]) -> Self {
        let s = |k: &str| raw.get(k).and_then(Value::as_str).unwrap_or_default().to_string();
        let n = |k: &str| raw.get(k).and_then(Value::as_i64).unwrap_or_default().to_string();
        let b = |k: &str| raw.get(k).and_then(Value::as_bool).unwrap_or_default();
        let theme = s("theme");
        let ou = s("default_ou");
        Self {
            lang: LANGS.iter().position(|l| l.0 == s("lang")).unwrap_or(1),
            theme: adm_core::theme::order().iter().position(|id| *id == theme).unwrap_or(0),
            flat: b("flat"),
            session: n("session_minutes"),
            swagger: b("swagger"),
            inactive: n("inactive_days"),
            pwlen: n("password_length"),
            ou: ous.iter().position(|o| o.dn.eq_ignore_ascii_case(&ou)).map_or(0, |i| i + 1),
            company: s("company"),
            support: s("support_url"),
            raw,
        }
    }

    /// The body of `PUT /api/settings`: the settings as loaded with the edited fields over them (numbers the server
    /// validates; a field that is not a number is sent as 0 and refused there).
    fn body(&self, ous: &[OrgUnit]) -> Value {
        let mut m = self.raw.clone();
        let num = |s: &str| s.trim().parse::<i64>().unwrap_or(0);
        let order = adm_core::theme::order();
        m.insert("lang".into(), json!(LANGS[self.lang.min(1)].0));
        m.insert("theme".into(), json!(order.get(self.theme).cloned().unwrap_or_default()));
        m.insert("flat".into(), json!(self.flat));
        m.insert("session_minutes".into(), json!(num(&self.session)));
        m.insert("swagger".into(), json!(self.swagger));
        m.insert("inactive_days".into(), json!(num(&self.inactive)));
        m.insert("password_length".into(), json!(num(&self.pwlen)));
        let ou =
            if self.ou == 0 { String::new() } else { ous.get(self.ou - 1).map(|o| o.dn.clone()).unwrap_or_default() };
        m.insert("default_ou".into(), json!(ou));
        m.insert("company".into(), json!(self.company.trim()));
        m.insert("support_url".into(), json!(self.support.trim()));
        Value::Object(m)
    }
}

#[derive(Default)]
pub struct Settings {
    loaded: Option<Loaded>,
    form: Form,
    ous: Vec<OrgUnit>,
    busy: bool,
    token: Option<AgentToken>,
}

/// The fields that show a generated token and its command: forgotten with the session (egui keeps their text).
fn token_field(k: usize) -> egui::Id {
    egui::Id::new(("settings-token", k))
}

impl Settings {
    /// Forgets the token fields' egui state (their text and undo history).
    pub fn forget(&mut self, ctx: &egui::Context) {
        self.token = None;
        for k in 0..2 {
            crate::widgets::forget_field(ctx, token_field(k));
        }
    }

    pub fn load(&mut self, cx: &Cx) {
        let api = cx.panel.clone();
        cx.run(
            async move {
                let l: Loaded = api.get("/api/settings").await?;
                let ous = crate::pages::users::fetch_ous(&api).await.unwrap_or_default();
                Ok((l, ous))
            },
            |app, (l, ous)| {
                app.with_shell(|s, _| {
                    let p = &mut s.settings;
                    p.form = Form::from(l.settings.clone(), &ous);
                    p.ous = ous;
                    p.loaded = Some(l);
                });
            },
        );
    }

    fn save(&mut self, cx: &Cx) {
        self.busy = true;
        let body = self.form.body(&self.ous);
        let api = cx.panel.clone();
        let session = cx.panel.id;
        cx.spawn(
            async move {
                api.act_with(Method::PUT, "/api/settings", &body).await?;
                api.get::<Me>("/api/me").await
            },
            move |app, r| {
                // a reply of a session replaced meanwhile (signed out) reaches nothing
                if app.cx.panel.id != session {
                    return;
                }
                app.with_shell(|s, _| s.settings.busy = false);
                match r {
                    Ok(me) => {
                        // the panel switches right away for whoever saved (as the web's)
                        if app.cx.cfg.lang.is_empty() {
                            i18n::set_lang(&me.settings.lang);
                        }
                        let (theme, flat) = (me.brand.theme.clone(), me.brand.flat);
                        app.cx.me = me;
                        app.cx.panel_look(&theme, flat);
                        app.cx.set_status(t("Settings saved."));
                        app.with_shell(|s, cx| s.settings.load(cx));
                    }
                    Err(e) => app.cx.handle_error(e),
                }
            },
        );
    }

    fn upload_logo(cx: &Cx) {
        crate::files::pick_file(cx, |app, path| {
            let api = app.cx.panel.clone();
            app.cx.run(
                async move {
                    let bytes =
                        tokio::fs::read(&path).await.map_err(|e| adm_core::api::ApiError::Other(e.to_string()))?;
                    let name = path.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default();
                    api.put_file("/api/settings/logo", "logo", &name, bytes).await?;
                    api.image("/api/brand/logo").await
                },
                |app, (bytes, ct)| {
                    app.cx.brand.logo = crate::logo::Logo::decode(bytes, &ct);
                    app.cx.me.brand.logo = true;
                    app.cx.set_status(t("Logo updated."));
                    app.with_shell(|s, _| {
                        if let Some(l) = &mut s.settings.loaded {
                            l.logo = true;
                        }
                    });
                },
            );
        });
    }

    fn remove_logo(cx: &mut Cx) {
        cx.confirm(
            t("Logo"),
            t("Remove the company logo and go back to the default one?").into(),
            t("Remove"),
            true,
            |cx| {
                let api = cx.panel.clone();
                cx.run(async move { api.act(Method::DELETE, "/api/settings/logo").await }, |app, ()| {
                    app.cx.brand.logo = None;
                    app.cx.me.brand.logo = false;
                    app.cx.set_status(t("Logo removed."));
                    app.with_shell(|s, _| {
                        if let Some(l) = &mut s.settings.loaded {
                            l.logo = false;
                        }
                    });
                });
            },
        );
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        let Some(loaded) = self.loaded.clone() else {
            ui.label(RichText::new(t("Loading…")).size(12.0).color(look.c("fg-muted")));
            return;
        };
        let muted = look.c("fg-muted");
        let note = |ui: &mut Ui, s: &str| {
            ui.add(egui::Label::new(RichText::new(s).size(11.0).color(muted)).wrap());
        };
        let mut save = false;
        let mut discard = false;
        let mut token = false;
        let (mut upload, mut remove) = (false, false);
        egui::ScrollArea::vertical().id_salt("settings").auto_shrink([false, false]).show(ui, |ui| {
            ui.horizontal_top(|ui| {
                ui.spacing_mut().item_spacing.x = 20.0;
                let right_w = 520.0_f32.min((ui.available_width() * 0.45).floor());
                let left_w = ui.available_width() - right_w - 20.0;
                ui.vertical(|ui| {
                    ui.set_width(left_w);
                    if !loaded.stored {
                        widgets::callout(ui, &look, widgets::Note::Warn, t("No local database (db.path): settings cannot be saved."));
                        ui.add_space(20.0);
                    }
                    let f = &mut self.form;
                    // General
                    widgets::panel(ui, &look, (t("General"), ""), |_| {}, 0.0, |ui| {
                        egui::Frame::new().inner_margin(egui::Margin::same(12)).show(ui, |ui| {
                            let cw = ((ui.available_width() - 16.0) / 2.0).floor();
                            ui.horizontal_top(|ui| {
                                ui.spacing_mut().item_spacing.x = 16.0;
                                ui.vertical(|ui| {
                                    ui.set_width(cw);
                                    let names: Vec<String> = LANGS.iter().map(|l| l.1.to_string()).collect();
                                    widgets::labeled_select(ui, &look, t("Default language"), "s-lang", &mut f.lang, &names, cw - 8.0);
                                    note(ui, t("Also selectable on the login page."));
                                });
                                ui.vertical(|ui| {
                                    ui.set_width(cw);
                                    let names: Vec<String> = adm_core::theme::order()
                                        .iter()
                                        .map(|id| adm_core::theme::palette(id).map_or_else(|| id.clone(), |(_, p)| t(&p.label).to_string()))
                                        .collect();
                                    widgets::labeled_select(ui, &look, t("Theme"), "s-theme", &mut f.theme, &names, cw - 8.0);
                                    note(ui, t("Look of the panel and the login page for every operator."));
                                    crate::widgets::check(ui, &mut f.flat, RichText::new(t("Flat look (no gradients or shadows)")).size(12.0));
                                });
                            });
                            ui.add_space(12.0);
                            widgets::above(ui, &look, t("Session timeout (minutes idle)"), cw, |ui, _| {
                                ui.add(crate::widgets::line(&mut f.session).desired_width(cw))
                            });
                            ui.add_space(12.0);
                            let label = crate::style::semibold(t("API documentation (Swagger) at /swagger"), 12.0).color(look.c("fg-strong"));
                            crate::widgets::check(ui, &mut f.swagger, label);
                            note(ui, t("Only for signed-in administrators, like the queue monitor. Keep it off in production."));
                        });
                    });
                    ui.add_space(20.0);
                    // Users
                    widgets::panel(ui, &look, (t("Users"), ""), |_| {}, 0.0, |ui| {
                        egui::Frame::new().inner_margin(egui::Margin::same(12)).show(ui, |ui| {
                            let cw = ((ui.available_width() - 32.0) / 3.0).floor();
                            ui.horizontal_top(|ui| {
                                ui.spacing_mut().item_spacing.x = 16.0;
                                widgets::above(ui, &look, t("Inactive after (days without logon)"), cw, |ui, _| {
                                    ui.add(crate::widgets::line(&mut f.inactive).desired_width(cw))
                                });
                                widgets::above(ui, &look, t("Generated password length"), cw, |ui, _| {
                                    ui.add(crate::widgets::line(&mut f.pwlen).desired_width(cw))
                                });
                                ui.vertical(|ui| {
                                    ui.set_width(cw);
                                    let mut names = vec!["CN=Users".to_string()];
                                    names.extend(self.ous.iter().map(|o| {
                                        let lower = o.dn.to_lowercase();
                                        lower.find(",dc=").map_or_else(|| o.dn.clone(), |i| o.dn[..i].to_string())
                                    }));
                                    widgets::labeled_select(ui, &look, t("Default OU for new users"), "s-ou", &mut f.ou, &names, cw - 8.0);
                                });
                            });
                        });
                    });
                    ui.add_space(20.0);
                    // Company
                    widgets::panel(ui, &look, (t("Company"), ""), |_| {}, 0.0, |ui| {
                        egui::Frame::new().inner_margin(egui::Margin::same(12)).show(ui, |ui| {
                            let cw = ((ui.available_width() - 16.0) / 2.0).floor();
                            ui.horizontal_top(|ui| {
                                ui.spacing_mut().item_spacing.x = 16.0;
                                widgets::above(ui, &look, t("Company name (footer)"), cw, |ui, _| {
                                    ui.add(crate::widgets::line(&mut f.company).char_limit(80).hint_text("AD Manager").desired_width(cw))
                                });
                                widgets::above(ui, &look, t("Support link"), cw, |ui, _| {
                                    let hint = "https://github.com/jniltinho/ad-manager/wiki";
                                    ui.add(crate::widgets::line(&mut f.support).hint_text(hint).desired_width(cw))
                                });
                            });
                            ui.add_space(12.0);
                            widgets::lbl(ui, &look, t("Logo (login page and header)"));
                            ui.add_space(4.0);
                            ui.horizontal_top(|ui| {
                                ui.spacing_mut().item_spacing.x = 12.0;
                                // the logo as the header draws it, on the third surface
                                egui::Frame::new()
                                    .fill(look.c("surface-3"))
                                    .stroke(egui::Stroke::new(1.0, look.c("line")))
                                    .inner_margin(egui::Margin::same(8))
                                    .show(ui, |ui| {
                                        let (r, _) = ui.allocate_exact_size(egui::vec2(208.0, 48.0), egui::Sense::hover());
                                        let img = match &cx.brand.logo {
                                            Some(l) if loaded.logo => l.image(),
                                            _ => crate::logo::product_navy(),
                                        };
                                        img.fit_to_exact_size(r.size()).paint_at(ui, r);
                                    });
                                ui.vertical(|ui| {
                                    ui.horizontal(|ui| {
                                        ui.spacing_mut().item_spacing.x = 8.0;
                                        let can = loaded.stored;
                                        upload = ui.add_enabled_ui(can, |ui| widgets::button(ui, &look, crate::icons::UPLOAD, t("Upload logo"), Kind::Secondary)).inner.clicked();
                                        if loaded.logo {
                                            remove = widgets::button(ui, &look, crate::icons::TRASH, t("Use the default logo"), Kind::Secondary).clicked();
                                        }
                                    });
                                    note(ui, t("PNG, JPEG or SVG up to 512 KiB."));
                                    note(ui, t("Maximum size: 660 × 220 px (width × height), ratio 3:1."));
                                    note(ui, t("Shown at up to 195 × 65 px on the login page and 40 px high in the header."));
                                });
                            });
                        });
                    });
                    ui.add_space(20.0);
                    ui.horizontal(|ui| {
                        ui.spacing_mut().item_spacing.x = 8.0;
                        let label = if self.busy { t("Saving…") } else { t("Save settings") };
                        let can = !self.busy && loaded.stored;
                        save = ui.add_enabled_ui(can, |ui| widgets::button(ui, &look, crate::icons::CHECK, label, Kind::Primary)).inner.clicked();
                        discard = widgets::button(ui, &look, crate::icons::ROTATE, t("Discard changes"), Kind::Secondary).clicked();
                    });
                    ui.add_space(20.0);
                });
                ui.vertical(|ui| {
                    ui.set_width(right_w);
                    self.readonly_card(ui, cx, &loaded);
                    ui.add_space(20.0);
                    token = self.token_card(ui, cx);
                });
            });
        });
        if save {
            self.save(cx);
        }
        if discard {
            self.load(cx);
        }
        if token {
            let api = cx.panel.clone();
            cx.run(
                async move { api.send::<AgentToken, _>(Method::POST, "/api/agents/token", None::<&()>).await },
                |app, tk| {
                    app.with_shell(|s, _| s.settings.token = Some(tk));
                },
            );
        }
        if upload {
            Self::upload_logo(cx);
        }
        if remove {
            Self::remove_logo(cx);
        }
    }

    /// The values from config.toml, read only.
    fn readonly_card(&self, ui: &mut Ui, cx: &Cx, l: &Loaded) {
        let look = &cx.look;
        let r = &l.readonly;
        let s = |k: &str| r.get(k).and_then(Value::as_str).unwrap_or_default().to_string();
        let b = |k: &str| r.get(k).and_then(Value::as_bool).unwrap_or_default();
        let yes = |v: bool| if v { t("yes") } else { t("no") }.to_string();
        let or = |v: String, d: &str| if v.is_empty() { d.to_string() } else { v };
        let https = if b("tls") {
            if b("self_signed") { t("self-signed certificate") } else { t("certificate from config.toml") }
        } else {
            t("behind a TLS proxy")
        };
        let rows: Vec<(String, String)> = vec![
            (t("Domain controller").into(), s("ad_url")),
            (t("Domain").into(), s("domain")),
            ("Base DN".into(), s("base_dn")),
            (t("LDAPS CA").into(), or(s("ca_file"), t("system trust store"))),
            (t("Administrators group").into(), or(s("admin_group"), "Domain Admins (RID 512)")),
            (t("Panel address").into(), s("listen")),
            ("HTTPS".into(), https.into()),
            (t("Trust proxy").into(), yes(b("trust_proxy"))),
            (t("Audit log").into(), s("audit_log")),
            (t("Local database").into(), s("db")),
            (t("Queue (Redis)").into(), or(s("redis"), t("disabled"))),
            (t("Version").into(), s("version")),
        ];
        widgets::panel(
            ui,
            look,
            (t("From config.toml (read-only)"), ""),
            |_| {},
            0.0,
            |ui| {
                let line = egui::Stroke::new(1.0, look.c("line"));
                // the keys' column as wide as the widest key, the values after it (the web's table)
                let font = egui::FontId::new(12.0, egui::FontFamily::Name(crate::fonts::SEMIBOLD.into()));
                let strong = look.c("fg-strong");
                let key_w = rows
                    .iter()
                    .map(|(k, _)| ui.painter().layout_no_wrap(k.clone(), font.clone(), strong).size().x)
                    .fold(0.0, f32::max);
                ui.spacing_mut().item_spacing.y = 0.0;
                for (i, (k, v)) in rows.iter().enumerate() {
                    let (r, _) = ui.allocate_exact_size(egui::vec2(ui.available_width(), 25.0), egui::Sense::hover());
                    if i % 2 == 1 {
                        ui.painter().rect_filled(r, 0.0, look.c("row-alt"));
                    }
                    if i > 0 {
                        ui.painter().hline(r.x_range(), r.top() + 0.5, line);
                    }
                    let g = ui.painter().layout_no_wrap(k.clone(), font.clone(), strong);
                    ui.painter().galley(egui::pos2(r.left() + 8.0, r.center().y - g.size().y / 2.0), g, strong);
                    let vr = egui::Rect::from_min_max(
                        egui::pos2(r.left() + key_w + 24.0, r.top()),
                        egui::pos2(r.right() - 8.0, r.bottom()),
                    );
                    let layout = egui::Layout::left_to_right(egui::Align::Center);
                    let mut child = ui.new_child(egui::UiBuilder::new().max_rect(vr).layout(layout));
                    child.add(egui::Label::new(RichText::new(v).monospace().size(11.0)).truncate()).on_hover_text(v);
                }
                let y = ui.cursor().top();
                ui.painter().hline(ui.max_rect().x_range(), y + 0.5, line);
                egui::Frame::new().inner_margin(egui::Margin::symmetric(12, 8)).show(ui, |ui| {
                    ui.set_width(ui.available_width());
                    let muted = look.c("fg-muted");
                    ui.add(
                        egui::Label::new(
                            RichText::new(t(
                                "Edit /opt/ad-manager/config.toml and restart the ad-manager service to change these.",
                            ))
                            .size(11.0)
                            .line_height(Some(widgets::LEADING * 11.0))
                            .color(muted),
                        )
                        .wrap(),
                    );
                    ui.add_space(8.0); // the web's paragraph gap
                    ui.label(RichText::new(tf!("Signed in as %s.", cx.me.user)).size(11.0).color(muted));
                });
            },
        );
    }

    /// A new file server agent token, shown once with the command to run on the server; true when asked.
    fn token_card(&self, ui: &mut Ui, cx: &mut Cx) -> bool {
        let look = cx.look.clone();
        let mut asked = false;
        widgets::panel(
            ui,
            &look,
            (t("File server agent token"), ""),
            |_| {},
            0.0,
            |ui| {
                egui::Frame::new().inner_margin(egui::Margin::same(12)).show(ui, |ui| {
                ui.set_width(ui.available_width());
                ui.spacing_mut().item_spacing.y = 8.0;
                let muted = look.c("fg-muted");
                ui.add(egui::Label::new(RichText::new(t("Register the token in Shares → + new, and run the command on the file server: it keeps only the hash.")).size(12.0).color(muted)).wrap());
                asked = widgets::button(ui, &look, crate::icons::KEY, t("Generate token"), Kind::Secondary).clicked();
                if let Some(tk) = &self.token {
                    let w = ui.available_width();
                    for (k, (label, value, done)) in [
                        (t("Token (shown only now)"), &tk.token, t("Token copied.")),
                        (t("Command on the file server"), &tk.command, t("Command copied.")),
                    ]
                    .into_iter()
                    .enumerate()
                    {
                        ui.vertical(|ui| {
                            ui.spacing_mut().item_spacing.y = 4.0;
                            let l = widgets::lbl(ui, &look, label);
                            ui.allocate_ui_with_layout(
                                egui::vec2(w, crate::style::control_h()),
                                egui::Layout::right_to_left(egui::Align::Center),
                                |ui| {
                                    ui.spacing_mut().item_spacing.x = 0.0;
                                    if widgets::button(ui, &look, crate::icons::COPY, "", Kind::Secondary).on_hover_text(t("Copy")).clicked() {
                                        ui.ctx().copy_text(value.clone());
                                        cx.set_status(done);
                                    }
                                    let mut v = value.clone();
                                    let edit = crate::widgets::line(&mut v)
                                        .id(token_field(k))
                                        .font(egui::FontId::monospace(11.0));
                                    ui.add(edit.desired_width(ui.available_width())).labelled_by(l.id);
                                },
                            );
                        });
                    }
                }
            });
            },
        );
        asked
    }
}
