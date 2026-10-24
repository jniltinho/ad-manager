//! Linux: XDG config dir (`~/.config/ad-manager`), mode-bit privacy.

use std::fs::File;
use std::io;
use std::path::{Path, PathBuf};

use adm_platform::{Platform, PlatformError, unix};

#[derive(Debug, Default, Clone, Copy)]
pub struct Linux;

impl Platform for Linux {
    fn name(&self) -> &'static str {
        "linux"
    }

    fn user_config_path(&self) -> PathBuf {
        dirs::config_dir().unwrap_or_default().join("ad-manager").join("desktop.toml")
    }

    fn check_private(&self, path: &Path) -> Result<(), PlatformError> {
        unix::check_private(path)
    }

    fn create_private(&self, path: &Path) -> io::Result<File> {
        unix::create_private(path)
    }

    fn create_private_dir(&self, dir: &Path) -> io::Result<()> {
        unix::create_private_dir(dir)
    }
}
