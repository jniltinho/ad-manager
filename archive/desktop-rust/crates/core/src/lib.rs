//! AD Manager desktop core: everything that is not drawing — the configuration file, the panel API client and its
//! TLS trust, the interface language, the theme data, the API models and the page rules. The UI crate (`adm-ui`)
//! draws with it; the platform crates give it the operating system (`adm_platform::Platform`).

pub mod api;
pub mod config;
pub mod forms;
pub mod gpu;
pub mod i18n;
pub mod logic;
pub mod model;
#[cfg(feature = "testkit")]
pub mod testkit;
pub mod theme;
pub mod tls;

/// Images bundled with the app.
pub mod assets {
    /// The AD Manager logo in navy ink (for light backgrounds).
    pub const LOGO_NAVY: &[u8] = include_bytes!("../assets/ad-manager-logo.png");
    /// The web's elliptic shadow under the login box (`login_shadow.png`), and its light glow for dark pages.
    pub const LOGIN_SHADOW: &[u8] = include_bytes!("../assets/login-shadow.png");
    pub const LOGIN_SHADOW_LIGHT: &[u8] = include_bytes!("../assets/login-shadow-light.png");
    /// The AD Manager logo in white ink (for dark backgrounds).
    pub const LOGO_WHITE: &[u8] = include_bytes!("../assets/ad-manager-logo-light.png");
    /// The window icon.
    pub const ICON: &[u8] = include_bytes!("../assets/icon.png");
}
