//! Against a real panel (the all-in-one test image: Samba 4 domain, panel and agent; docs/docker.md): the app signs
//! in, opens every page and tab, and no error is reported; with `ADM_LIVE_SHOTS` it saves a screenshot of each.
//! Ignored by default: `ADM_LIVE_URL` (e.g. https://127.0.0.1:18443/) and `ADM_LIVE_PASSWORD_FILE` (the test
//! domain's Administrator password, a 0600 file outside the repository) set it going. Never point it at production.
// a test binary: its helpers panic on purpose (clippy's allow-unwrap-in-tests covers only #[test] functions)
#![allow(clippy::unwrap_used, clippy::expect_used)]

use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;

use adm_core::config::{Config, Loaded};
use adm_platform::TestPlatform;
use adm_ui::app::Screen;
use adm_ui::{App, Start};
use egui_kittest::Harness;
use egui_kittest::kittest::Queryable;

/// Frames for about `ms` milliseconds (the panel's answers arrive between frames).
fn settle(h: &mut Harness<'static, App>, ms: u64) {
    for _ in 0..ms / 20 {
        h.step();
        std::thread::sleep(Duration::from_millis(20));
    }
}

/// No error dialog open: a failed request shows one (an expired session goes back to sign-in instead).
fn no_error(h: &Harness<'static, App>, page: &str) {
    let app = h.state();
    assert!(matches!(app.screen, Screen::Shell(_)), "{page}: not in the shell any more");
    assert!(app.cx.dialogs.is_empty(), "{page}: a dialog opened (an error?), status: {}", app.cx.status);
}

#[test]
#[ignore = "needs a test panel: ADM_LIVE_URL and ADM_LIVE_PASSWORD_FILE"]
fn every_page_against_a_real_panel() {
    let (Some(url), Some(pw)) = (std::env::var_os("ADM_LIVE_URL"), std::env::var_os("ADM_LIVE_PASSWORD_FILE")) else {
        return;
    };
    let password = std::fs::read_to_string(pw).unwrap().trim().to_string();
    let shots = std::env::var_os("ADM_LIVE_SHOTS").map(PathBuf::from);
    adm_core::i18n::set_lang("en");
    let rt = tokio::runtime::Builder::new_multi_thread().worker_threads(2).enable_all().build().unwrap();
    let mut config = Config::default();
    config.panel.url = url.to_string_lossy().into_owned();
    config.panel.insecure = true; // the test image's own certificate
    config.panel.user = "Administrator".into();
    config.lang = "en".into();
    let dir = std::env::temp_dir().join(format!("adm-live-{}", std::process::id()));
    std::fs::create_dir_all(&dir).unwrap();
    let loaded = Loaded { config, path: dir.join("desktop.toml"), needs_setup: false, notice: None };
    let platform = Arc::new(TestPlatform { user_config: dir.join("user.toml") });
    let start = Start { platform, config: Ok(loaded), version: "live", gpu: None };
    let handle = rt.handle().clone();
    let mut h = Harness::builder()
        .with_size(egui::vec2(1280.0, 800.0))
        .with_pixels_per_point(if shots.is_some() { 2.0 } else { 1.0 })
        .with_max_steps(10_000)
        .build_eframe(move |cc| App::with_runtime(&cc.egui_ctx, start, handle, None).unwrap());
    let save = |h: &mut Harness<'static, App>, name: &str| {
        if let Some(d) = &shots {
            h.event(egui::Event::PointerGone);
            h.run_steps(4);
            h.render().unwrap().save(d.join(format!("live-{name}.png"))).unwrap();
        }
    };
    settle(&mut h, 1500);
    h.event(egui::Event::Text(password));
    h.step();
    h.key_press(egui::Key::Enter);
    for _ in 0..500 {
        h.step();
        if matches!(h.state().screen, Screen::Shell(_)) {
            break;
        }
        std::thread::sleep(Duration::from_millis(20));
    }
    assert!(matches!(h.state().screen, Screen::Shell(_)), "signed in, status: {}", h.state().cx.status);
    settle(&mut h, 3000);
    no_error(&h, "Home");
    save(&mut h, "01-home");
    let pages = ["Users", "Groups", "OUs", "Computers", "Shares", "Import/export", "Backup", "Logs", "Settings"];
    for (i, page) in pages.iter().enumerate() {
        h.get_all_by_label(page).next().unwrap().click();
        settle(&mut h, 4000);
        no_error(&h, page);
        save(&mut h, &format!("{:02}-{}", i + 2, page.to_lowercase().replace('/', "-")));
    }
    // the tabs inside pages: Sync, the HTTP and Samba logs
    h.get_all_by_label("Backup").next().unwrap().click();
    settle(&mut h, 1500);
    h.get_all_by_label("Sync").next().unwrap().click();
    settle(&mut h, 4000);
    no_error(&h, "Sync");
    save(&mut h, "12-sync");
    h.get_all_by_label("Logs").next().unwrap().click();
    settle(&mut h, 1500);
    for (tab, name) in [("HTTP — panel", "13-http"), ("Samba — this server", "14-samba")] {
        h.get_all_by_label(tab).next().unwrap().click();
        settle(&mut h, 4000);
        no_error(&h, tab);
        save(&mut h, name);
    }
}
