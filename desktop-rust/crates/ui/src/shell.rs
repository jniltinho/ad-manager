//! The main window: header (logo, domain, operator, Configure, Sign out), the sidebar with the nine pages (the theme's
//! top-tab colors), the page and the footer (company, status line, version). Each page loads the first time it is
//! opened.

use adm_core::i18n::t;
use egui::{Frame, Margin, RichText, Ui};

use crate::app::{Cx, Go};
use crate::pages::{backup, computers, groups, home, logs, ous, shares, sync, users};
use crate::widgets::{self, Kind};

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Page {
    Home,
    Users,
    Groups,
    Ous,
    Computers,
    Shares,
    Backup,
    Sync,
    Logs,
}

impl Page {
    pub const ALL: [Page; 9] = [
        Self::Home,
        Self::Users,
        Self::Groups,
        Self::Ous,
        Self::Computers,
        Self::Shares,
        Self::Backup,
        Self::Sync,
        Self::Logs,
    ];

    pub fn label(self) -> &'static str {
        match self {
            Self::Home => t("Home"),
            Self::Users => t("Users"),
            Self::Groups => t("Groups"),
            Self::Ous => t("OUs"),
            Self::Computers => t("Computers"),
            Self::Shares => t("Shares"),
            Self::Backup => t("Backup"),
            Self::Sync => t("Sync"),
            Self::Logs => t("Logs"),
        }
    }

    pub fn icon(self) -> &'static str {
        match self {
            Self::Home => "🏠",
            Self::Users => "👤",
            Self::Groups => "👥",
            Self::Ous => "📂",
            Self::Computers => "🖥",
            Self::Shares => "🗄",
            Self::Backup => "🕘",
            Self::Sync => "🔄",
            Self::Logs => "📄",
        }
    }
}

/// What a page asks of the shell.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Nav {
    To(Page),
    /// Users with the password policy dialog over it (Home's policy alert).
    Policy,
}

pub struct Shell {
    page: Page,
    opened: [bool; 9],
    pub home: home::Home,
    pub users: users::Users,
    pub groups: groups::Groups,
    pub ous: ous::Ous,
    pub computers: computers::Computers,
    pub shares: shares::Shares,
    pub backup: backup::Backup,
    pub sync: sync::Sync,
    pub logs: logs::Logs,
}

impl Shell {
    pub fn new(cx: &mut Cx) -> Self {
        let mut s = Self {
            page: Page::Home,
            opened: [false; 9],
            home: home::Home::default(),
            users: users::Users::default(),
            groups: groups::Groups::default(),
            ous: ous::Ous::default(),
            computers: computers::Computers::default(),
            shares: shares::Shares::default(),
            backup: backup::Backup::default(),
            sync: sync::Sync::default(),
            logs: logs::Logs::default(),
        };
        s.go_to(cx, Page::Home);
        s
    }

    pub fn page(&self) -> Page {
        self.page
    }

