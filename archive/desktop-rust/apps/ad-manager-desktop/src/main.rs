//! ad-manager-desktop: the AD Manager desktop in Rust (egui). It reads desktop.toml (the same file as the Fyne
//! app), picks the platform of the system it runs on, plans the graphics backend (crash-safe start, `adm_core::gpu`)
//! and opens the window; a configuration that cannot be read is shown in the window instead of the app.
#![cfg_attr(windows, windows_subsystem = "windows")]

mod gpu;

use std::path::PathBuf;
use std::process::ExitCode;
use std::sync::Arc;

use adm_core::gpu::{Backend, Marker, Os, Take};
use adm_platform::Platform;
use adm_ui::{App, GpuStart, Start};

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

/// The renderer for the plan: wgpu everywhere, except macOS's fallback (Metal → glow). `WGPU_BACKEND` keeps wgpu.
fn renderer(backend: Backend, forced: bool) -> eframe::Renderer {
    #[cfg(target_os = "macos")]
    if !forced && matches!(backend, Backend::Gl | Backend::Cpu) {
        return eframe::Renderer::Glow;
    }
    let _ = (backend, forced);
    eframe::Renderer::Wgpu
}

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.iter().any(|a| a == "--version" || a == "-version") {
        println!("ad-manager-desktop {}", env!("CARGO_PKG_VERSION"));
        return ExitCode::SUCCESS;
    }
    let safe = args.iter().any(|a| a == "--safe-gpu");
    let info = args.iter().any(|a| a == "--gpu-info");
    let platform = platform();
    let flag = adm_core::config::parse_flags(&args);
    let config = adm_core::config::load(platform.as_ref(), flag.as_deref()).map_err(|e| e.to_string());
    // the marker lives next to desktop.toml (the user config folder when it could not be read)
    let dir = config
        .as_ref()
        .ok()
        .and_then(|l| l.path.parent().map(PathBuf::from))
        .or_else(|| platform.user_config_path().parent().map(PathBuf::from))
        .unwrap_or_default();

    // Crash-safe graphics start: a marker left by a start that died picks a safer backend.
    let os = Os::current();
    let forced = std::env::var_os("WGPU_BACKEND").is_some();
    let preference = config.as_ref().map_or(Backend::Auto, |l| adm_core::gpu::choice(&l.config));
    let (plan, marker, warning) = match Marker::take(&dir) {
        Ok(Take::Held { mut marker, died }) => {
            let plan = adm_core::gpu::plan(preference, died, safe, forced, os);
            match marker.write(plan.marker_text()) {
                Ok(()) => (plan, Some(marker), None),
                Err(e) => (plan, None, Some(e.to_string())),
            }
        }
        // another instance is starting: its marker covers it; this launch neither reads nor writes it
        Ok(Take::Busy) => (adm_core::gpu::plan(preference, None, safe, forced, os), None, None),
        Err(e) => (adm_core::gpu::plan(preference, None, safe, forced, os), None, Some(e.to_string())),
    };
    if let Some((crashed, now)) = plan.notice() {
        eprintln!(
            "ad-manager-desktop: the graphics driver stopped the last start ({}); now using {}",
            crashed.name(),
            now.name()
        );
    }
    let renderer = renderer(plan.backend, plan.forced);
    if info {
        let backend = if plan.forced { "env (WGPU_BACKEND)" } else { plan.backend.name() };
        let r = if matches!(renderer, eframe::Renderer::Wgpu) { "wgpu" } else { "glow" };
        println!("gpu: backend {backend}, renderer {r}");
    }
    let mut viewport = egui::ViewportBuilder::default()
        .with_title("AD Manager")
        .with_app_id("io.github.jniltinho.admanager")
        .with_inner_size([1280.0, 800.0])
        .with_min_inner_size([800.0, 600.0]);
    if let Some(i) = icon() {
        viewport = viewport.with_icon(i);
    }
    let options = eframe::NativeOptions {
        viewport,
        renderer,
        // size, maximized and position are restored by eframe, which keeps the window on a connected monitor; the app
        // never moves the window itself (it cannot know monitor origins)
        persist_window: true,
        wgpu_options: gpu::configuration(plan.backend, os, plan.forced).unwrap_or_default(),
        ..Default::default()
    };
    let gpu = GpuStart {
        plan,
        warning,
        on_started: Box::new(move || {
            if let Some(mut m) = marker {
                m.clear();
            }
        }),
    };
    let start = Start { platform, config, version: env!("CARGO_PKG_VERSION"), gpu: Some(gpu) };
    // winit dlopens libxkbcommon(-x11) and panics when it is missing (a minimal desktop install): say so instead, with
    // the panic report of that one case silenced (any other panic keeps the default report)
    let report = std::panic::take_hook();
    std::panic::set_hook(Box::new(move |p| {
        if missing_library(p.payload()).is_none() {
            report(p);
        }
    }));
    let run = std::panic::catch_unwind(std::panic::AssertUnwindSafe(move || {
        eframe::run_native(
            "ad-manager-desktop",
            options,
            Box::new(move |cc| {
                // glow (macOS's fallback) has no adapter to report: only the backend line above
                if info && let Some(rs) = &cc.wgpu_render_state {
                    let a = rs.adapter.get_info();
                    println!("gpu: adapter {} ({:?}, {:?})", a.name, a.device_type, a.backend);
                }
                Ok(Box::new(App::new(&cc.egui_ctx, start)?) as Box<dyn eframe::App>)
            }),
        )
    }));
    let r = match run {
        Ok(r) => r,
        Err(panic) => match missing_library(panic.as_ref()) {
            Some(lib) => {
                eprintln!("ad-manager-desktop: cannot open the window: {lib} is not installed");
                return ExitCode::FAILURE;
            }
            None => std::panic::resume_unwind(panic),
        },
    };
    match r {
        Ok(()) => ExitCode::SUCCESS,
        Err(e) => {
            // the marker stays: the next start tries a safer backend
            eprintln!("ad-manager-desktop: cannot open the window: {e}; the next start tries a safer graphics backend");
            ExitCode::FAILURE
        }
    }
}

/// The library named by xkbcommon-dl's "Library <name> could not be loaded." panic.
fn missing_library(panic: &(dyn std::any::Any + Send)) -> Option<&str> {
    let msg = panic.downcast_ref::<&str>().copied().or_else(|| panic.downcast_ref::<String>().map(String::as_str))?;
    msg.strip_prefix("Library ")?.strip_suffix(" could not be loaded.")
}

#[cfg(test)]
mod tests {
    #[test]
    fn missing_library_message() {
        // xkbcommon-dl's own text (it dlopens the unversioned name)
        let p: Box<dyn std::any::Any + Send> =
            Box::new(String::from("Library libxkbcommon-x11.so could not be loaded."));
        assert_eq!(super::missing_library(p.as_ref()), Some("libxkbcommon-x11.so"));
        let p: Box<dyn std::any::Any + Send> = Box::new(String::from("something else"));
        assert_eq!(super::missing_library(p.as_ref()), None);
    }
}
