//! Crash-safe graphics start (design D12, from rustXterm's `gpu.rs`). A graphics driver that crashes while the
//! window's device is created kills the process before any Rust code can react, so what survives is a file: before
//! the window exists the app takes `gpu-starting` (in the folder of desktop.toml) with an OS lock and writes the
//! backend it tries; it empties the file once [`START_FRAMES`] frames painted. A later start that finds a backend in
//! the file with nobody holding it knows that start died and tries the next safer backend:
//!
//! - Windows: OpenGL → DirectX 12 → Vulkan → software (WARP)
//! - Linux: Vulkan → OpenGL → software (llvmpipe/lavapipe)
//! - macOS: Metal (wgpu) → OpenGL (eframe's glow renderer; `cpu` means glow too, the safest renderer once Metal is out)
//!
//! A hardware fallback that worked is saved as `[graphics] gpu_backend` in desktop.toml (Configure › Graphics); the
//! software path is never saved silently. `WGPU_BACKEND` forces wgpu's own choice for a launch and `--safe-gpu` the
//! software path. This module is the plan and the files; the wgpu setup is the binary's.

use std::fs::{self, File, OpenOptions};
use std::io::{self, Read, Seek, SeekFrom, Write};
use std::path::Path;

use crate::config::Config;

/// The marker file, in the folder of desktop.toml.
pub const MARKER: &str = "gpu-starting";
/// Frames painted before a start counts as working.
pub const START_FRAMES: u32 = 3;
/// What the marker holds on a launch forced by `WGPU_BACKEND` (not a rung of any ladder).
const FORCED: &str = "env";

/// A rendering backend; stored lowercase.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Backend {
    /// The platform default: hardware OpenGL first on Windows, wgpu's choice on Linux, Metal on macOS.
    Auto,
    Gl,
    Dx12,
    Vulkan,
    /// Software rendering (WARP on Windows, llvmpipe/lavapipe on Linux; glow on macOS).
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

    /// The label in Configure › Graphics (English source string).
    pub fn label(self, os: Os) -> &'static str {
        match (self, os) {
            (Self::Auto, Os::Macos) => "Automatic (Metal)",
            (Self::Auto, _) => "Automatic",
            (Self::Gl, _) => "OpenGL",
            (Self::Dx12, _) => "DirectX 12",
            (Self::Vulkan, _) => "Vulkan",
            (Self::Cpu, _) => "Software (CPU)",
        }
    }
}

/// The host OS: the ladder and the renderer differ per platform.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Os {
    Windows,
    Linux,
    Macos,
}

impl Os {
    /// This build's OS.
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

/// Backends from the preferred one to the safest: hardware first, software only as the last fallback. Windows starts
/// with OpenGL: DX12 presentation flickers on Intel graphics. macOS has Metal (`Auto`) and then the glow renderer
/// (`Gl`); `Cpu` is only the `--safe-gpu` entry there.
pub fn ladder(os: Os) -> &'static [Backend] {
    match os {
        Os::Windows => &[Backend::Gl, Backend::Dx12, Backend::Vulkan, Backend::Cpu],
        Os::Linux => &[Backend::Vulkan, Backend::Gl, Backend::Cpu],
        Os::Macos => &[Backend::Auto, Backend::Gl],
    }
}

/// The choices of Configure › Graphics: automatic, then the ladder.
pub fn choices(os: Os) -> Vec<Backend> {
    let mut v = vec![Backend::Auto];
    v.extend(ladder(os).iter().copied().filter(|b| *b != Backend::Auto));
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

/// What a dead start left in the marker.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Died {
    /// A start on this backend died.
    On(Backend),
    /// A forced (`WGPU_BACKEND`) start died: not a crash of any rung.
    Forced,
}

/// This launch's backend and why.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Plan {
    pub backend: Backend,
    /// The backend whose start died (a fallback launch).
    pub crashed: Option<Backend>,
    /// `WGPU_BACKEND` chose: wgpu's own selection, nothing promoted, saved or announced.
    pub forced: bool,
}

impl Plan {
    /// The choice to save once this launch painted: a hardware backend reached after a crash.
    pub fn to_save(&self) -> Option<Backend> {
        if self.forced || self.backend == Backend::Cpu { None } else { self.crashed.map(|_| self.backend) }
    }

    /// The notice of a fallback launch (English source string, with the two backend names).
    pub fn notice(&self) -> Option<(Backend, Backend)> {
        self.crashed.map(|c| (c, self.backend))
    }

    /// What the marker records for this launch.
    pub fn marker_text(&self) -> &'static str {
        if self.forced { FORCED } else { self.backend.name() }
    }
}

