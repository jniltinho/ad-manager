//! Acceptance tests of the interface: the real app, driven through AccessKit (clicks, keys, typed text), against the
//! in-process mock panel, asserting the requests the panel received.
// a test binary: its helpers panic on purpose (clippy's allow-unwrap-in-tests covers only #[test] functions)
#![allow(clippy::unwrap_used, clippy::expect_used)]

use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;

use adm_core::config::{Config, Loaded};
use adm_core::testkit::{MockPanel, Req, demo};
use adm_platform::TestPlatform;
use adm_ui::app::Screen;
use adm_ui::{App, GpuStart, Start};
use egui_kittest::Harness;
use egui_kittest::kittest::{NodeT, Queryable};

struct Env {
    h: Harness<'static, App>,
    mock: MockPanel,
    _rt: tokio::runtime::Runtime,
}

/// The app signed out, on the sign-in screen of the mock (English), its window `w` × 800 at `ppp` pixels per point.
fn env_sized(users: usize, w: f32) -> Env {
    env_scaled(users, w, 1.0)
}

fn env_scaled(users: usize, w: f32, ppp: f32) -> Env {
    env_at(users, w, ppp, "localhost", 0, "test", |_| None)
}

fn env_at(
    users: usize,
    w: f32,
    ppp: f32,
    host: &str,
    port: u16,
    version: &'static str,
    gpu: impl FnOnce(&std::path::Path) -> Option<GpuStart>,
) -> Env {
    adm_core::i18n::set_lang("en");
    let rt = tokio::runtime::Builder::new_multi_thread().worker_threads(2).enable_all().build().unwrap();
    let mock = rt.block_on(MockPanel::start_at(host, port, demo(users))).unwrap();
    let mut config = Config::default();
    config.panel.url = mock.url.clone();
    config.panel.fingerprint = mock.pin.clone();
    config.panel.user = "administrator".into();
    config.lang = "en".into();
    let dir = std::env::temp_dir().join(format!("adm-ui-{}-{}", std::process::id(), rand_suffix()));
    std::fs::create_dir_all(&dir).unwrap();
    let loaded = Loaded { config, path: dir.join("desktop.toml"), needs_setup: false, notice: None };
    let platform = Arc::new(TestPlatform { user_config: dir.join("user.toml") });
    let start = Start { platform, config: Ok(loaded), version, gpu: gpu(&dir) };
    let handle = rt.handle().clone();
    let h = Harness::builder()
        .with_size(egui::vec2(w, 800.0))
        .with_pixels_per_point(ppp)
        .with_max_steps(10_000)
        .build_eframe(move |cc| App::with_runtime(&cc.egui_ctx, start, handle, None).unwrap());
    let mut e = Env { h, mock, _rt: rt };
    e.h.step();
    e
}

fn env() -> Env {
    env_sized(60, 1280.0)
}

fn rand_suffix() -> u128 {
    std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map_or(0, |d| d.as_nanos())
}

impl Env {
    fn app(&self) -> &App {
        self.h.state()
    }

    /// Steps frames until `cond` holds (background answers arrive between frames).
    fn wait(&mut self, what: &str, cond: impl Fn(&App, &MockPanel) -> bool) {
        for _ in 0..600 {
            self.h.step();
            if cond(self.app(), &self.mock) {
                self.h.step();
                self.h.step();
                return;
            }
            std::thread::sleep(Duration::from_millis(10));
        }
        panic!("timed out waiting for {what}");
    }

    fn sent(&self, method: &str, prefix: &str) -> Vec<Req> {
        self.mock.sent(method, prefix)
    }

    /// Focuses the `n`th text field on screen (dialogs are drawn last: -1 is the top dialog's last field).
    fn field(&mut self, n: isize) {
        let fields: Vec<_> = self.h.get_all_by_role(egui::accesskit::Role::TextInput).collect();
        let i = if n < 0 { fields.len() as isize + n } else { n } as usize;
        fields[i].focus();
        self.h.step();
        self.h.key_press_modifiers(egui::Modifiers::COMMAND, egui::Key::A);
        self.h.step();
    }

    fn type_text(&mut self, s: &str) {
        self.h.event(egui::Event::Text(s.into()));
        self.h.step();
    }

    fn key(&mut self, k: egui::Key) {
        self.h.key_press(k);
        self.h.step();
    }

