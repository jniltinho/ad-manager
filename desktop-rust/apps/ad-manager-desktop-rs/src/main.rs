//! ad-manager-desktop-rs: the AD Manager desktop in Rust (egui). It reads desktop.toml (the same file as the Fyne
//! app), picks the platform of the system it runs on and opens the window; a configuration that cannot be read is
//! shown in the window instead of the app.
#![cfg_attr(windows, windows_subsystem = "windows")]

use std::process::ExitCode;
use std::sync::Arc;

use adm_platform::Platform;
use adm_ui::{App, Start};

#[cfg(target_os = "linux")]
fn platform() -> Arc<dyn Platform> {
    Arc::new(adm_platform_linux::Linux)
}

#[cfg(windows)]
fn platform() -> Arc<dyn Platform> {
    Arc::new(adm_platform_windows::Windows)
}

#[cfg(target_os = "macos")]
fn platform() -> Arc<dyn Platform> {
    Arc::new(adm_platform_macos::MacOs)
}

fn icon() -> Option<egui::IconData> {
    let img = image::load_from_memory(adm_core::assets::ICON).ok()?.into_rgba8();
    let (w, h) = img.dimensions();
    Some(egui::IconData { rgba: img.into_raw(), width: w, height: h })
}

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.iter().any(|a| a == "--version" || a == "-version") {
        println!("ad-manager-desktop-rs {}", env!("CARGO_PKG_VERSION"));
        return ExitCode::SUCCESS;
    }
    let platform = platform();
    let flag = adm_core::config::parse_flags(&args);
    let config = adm_core::config::load(platform.as_ref(), flag.as_deref()).map_err(|e| e.to_string());
    let mut viewport = egui::ViewportBuilder::default()
        .with_title("AD Manager")
        .with_app_id("io.github.jniltinho.admanager.rs")
        .with_inner_size([1280.0, 800.0])
        .with_min_inner_size([800.0, 600.0]);
    if let Some(i) = icon() {
        viewport = viewport.with_icon(i);
    }
    let options = eframe::NativeOptions { viewport, ..Default::default() };
    let start = Start { platform, config, version: env!("CARGO_PKG_VERSION") };
    let r = eframe::run_native(
        "ad-manager-desktop-rs",
        options,
        Box::new(move |cc| Ok(Box::new(App::new(&cc.egui_ctx, start)?) as Box<dyn eframe::App>)),
    );
    match r {
        Ok(()) => ExitCode::SUCCESS,
        Err(e) => {
            eprintln!("ad-manager-desktop-rs: {e}");
            ExitCode::FAILURE
        }
    }
}
