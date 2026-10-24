//! The main window as the web's App.vue: an app box (16 points from the window, at most 1840 wide) with the header
//! (logo, "Active Directory" with the domain, global search, the operator with Configure, my password and sign out),
//! the top tab strip in the web's order, the page (20 points of padding) and the footer (company, Support, domain,
//! status line, operator, session idle time, version). Each page loads the first time it is opened.

use adm_core::i18n::t;
use egui::{Frame, Margin, RichText, Ui};

use crate::app::{Cx, Go};
use crate::pages::{backup, computers, groups, home, import_export, logs, ous, settings, shares, sync, users};
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
    ImportExport,
    Settings,
}

impl Page {
    pub const ALL: [Page; 11] = [
        Self::Home,
        Self::Users,
        Self::Groups,
        Self::Ous,
        Self::Computers,
        Self::Shares,
        Self::Backup,
        Self::Sync,
        Self::Logs,
        Self::ImportExport,
        Self::Settings,
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
            Self::ImportExport => t("Import/export"),
            Self::Settings => t("Settings"),
        }
    }

    pub fn icon(self) -> &'static str {
        match self {
            Self::Home => crate::icons::HOME,
            Self::Users => crate::icons::USER,
            Self::Groups => crate::icons::USERS_ROUND,
            Self::Ous => crate::icons::FOLDER_TREE,
            Self::Computers => crate::icons::MONITOR,
            Self::Shares => crate::icons::FOLDER_OPEN,
            Self::Backup => crate::icons::ARCHIVE,
            Self::Sync => crate::icons::FOLDER_SYNC,
            Self::Logs => crate::icons::SCROLL,
            Self::ImportExport => crate::icons::ARROWS,
            Self::Settings => crate::icons::SETTINGS,
        }
    }
}

/// What a page asks of the shell.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Nav {
    To(Page),
    /// Users with the password policy dialog over it (Home's policy alert).
    Policy,
    /// Users filtered by the header's global search.
    Search(String),
    /// Users with this status filter (Home's tiles and alerts).
    Users(adm_core::logic::UserFilter),
}

pub struct Shell {
    page: Page,
    /// The header's global search.
    search: String,
    opened: [bool; 11],
    pub home: home::Home,
    pub users: users::Users,
    pub groups: groups::Groups,
    pub ous: ous::Ous,
    pub computers: computers::Computers,
    pub shares: shares::Shares,
    pub backup: backup::Backup,
    pub sync: sync::Sync,
    pub logs: logs::Logs,
    pub settings: settings::Settings,
    pub import: import_export::ImportExport,
}

impl Shell {
    pub fn new(cx: &mut Cx) -> Self {
        let mut s = Self {
            page: Page::Home,
            search: String::new(),
            opened: [false; 11],
            home: home::Home::default(),
            users: users::Users::default(),
            groups: groups::Groups::default(),
            ous: ous::Ous::default(),
            computers: computers::Computers::default(),
            shares: shares::Shares::default(),
            backup: backup::Backup::default(),
            sync: sync::Sync::default(),
            logs: logs::Logs::default(),
            settings: settings::Settings::default(),
            import: import_export::ImportExport::default(),
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
            Page::Settings => self.settings.load(cx),
            Page::ImportExport => self.import.load(cx),
        }
    }