    fn click(&mut self, label: &str) {
        self.h.get_by_label_contains(label).click();
        self.h.step();
        self.h.step();
    }

    fn sign_in(&mut self) {
        self.type_text("Secret#1");
        self.key(egui::Key::Enter);
        self.wait("the shell", |a, _| matches!(a.screen, Screen::Shell(_)));
    }

    fn users_page(&mut self) {
        self.sign_in();
        self.h.get_by_label("Users").click();
        self.wait("the users", |a, _| match &a.screen {
            Screen::Shell(s) => s.users.visible().count() > 0,
            _ => false,
        });
    }

    fn visible_users(&self) -> Vec<adm_core::model::User> {
        match &self.app().screen {
            Screen::Shell(s) => s.users.visible().cloned().collect(),
            _ => Vec::new(),
        }
    }
}

/// A start after a crash: the notice is under the sign-in box; after 3 painted frames the marker is cleared and the
/// hardware fallback saved as `[graphics]` in desktop.toml (the rest of the file as configured); after sign-in the
/// notice is in the status line.
#[test]
fn graphics_fallback_start_is_announced_and_saved_after_three_frames() {
    use adm_core::gpu::{self, Backend, Died, Os};
    use std::sync::atomic::{AtomicBool, Ordering};
    let cleared = Arc::new(AtomicBool::new(false));
    let flag = cleared.clone();
    let mut e = env_at(5, 1280.0, 1.0, "localhost", 0, "test", |dir| {
        // an existing desktop.toml (not a first run), rewritten with the session's configuration
        std::fs::write(dir.join("desktop.toml"), "lang = \"en\"\n").unwrap();
        Some(GpuStart {
            plan: gpu::plan(Backend::Auto, Some(Died::On(Backend::Gl)), false, false, Os::Windows),
            warning: None,
            on_started: Box::new(move || flag.store(true, Ordering::SeqCst)),
        })
    });
    let notice = "The graphics driver stopped the last start (gl); now using dx12. Change it in Configure.";
    assert!(e.h.query_by_label(notice).is_some(), "notice under the sign-in box");
    for _ in 0..4 {
        e.h.step();
    }
    assert!(cleared.load(Ordering::SeqCst), "marker cleared after the first frames");
    let path = e.app().cx.cfg_path.clone();
    let saved = adm_core::config::decode(&std::fs::read_to_string(&path).unwrap()).unwrap();
    assert_eq!(gpu::choice(&saved), Backend::Dx12, "the working fallback is saved in desktop.toml");
    assert_eq!(saved.panel.url, e.mock.url, "the rest of the configuration kept");
    e.sign_in();
    assert_eq!(e.app().cx.status, notice, "and said again in the status line");
}

#[test]
fn sign_in_sends_the_password_once_and_opens_the_shell() {
    let mut e = env();
    e.sign_in();
    let login = e.sent("POST", "/api/login");
    assert_eq!(login.len(), 1);
    assert_eq!(login[0].json()["user"], "administrator");
    assert_eq!(login[0].json()["password"], "Secret#1");
    assert_eq!(e.app().cx.look.id, "classic-blue", "the session's theme");
    // Home loads; Users does not until it is opened
    e.wait("the summary", |_, m| !m.sent("GET", "/api/summary").is_empty());
    assert!(e.sent("GET", "/api/computers").is_empty());
}

#[test]
fn delete_link_acts_on_its_row_not_the_selected_one() {
    let mut e = env();
    e.users_page();
    let users = e.visible_users();
    let me = adm_core::model::Me { user: "administrator".into(), ..Default::default() };
    let editable: Vec<&adm_core::model::User> = users.iter().filter(|u| adm_core::logic::editable(u, &me)).collect();
    let (a, b) = (editable[0].clone(), editable[1].clone());
    // select row A by its name, then click the delete link of row B
    e.h.get_all_by_label(&a.name).next().unwrap().click();
    e.h.step();
    // the link's accessible name says what it does to whom (the icon alone is drawn at 1280 px)
    e.h.get_by_label(&format!("Delete {}", b.sam)).click();
    e.h.step();
    e.h.step();
    // a wrong login sends nothing
    e.type_text("nobody");
    e.click("Confirm");
    assert!(e.h.query_by_label("The text does not match.").is_some());
    assert!(e.sent("DELETE", "/api/users/").is_empty());
    // B's login deletes B
    e.field(-1);
    e.type_text(&b.sam.to_uppercase());
    e.click("Confirm");
    e.wait("the delete", |_, m| !m.sent("DELETE", "/api/users/").is_empty());
    let sent = e.sent("DELETE", "/api/users/");
    assert_eq!(sent.len(), 1);
    assert_eq!(sent[0].path, format!("/api/users/{}", b.sam));
}

