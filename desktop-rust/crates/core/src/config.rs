//! desktop.toml — the same file, keys and lookup as the Fyne app (`desktop/config.go`), so either app opens the
//! other's configuration. It holds no secret: the password is asked on sign-in and file servers are reached only
//! through the panel. Files of older versions use Portuguese keys or still list `[[agents]]` with tokens: both are
//! read, and a file with agent tokens is rewritten without them.

use std::fs;
use std::io::Write;
use std::path::{Path, PathBuf};

use adm_platform::{Platform, PlatformError};
use serde::{Deserialize, Serialize};

#[derive(Debug, thiserror::Error)]
pub enum ConfigError {
    #[error("{path}: {source}")]
    Io { path: String, source: std::io::Error },
    #[error("{path}: {source}")]
    Parse { path: String, source: toml::de::Error },
    #[error(transparent)]
    Platform(#[from] PlatformError),
    #[error("{0}")]
    Encode(#[from] toml::ser::Error),
}

#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct Panel {
    #[serde(default)]
    pub url: String,
    /// Legacy "accept any certificate": replaced by `fingerprint` on the next Save.
    #[serde(default, skip_serializing_if = "std::ops::Not::not")]
    pub insecure: bool,
    /// Pins a self-signed panel certificate: "sha256:" and the hex SHA-256 of the leaf certificate.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub fingerprint: String,
    #[serde(default)]
    pub user: String,
}

/// The graphics backend chosen in Configure › Graphics or reached after a driver crash (design D12).
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct Graphics {
    /// "auto", "gl", "dx12", "vulkan" or "cpu" (`adm_core::gpu::Backend`); empty = auto.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub gpu_backend: String,
}

impl Graphics {
    pub fn is_empty(&self) -> bool {
        self.gpu_backend.is_empty()
    }
}

#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
pub struct Config {
    #[serde(default)]
    pub panel: Panel,
    /// "pt-BR" or "en"; empty = the panel's default.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub lang: String,
    /// `[graphics]`: the rendering backend (the Fyne app keeps it when it rewrites the file).
    #[serde(default, skip_serializing_if = "Graphics::is_empty")]
    pub graphics: Graphics,
    /// The file still listed file servers (`[[agents]]` with tokens, no longer used).
    #[serde(skip)]
    pub had_agents: bool,
}

#[derive(Default, Deserialize)]
struct Legacy {
    #[serde(default)]
    painel: LegacyPanel,
    #[serde(default)]
    agentes: Vec<toml::Value>,
    #[serde(default)]
    agents: Vec<toml::Value>,
    #[serde(default)]
    idioma: String,
}

#[derive(Default, Deserialize)]
struct LegacyPanel {
    #[serde(default)]
    url: String,
    #[serde(default)]
    insecure: bool,
    #[serde(default)]
    usuario: String,
}

/// The panel URLs of the examples earlier versions wrote on first run: a file with one of them and no agents was
/// never filled in.
const EXAMPLE_URLS: [&str; 3] =
    ["https://panel.example.local:8443", "https://painel.empresa.local:8443", "https://painel.empresa.net:8443"];

/// Reads a desktop.toml: the current keys, and for each part left empty the old Portuguese keys.
pub fn decode(text: &str) -> Result<Config, toml::de::Error> {
    let mut c: Config = toml::from_str(text)?;
    let old: Legacy = toml::from_str(text)?;
    if c.panel.url.is_empty() && c.panel.user.is_empty() && !c.panel.insecure {
        c.panel.url = old.painel.url;
        c.panel.insecure = old.painel.insecure;
        c.panel.user = old.painel.usuario;
    }
    c.had_agents = !old.agents.is_empty() || !old.agentes.is_empty();
    if c.lang.is_empty() {
        c.lang = old.idioma;
    }
    Ok(c)
}

/// The `--config` value of the command line (also `-config`, `--config=path`); other arguments are ignored.
pub fn parse_flags(args: &[String]) -> Option<String> {
    let mut found = None;
    let mut i = 0;
    while i < args.len() {
        let a = args[i].strip_prefix('-').unwrap_or(&args[i]);
        if a == "-config" || a == "config" {
            if let Some(v) = args.get(i + 1) {
                found = Some(v.clone());
                i += 1;
            }
        } else if let Some(v) = a.strip_prefix("-config=").or_else(|| a.strip_prefix("config=")) {
            found = Some(v.to_string());
        }
        i += 1;
    }
    found
}

/// An example written on first run by an earlier version: an example panel URL and no file server.
fn untouched_example(text: &str) -> bool {
    decode(text).is_ok_and(|c| !c.had_agents && EXAMPLE_URLS.contains(&c.panel.url.as_str()))
}

/// Whether a file can be created in dir: a new file of a unique name, created exclusively (never an existing one or
/// a link), removed only when this probe made it.
fn writable_dir(dir: &Path) -> bool {
    let probe = dir.join(format!(".ad-manager-write-test-{}-{}", std::process::id(), nanos()));
    match fs::OpenOptions::new().write(true).create_new(true).open(&probe) {
        Ok(f) => {
            drop(f);
            let _ = fs::remove_file(&probe);
            true
        }
        Err(_) => false,
    }
}

/// Picks the config file: --config, AD_MANAGER_DESKTOP, .config/desktop.toml next to the executable, then the
/// per-user file when it was filled in. `create` reports a file that does not exist yet: next to the executable when
/// that folder is writable, otherwise in the per-user location.
pub fn resolve_path(flag: Option<&str>, env: Option<&str>, local: Option<PathBuf>, user: PathBuf) -> (PathBuf, bool) {
    for p in [flag, env].into_iter().flatten() {
        if !p.is_empty() {
            let p = PathBuf::from(p);
            let create = !p.exists();
            return (p, create);
        }
    }
    if let Some(l) = &local
        && l.exists()
    {
        return (l.clone(), false);
    }
    if let Ok(text) = fs::read_to_string(&user)
        && !untouched_example(&text)
    {
        return (user, false);
    }
    if let Some(l) = local
        && l.parent().and_then(Path::parent).is_some_and(writable_dir)
    {
        return (l, true);
    }
    let create = !user.exists();
    (user, create)
}

/// What loading found: the config, where it lives, whether Configure must open first, and a notice for the status
/// line (already in English, translated where shown).
pub struct Loaded {
    pub config: Config,
    pub path: PathBuf,
    pub needs_setup: bool,
    pub notice: Option<(String, String)>,
}

/// Reads the config file; a missing file or the untouched example opens Configure (nothing is written before Save).
pub fn load(platform: &dyn Platform, flag: Option<&str>) -> Result<Loaded, ConfigError> {
    let env = std::env::var("AD_MANAGER_DESKTOP").ok();
    let local = std::env::current_exe()
        .ok()
        .map(|e| fs::canonicalize(&e).unwrap_or(e))
        .and_then(|e| platform.local_config_path(&e));
    let (path, create) = resolve_path(flag, env.as_deref(), local, platform.user_config_path());
    load_from(platform, path, create)
}

pub fn load_from(platform: &dyn Platform, path: PathBuf, create: bool) -> Result<Loaded, ConfigError> {
    let shown = path.display().to_string();
    if create {
        let mut c = Config::default();
        c.panel.user = "administrator".into();
        return Ok(Loaded { config: c, path, needs_setup: true, notice: None });
    }
    let text = fs::read_to_string(&path).map_err(|source| ConfigError::Io { path: shown.clone(), source })?;
    platform.check_private(&path)?;
    let mut c = decode(&text).map_err(|source| ConfigError::Parse { path: shown, source })?;
    if c.panel.user.is_empty() {
        c.panel.user = "administrator".into();
    }
    let mut notice = None;
    if c.had_agents {
        notice = Some(match save(platform, &path, &c) {
            Ok(()) => (
                "File-server tokens are no longer used: they were removed from the configuration file.".into(),
                String::new(),
            ),
            Err(e) => (
                "File-server tokens are no longer used, but they could not be removed from the configuration file: "
                    .into(),
                e.to_string(),
            ),
        });
    }
    let needs_setup = untouched_example(&text) || c.panel.url.is_empty();
    Ok(Loaded { config: c, path, needs_setup, notice })
}

/// Writes c to path (0600, through a temp file and rename so a crash never leaves half a file); a --config link
/// rewrites the file it points to.
pub fn save(platform: &dyn Platform, path: &Path, c: &Config) -> Result<(), ConfigError> {
    let path = fs::canonicalize(path).unwrap_or_else(|_| path.to_path_buf());
    let io = |source| ConfigError::Io { path: path.display().to_string(), source };
    let body = toml::to_string(c)?;
    let text = format!("# ad-manager desktop — written by the Settings screen (can be edited by hand)\n{body}");
    let dir = path.parent().map(Path::to_path_buf).unwrap_or_default();
    platform.create_private_dir(&dir).map_err(io)?;
    let tmp = dir.join(format!(".desktop-{}-{}.toml", std::process::id(), nanos()));
    let result = (|| {
        let mut f = platform.create_private(&tmp)?;
        f.write_all(text.as_bytes())?;
        f.sync_all()?;
        drop(f);
        fs::rename(&tmp, &path)
    })();
    if result.is_err() {
        let _ = fs::remove_file(&tmp);
    }
    result.map_err(io)
}

fn nanos() -> u128 {
    std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map_or(0, |d| d.as_nanos())
}

#[cfg(test)]
mod tests {
    use super::*;
    use adm_platform::TestPlatform;

