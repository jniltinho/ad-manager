//! Crash-safe graphics start, the design of rustXterm (`apps/rustxterm/src/gpu.rs`). A driver that crashes while the
//! window's graphics device is created kills the process where no Rust error handling runs, so every launch would
//! die the same way. Before the window exists the app writes a marker (`gpu-starting` in the folder of desktop.toml:
//! the backend being tried) under an OS lock and removes it once the first frames painted. A later launch that finds
//! a marker nobody holds knows that start died, and tries the next safer backend:
//!
//! - Windows: OpenGL → DirectX 12 → Vulkan → software (WARP)
//! - Linux: Vulkan → OpenGL → software (llvmpipe/lavapipe)
//! - macOS: Metal (wgpu) → OpenGL (eframe's glow renderer); `Cpu` also means glow there, the safest renderer
//!
//! A hardware fallback that worked is saved to `graphics.toml` (Configure › Graphics changes or resets it); software
//! is never saved silently. `--safe-gpu` forces software for one launch. `WGPU_BACKEND` forces wgpu's own choice: such
//! a launch promotes and saves nothing, and its marker (`env`) is not a crash of any rung.
//!
//! Differences from rustXterm: ownership is a lock on a separate, never-removed `gpu-starting.lock` (there, a second
//! starting instance could truncate the first one's marker, or lock a marker already unlinked); the removable marker
//! is written and deleted only by the lock holder. Both live in a private state folder that is refused when it is a
//! link or open to others. Forced launches as above.

use std::fs::{File, OpenOptions, TryLockError};
use std::io::{Read, Write};
use std::path::{Path, PathBuf};
use std::sync::Arc;

use eframe::egui_wgpu::{WgpuConfiguration, WgpuSetup};
use eframe::wgpu::{self, Backends, DeviceType};

/// Marker file name in the state folder.
pub const MARKER: &str = "gpu-starting";

/// The lock that says who owns the marker: never removed, so its lock is always on the same file.
const LOCK: &str = "gpu-starting.lock";

/// The state folder of the graphics start and the window's place: `desktop-rs/` beside desktop.toml, created
/// private. Refused (no crash protection, no saved window) when it is a link, not a folder, or open to others, so
/// nothing this app writes there can be redirected into another file.
pub fn state_dir(platform: &dyn adm_platform::Platform, config_dir: &Path) -> Result<PathBuf, String> {
    let dir = config_dir.join("desktop-rs");
    platform.create_private_dir(&dir).map_err(|e| format!("{}: {e}", dir.display()))?;
    let meta = std::fs::symlink_metadata(&dir).map_err(|e| format!("{}: {e}", dir.display()))?;
    if meta.file_type().is_symlink() || !meta.is_dir() {
        return Err(format!("{} is not a folder of its own", dir.display()));
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mode = meta.permissions().mode() & 0o777;
        if mode & 0o077 != 0 {
            return Err(format!("{} must be private and has mode {mode:04o}: run chmod 700", dir.display()));
        }
    }
    Ok(dir)
}

/// Opens the lock file without truncating it and without following a link.
fn lock_file(dir: &Path) -> std::io::Result<File> {
    let path = dir.join(LOCK);
    if std::fs::symlink_metadata(&path).is_ok_and(|m| m.file_type().is_symlink()) {
        return Err(std::io::Error::other(format!("{} is a link", path.display())));
    }
    OpenOptions::new().create(true).truncate(false).read(true).write(true).open(path)
}

/// What a forced launch (`WGPU_BACKEND`) writes in the marker.
const FORCED: &str = "env";

/// Rendering backend; stored lowercase in `graphics.toml`.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Backend {
    /// Platform default: hardware OpenGL first on Windows, wgpu's choice elsewhere.
    Auto,
    Gl,
    Dx12,
    Vulkan,
    /// Software rendering (WARP on Windows, llvmpipe/lavapipe elsewhere, glow on macOS).
    Cpu,
}

impl Backend {
    pub fn parse(s: &str) -> Self {
        match s.trim().to_ascii_lowercase().as_str() {
            "gl" | "opengl" => Self::Gl,
            "dx12" => Self::Dx12,
            "vulkan" => Self::Vulkan,
            "cpu" | "software" => Self::Cpu,
            _ => Self::Auto,
        }
    }

    pub fn name(self) -> &'static str {
        match self {
            Self::Auto => "auto",
            Self::Gl => "gl",
            Self::Dx12 => "dx12",
            Self::Vulkan => "vulkan",
            Self::Cpu => "cpu",
        }
    }
}