    pub fn ui(&mut self, ui: &mut Ui, cx: &mut Cx, modal: bool) {
        let look = cx.look.clone();
        cx.title = format!("AD Manager — {} — {}", cx.me.domain, cx.me.user);
        let mut nav = None;
        egui::CentralPanel::default().frame(Frame::new().fill(look.c("page"))).show(ui, |ui| {
            look.page_pattern(ui.painter(), ui.max_rect());
            // the web's app box: the window less 16 points around, at most 1840 wide, centered (egui's zoom scales it)
            let full = ui.max_rect();
            let w = (full.width() - 32.0).clamp(0.0, 1840.0);
            let rect = egui::Rect::from_min_max(
                egui::pos2(full.center().x - w / 2.0, full.top() + 16.0),
                egui::pos2(full.center().x + w / 2.0, full.bottom() - 16.0),
            );
            look.paint(ui.painter(), rect, "", "app", look.c("surface"));
            let mut app =
                ui.new_child(egui::UiBuilder::new().max_rect(rect).layout(egui::Layout::top_down(egui::Align::Min)));
            app.set_clip_rect(rect);
            app.spacing_mut().item_spacing = egui::vec2(0.0, 0.0);
            if let Some(n) = self.header(&mut app, cx) {
                nav = Some(n);
            }
            if let Some(n) = self.tabs(&mut app, cx) {
                nav = Some(n);
            }
            let footer_h = 32.0 + look.px("footer-bar-w", 3.0);
            let main = egui::Rect::from_min_max(app.cursor().min, egui::pos2(rect.right(), rect.bottom() - footer_h));
            let mut page = app.new_child(
                egui::UiBuilder::new().max_rect(main.shrink(20.0)).layout(egui::Layout::top_down(egui::Align::Min)),
            );
            page.set_clip_rect(main);
            page.spacing_mut().item_spacing = egui::vec2(look.px("toolbar-gap", 8.0), 6.0);
            if let Some(n) = self.page_ui(&mut page, cx, !modal) {
                nav = Some(n);
            }
            let foot = egui::Rect::from_min_max(egui::pos2(rect.left(), rect.bottom() - footer_h), rect.max);
            let mut f = app.new_child(egui::UiBuilder::new().max_rect(foot));
            self.footer(&mut f, cx);
        });
        match nav {
            Some(Nav::To(p)) => self.go_to(cx, p),
            Some(Nav::Policy) => {
                self.go_to(cx, Page::Users);
                crate::pages::policy::open(cx);
            }
            Some(Nav::Users(f)) => {
                self.users.show_filter(cx, f);
                self.go_to(cx, Page::Users);
            }
            Some(Nav::Search(term)) => {
                self.users.search_for(cx, &term);
                self.go_to(cx, Page::Users);
            }
            None => {}
        }
    }

    /// The current page; Backup and Sync share the Backup tab and show the web's Backup | Sync page tabs.
    fn page_ui(&mut self, ui: &mut Ui, cx: &mut Cx, keys: bool) -> Option<Nav> {
        if matches!(self.page, Page::Backup | Page::Sync) {
            let tabs = [(crate::icons::ARCHIVE, t("Backup")), (crate::icons::FOLDER_SYNC, t("Sync"))];
            let sel = usize::from(self.page == Page::Sync);
            if let Some(i) = widgets::subtabs(ui, &cx.look, "backup-tabs", &tabs, sel) {
                return Some(Nav::To(if i == 1 { Page::Sync } else { Page::Backup }));
            }
        }
        match self.page {
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
            Page::Settings => {
                self.settings.ui(ui, cx);
                None
            }
            Page::ImportExport => {
                self.import.ui(ui, cx);
                None
            }
        }
    }