    fn plat() -> TestPlatform {
        TestPlatform { user_config: PathBuf::from("/nonexistent/user/desktop.toml") }
    }

    fn tmp(name: &str) -> PathBuf {
        let d = std::env::temp_dir().join(format!("adm-rs-{}-{name}-{}", std::process::id(), nanos()));
        fs::create_dir_all(&d).unwrap();
        d
    }

    #[test]
    fn reads_current_and_legacy_keys() {
        let c =
            decode("lang = \"en\"\n[panel]\nurl = \"https://p:8443\"\nuser = \"adm\"\nfingerprint = \"sha256:ab\"\n")
                .unwrap();
        assert_eq!((c.panel.url.as_str(), c.panel.user.as_str(), c.lang.as_str()), ("https://p:8443", "adm", "en"));
        assert_eq!(c.panel.fingerprint, "sha256:ab");
        let old = decode(
            "idioma = \"pt-BR\"\n[painel]\nurl = \"https://x\"\nusuario = \"u\"\ninsecure = true\n[[agentes]]\nnome = \"fs\"\ntoken = \"t\"\n",
        )
        .unwrap();
        assert_eq!((old.panel.url.as_str(), old.panel.user.as_str(), old.lang.as_str()), ("https://x", "u", "pt-BR"));
        assert!(old.panel.insecure && old.had_agents);
    }