/// Decides from the saved preference, what a dead start left, `--safe-gpu` and `WGPU_BACKEND`.
pub fn plan(preference: Backend, died: Option<Died>, safe: bool, forced: bool, os: Os) -> Plan {
    if safe {
        return Plan { backend: Backend::Cpu, crashed: None, forced: false };
    }
    if forced {
        return Plan { backend: Backend::Auto, crashed: None, forced: true };
    }
    match died {
        Some(Died::On(tried)) => Plan { backend: next(tried, os), crashed: Some(tried), forced: false },
        Some(Died::Forced) | None => Plan { backend: preference, crashed: None, forced: false },
    }
}

/// The marker of this start, held under an OS lock until dropped or [`Marker::clear`]ed. The file is never removed:
/// an empty marker means the last start painted (removing it would let a start that opened it just before take the
/// lock of an unlinked file).
pub struct Marker {
    file: Option<File>,
}

/// What taking the marker found.
pub enum Take {
    /// This launch holds the marker; `died` is what a dead start left in it.
    Held { marker: Marker, died: Option<Died> },
    /// Another instance is starting and holds it: not a crash, and this launch leaves it alone.
    Busy,
}

impl Marker {
    /// Opens the marker without truncating it and takes its lock; only the holder reads what a dead start left (a
    /// file nobody holds that still has a backend in it) and later writes its own (fixes rustXterm's truncate-then-
    /// lock race, where a second instance could erase a starting one's marker).
    pub fn take(dir: &Path) -> io::Result<Take> {
        fs::create_dir_all(dir)?;
        let path = dir.join(MARKER);
        let mut file = OpenOptions::new().read(true).write(true).create(true).truncate(false).open(&path)?;
        match file.try_lock() {
            Ok(()) => {}
            Err(fs::TryLockError::WouldBlock) => return Ok(Take::Busy),
            Err(fs::TryLockError::Error(e)) => return Err(e),
        }
        let mut text = String::new();
        (&mut file).take(64).read_to_string(&mut text)?;
        let died = match text.trim() {
            "" => None,
            FORCED => Some(Died::Forced),
            t => Some(Died::On(Backend::parse(t))),
        };
        Ok(Take::Held { marker: Self { file: Some(file) }, died })
    }

    /// Records the backend this launch tries (only the holder writes).
    pub fn write(&mut self, text: &str) -> io::Result<()> {
        let Some(f) = self.file.as_mut() else { return Ok(()) };
        f.set_len(0)?;
        f.seek(SeekFrom::Start(0))?;
        f.write_all(text.as_bytes())?;
        f.sync_all()
    }

    /// The start painted: empty the marker while still holding its lock, then let it go.
    pub fn clear(&mut self) {
        if let Some(f) = self.file.take() {
            let _ = f.set_len(0).and_then(|()| f.sync_all());
            drop(f);
        }
    }
}

/// The saved choice of a configuration (`auto` when none).
pub fn choice(c: &Config) -> Backend {
    Backend::parse(&c.graphics.gpu_backend)
}

/// Sets the choice in a configuration (`auto` is stored as no key).
pub fn set_choice(c: &mut Config, b: Backend) {
    c.graphics.gpu_backend = if b == Backend::Auto { String::new() } else { b.name().to_string() };
}

#[cfg(test)]
mod tests {
    #![allow(clippy::unwrap_used, clippy::expect_used)]
    use super::*;
    use std::path::PathBuf;