    /// The web's header (px 20, py 12, 40 high): logo, "Active Directory" with the domain behind the theme's bar,
    /// and at the right the global search then the operator with Configure, my password and sign out. Laid out by
    /// rects: egui reverses a row nested in a right-to-left layout.
    fn header(&mut self, ui: &mut Ui, cx: &mut Cx) -> Option<Nav> {
        let look = cx.look.clone();
        let (on, muted) = (look.c("on-header"), look.c("on-header-muted"));
        let mut nav = None;
        let (rect, _) = ui.allocate_exact_size(egui::vec2(ui.available_width(), 64.0), egui::Sense::hover());
        crate::depth::paint_box(ui.painter(), rect, look.gradient("header"), look.c("header"), &[]);
        let inner = rect.shrink2(egui::vec2(20.0, 12.0));
        // logo, 40 high (at most 256 wide)
        let logo = match &cx.brand.logo {
            Some(l) if cx.me.brand.logo => l.image(),
            _ if adm_core::theme::is_dark(adm_core::theme::Rgba(look.c("header").to_srgba_unmultiplied())) => {
                crate::logo::product_white()
            }
            _ => crate::logo::product_navy(),
        };
        // painted, not allocated (the header's rect is already taken), left-aligned at its own aspect
        let size = logo.load_and_calc_size(ui, egui::vec2(256.0, 40.0)).unwrap_or(egui::vec2(160.0, 40.0));
        let size = size * (40.0 / size.y.max(1.0)).min(256.0 / size.x.max(1.0));
        let lr = egui::Rect::from_min_size(egui::pos2(inner.left(), inner.center().y - size.y / 2.0), size);
        logo.paint_at(ui, lr);
        let logo_w = size.x;
        // the title block: the bar, then "Active Directory" and the domain
        let x = inner.left() + logo_w + 16.0;
        let title = ui.painter().layout_no_wrap(
            "Active Directory".into(),
            egui::FontId::new(15.0, egui::FontFamily::Name(crate::fonts::SEMIBOLD.into())),
            on,
        );
        let dom = ui.painter().layout_no_wrap(cx.me.domain.clone(), egui::FontId::proportional(11.0), muted);
        let block_h = title.size().y + dom.size().y;
        let top = inner.center().y - block_h / 2.0;
        ui.painter().rect_filled(
            egui::Rect::from_min_size(egui::pos2(x, top), egui::vec2(2.0, block_h)),
            0.0,
            look.c("header-bar"),
        );
        ui.painter().galley(egui::pos2(x + 14.0, top), title, on);
        ui.painter().galley(egui::pos2(x + 14.0, top + block_h - dom.size().y), dom, muted);
        // the operator block, right-aligned
        let ur = egui::Rect::from_min_max(egui::pos2(inner.right() - 360.0, inner.top()), inner.max);
        let mut user =
            ui.new_child(egui::UiBuilder::new().max_rect(ur).layout(egui::Layout::top_down(egui::Align::Max)));
        self.user_block(&mut user, cx);
        let user_left = user.min_rect().left();
        // the global search, 288 wide, left of the operator
        let sr = egui::Rect::from_center_size(
            egui::pos2(user_left - 16.0 - 144.0, inner.center().y),
            egui::vec2(288.0, crate::style::control_h()),
        );
        let mut search =
            ui.new_child(egui::UiBuilder::new().max_rect(sr).layout(egui::Layout::right_to_left(egui::Align::Center)));
        search.spacing_mut().item_spacing.x = 0.0;
        let go = widgets::button(&mut search, &look, crate::icons::SEARCH, t("Search"), Kind::Primary).clicked();
        let w = search.available_width();
        let field =
            search.add(widgets::line(&mut self.search).hint_text(t("Search user, login or e-mail…")).desired_width(w));
        let _ = search.ctx().accesskit_node_builder(field.id, |n| n.set_label(t("Global search")));
        let enter = field.lost_focus() && search.input(|i| i.key_pressed(egui::Key::Enter));
        if (go || enter) && !self.search.trim().is_empty() {
            // the term stays in the field, as the web's
            nav = Some(Nav::Search(self.search.trim().to_string()));
        }
        nav
    }

    /// The operator's name, and under it Configure (desktop only), my password and sign out as the web's links.
    fn user_block(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        ui.spacing_mut().item_spacing.y = 2.0;
        ui.add_space(2.0);
        ui.label(crate::style::semibold(&cx.me.user, 12.0).color(look.c("on-header")));
        // right to left: the last link first
        ui.allocate_ui_with_layout(
            egui::vec2(ui.available_width(), 16.0),
            egui::Layout::right_to_left(egui::Align::Center),
            |ui| {
                ui.spacing_mut().item_spacing.x = 4.0;
                let link = |ui: &mut Ui, icon: &str, s: &str| {
                    let r = ui.add(
                        egui::Button::new(RichText::new(format!("{icon} {s}")).size(11.0).color(look.c("header-link")))
                            .frame(false),
                    );
                    r.on_hover_cursor(egui::CursorIcon::PointingHand)
                };
                let dot = |ui: &mut Ui| {
                    ui.label(RichText::new("·").size(11.0).color(look.c("on-header-subtle")));
                };
                if link(ui, crate::icons::LOG_OUT, t("sign out")).clicked() {
                    let api = cx.panel.clone();
                    cx.spawn(async move { api.logout().await }, |_, ()| {});
                    cx.go = Some(Go::Login);
                }
                dot(ui);
                if link(ui, crate::icons::KEY, t("my password")).clicked() {
                    cx.push(crate::own_password::OwnPassword::new());
                }
                dot(ui);
                if link(ui, crate::icons::SETTINGS, t("Configure")).clicked() {
                    cx.go = Some(Go::Setup);
                }
            },
        );
    }