/// Host OS: the ladder and the wgpu/renderer choice differ per platform.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Os {
    Windows,
    Linux,
    Macos,
}

impl Os {
    pub fn current() -> Self {
        if cfg!(windows) {
            Self::Windows
        } else if cfg!(target_os = "macos") {
            Self::Macos
        } else {
            Self::Linux
        }
    }
}

/// Backends from the preferred one to the safest, per platform. On macOS wgpu only exposes Metal: Metal (`Auto`) →
/// the glow renderer (`Gl`), `Cpu` the terminal `--safe-gpu` entry (glow too).
pub fn ladder(os: Os) -> &'static [Backend] {
    match os {
        Os::Windows => &[Backend::Gl, Backend::Dx12, Backend::Vulkan, Backend::Cpu],
        Os::Linux => &[Backend::Vulkan, Backend::Gl, Backend::Cpu],
        Os::Macos => &[Backend::Auto, Backend::Gl],
    }
}

/// The choices Configure › Graphics offers: `auto` and the platform's ladder.
pub fn choices(os: Os) -> Vec<&'static str> {
    let mut v = vec![Backend::Auto.name()];
    v.extend(ladder(os).iter().map(|b| b.name()).filter(|n| *n != "auto"));
    if !v.contains(&"cpu") {
        v.push("cpu");
    }
    v
}

/// The backend `Auto` stands for on the ladder (where a crash moves on from).
fn effective(b: Backend, os: Os) -> Backend {
    if b == Backend::Auto { ladder(os)[0] } else { b }
}

/// The next safer backend after a start on `tried` died.
pub fn next(tried: Backend, os: Os) -> Backend {
    let l = ladder(os);
    let tried = effective(tried, os);
    l.iter().position(|b| *b == tried).and_then(|i| l.get(i + 1)).copied().unwrap_or(Backend::Cpu)
}

/// This launch's backend and why.
#[derive(Debug, PartialEq, Eq)]
pub struct Plan {
    pub backend: Backend,
    /// The backend whose start died (a fallback launch).
    pub crashed: Option<Backend>,
    /// `WGPU_BACKEND` decides: nothing is promoted, saved or announced.
    pub forced: bool,
}

impl Plan {
    /// Setting to store once this launch painted: a working hardware fallback only.
    pub fn to_save(&self) -> Option<Backend> {
        if self.forced {
            return None;
        }
        self.crashed.filter(|_| self.backend != Backend::Cpu).map(|_| self.backend)
    }

    /// The notice of a fallback launch (English source string of the catalog, filled with the two names).
    pub fn notice(&self) -> Option<(Backend, Backend)> {
        if self.forced {
            return None;
        }
        Some((self.crashed?, self.backend))
    }

    /// What the marker of this launch says.
    pub fn marker_name(&self) -> &'static str {
        if self.forced { FORCED } else { self.backend.name() }
    }
}

/// Decide from the saved preference, a marker left by a start that died, `--safe-gpu` and `WGPU_BACKEND`.
pub fn plan(preference: Backend, died: Option<Backend>, safe: bool, forced: bool, os: Os) -> Plan {
    if forced {
        return Plan { backend: preference, crashed: None, forced: true };
    }
    if safe {
        return Plan { backend: Backend::Cpu, crashed: None, forced: false };
    }
    match died {
        Some(tried) => Plan { backend: next(tried, os), crashed: Some(tried), forced: false },
        None => Plan { backend: preference, crashed: None, forced: false },
    }
}

/// The marker of this start, owned through the lock file until [`Marker::clear`]ed (or the process ends, leaving the
/// marker for the next start).
pub struct Marker {
    path: PathBuf,
    lock: Option<File>,
}

impl Marker {
    /// Backend of a start that died: a marker while nobody holds the lock. A forced launch's marker is no rung.
    pub fn died(dir: &Path) -> Option<Backend> {
        let lock = lock_file(dir).ok()?;
        // held: another instance is starting right now, not a crash
        lock.try_lock().ok()?;
        let mut text = String::new();
        File::open(dir.join(MARKER)).ok()?.take(64).read_to_string(&mut text).ok()?;
        let text = text.trim();
        if text == FORCED {
            return None;
        }
        Some(Backend::parse(text))
    }