    fn tmp(name: &str) -> PathBuf {
        let d = std::env::temp_dir().join(format!("adm-gpu-{name}-{}", std::process::id()));
        let _ = fs::remove_dir_all(&d);
        d
    }

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
        assert_eq!(next(Backend::Auto, Os::Macos), Backend::Gl, "Metal falls back to glow");
        assert_eq!(next(Backend::Gl, Os::Macos), Backend::Cpu);
        assert_eq!(next(Backend::Vulkan, Os::Macos), Backend::Cpu, "not on the macOS ladder");
        assert_eq!(choices(Os::Macos), vec![Backend::Auto, Backend::Gl]);
        assert_eq!(choices(Os::Linux), vec![Backend::Auto, Backend::Vulkan, Backend::Gl, Backend::Cpu]);
    }

    #[test]
    fn plan_and_what_is_saved() {
        let p = plan(Backend::Auto, None, false, false, Os::Windows);
        assert_eq!((p.backend, p.to_save(), p.notice()), (Backend::Auto, None, None));
        let p = plan(Backend::Auto, Some(Died::On(Backend::Gl)), false, false, Os::Windows);
        assert_eq!((p.backend, p.to_save()), (Backend::Dx12, Some(Backend::Dx12)));
        assert_eq!(p.notice(), Some((Backend::Gl, Backend::Dx12)));
        let p = plan(Backend::Dx12, Some(Died::On(Backend::Vulkan)), false, false, Os::Windows);
        assert_eq!((p.backend, p.to_save()), (Backend::Cpu, None), "software is never saved silently");
        let p = plan(Backend::Gl, Some(Died::On(Backend::Gl)), true, false, Os::Windows);
        assert_eq!(p, Plan { backend: Backend::Cpu, crashed: None, forced: false }, "--safe-gpu");
        let p = plan(Backend::Auto, Some(Died::On(Backend::Auto)), false, false, Os::Macos);
        assert_eq!((p.backend, p.to_save()), (Backend::Gl, Some(Backend::Gl)), "Metal died: glow, saved");
        assert_eq!(Backend::parse(" OpenGL\n"), Backend::Gl);
        assert_eq!(Backend::parse("weird"), Backend::Auto);
    }

    #[test]
    fn forced_launches_promote_nothing() {
        let p = plan(Backend::Dx12, Some(Died::On(Backend::Gl)), false, true, Os::Windows);
        assert!(p.forced);
        assert_eq!((p.to_save(), p.notice(), p.marker_text()), (None, None, "env"));
        // A forced start that died is not a crash of any rung: the next start uses the preference.
        let p = plan(Backend::Dx12, Some(Died::Forced), false, false, Os::Windows);
        assert_eq!((p.backend, p.crashed), (Backend::Dx12, None));
    }

    #[test]
    fn marker_left_by_a_dead_start_and_cleared_one() {
        let dir = tmp("marker");
        let Take::Held { mut marker, died } = Marker::take(&dir).unwrap() else { panic!("busy") };
        assert_eq!(died, None, "a new marker is no crash");
        marker.write("vulkan").unwrap();
        drop(marker); // the start died: file left, lock gone
        let Take::Held { mut marker, died } = Marker::take(&dir).unwrap() else { panic!("busy") };
        assert_eq!(died, Some(Died::On(Backend::Vulkan)));
        marker.write("gl").unwrap();
        marker.clear();
        assert_eq!(fs::read_to_string(dir.join(MARKER)).unwrap(), "", "emptied, not removed");
        let Take::Held { marker, died } = Marker::take(&dir).unwrap() else { panic!("busy") };
        assert_eq!(died, None);
        drop(marker);
        let _ = fs::remove_dir_all(&dir);
    }

    /// The child half of [`a_held_marker_is_neither_erased_nor_a_crash`]: holds the marker until stdin closes.
    #[test]
    #[ignore = "run by a_held_marker_is_neither_erased_nor_a_crash in a second process"]
    fn hold_marker_child() {
        let Ok(dir) = std::env::var("ADM_GPU_MARKER_DIR") else { return };
        let Take::Held { mut marker, .. } = Marker::take(Path::new(&dir)).unwrap() else { panic!("busy") };
        marker.write("dx12").unwrap();
        println!("ready");
        io::stdout().flush().unwrap();
        let mut sink = String::new();
        let _ = io::stdin().read_line(&mut sink);
        drop(marker);
    }

    #[test]
    fn a_held_marker_is_neither_erased_nor_a_crash() {
        use std::io::BufRead;
        use std::process::{Command, Stdio};
        let dir = tmp("two");
        fs::create_dir_all(&dir).unwrap();
        let mut child = Command::new(std::env::current_exe().unwrap())
            .args(["--exact", "gpu::tests::hold_marker_child", "--ignored", "--nocapture", "--test-threads=1"])
            .env("ADM_GPU_MARKER_DIR", &dir)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .spawn()
            .unwrap();
        let mut out = io::BufReader::new(child.stdout.take().unwrap());
        let mut line = String::new();
        while out.read_line(&mut line).unwrap() > 0 && !line.contains("ready") {
            line.clear();
        }
        assert!(line.contains("ready"), "the child holds the marker");
        assert!(matches!(Marker::take(&dir).unwrap(), Take::Busy), "held: not a crash");
        child.kill().unwrap(); // the holder dies without clearing
        child.wait().unwrap();
        // what it wrote survived the other instance's attempt (read only now: Windows locks are mandatory)
        let Take::Held { marker, died } = Marker::take(&dir).unwrap() else { panic!("still busy") };
        assert_eq!(died, Some(Died::On(Backend::Dx12)), "not erased by the busy attempt");
        drop(marker);
        let _ = fs::remove_dir_all(&dir);
    }

    #[test]
    fn choice_lives_in_desktop_toml() {
        let mut c =
            crate::config::decode("lang = \"en\"\n\n[panel]\nurl = \"https://p:8443\"\nuser = \"a\"\n").unwrap();
        assert_eq!(choice(&c), Backend::Auto, "no [graphics]");
        set_choice(&mut c, Backend::Dx12);
        let text = toml::to_string(&c).unwrap();
        assert!(text.contains("[graphics]") && text.contains("gpu_backend = \"dx12\""), "{text}");
        let back = crate::config::decode(&text).unwrap();
        assert_eq!((choice(&back), back.panel.url.as_str()), (Backend::Dx12, "https://p:8443"));
        set_choice(&mut c, Backend::Auto);
        assert!(!toml::to_string(&c).unwrap().contains("graphics"), "auto writes no key");
    }
}