#[test]
fn cancelled_disable_sends_nothing_and_enable_needs_no_confirmation() {
    let mut e = env();
    e.users_page();
    let users = e.visible_users();
    let on = users.iter().find(|u| !u.protected && u.enabled && !u.locked).unwrap();
    e.field(1); // 0 is the header's global search
    e.type_text(&on.sam);
    e.h.get_by_label("Click to disable").click();
    e.h.step();
    e.h.step();
    assert!(e.h.query_by_label("Disable user").is_some(), "confirmation for {}", on.sam);
    e.click("Cancel");
    assert!(e.sent("POST", "/api/users/").is_empty());
    // a disabled account (off screen, maybe): find it by its login
    let off = users.iter().find(|u| !u.enabled && !u.protected).unwrap();
    e.field(1); // 0 is the header's global search
    e.type_text(&off.sam);
    e.h.get_by_label("Click to enable").click();
    e.wait("the enable", |_, m| m.sent("POST", "/api/users/").iter().any(|r| r.path.ends_with("/enable")));
}

#[test]
fn enter_opens_the_menu_of_the_selected_row() {
    let mut e = env();
    e.users_page();
    // select the Administrator row by its plain "system" tag (its login and name are both links/duplicates)
    e.h.get_all_by_label("system").next().unwrap().click();
    e.h.step();
    e.h.step();
    assert!(matches!(&e.app().screen, Screen::Shell(s) if s.users.selected() == Some(0)), "the tag selects its row");
    e.key(egui::Key::Enter);
    for item in ["Edit", "Password", "Copy"] {
        assert!(e.h.query_by_label(item).is_some(), "{item}");
    }
    // a built-in account: no toggle, move or delete in its menu
    assert!(e.h.query_by_label("Move").is_none());
    assert!(e.h.query_by_label("Delete").is_none());
    e.key(egui::Key::Escape);
    assert!(e.h.query_by_label("Edit").is_none());
}

#[test]
fn samba_tabs_load_only_when_opened() {
    let mut e = env();
    e.sign_in();
    e.h.get_by_label("Logs").click();
    e.wait("the audit", |_, m| !m.sent("GET", "/api/audit").is_empty());
    e.wait("the file servers", |_, m| !m.sent("GET", "/api/agents").is_empty());
    assert!(e.sent("GET", "/api/samba").is_empty() && e.sent("GET", "/api/agents/1/samba").is_empty());
    e.click("Samba — this server");
    e.wait("this server's logs", |_, m| !m.sent("GET", "/api/samba/logs").is_empty());
    assert!(e.sent("GET", "/api/agents/1/samba").is_empty(), "another server's logs were not opened");
}

/// Configure › Graphics lists automatic and the OS's backends; a choice is saved at once as `[graphics]` in
/// desktop.toml (no panel test, the rest of the file unchanged), and applies at the next start.
#[test]
fn configure_graphics_choice_is_saved_alone() {
    use adm_core::gpu::{self, Backend, Os};
    let mut e = env();
    e.click("Configure");
    assert!(matches!(e.app().screen, Screen::Setup(_)));
    let path = e.app().cx.cfg_path.clone();
    adm_core::config::save(&TestPlatform { user_config: path.with_file_name("user.toml") }, &path, &e.app().cx.cfg)
        .unwrap();
    e.h.get_by_label("Rendering backend").click();
    e.h.step();
    let pick = if Os::current() == Os::Macos { Backend::Gl } else { Backend::Cpu };
    e.h.get_by_label(pick.label(Os::current())).click();
    e.h.step();
    e.h.step();
    let saved = adm_core::config::decode(&std::fs::read_to_string(&path).unwrap()).unwrap();
    assert_eq!(gpu::choice(&saved), pick, "saved at once as [graphics] in desktop.toml");
    assert_eq!(saved.panel.url, e.mock.url, "the rest of the configuration kept");
    assert!(
        e.h.query_by_label("Applies at the next start. After a driver crash AD Manager picks a safer one.").is_some()
    );
    assert!(e.mock.sent("POST", "/api/login").is_empty(), "no panel test");
}

