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
    // the file's line, its spaces kept (only the line ending goes)
    let password = std::fs::read_to_string(pw).unwrap().trim_end_matches(['\n', '\r']).to_string();
    let shots = std::env::var_os("ADM_LIVE_SHOTS").map(PathBuf::from);
    if let Some(d) = &shots {
        std::fs::create_dir_all(d).unwrap();
    }
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
    // ADM_LIVE_SIZE=WxH for another window (e.g. 1856x980 to compare a wide browser)
    let size = std::env::var("ADM_LIVE_SIZE")
        .ok()
        .and_then(|v| v.split_once('x').and_then(|(w, h)| Some(egui::vec2(w.parse().ok()?, h.parse().ok()?))))
        .unwrap_or(egui::vec2(1280.0, 800.0));
    let mut h = Harness::builder()
        .with_size(size)
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
    // every theme chosen in Configure › Appearance: applied at once, saved in desktop.toml, and Home in it
    // the file the theme is saved into (Configure keeps the choice in it at once)
    let cfg = h.state().cx.cfg_path.clone();
    let user_toml = cfg.with_file_name("user.toml");
    adm_core::config::save(&TestPlatform { user_config: user_toml }, &cfg, &h.state().cx.cfg).unwrap();
    for id in adm_core::theme::order() {
        let label = adm_core::i18n::t(&adm_core::theme::palette(id).unwrap().1.label).to_string();
        h.get_all_by_label("Configure").next().unwrap().click();
        settle(&mut h, 600);
        h.get_by_label("Theme").click();
        h.run_steps(3); // the popup's first frame only measures it
        h.get_all_by_label(&label).last().unwrap().click_accesskit();
        h.run_steps(3);
        assert_eq!(&h.state().cx.look.id, id, "{label}: applied at once");
        let saved = adm_core::config::decode(&std::fs::read_to_string(&cfg).unwrap()).unwrap();
        assert_eq!(&saved.appearance.theme, id, "{label}: saved in desktop.toml");
        h.get_all_by_label("Cancel").last().unwrap().click();
        settle(&mut h, 600);
        h.get_all_by_label("Home").next().unwrap().click();
        settle(&mut h, 2500);
        no_error(&h, &label);
        save(&mut h, &format!("theme-{id}"));
    }
    // a Samba tab shows a failure inline (no dialog): its list must have loaded without one
    match &h.state().screen {
        Screen::Shell(s) => {
            assert!(s.logs.samba_loaded().first().copied().unwrap_or(false), "the Samba files were asked for");
            assert!(s.logs.samba_errors().iter().all(String::is_empty), "Samba: {:?}", s.logs.samba_errors());
        }
        _ => panic!("left the shell"),
    }
}