    /// Takes the lock and writes the marker. `Ok(None)`: another instance holds the lock (its protection covers this
    /// start; nothing is touched). Errors only lose the crash protection.
    pub fn start(dir: &Path, name: &str) -> std::io::Result<Option<Self>> {
        let lock = lock_file(dir)?;
        match lock.try_lock() {
            Ok(()) => {}
            Err(TryLockError::WouldBlock) => return Ok(None),
            Err(TryLockError::Error(e)) => return Err(e),
        }
        let path = dir.join(MARKER);
        if std::fs::symlink_metadata(&path).is_ok_and(|m| m.file_type().is_symlink()) {
            std::fs::remove_file(&path)?; // a link is never written through
        }
        let mut file = OpenOptions::new().create(true).truncate(true).write(true).open(&path)?;
        file.write_all(name.as_bytes())?;
        file.sync_all()?;
        Ok(Some(Self { path, lock: Some(lock) }))
    }

    /// The start painted: remove the marker while still holding the lock, then release it.
    pub fn clear(&mut self) {
        if let Some(lock) = self.lock.take() {
            let _ = std::fs::remove_file(&self.path);
            drop(lock);
        }
    }
}

/// wgpu setup for `backend`; `None` keeps eframe's default (`WGPU_BACKEND` set, or `Auto` off Windows, or macOS).
pub fn configuration(backend: Backend, os: Os, forced: bool) -> Option<WgpuConfiguration> {
    // macOS renders through Metal (`Auto`) or the glow renderer (`Gl`/`Cpu`, chosen in main.rs)
    if os == Os::Macos || forced {
        return None;
    }
    let windows = os == Os::Windows;
    if backend == Backend::Auto && !windows {
        return None;
    }
    let backends = match backend {
        Backend::Auto => Backends::all(),
        Backend::Gl => Backends::GL,
        Backend::Dx12 => Backends::DX12,
        Backend::Vulkan => Backends::VULKAN,
        Backend::Cpu if windows => Backends::DX12,
        Backend::Cpu => Backends::GL | Backends::VULKAN,
    };
    let mut config = WgpuConfiguration::default();
    if let WgpuSetup::CreateNew(setup) = &mut config.wgpu_setup {
        setup.instance_descriptor.backends = backends;
        let software = backend == Backend::Cpu;
        setup.native_adapter_selector = Some(Arc::new(move |adapters, surface| {
            let usable: Vec<&wgpu::Adapter> =
                adapters.iter().filter(|a| surface.is_none_or(|s| a.is_surface_supported(s))).collect();
            let infos: Vec<_> = usable.iter().map(|a| a.get_info()).collect();
            let list: Vec<_> = infos.iter().map(|i| (i.backend, i.device_type, i.name.as_str())).collect();
            pick_adapter(&list, software)
                .and_then(|i| usable.get(i))
                .map(|a| (*a).clone())
                .ok_or_else(|| "no usable wgpu adapter".to_owned())
        }));
    }
    Some(config)
}

/// Index of the adapter to render on. Normal: a hardware OpenGL one first (DX12 presentation flickers on Intel
/// graphics), then by device type. Software: a CPU adapter (WARP's output-owning entry first), else the first usable
/// one (NVIDIA's EGL hides llvmpipe), so the window still opens.
pub fn pick_adapter(adapters: &[(wgpu::Backend, DeviceType, &str)], software: bool) -> Option<usize> {
    let warp = |name: &str| name.contains("Microsoft Basic Render Driver");
    let rank = |&(backend, kind, name): &(wgpu::Backend, DeviceType, &str)| {
        let device = match kind {
            DeviceType::DiscreteGpu => 0,
            DeviceType::IntegratedGpu => 1,
            DeviceType::VirtualGpu => 2,
            DeviceType::Other => 3,
            DeviceType::Cpu => 4,
        };
        if software {
            let cpu = kind == DeviceType::Cpu || warp(name);
            // WARP is listed twice on GPU-less machines; the non-CPU entry owns the output
            return (!cpu, !(warp(name) && kind != DeviceType::Cpu), device);
        }
        (false, !(backend == wgpu::Backend::Gl && kind != DeviceType::Cpu), device)
    };
    (0..adapters.len()).min_by_key(|&i| rank(&adapters[i]))
}

#[cfg(test)]
mod tests {
    #![allow(clippy::unwrap_used, clippy::expect_used)]
    use super::*;
    use wgpu::Backend as W;

