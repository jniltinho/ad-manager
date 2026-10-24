//! AD Manager desktop interface (egui): the screens, pages, dialogs and widgets. The binary builds an [`app::App`]
//! with the platform of the system it runs on and hands it to eframe.

pub mod app;
pub mod depth;
pub mod files;
pub mod fonts;
pub mod login;
pub mod logo;
pub mod modal;
pub mod own_password;
pub mod pages;
pub mod setup;
pub mod shell;
pub mod style;
pub mod table;
pub mod widgets;

pub use app::{App, GpuStart, Start, StartError};
