//! macOS: `~/Library/Application Support/ad-manager`, mode-bit privacy, and nothing written inside a signed `.app`.

use std::fs::File;
use std::io;
use std::path::{Path, PathBuf};

use adm_platform::{Platform, PlatformError, unix};

#[derive(Debug, Default, Clone, Copy)]
pub struct MacOs;

/// Whether `exe` runs from inside an application bundle.
pub fn in_app_bundle(exe: &Path) -> bool {
    exe.to_string_lossy().contains(".app/Contents/MacOS/")
}

impl Platform for MacOs {
    fn name(&self) -> &'static str {
        "macos"
    }

    fn local_config_path(&self, exe: &Path) -> Option<PathBuf> {
        if in_app_bundle(exe) {
            return None;
        }
        Some(exe.parent()?.join(".config").join("desktop.toml"))
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn no_config_inside_a_bundle() {
        let exe = Path::new("/Applications/AD Manager.app/Contents/MacOS/ad-manager-desktop-rs");
        assert!(MacOs.local_config_path(exe).is_none());
        let loose = Path::new("/Users/me/bin/ad-manager-desktop-rs");
        assert_eq!(MacOs.local_config_path(loose), Some(PathBuf::from("/Users/me/bin/.config/desktop.toml")));
    }
}