#[test]
fn configure_test_abandoned_by_an_edit_is_signed_out() {
    let mut e = env();
    e.click("Configure");
    assert!(matches!(e.app().screen, Screen::Setup(_)));
    // the password field has the focus
    e.type_text("Secret#1");
    e.click("Test connection");
    // edit the URL before (or right after) the answer: either way the tested session must end
    e.field(0);
    let url = e.mock.url.clone();
    e.type_text(&format!("{url}/"));
    e.wait("the test", |_, m| !m.sent("POST", "/api/login").is_empty());
    e.wait("the sign-out", |_, m| !m.sent("POST", "/api/logout").is_empty());
    let save = e.h.get_by_label_contains("Save");
    assert!(save.accesskit_node().is_disabled(), "Save stays disabled after an edit");
}

#[test]
fn share_edit_sends_the_same_name_and_the_lists() {
    let mut e = env();
    e.sign_in();
    e.h.get_by_label("Shares").click();
    e.wait("the shares", |_, m| !m.sent("GET", "/api/agents/1/shares").is_empty());
    e.wait("the list", |a, _| matches!(&a.screen, Screen::Shell(_)));
    e.h.step();
    e.click("Edit finance"); // the row's edit link
    assert!(e.h.query_by_label_contains("Edit share finance").is_some());
    e.click("✔  Save");
    e.wait("the task", |_, m| !m.sent("POST", "/api/agents/1/tasks").is_empty());
    let t = &e.sent("POST", "/api/agents/1/tasks")[0].json();
    assert_eq!(t["type"], "share:create");
    assert_eq!(t["payload"]["name"], "finance");
    assert_eq!(t["payload"]["path"], "/srv/shares/finance");
    assert_eq!(t["payload"]["rw_groups"], serde_json::json!(["DOMAIN\\Finance"]));
    assert_eq!(t["payload"]["ro_groups"], serde_json::json!(["DOMAIN\\Auditors"]));
}

#[test]
fn compact_icons_at_1280_and_names_when_wide() {
    let compact = |e: &Env| matches!(&e.app().screen, Screen::Shell(s) if s.users.compact());
    let mut e = env();
    e.users_page();
    assert!(compact(&e), "1280 px: icons only");
    let mut w = env_sized(60, 1900.0);
    w.users_page();
    assert!(!compact(&w), "wide: icon and name");
}

#[test]
fn password_field_history_goes_with_the_sign_in_screen() {
    let mut e = env();
    let id = match &e.app().screen {
        Screen::Login(l) => l.password_field_id(),
        _ => panic!("not on the sign-in screen"),
    };
    e.type_text("Secret#1");
    assert!(e.h.ctx.data_mut(|d| d.get_persisted::<egui::text_edit::TextEditState>(id)).is_some());
    e.key(egui::Key::Enter);
    e.wait("the shell", |a, _| matches!(a.screen, Screen::Shell(_)));
    assert!(
        e.h.ctx.data_mut(|d| d.get_persisted::<egui::text_edit::TextEditState>(id)).is_none(),
        "the password's text and undo history are gone"
    );
}

#[test]
fn sign_out_starts_a_new_session() {
    let mut e = env();
    e.sign_in();
    let first = e.app().cx.panel.id;
    e.click("sign out");
    e.wait("the sign-in screen", |a, _| matches!(a.screen, Screen::Login(_)));
    assert_ne!(e.app().cx.panel.id, first, "a new cookie jar and identity: replies of the old session are dropped");
    e.wait("the sign-out", |_, m| !m.sent("POST", "/api/logout").is_empty());
}

#[test]
fn a_reply_reaches_only_the_dialog_that_asked() {
    let mut e = env();
    let a = e.h.state_mut().cx.push(adm_ui::modal::Info::new("A", "first"));
    e.h.state_mut().cx.dialogs.retain(|d| d.0 != a); // A closed
    let b = e.h.state_mut().cx.push(adm_ui::modal::Info::new("B", "second"));
    assert!(e.h.state_mut().cx.dialog_mut::<adm_ui::modal::Info>(a).is_none(), "A's reply has nowhere to go");
    assert!(e.h.state_mut().cx.dialog_mut::<adm_ui::modal::Info>(b).is_some());
}

