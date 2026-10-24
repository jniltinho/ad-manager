//! The operating-system boundary of the app. Everything that differs between Linux, Windows and macOS lives behind
//! [`Platform`]; the binary picks the implementation (`adm-platform-linux`, `-windows`, `-macos`) at compile time and
//! hands it to the core as `&dyn Platform`, so the core and the UI never test `cfg(target_os)` themselves.

use std::fs::File;
use std::io;
use std::path::{Path, PathBuf};

#[cfg(unix)]
pub mod unix;

#[derive(Debug, thiserror::Error)]
pub enum PlatformError {
    /// A file others can read (Unix): the config holds the panel and the pinned certificate.
    #[error("{0} must be private and has mode {1:04o}: run chmod 600")]
    NotPrivate(String, u32),
    #[error("{0}: {1}")]
    Io(String, io::Error),
}

/// What the app needs from the operating system.
pub trait Platform: Send + Sync {
    /// "linux", "windows" or "macos" (diagnostics, the About line).
    fn name(&self) -> &'static str;

    /// `.config/desktop.toml` beside the executable `exe`, or `None` where nothing may be written beside it (inside a
    /// macOS `.app`, writing breaks the signature).
    fn local_config_path(&self, exe: &Path) -> Option<PathBuf> {
        Some(exe.parent()?.join(".config").join("desktop.toml"))
    }

    /// The per-user location used up to 0.3 of the Fyne app (`…/ad-manager/desktop.toml` in the user config dir).
    fn user_config_path(&self) -> PathBuf;

    /// Fails when the file is readable or writable by others (where the system tells it by mode bits).
    fn check_private(&self, path: &Path) -> Result<(), PlatformError>;

    /// Creates a new file only the user can read and write (fails if it exists).
    fn create_private(&self, path: &Path) -> io::Result<File>;

    /// Creates a directory (and its parents) only the user can enter.
    fn create_private_dir(&self, dir: &Path) -> io::Result<()>;
}

/// A platform for tests: Unix-like rules where the system has them, a fixed user config path.
#[derive(Debug, Clone)]
pub struct TestPlatform {
    pub user_config: PathBuf,
}

impl Platform for TestPlatform {
    fn name(&self) -> &'static str {
        "test"
    }

    fn user_config_path(&self) -> PathBuf {
        self.user_config.clone()
    }

    fn check_private(&self, path: &Path) -> Result<(), PlatformError> {
        #[cfg(unix)]
        return unix::check_private(path);
        #[cfg(not(unix))]
        {
            let _ = path;
            Ok(())
        }
    }

    fn create_private(&self, path: &Path) -> io::Result<File> {
        #[cfg(unix)]
        return unix::create_private(path);
        #[cfg(not(unix))]
        std::fs::OpenOptions::new().write(true).create_new(true).open(path)
    }

    fn create_private_dir(&self, dir: &Path) -> io::Result<()> {
        #[cfg(unix)]
        return unix::create_private_dir(dir);
        #[cfg(not(unix))]
        std::fs::create_dir_all(dir)
    }
}
