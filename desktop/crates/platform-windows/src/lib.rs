//! Windows: `%AppData%\ad-manager`; privacy follows the folder's DACL (the user profile is already private), so mode
//! bits are not checked — keep the app in a folder of your own, as the Fyne app's docs say.

use std::fs::{File, OpenOptions};
use std::io;
use std::path::{Path, PathBuf};

use adm_platform::{Platform, PlatformError};

#[derive(Debug, Default, Clone, Copy)]
pub struct Windows;

impl Platform for Windows {
    fn name(&self) -> &'static str {
        "windows"
    }

    fn user_config_path(&self) -> PathBuf {
        dirs::config_dir().unwrap_or_default().join("ad-manager").join("desktop.toml")
    }

    fn check_private(&self, _: &Path) -> Result<(), PlatformError> {
        Ok(())
    }

    fn create_private(&self, path: &Path) -> io::Result<File> {
        OpenOptions::new().write(true).create_new(true).open(path)
    }

    fn create_private_dir(&self, dir: &Path) -> io::Result<()> {
        std::fs::create_dir_all(dir)
    }
}