    /// Shows a page, loading it the first time.
    pub fn go_to(&mut self, cx: &mut Cx, p: Page) {
        self.page = p;
        let i = Page::ALL.iter().position(|x| *x == p).unwrap_or(0);
        if std::mem::replace(&mut self.opened[i], true) {
            return;
        }
        match p {
            Page::Home => self.home.load(cx),
            Page::Users => self.users.load(cx),
            Page::Groups => self.groups.load(cx),
            Page::Ous => self.ous.load(cx),
            Page::Computers => self.computers.load(cx),
            Page::Shares => self.shares.load(cx),
            Page::Backup => self.backup.load(cx),
            Page::Sync => self.sync.load(cx),
            Page::Logs => self.logs.load(cx),
        }
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, modal: bool) {
        let look = cx.look.clone();
        cx.title = format!("AD Manager — {} — {}", cx.me.domain, cx.me.user);
        egui::Panel::top("header")
            .frame(Frame::new().fill(look.c("header")).inner_margin(Margin::symmetric(10, 6)))
            .show(ui, |ui| {
                self.header(ui, cx);
            });
        egui::Panel::bottom("footer")
            .exact_size(26.0)
            .frame(Frame::new().fill(look.c("footer")).inner_margin(Margin::symmetric(10, 4)))
            .show(ui, |ui| {
                let strong = look.c("footer-strong");
                ui.horizontal_centered(|ui| {
                    let company = if cx.me.brand.company.is_empty() {
                        "AD Manager".to_string()
                    } else {
                        cx.me.brand.company.clone()
                    };
                    ui.label(RichText::new(format!("© {company}")).size(11.0).color(strong));
                    ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                        ui.label(
                            RichText::new(format!("ad-manager desktop-rs {}", cx.version)).size(11.0).color(strong),
                        );
                        ui.centered_and_justified(|ui| {
                            let s = cx.hover.clone().unwrap_or_else(|| cx.status.clone());
                            ui.add(egui::Label::new(RichText::new(s).size(11.0).color(strong)).truncate());
                        });
                    });
                });
            });
        let mut nav = None;
        egui::Panel::left("sidebar")
            .exact_size(200.0)
            .resizable(false)
            .frame(Frame::new().fill(look.c("tabs")).inner_margin(Margin::same(6)))
            .show(ui, |ui| {
                ui.spacing_mut().item_spacing.y = 2.0;
                for p in Page::ALL {
                    if nav_item(ui, &look, p, p == self.page).clicked() {
                        nav = Some(Nav::To(p));
                    }
                }
            });
        egui::CentralPanel::default().frame(Frame::new().fill(look.c("surface-2")).inner_margin(Margin::same(8))).show(
            ui,
            |ui| {
                let keys = !modal;
                let out = match self.page {
                    Page::Home => self.home.ui(ui, cx),
                    Page::Users => {
                        self.users.ui(ui, cx, keys);
                        None
                    }
                    Page::Groups => {
                        self.groups.ui(ui, cx, keys);
                        None
                    }
                    Page::Ous => {
                        self.ous.ui(ui, cx, keys);
                        None
                    }
                    Page::Computers => {
                        self.computers.ui(ui, cx, keys);
                        None
                    }
                    Page::Shares => {
                        self.shares.ui(ui, cx, keys);
                        None
                    }
                    Page::Backup => {
                        self.backup.ui(ui, cx, keys);
                        None
                    }
                    Page::Sync => {
                        self.sync.ui(ui, cx, keys);
                        None
                    }
                    Page::Logs => {
                        self.logs.ui(ui, cx, keys);
                        None
                    }
                };
                nav = nav.or(out);
            },
        );
        match nav {
            Some(Nav::To(p)) => self.go_to(cx, p),
            Some(Nav::Policy) => {
                self.go_to(cx, Page::Users);
                crate::pages::policy::open(cx);
            }
            None => {}
        }
    }

    fn header(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        let (on, muted) = (look.c("on-header"), look.c("on-header-muted"));
        ui.horizontal(|ui| {
            let logo = if adm_core::theme::is_dark(adm_core::theme::Rgba(look.c("header").to_srgba_unmultiplied())) {
                crate::logo::product_white()
            } else {
                crate::logo::product_navy()
            };
            ui.add(logo.fit_to_exact_size(egui::vec2(180.0, 60.0)));
            let (r, _) = ui.allocate_exact_size(egui::vec2(2.0, 40.0), egui::Sense::hover());
            ui.painter().rect_filled(r, 0.0, look.c("header-bar"));
            ui.add_space(6.0);
            ui.vertical(|ui| {
                ui.add_space(8.0);
                ui.add(egui::Label::new(RichText::new("Active Directory").strong().size(16.0).color(on)).extend());
                ui.add(egui::Label::new(RichText::new(&cx.me.domain).size(12.0).color(muted)).extend());
            });
            ui.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
                if widgets::button(ui, &look, "↩", t("Sign out"), Kind::Secondary).clicked() {
                    let api = cx.panel.clone();
                    cx.spawn(async move { api.logout().await }, |_, ()| {});
                    cx.go = Some(Go::Login);
                }
                if widgets::button(ui, &look, "⚙", t("Configure"), Kind::Secondary).clicked() {
                    cx.go = Some(Go::Setup);
                }
                ui.add_space(8.0);
                // the operator, right-aligned beside the buttons (as the Fyne header)
                ui.allocate_ui_with_layout(egui::vec2(220.0, 40.0), egui::Layout::top_down(egui::Align::Max), |ui| {
                    ui.add(egui::Label::new(RichText::new(&cx.me.user).strong().size(13.0).color(on)).truncate());
                    ui.add(egui::Label::new(RichText::new(t("domain administrator")).size(11.0).color(muted)).extend());
                });
            });
        });
    }
}

/// A sidebar item: the theme's tab colors (tab, hover, current with its mark on the left).
fn nav_item(ui: &mut Ui, look: &crate::style::Look, p: Page, active: bool) -> egui::Response {
    let (rect, resp) = ui.allocate_exact_size(egui::vec2(ui.available_width(), 34.0), egui::Sense::click());
    let resp = resp.on_hover_cursor(egui::CursorIcon::PointingHand);
    let (bg, ink) = if active {
        (look.c("tab-on"), look.c("on-tab-on"))
    } else if resp.hovered() {
        (look.c("tab-hover"), look.c("on-tab-hover"))
    } else {
        (look.c("tab"), look.c("on-tab"))
    };
    let painter = ui.painter();
    painter.rect_filled(rect, 0.0, bg);
    if active && let Some(m) = look.p.tab_mark {
        painter.rect_filled(
            egui::Rect::from_min_size(rect.min, egui::vec2(3.0, rect.height())),
            0.0,
            crate::style::c32(m),
        );
    }
    let icon_ink = if active { look.c("tab-on-icon") } else { ink };
    painter.text(
        rect.left_center() + egui::vec2(12.0, 0.0),
        egui::Align2::LEFT_CENTER,
        p.icon(),
        egui::FontId::proportional(15.0),
        icon_ink,
    );
    painter.text(
        rect.left_center() + egui::vec2(38.0, 0.0),
        egui::Align2::LEFT_CENTER,
        p.label(),
        egui::FontId::proportional(13.5),
        ink,
    );
    resp.widget_info(|| egui::WidgetInfo::selected(egui::WidgetType::Button, true, active, p.label()));
    resp
}
