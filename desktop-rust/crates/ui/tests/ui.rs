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
use adm_ui::{App, Start};
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
    env_at(users, w, ppp, "localhost", 0, "test")
}

fn env_at(users: usize, w: f32, ppp: f32, host: &str, port: u16, version: &'static str) -> Env {
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
    let start = Start { platform, config: Ok(loaded), version, gpu: None };
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
    e.field(0);
    e.type_text(&on.sam);
    e.h.get_by_label("Click to disable").click();
    e.h.step();
    e.h.step();
    assert!(e.h.query_by_label("Disable user").is_some(), "confirmation for {}", on.sam);
    e.click("Cancel");
    assert!(e.sent("POST", "/api/users/").is_empty());
    // a disabled account (off screen, maybe): find it by its login
    let off = users.iter().find(|u| !u.enabled && !u.protected).unwrap();
    e.field(0);
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
    e.h.get_by_label("finance").click();
    e.h.step();
    e.click("✏  Edit");
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
fn graphics_fallback_is_saved_after_three_frames_and_announced() {
    adm_core::i18n::set_lang("en");
    let dir = std::env::temp_dir().join(format!("adm-gpu-ui-{}-{}", std::process::id(), rand_suffix()));
    std::fs::create_dir_all(&dir).unwrap();
    let cleared = std::sync::Arc::new(std::sync::atomic::AtomicBool::new(false));
    let flag = cleared.clone();
    let rt = tokio::runtime::Builder::new_multi_thread().worker_threads(1).enable_all().build().unwrap();
    let mut config = Config::default();
    config.panel.url = "https://panel.domain.local:8443".into();
    let loaded = Loaded { config, path: dir.join("desktop.toml"), needs_setup: false, notice: None };
    let gpu = adm_ui::app::GpuStart {
        dir: dir.clone(),
        save: Some("dx12"),
        notice: Some(("gl", "dx12")),
        warning: None,
        choices: vec!["auto", "gl", "dx12", "vulkan", "cpu"],
        current: String::new(),
        on_started: Box::new(move || flag.store(true, std::sync::atomic::Ordering::SeqCst)),
    };
    let platform = Arc::new(TestPlatform { user_config: dir.join("user.toml") });
    let start = Start { platform, config: Ok(loaded), version: "test", gpu: Some(gpu) };
    let handle = rt.handle().clone();
    let mut h = Harness::builder()
        .with_size(egui::vec2(1280.0, 800.0))
        .build_eframe(move |cc| App::with_runtime(&cc.egui_ctx, start, handle, None).unwrap());
    // counted in completed frames, not passes: nothing is saved or cleared before GPU_START_FRAMES were painted
    let need = u64::from(adm_ui::app::GPU_START_FRAMES);
    for _ in 0..20 {
        if cleared.load(std::sync::atomic::Ordering::SeqCst) {
            break;
        }
        assert!(adm_core::config::load_graphics(&dir).is_empty(), "not saved before the frames are painted");
        h.step();
    }
    assert!(h.ctx.cumulative_frame_nr() >= need, "cleared only after {need} painted frames");
    assert!(cleared.load(std::sync::atomic::Ordering::SeqCst), "the marker is cleared once painted");
    assert_eq!(adm_core::config::load_graphics(&dir), "dx12", "a working hardware fallback is saved");
    assert!(!dir.join("desktop.toml").exists(), "desktop.toml is not written");
    assert!(
        h.query_by_label_contains("The graphics driver stopped the last start (gl); now using dx12").is_some(),
        "said before sign-in"
    );
    use eframe::App as _;
    assert!(!h.state().persist_egui_memory(), "egui's memory (text fields) never goes to the disk");
    let _ = std::fs::remove_dir_all(&dir);
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
    e.click("Sign out");
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
    let mut e = env_at(60, 1280.0, 2.0, &host, port, env!("CARGO_PKG_VERSION"));
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
    e.h.get_by_label("Finance").click();
    save(&mut e, "06-groups");
    page(&mut e, "Computers");
    save(&mut e, "07-computers");
    page(&mut e, "Shares");
    save(&mut e, "08-shares");
    page(&mut e, "Logs");
    save(&mut e, "09-logs");
    page(&mut e, "Users");
    e.field(0);
    e.type_text("bruno.silva");
    e.h.get_by_label("Set a password for bruno.silva").click();
    e.h.run_steps(3);
    e.click("Send link to the user");
    save(&mut e, "10-password-link");
    e.click("✔  OK");
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
}