    #[test]
    fn ladders_and_next() {
        assert_eq!(next(Backend::Auto, Os::Windows), Backend::Dx12, "auto starts at GL on Windows");
        assert_eq!(next(Backend::Gl, Os::Windows), Backend::Dx12);
        assert_eq!(next(Backend::Dx12, Os::Windows), Backend::Vulkan);
        assert_eq!(next(Backend::Vulkan, Os::Windows), Backend::Cpu);
        assert_eq!(next(Backend::Cpu, Os::Windows), Backend::Cpu);
        assert_eq!(next(Backend::Auto, Os::Linux), Backend::Gl);
        assert_eq!(next(Backend::Gl, Os::Linux), Backend::Cpu);
        assert_eq!(next(Backend::Dx12, Os::Linux), Backend::Cpu, "not on the Linux ladder");
        assert_eq!(next(Backend::Auto, Os::Macos), Backend::Gl, "Metal falls back to the glow renderer");
        assert_eq!(next(Backend::Gl, Os::Macos), Backend::Cpu);
        assert_eq!(next(Backend::Vulkan, Os::Macos), Backend::Cpu, "not on the macOS ladder");
        assert_eq!(choices(Os::Windows), ["auto", "gl", "dx12", "vulkan", "cpu"]);
        assert_eq!(choices(Os::Linux), ["auto", "vulkan", "gl", "cpu"]);
        assert_eq!(choices(Os::Macos), ["auto", "gl", "cpu"]);
    }

    #[test]
    fn plan_and_what_is_saved() {
        let p = plan(Backend::Auto, None, false, false, Os::Windows);
        assert_eq!((p.backend, p.to_save(), p.notice()), (Backend::Auto, None, None));
        let p = plan(Backend::Auto, Some(Backend::Gl), false, false, Os::Windows);
        assert_eq!((p.backend, p.to_save()), (Backend::Dx12, Some(Backend::Dx12)));
        assert_eq!(p.notice(), Some((Backend::Gl, Backend::Dx12)));
        let p = plan(Backend::Dx12, Some(Backend::Vulkan), false, false, Os::Windows);
        assert_eq!((p.backend, p.to_save()), (Backend::Cpu, None), "software is never saved silently");
        assert_eq!(plan(Backend::Gl, Some(Backend::Gl), true, false, Os::Windows).backend, Backend::Cpu);
        let p = plan(Backend::Auto, Some(Backend::Auto), false, false, Os::Macos);
        assert_eq!((p.backend, p.to_save()), (Backend::Gl, Some(Backend::Gl)));
        assert_eq!(Backend::parse(" OpenGL\n"), Backend::Gl);
        assert_eq!(Backend::parse("weird"), Backend::Auto);
    }