    #[test]
    fn flags() {
        let a = |v: &[&str]| parse_flags(&v.iter().map(|s| s.to_string()).collect::<Vec<_>>());
        assert_eq!(a(&["--config", "/a"]), Some("/a".into()));
        assert_eq!(a(&["-config", "/b"]), Some("/b".into()));
        assert_eq!(a(&["--config=/c"]), Some("/c".into()));
        assert_eq!(a(&["-psn_0_1"]), None);
    }

    #[test]
    fn save_is_private_and_drops_agents() {
        let d = tmp("save");
        let p = d.join("desktop.toml");
        fs::write(&p, "[panel]\nurl = \"https://p\"\nuser = \"a\"\n[[agents]]\nname = \"fs\"\ntoken = \"secret\"\n")
            .unwrap();
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            fs::set_permissions(&p, fs::Permissions::from_mode(0o600)).unwrap();
        }
        let l = load_from(&plat(), p.clone(), false).unwrap();
        assert!(l.notice.is_some());
        let text = fs::read_to_string(&p).unwrap();
        assert!(!text.contains("secret") && !text.contains("agents"), "{text}");
        assert!(text.contains("url = \"https://p\""));
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            assert_eq!(fs::metadata(&p).unwrap().permissions().mode() & 0o777, 0o600);
        }
    }

    #[cfg(unix)]
    #[test]
    fn refuses_loose_mode() {
        use std::os::unix::fs::PermissionsExt;
        let d = tmp("mode");
        let p = d.join("desktop.toml");
        fs::write(&p, "[panel]\nurl = \"https://p\"\n").unwrap();
        fs::set_permissions(&p, fs::Permissions::from_mode(0o644)).unwrap();
        let e = load_from(&plat(), p, false).err().unwrap().to_string();
        assert!(e.contains("must be private and has mode 0644: run chmod 600"), "{e}");
    }

    #[test]
    fn lookup_order() {
        let d = tmp("lookup");
        let local = d.join("app/.config/desktop.toml");
        let user = d.join("user/desktop.toml");
        // nothing yet: created beside the executable (its folder is writable)
        fs::create_dir_all(d.join("app")).unwrap();
        assert_eq!(resolve_path(None, None, Some(local.clone()), user.clone()), (local.clone(), true));
        // a filled-in per-user file wins over a missing local one
        fs::create_dir_all(user.parent().unwrap()).unwrap();
        fs::write(&user, "[panel]\nurl = \"https://real\"\n").unwrap();
        assert_eq!(resolve_path(None, None, Some(local.clone()), user.clone()), (user.clone(), false));
        // an untouched example does not
        fs::write(&user, "[panel]\nurl = \"https://panel.example.local:8443\"\n").unwrap();
        assert_eq!(resolve_path(None, None, Some(local.clone()), user.clone()).0, local);
        // the flag and the variable come first
        assert_eq!(
            resolve_path(Some("/x.toml"), Some("/y.toml"), Some(local.clone()), user.clone()).0,
            PathBuf::from("/x.toml")
        );
        assert_eq!(resolve_path(None, Some("/y.toml"), Some(local), user).0, PathBuf::from("/y.toml"));
    }

    #[test]
    fn first_run_needs_setup() {
        let l = load_from(&plat(), PathBuf::from("/nonexistent/desktop.toml"), true).unwrap();
        assert!(l.needs_setup);
        assert_eq!(l.config.panel.user, "administrator");
    }
}