/// Screenshots of the docs (2×, Classic blue, English, Example Company Ltd, domain.local), written only when
/// ADM_SHOTS is set to a folder; the mock answers as https://panel.domain.local:8443 when ADM_SHOTS_HOST names it
/// (mapped to 127.0.0.1): `make shots`.
#[test]
#[ignore = "writes screenshots; needs a GPU or lavapipe"]
fn shots() {
    let Some(dir) = std::env::var_os("ADM_SHOTS").map(PathBuf::from) else { return };
    let host = std::env::var("ADM_SHOTS_HOST").unwrap_or_else(|_| "localhost".into());
    let port = if host == "localhost" { 0 } else { 8443 };
    let mut e = env_at(60, 1280.0, 2.0, &host, port, env!("CARGO_PKG_VERSION"), |_| None);
    let save = |e: &mut Env, name: &str| {
        std::thread::sleep(Duration::from_millis(300));
        e.h.run_steps(4);
        e.h.render().unwrap().save(dir.join(format!("desktop-rs-{name}.png"))).unwrap();
    };
    let page = |e: &mut Env, label: &str| {
        // the sidebar is drawn first: its item comes before a cell with the same text (the Users container)
        e.h.get_all_by_label(label).next().unwrap().click();
        std::thread::sleep(Duration::from_millis(300));
        e.h.run_steps(4);
    };
    e.wait("the brand", |a, _| a.cx.look.id == "classic-blue");
    save(&mut e, "01-login");
    // Configure: a successful test of the configured (pinned) panel
    e.click("Configure");
    e.type_text("Secret#1");
    e.click("Test connection");
    e.wait("the test", |_, m| !m.sent("GET", "/api/me").is_empty());
    save(&mut e, "02-configure");
    e.click("Cancel");
    e.sign_in();
    save(&mut e, "03-home");
    page(&mut e, "Users");
    e.wait("the users", |a, _| matches!(&a.screen, Screen::Shell(s) if s.users.visible().count() > 0));
    save(&mut e, "04-users");
    e.click("➕  New user");
    save(&mut e, "05-new-user");
    e.key(egui::Key::Escape);
    page(&mut e, "Groups");
    e.wait("the groups", |a, _| matches!(&a.screen, Screen::Shell(s) if s.groups.count() > 0));
    save(&mut e, "06-groups");
    page(&mut e, "Computers");
    save(&mut e, "07-computers");
    page(&mut e, "Shares");
    save(&mut e, "08-shares");
    page(&mut e, "Logs");
    save(&mut e, "09-logs");
    page(&mut e, "Users");
    e.field(1); // 0 is the header's global search
    e.type_text("bruno.silva");
    e.h.get_by_label("Set a password for bruno.silva").click();
    e.h.run_steps(3);
    e.click("Send link to the user");
    save(&mut e, "10-password-link");
    e.click("✔  Generate link");
    e.wait("the link", |_, m| !m.sent("POST", "/api/users/bruno.silva/password-link").is_empty());
    save(&mut e, "11-link-generated");
    e.key(egui::Key::Escape);
    page(&mut e, "OUs");
    save(&mut e, "12-ous");
    page(&mut e, "Users");
    e.click("Password policy");
    save(&mut e, "13-password-policy");
    e.key(egui::Key::Escape);
    page(&mut e, "Backup");
    save(&mut e, "14-backup");
    page(&mut e, "Sync");
    save(&mut e, "15-sync");
    page(&mut e, "Computers");
    e.click("🌐  DNS");
    e.wait("the records", |_, m| !m.sent("GET", "/api/dns/records").is_empty());
    save(&mut e, "16-dns");
    e.key(egui::Key::Escape);
    e.h.get_by_label("SMB sessions on DC01").click();
    e.wait("the sessions", |_, m| !m.sent("GET", "/api/computers/sessions").is_empty());
    save(&mut e, "17-smb-sessions");
    e.key(egui::Key::Escape);
    page(&mut e, "Shares");
    e.click("👥  Sessions");
    e.wait("the sessions", |_, m| !m.sent("GET", "/api/agents/1/sessions").is_empty());
    save(&mut e, "18-share-sessions");
    e.key(egui::Key::Escape);
    page(&mut e, "Logs");
    e.click("Samba — this server");
    e.wait("the entries", |_, m| !m.sent("GET", "/api/samba/logs/").is_empty());
    save(&mut e, "19-logs-samba");
    e.click("HTTP — panel");
    e.wait("the requests", |_, m| !m.sent("GET", "/api/http-log").is_empty());
    save(&mut e, "20-logs-http");
}