    #[test]
    fn forced_launch_promotes_nothing() {
        for os in [Os::Windows, Os::Linux, Os::Macos] {
            let p = plan(Backend::Gl, Some(Backend::Gl), true, true, os);
            assert!(p.forced && p.crashed.is_none());
            assert_eq!((p.to_save(), p.notice(), p.marker_name()), (None, None, "env"));
            assert!(configuration(Backend::Dx12, os, true).is_none(), "wgpu's own choice");
        }
        let dir = std::env::temp_dir().join(format!("adm-gpu-env-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let m = Marker::start(&dir, "env").unwrap().unwrap();
        drop(m); // the forced start died
        assert_eq!(Marker::died(&dir), None, "a forced start is no rung");
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn left_marker_is_a_crash_and_a_cleared_one_is_not() {
        let dir = std::env::temp_dir().join(format!("adm-gpu-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        assert_eq!(Marker::died(&dir), None);
        let m = Marker::start(&dir, "vulkan").unwrap().unwrap();
        drop(m); // a start that died leaves the file but no lock
        assert_eq!(Marker::died(&dir), Some(Backend::Vulkan));
        let mut ok = Marker::start(&dir, "gl").unwrap().unwrap();
        ok.clear();
        assert_eq!(Marker::died(&dir), None);
        assert!(!dir.join(MARKER).exists());
        let _ = std::fs::remove_dir_all(&dir);
    }

    /// The child of [`marker_held_by_another_process`]: holds a marker until told to stop.
    #[test]
    fn marker_holder_child() {
        let Some(dir) = std::env::var_os("ADM_GPU_HOLD").map(PathBuf::from) else { return };
        let _m = Marker::start(&dir, "dx12").unwrap().unwrap();
        std::fs::write(dir.join("held"), b"").unwrap();
        while !dir.join("stop").exists() {
            std::thread::sleep(std::time::Duration::from_millis(10));
        }
    }

    /// Two processes: a marker another instance holds is neither read as a crash nor truncated or rewritten.
    #[test]
    fn marker_held_by_another_process() {
        let dir = std::env::temp_dir().join(format!("adm-gpu-two-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        /// Kills and reaps the child whatever happens to the test.
        struct Child(std::process::Child);
        impl Drop for Child {
            fn drop(&mut self) {
                let _ = self.0.kill();
                let _ = self.0.wait();
            }
        }
        let mut child = Child(
            std::process::Command::new(std::env::current_exe().unwrap())
                .args(["--exact", "gpu::tests::marker_holder_child", "--nocapture"])
                .env("ADM_GPU_HOLD", &dir)
                .spawn()
                .unwrap(),
        );
        let start = std::time::Instant::now();
        while !dir.join("held").exists() {
            assert!(child.0.try_wait().unwrap().is_none(), "the child ended before holding the marker");
            assert!(start.elapsed().as_secs() < 30, "the child never held the marker");
            std::thread::sleep(std::time::Duration::from_millis(10));
        }
        assert_eq!(Marker::died(&dir), None, "held by a starting instance: not a crash");
        assert!(Marker::start(&dir, "gl").unwrap().is_none(), "this start leaves it alone");
        assert_eq!(std::fs::read_to_string(dir.join(MARKER)).unwrap(), "dx12", "not truncated nor rewritten");
        std::fs::write(dir.join("stop"), b"").unwrap();
        let end = std::time::Instant::now();
        let status = loop {
            if let Some(st) = child.0.try_wait().unwrap() {
                break st;
            }
            assert!(end.elapsed().as_secs() < 30, "the child did not stop");
            std::thread::sleep(std::time::Duration::from_millis(10));
        };
        assert!(status.success());
        // the child ended without clearing: its start "died"
        assert_eq!(Marker::died(&dir), Some(Backend::Dx12));
        let _ = std::fs::remove_dir_all(&dir);
    }

    #[test]
    fn state_folder_is_private_and_never_a_link() {
        let base = std::env::temp_dir().join(format!("adm-gpu-state-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&base);
        std::fs::create_dir_all(&base).unwrap();
        let p = adm_platform::TestPlatform { user_config: base.join("u.toml") };
        let d = state_dir(&p, &base).unwrap();
        assert_eq!(d, base.join("desktop-rs"));
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            std::fs::set_permissions(&d, std::fs::Permissions::from_mode(0o755)).unwrap();
            assert!(state_dir(&p, &base).unwrap_err().contains("must be private"));
            std::fs::remove_dir_all(&d).unwrap();
            let elsewhere = base.join("elsewhere");
            std::fs::create_dir_all(&elsewhere).unwrap();
            std::os::unix::fs::symlink(&elsewhere, &d).unwrap();
            assert!(state_dir(&p, &base).is_err(), "a link is refused");
            // a marker or lock that is a link is never written through
            let ok = base.join("ok");
            std::fs::create_dir_all(&ok).unwrap();
            std::os::unix::fs::symlink(base.join("victim"), ok.join(LOCK)).unwrap();
            assert!(Marker::start(&ok, "gl").is_err());
            assert!(!base.join("victim").exists());
        }
        let _ = std::fs::remove_dir_all(&base);
    }

    #[test]
    fn adapter_choice() {
        let intel = [(W::Dx12, DeviceType::IntegratedGpu, "Intel"), (W::Gl, DeviceType::IntegratedGpu, "Intel")];
        assert_eq!(pick_adapter(&intel, false), Some(1), "hardware OpenGL first");
        let gpus = [(W::Vulkan, DeviceType::IntegratedGpu, "iGPU"), (W::Vulkan, DeviceType::DiscreteGpu, "dGPU")];
        assert_eq!(pick_adapter(&gpus, false), Some(1), "discrete before integrated");
        let vm = [
            (W::Dx12, DeviceType::Cpu, "Microsoft Basic Render Driver"),
            (W::Dx12, DeviceType::Other, "Microsoft Basic Render Driver"),
            (W::Gl, DeviceType::Cpu, "llvmpipe"),
        ];
        assert_eq!(pick_adapter(&vm, true), Some(1), "output-owning WARP");
        let linux = [(W::Vulkan, DeviceType::DiscreteGpu, "RTX"), (W::Gl, DeviceType::Cpu, "llvmpipe")];
        assert_eq!(pick_adapter(&linux, true), Some(1));
        let nvidia_egl = [(W::Vulkan, DeviceType::DiscreteGpu, "RTX")];
        assert_eq!(pick_adapter(&nvidia_egl, true), Some(0), "no software adapter: still opens");
        assert_eq!(pick_adapter(&[], false), None);
    }
}