    /// The web's top tab strip: the pages in the web's order, Import/export and Settings (opened in the browser), and
    /// API and Queues at the right when the panel serves them.
    fn tabs(&mut self, ui: &mut Ui, cx: &mut Cx) -> Option<Nav> {
        let look = cx.look.clone();
        let mut nav = None;
        let pad = look.px("tabs-pad", 0.0);
        let under = ui.painter().add(egui::Shape::Noop);
        let current = if self.page == Page::Sync { Page::Backup } else { self.page };
        let base = cx.cfg.panel.url.trim_end_matches('/').to_string();
        let r = Frame::new()
            .inner_margin(Margin { left: pad as i8, right: pad as i8, top: pad as i8, bottom: 0 })
            .show(ui, |ui| {
                ui.set_width(ui.available_width());
                ui.horizontal(|ui| {
                    ui.spacing_mut().item_spacing.x = look.px("tab-gap", 0.0);
                    for p in TABS {
                        if top_tab(ui, &look, p.icon(), p.label(), p == current, "").clicked() {
                            nav = Some(Nav::To(p));
                        }
                    }
                    ui.with_layout(egui::Layout::right_to_left(egui::Align::Max), |ui| {
                        if cx.me.asynqmon
                            && top_tab(
                                ui,
                                &look,
                                crate::icons::CHECKS,
                                t("Queues"),
                                false,
                                t("Agent queue monitor (opens in a new tab)"),
                            )
                            .clicked()
                        {
                            ui.ctx().open_url(egui::OpenUrl::new_tab(format!("{base}/asynqmon")));
                        }
                        if cx.me.settings.swagger
                            && top_tab(
                                ui,
                                &look,
                                crate::icons::BOOK,
                                "API",
                                false,
                                t("API documentation (opens in a new tab)"),
                            )
                            .clicked()
                        {
                            ui.ctx().open_url(egui::OpenUrl::new_tab(format!("{base}/swagger")));
                        }
                    });
                });
            });
        ui.painter().set(
            under,
            egui::Shape::Vec(crate::depth::box_shapes(
                r.response.rect,
                look.gradient("tabs"),
                look.c("tabs"),
                look.shadows("tabs"),
            )),
        );
        nav
    }

    /// The web's footer: its bar on top, the company, Support and the domain at the left; the status line; the
    /// operator, the session's idle time and the version at the right.
    fn footer(&mut self, ui: &mut Ui, cx: &mut Cx) {
        let look = cx.look.clone();
        let rect = ui.max_rect();
        let bar = look.px("footer-bar-w", 3.0);
        let body = egui::Rect::from_min_max(egui::pos2(rect.left(), rect.top() + bar), rect.max);
        // the theme's line inside the top (the 98's white edge)
        crate::depth::paint_box(
            ui.painter(),
            body,
            look.gradient("footer"),
            look.c("footer"),
            look.shadows("footer-line"),
        );
        if bar > 0.0 {
            ui.painter().rect_filled(
                egui::Rect::from_min_size(rect.min, egui::vec2(rect.width(), bar)),
                0.0,
                look.c("footer-bar"),
            );
        }
        let (ink, strong) = (look.c("on-footer"), look.c("footer-strong"));
        let mut inner = ui.new_child(
            egui::UiBuilder::new()
                .max_rect(body.shrink2(egui::vec2(20.0, 0.0)))
                .layout(egui::Layout::left_to_right(egui::Align::Center)),
        );
        inner.spacing_mut().item_spacing.x = look.px("footer-gap", 20.0);
        inner.spacing_mut().interact_size.y = 0.0; // the Support link as high as its text (its pane around it)
        let small = |s: String, c| RichText::new(s).size(11.0).color(c);
        // each item in the theme's pane (the 98's sunken status-bar boxes), none in the others
        let relief = look.shadows("footer-pane-relief");
        let (py, px) = crate::modal::head_pad(look.p.text("footer-pane-pad"));
        let pane = |ui: &mut Ui, add: &mut dyn FnMut(&mut Ui) -> egui::Response| -> egui::Response {
            if relief.is_empty() {
                return add(ui);
            }
            // the pane around the item, its padding outside the item's own rect
            ui.add_space(px);
            let r = add(ui);
            ui.add_space(px);
            let pane = r.rect.expand2(egui::vec2(px, py));
            ui.painter().extend(crate::depth::box_shapes(pane, None, egui::Color32::TRANSPARENT, relief));
            r
        };
        let company =
            if cx.me.brand.company.is_empty() { "AD Manager".to_string() } else { cx.me.brand.company.clone() };
        pane(&mut inner, &mut |ui| ui.label(crate::style::semibold(format!("© {company}"), 11.0).color(strong)));
        let support = if cx.me.settings.support_url.is_empty() {
            SUPPORT.to_string()
        } else {
            cx.me.settings.support_url.clone()
        };
        if pane(&mut inner, &mut |ui| {
            ui.add(egui::Button::new(small(format!("{} {}", SUPPORT_ICON, t("Support")), ink)).frame(false))
        })
        .on_hover_cursor(egui::CursorIcon::PointingHand)
        .clicked()
        {
            inner.ctx().open_url(egui::OpenUrl::new_tab(support));
        }
        let domain = if cx.me.netbios.is_empty() {
            cx.me.domain.clone()
        } else {
            format!("{} ({})", cx.me.domain, cx.me.netbios)
        };
        pane(&mut inner, &mut |ui| ui.label(small(format!("{} {domain}", crate::icons::SERVER), ink)));
        inner.with_layout(egui::Layout::right_to_left(egui::Align::Center), |ui| {
            pane(ui, &mut |ui| ui.label(small(format!("ad-manager desktop-rs {}", cx.version), strong)));
            if cx.me.settings.session_minutes > 0 {
                let idle = adm_core::tf!("session expires after %d min idle", cx.me.settings.session_minutes);
                pane(ui, &mut |ui| ui.label(small(format!("{} {idle}", crate::icons::CLOCK), ink)));
            }
            pane(ui, &mut |ui| ui.label(small(format!("{} {}", crate::icons::USER, cx.me.user), ink)));
            let s = cx.hover.clone().unwrap_or_else(|| cx.status.clone());
            if !s.is_empty() {
                pane(ui, &mut |ui| ui.add(egui::Label::new(small(s.clone(), strong)).truncate()));
            }
        });
    }
}

