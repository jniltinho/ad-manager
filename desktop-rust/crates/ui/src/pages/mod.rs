//! The pages of the shell and their dialogs. Every page keeps its own state, draws itself every frame with the
//! services of [`crate::app::Cx`] and reloads through it; results of a reload older than the last one are dropped
//! (each page counts its reloads).

pub mod backup;
pub mod computers;
pub mod dns;
pub mod file_servers;
pub mod gpos;
pub mod groups;
pub mod home;
pub mod logs;
pub mod ous;
pub mod policy;
pub mod quarantine;
pub mod settings;
pub mod shares;
pub mod sync;
pub mod sync_wizard;
pub mod users;

use adm_core::logic::ou_path;
use adm_core::model::OrgUnit;
use egui::Ui;

/// A select of OUs by readable path.
#[derive(Debug, Clone, Default)]
pub struct OuPicker {
    labels: Vec<String>,
    dns: Vec<String>,
    sel: Option<usize>,
}

impl OuPicker {
    /// OUs (sorted by path) with `current` selected (case-insensitive DN), none when absent.
    pub fn new(ous: &[OrgUnit], current: &str) -> Self {
        Self {
            labels: ous.iter().map(|o| ou_path(&o.dn)).collect(),
            dns: ous.iter().map(|o| o.dn.clone()).collect(),
            sel: ous.iter().position(|o| o.dn.eq_ignore_ascii_case(current)),
        }
    }

    /// Adds a first entry (the domain root: DN "").
    pub fn with_root(mut self, label: &str) -> Self {
        self.labels.insert(0, label.into());
        self.dns.insert(0, String::new());
        self.sel = Some(self.sel.map_or(0, |s| s + 1));
        self
    }

    /// The DN chosen ("" when nothing is).
    pub fn dn(&self) -> String {
        self.sel.and_then(|i| self.dns.get(i).cloned()).unwrap_or_default()
    }

    pub fn chosen(&self) -> bool {
        self.sel.is_some()
    }

    pub fn ui(&mut self, ui: &mut Ui, id: &str, width: f32) {
        let shown = self.sel.and_then(|i| self.labels.get(i).cloned()).unwrap_or_default();
        egui::ComboBox::from_id_salt(id).width(width).selected_text(shown).show_ui(ui, |ui| {
            for (i, l) in self.labels.iter().enumerate() {
                if ui.selectable_label(self.sel == Some(i), l).clicked() {
                    self.sel = Some(i);
                }
            }
        });
    }
}

/// The search field of a list page, as the web's (its placeholder, no icon).
pub fn search(ui: &mut Ui, text: &mut String, hint: &str, width: f32) -> egui::Response {
    let r = ui.add(crate::widgets::line(text).hint_text(hint).desired_width(width));
    let _ = ui.ctx().accesskit_node_builder(r.id, |n| n.set_label(adm_core::i18n::t("Filter")));
    r
}
