//! Mode-bit rules shared by the Linux and macOS platforms.

use std::fs::{self, DirBuilder, File, OpenOptions};
use std::io;
use std::os::unix::fs::{DirBuilderExt, OpenOptionsExt, PermissionsExt};
use std::path::Path;

use crate::PlatformError;

/// Refuses a file with any group or other permission bit.
pub fn check_private(path: &Path) -> Result<(), PlatformError> {
    let shown = path.display().to_string();
    let mode = fs::metadata(path).map_err(|e| PlatformError::Io(shown.clone(), e))?.permissions().mode() & 0o777;
    if mode & 0o077 != 0 {
        return Err(PlatformError::NotPrivate(shown, mode));
    }
    Ok(())
}

/// A new file with mode 0600.
pub fn create_private(path: &Path) -> io::Result<File> {
    OpenOptions::new().write(true).create_new(true).mode(0o600).open(path)
}

/// A directory tree with mode 0700 for the parts it creates.
pub fn create_private_dir(dir: &Path) -> io::Result<()> {
    DirBuilder::new().recursive(true).mode(0o700).create(dir)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn modes() {
        let d = std::env::temp_dir().join(format!("adm-unix-{}", std::process::id()));
        create_private_dir(&d).unwrap();
        let p = d.join("f");
        let _ = fs::remove_file(&p);
        drop(create_private(&p).unwrap());
        assert!(check_private(&p).is_ok());
        fs::set_permissions(&p, fs::Permissions::from_mode(0o640)).unwrap();
        assert!(matches!(check_private(&p), Err(PlatformError::NotPrivate(_, 0o640))));
        fs::remove_dir_all(&d).unwrap();
    }
}