/// The footer's Support icon (the web's life buoy has no glyph in egui's fonts).
const SUPPORT_ICON: &str = crate::icons::BUOY;

/// The project's support page (the web's default).
const SUPPORT: &str = "https://github.com/jniltinho/ad-manager/wiki";

/// The web's tab order (App.vue); Sync opens from Backup's subtabs.
const TABS: [Page; 10] = [
    Page::Home,
    Page::Users,
    Page::Groups,
    Page::Ous,
    Page::Computers,
    Page::Shares,
    Page::ImportExport,
    Page::Backup,
    Page::Logs,
    Page::Settings,
];

/// A top tab as the web's: px 16 py 8, a 15 point icon and the 12 point semibold label; the current one in the
/// theme's tab-on recipe (fill or gradient, its inset marks), the others lower by the theme's inset.
fn top_tab(
    ui: &mut Ui,
    look: &crate::style::Look,
    icon: &str,
    label: &str,
    active: bool,
    hint: &str,
) -> egui::Response {
    let font = egui::FontId::new(12.0, egui::FontFamily::Name(crate::fonts::SEMIBOLD.into()));
    let galley = ui.painter().layout_no_wrap(label.to_string(), font, egui::Color32::PLACEHOLDER);
    let icon_g =
        ui.painter().layout_no_wrap(icon.to_string(), egui::FontId::proportional(15.0), egui::Color32::PLACEHOLDER);
    let inset = if active { 0.0 } else { look.px("tab-inset-top", 0.0) };
    let w = 16.0 + icon_g.size().x + 6.0 + galley.size().x + 16.0;
    let h = 8.0 + galley.size().y.max(icon_g.size().y) + 8.0;
    let (rect, resp) = ui.allocate_exact_size(egui::vec2(w, h + look.px("tab-inset-top", 0.0)), egui::Sense::click());
    let resp = resp.on_hover_cursor(egui::CursorIcon::PointingHand);
    let resp = if hint.is_empty() { resp } else { resp.on_hover_text(hint) };
    let box_rect = egui::Rect::from_min_max(egui::pos2(rect.left(), rect.top() + inset), rect.max);
    let (fill, ink, part, shadow) = if active {
        (look.c("tab-on"), look.c("on-tab-on"), "tab-on", "tab-on")
    } else if resp.hovered() {
        (look.c("tab-hover"), look.c("on-tab-hover"), "", "tab")
    } else {
        (look.c("tab"), look.c("on-tab"), "", "tab")
    };
    look.paint(ui.painter(), box_rect, part, shadow, fill);
    let icon_ink = if active { look.c("tab-on-icon") } else { ink };
    let y = box_rect.center().y;
    ui.painter().galley(egui::pos2(box_rect.left() + 16.0, y - icon_g.size().y / 2.0), icon_g.clone(), icon_ink);
    ui.painter().galley(
        egui::pos2(box_rect.left() + 16.0 + icon_g.size().x + 6.0, y - galley.size().y / 2.0),
        galley,
        ink,
    );
    resp.widget_info(|| egui::WidgetInfo::selected(egui::WidgetType::Button, true, active, label));
    resp
}
