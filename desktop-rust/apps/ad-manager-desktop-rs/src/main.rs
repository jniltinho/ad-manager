//! ad-manager-desktop-rs: the AD Manager desktop in Rust (egui). It reads desktop.toml (the same file as the Fyne
//! app), picks the platform of the system it runs on, plans its graphics backend (a crash-safe start, see `gpu`) and
//! opens the window; a configuration that cannot be read is shown in the window instead of the app.
#![cfg_attr(windows, windows_subsystem = "windows")]

mod gpu;

use std::path::{Path, PathBuf};
use std::process::ExitCode;
use std::sync::Arc;

use adm_platform::Platform;
use adm_ui::app::GpuStart;
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

const USAGE: &str = "usage: ad-manager-desktop-rs [--config PATH] [--safe-gpu] [--gpu-info] [--version]
  --config PATH  desktop.toml to use (also AD_MANAGER_DESKTOP)
  --safe-gpu     software rendering for this launch (a graphics driver keeps crashing)
  --gpu-info     print the renderer and graphics adapter in use once the window is up
  WGPU_BACKEND   (environment) forces wgpu's backend choice; nothing is saved";

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let has = |f: &str| args.iter().any(|a| a == f || a == &f[1..]);
    if has("--version") {
        println!("ad-manager-desktop-rs {}", env!("CARGO_PKG_VERSION"));
        return ExitCode::SUCCESS;
    }
    if has("--help") {
        println!("{USAGE}");
        return ExitCode::SUCCESS;
    }
    let platform = platform();
    let flag = adm_core::config::parse_flags(&args);
    let config = adm_core::config::load(platform.as_ref(), flag.as_deref()).map_err(|e| e.to_string());
    // the folder of desktop.toml holds the start marker, graphics.toml and the window's place
    let dir = match &config {
        Ok(l) => l.path.parent().map(Path::to_path_buf),
        Err(_) => platform.user_config_path().parent().map(Path::to_path_buf),
    }
    .unwrap_or_else(|| PathBuf::from("."));

    // crash-safe graphics start: a marker left by a start that died picks a safer backend
    let os = gpu::Os::current();
    let forced = std::env::var_os("WGPU_BACKEND").is_some();
    let saved = adm_core::config::load_graphics(&dir);
    let state = gpu::state_dir(platform.as_ref(), &dir);
    let died = state.as_ref().ok().and_then(|d| gpu::Marker::died(d));
    let plan = gpu::plan(gpu::Backend::parse(&saved), died, has("--safe-gpu"), forced, os);
    let marker = state.clone().and_then(|d| gpu::Marker::start(&d, plan.marker_name()).map_err(|e| e.to_string()));
    let (marker, warning) = match marker {
        Ok(m) => (m, None),
        Err(e) => (None, Some(e)),
    };
    if let Some(w) = &warning {
        // said now (a crash may follow) and on the sign-in screen
        eprintln!("ad-manager-desktop-rs: the graphics start protection is off: {w}");
    }
    let gpu_start = GpuStart {
        dir: dir.clone(),
        save: plan.to_save().map(gpu::Backend::name),
        notice: plan.notice().map(|(a, b)| (a.name(), b.name())),
        warning,
        choices: gpu::choices(os),
        current: if saved.is_empty() { "auto".into() } else { gpu::Backend::parse(&saved).name().into() },
        on_started: Box::new(move || {
            if let Some(mut m) = marker {
                m.clear();
            }
        }),
    };

    // macOS falls back from Metal (wgpu) to the glow renderer; every other platform renders through wgpu
    #[cfg(target_os = "macos")]
    let renderer = if !forced && matches!(plan.backend, gpu::Backend::Gl | gpu::Backend::Cpu) {
        eframe::Renderer::Glow
    } else {
        eframe::Renderer::Wgpu
    };
    #[cfg(not(target_os = "macos"))]
    let renderer = eframe::Renderer::Wgpu;

    let mut viewport = egui::ViewportBuilder::default()
        .with_title("AD Manager")
        .with_app_id("io.github.jniltinho.admanager.rs")
        .with_inner_size([1280.0, 800.0])
        .with_min_inner_size([800.0, 600.0]);
    if let Some(i) = icon() {
        viewport = viewport.with_icon(i);
    }
    let options = eframe::NativeOptions {
        viewport,
        renderer,
        // eframe restores the window where it was (and keeps it on a monitor); stored in the private state folder,
        // and not at all when that folder was refused
        persist_window: state.is_ok(),
        persistence_path: state.as_ref().ok().map(|d| d.join("window")),
        wgpu_options: gpu::configuration(plan.backend, os, forced).unwrap_or_default(),
        ..Default::default()
    };
    let gpu_info = has("--gpu-info");
    let start = Start { platform, config, version: env!("CARGO_PKG_VERSION"), gpu: Some(gpu_start) };

    // winit dlopens libxkbcommon(-x11) and panics when it is missing: report that as a startup error; any other
    // panic propagates unchanged
    let run = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| {
        eframe::run_native(
            "ad-manager-desktop-rs",
            options,
            Box::new(move |cc| {
                if gpu_info {
                    print_gpu(cc);
                }
                let app = App::new(&cc.egui_ctx, start)?;
                Ok(Box::new(app) as Box<dyn eframe::App>)
            }),
        )
    }));
    let result = match run {
        Ok(r) => r.map_err(|e| format!("cannot open the window: {e}; the next start tries a safer graphics backend")),
        Err(panic) => match missing_library(panic.as_ref()) {
            Some(lib) => Err(format!("cannot open the window: {lib} is not installed")),
            None => std::panic::resume_unwind(panic),
        },
    };
    match result {
        Ok(()) => ExitCode::SUCCESS,
        Err(e) => {
            eprintln!("ad-manager-desktop-rs: {e}");
            ExitCode::FAILURE
        }
    }
}

/// The renderer and adapter in use (`--gpu-info`, read by bench/run.sh).
fn print_gpu(cc: &eframe::CreationContext<'_>) {
    use std::io::Write as _;
    let line = match cc.wgpu_render_state.as_ref() {
        Some(s) => {
            let i = s.adapter.get_info();
            format!("gpu: wgpu {:?} {:?} {} ({})", i.backend, i.device_type, i.name, i.driver)
        }
        None => "gpu: glow".to_string(),
    };
    let mut out = std::io::stdout();
    let _ = writeln!(out, "{line}");
    let _ = out.flush();
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
        let p: Box<dyn std::any::Any + Send> = Box::new("Library libxkbcommon-x11.so.0 could not be loaded.");
        assert_eq!(super::missing_library(p.as_ref()), Some("libxkbcommon-x11.so.0"));
        let other: Box<dyn std::any::Any + Send> = Box::new(String::from("index out of bounds"));
        assert_eq!(super::missing_library(other.as_ref()), None);
    }
}
