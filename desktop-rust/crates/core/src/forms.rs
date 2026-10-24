//! The forms whose payloads carry protections (shares, restores, sync jobs, users): their state and the exact body
//! they send, built here so the bodies are unit-tested without a GUI. The UI only binds widgets to these fields.

use serde_json::{Value, json};

use crate::logic::{split_list, unc};
use crate::model::{Share, SyncJob, User};

/// The editable attributes of a user (ad.UserInput without the login), in form order: the first four go in the
/// left column.
pub const USER_FIELDS: [(&str, &str); 8] = [
    ("name", "Display name"),
    ("given_name", "First name"),
    ("surname", "Last name"),
    ("mail", "E-mail"),
    ("phone", "Phone"),
    ("title", "Job title"),
    ("department", "Department"),
    ("description", "Description"),
];

/// The values of USER_FIELDS, in the same order.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct UserFields(pub [String; 8]);

impl UserFields {
    pub fn from_user(u: &User) -> Self {
        Self([
            u.name.clone(),
            u.given_name.clone(),
            u.surname.clone(),
            u.mail.clone(),
            u.phone.clone(),
            u.title.clone(),
            u.department.clone(),
            u.description.clone(),
        ])
    }

    /// A copy of a template user: only job title, department and description.
    pub fn from_template(u: &User) -> Self {
        let mut f = Self::default();
        f.0[5] = u.title.clone();
        f.0[6] = u.department.clone();
        f.0[7] = u.description.clone();
        f
    }

    /// The JSON body (trimmed values).
    pub fn body(&self) -> serde_json::Map<String, Value> {
        USER_FIELDS.iter().zip(&self.0).map(|((k, _), v)| ((*k).to_string(), Value::from(v.trim()))).collect()
    }
}

/// The groups a copied user starts with: the template's, without Domain Users (the primary group).
pub fn template_groups(u: &User) -> Vec<String> {
    u.groups.iter().filter(|g| !g.eq_ignore_ascii_case("Domain Users")).cloned().collect()
}

/// The new/edit share form (the same `share:create` task for both).
#[derive(Debug, Clone, Default, PartialEq)]
pub struct ShareForm {
    pub editing: bool,
    pub name: String,
    /// The folder: an edit's path as published; a new share's path is `root`/`sub` (`path`).
    pub folder: String,
    /// A new share: one of the agent's roots and the folder under it (filled from the name until edited).
    pub root: String,
    pub sub: String,
    pub comment: String,
    pub full: String,
    pub rw: String,
    pub ro: String,
    pub full_users: String,
    pub rw_users: String,
    pub ro_users: String,
    pub hidden: bool,
    pub recycle: bool,
    pub abe: bool,
    pub audit: bool,
    pub create_path: bool,
}

impl ShareForm {
    /// A new share under `root` (the agent's first allowed folder; /srv/shares when it lists none).
    pub fn new_share(root: &str) -> Self {
        let root = if root.is_empty() { "/srv/shares" } else { root };
        Self { root: root.into(), create_path: true, ..Default::default() }
    }

    /// The edit form: name and folder fixed, the lists and flags of the share; "create folder" stays on (on edit it
    /// reapplies the folder ACLs, the content is untouched).
    pub fn edit(s: &Share) -> Self {
        Self {
            editing: true,
            name: s.name.clone(),
            folder: s.path.clone(),
            comment: s.comment.clone(),
            full: s.full_groups.join(", "),
            // write access without the names that have full control (the web's notIn)
            rw: not_in(&s.rw_groups, &s.full_groups).join(", "),
            ro: s.ro_groups.join(", "),
            full_users: s.full_users.join(", "),
            rw_users: not_in(&s.rw_users, &s.full_users).join(", "),
            ro_users: s.ro_users.join(", "),
            hidden: !s.browseable,
            recycle: s.recycle,
            abe: s.abe,
            audit: s.audit,
            create_path: true,
            ..Default::default()
        }
    }

    /// Typing a new share's name fills its folder, until the folder is edited apart (`old`: the name before).
    pub fn name_changed(&mut self, old: &str) {
        if !self.editing && (self.sub.is_empty() || self.sub == crate::logic::share_slug(old)) {
            self.sub = crate::logic::share_slug(&self.name);
        }
    }

    /// The folder the share publishes.
    pub fn path(&self) -> String {
        if self.editing {
            self.folder.clone()
        } else {
            format!("{}/{}", self.root.trim_end_matches('/'), self.sub.trim())
        }
    }

    pub fn ready(&self) -> bool {
        !self.name.trim().is_empty() && (self.editing || !self.sub.trim().is_empty())
    }

    /// The task payload.
    pub fn payload(&self) -> Value {
        json!({
            "name": self.name.trim(), "path": self.path(), "comment": self.comment,
            "rw_groups": split_list(&self.rw), "ro_groups": split_list(&self.ro),
            "rw_users": split_list(&self.rw_users), "ro_users": split_list(&self.ro_users),
            "full_groups": split_list(&self.full), "full_users": split_list(&self.full_users),
            "hidden": self.hidden, "create_path": self.create_path, "recycle": self.recycle, "abe": self.abe,
            "audit": self.audit,
        })
    }

    pub fn unc(&self, server: &str) -> String {
        format!(r"\\{server}\{}", self.name.trim())
    }
}

/// `list` without the names of `drop` (case-insensitive).
fn not_in(list: &[String], drop: &[String]) -> Vec<String> {
    list.iter().filter(|n| !drop.iter().any(|d| d.eq_ignore_ascii_case(n))).cloned().collect()
}

/// What a restore asks, per backup kind.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct RestoreForm {
    pub kind: String,
    /// Data: the backup's folders and which are checked (all by default).
    pub folders: Vec<(String, bool)>,
    /// Data: only one folder inside them.
    pub sub: String,
    /// Data: over the original folders instead of a new one.
    pub original: bool,
    /// Configuration: shares, ACLs of the share roots, smb.conf (the first two by default).
    pub parts: [bool; 3],
    pub confirm: String,
}

pub const RESTORE_PARTS: [&str; 3] = ["shares", "acls", "smbconf"];

impl RestoreForm {
    pub fn new(kind: &str, folders: &[String]) -> Self {
        Self {
            kind: kind.into(),
            folders: folders.iter().map(|f| (f.clone(), true)).collect(),
            parts: [true, true, false],
            ..Default::default()
        }
    }

    /// A DC backup is never restored over a DC in use (it is restored by the setup of a new server).
    pub fn refused(&self) -> bool {
        self.kind == "dc"
    }

    /// Whether the typed code is asked: everything that replaces what is in use (data only over the originals).
    pub fn needs_code(&self) -> bool {
        match self.kind.as_str() {
            "fileserver-data" => self.original,
            "fileserver-config" | "samba-files" | "panel" => true,
            _ => false,
        }
    }

    pub fn body(&self) -> Value {
        let mut b = json!({ "confirm": self.confirm.trim() });
        if self.kind == "fileserver-data" {
            let sub = self.sub.trim();
            let folders: Vec<&str> = if sub.is_empty() {
                self.folders.iter().filter(|f| f.1).map(|f| f.0.as_str()).collect()
            } else {
                vec![sub]
            };
            b["folders"] = json!(folders);
            b["target"] = json!(if self.original { "original" } else { "new" });
        }
        if self.kind == "fileserver-config" {
            let parts: Vec<&str> = RESTORE_PARTS.iter().zip(self.parts).filter(|p| p.1).map(|p| *p.0).collect();
            b["parts"] = json!(parts);
        }
        b
    }
}

/// Step 4 of the sync wizard: how and when.
#[derive(Debug, Clone, Default, PartialEq)]
pub struct SyncForm {
    pub mirror: bool,
    pub max_delete: String,
    pub max_delete_pct: String,
    pub keep_days: String,
    pub keep_gib: String,
    pub cron: String,
    pub name: String,
    pub id: String,
    pub exclude: String,
    pub bwlimit: String,
    pub transfers: String,
    pub verify: bool,
    pub reinherit: bool,
    pub confirm: String,
}

/// Why a sync job cannot be saved (English source strings of the catalog).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SyncFormError {
    /// The mirror word was not typed.
    Mirror,
}

impl SyncForm {
    pub fn from_job(j: &SyncJob) -> Self {
        Self {
            mirror: j.mode == "mirror",
            max_delete: j.max_delete.to_string(),
            max_delete_pct: j.max_delete_pct.to_string(),
            keep_days: if j.keep_days == 0 { "30".into() } else { j.keep_days.to_string() },
            keep_gib: j.keep_gib.to_string(),
            cron: j.cron.clone(),
            name: j.name.clone(),
            id: j.id.clone(),
            exclude: j.exclude.join("\n"),
            bwlimit: j.bwlimit.clone(),
            transfers: j.transfers.to_string(),
            verify: j.verify,
            reinherit: j.reinherit,
            confirm: String::new(),
        }
    }

    /// The job to PUT: `base` (source, destination, stored password file) with this form's values. `mirror_word` is
    /// the translated "MIRROR" the operator must type; `password` the one typed in step 1 (NTLM only; empty keeps
    /// the stored one).
    pub fn to_job(&self, base: &SyncJob, mirror_word: &str, password: &str) -> Result<SyncJob, SyncFormError> {
        let num = |s: &str| s.trim().parse::<i64>().unwrap_or(0);
        let mut j = base.clone();
        j.name = self.name.trim().into();
        j.id = self.id.trim().into();
        j.cron = self.cron.trim().into();
        j.enabled = !j.cron.is_empty();
        j.mode = "copy".into();
        j.confirm.clear();
        if self.mirror {
            if !self.confirm.trim().eq_ignore_ascii_case(mirror_word) {
                return Err(SyncFormError::Mirror);
            }
            j.mode = "mirror".into();
            j.confirm = j.id.clone(); // the word was typed: the agent wants the job's id
        }
        j.max_delete = num(&self.max_delete);
        j.max_delete_pct = num(&self.max_delete_pct);
        j.keep_days = num(&self.keep_days);
        j.keep_gib = num(&self.keep_gib);
        j.transfers = num(&self.transfers);
        j.bwlimit = self.bwlimit.trim().into();
        j.verify = self.verify;
        j.reinherit = self.reinherit;
        j.exclude = self.exclude.lines().map(str::trim).filter(|x| !x.is_empty()).map(str::to_string).collect();
        j.password.clear();
        if j.source.auth == "ntlm" {
            j.password = crate::model::Secret::new(password);
        } else {
            j.password_file.clear();
        }
        Ok(j)
    }
}

/// A new sync job with the wizard's defaults (Kerberos when the server can use it).
pub fn new_sync_job(kerberos: bool) -> SyncJob {
    let mut j = SyncJob {
        mode: "copy".into(),
        max_delete: 100,
        max_delete_pct: 20,
        keep_days: 30,
        transfers: 4,
        verify: true,
        reinherit: true,
        exclude: vec!["Thumbs.db".into(), "~$*".into(), "*.tmp".into()],
        ..Default::default()
    };
    j.source.auth = if kerberos { "kerberos" } else { "ntlm" }.into();
    j
}

/// The UNC the wizard is looking at.
pub fn sync_unc(j: &SyncJob) -> String {
    unc(&j.source.host, &j.source.share, &j.source.path)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn share_payload_keeps_name_and_lists() {
        let s = Share {
            name: "dados".into(),
            path: "/srv/shares/dados".into(),
            rw_groups: vec!["DOMAIN\\Finance".into()],
            ro_users: vec!["DOMAIN\\ana".into()],
            browseable: false,
            audit: true,
            ..Default::default()
        };
        let mut f = ShareForm::edit(&s);
        f.name = "renamed".into(); // the form does not offer it; even so the name follows the field
        f.name_changed("dados");
        assert_eq!(f.folder, "/srv/shares/dados", "an edit never moves the folder");
        f.ro = " DOMAIN\\Sales , ,DOMAIN\\HR".into();
        let p = f.payload();
        assert_eq!(p["path"], "/srv/shares/dados");
        assert_eq!(p["ro_groups"], json!(["DOMAIN\\Sales", "DOMAIN\\HR"]));
        assert_eq!(p["rw_groups"], json!(["DOMAIN\\Finance"]));
        assert_eq!(p["ro_users"], json!(["DOMAIN\\ana"]));
        assert_eq!(
            (p["hidden"].as_bool(), p["audit"].as_bool(), p["create_path"].as_bool()),
            (Some(true), Some(true), Some(true))
        );
        let mut n = ShareForm::new_share("/srv/shares/");
        n.name = "Finanças Equipe".into();
        n.name_changed("");
        assert_eq!(n.path(), "/srv/shares/financas-equipe", "the web's slug: no accents, others as dashes");
        n.sub = "fin".into(); // edited apart: the name no longer moves it
        let old = n.name.clone();
        n.name = "Finance".into();
        n.name_changed(&old);
        assert_eq!(n.path(), "/srv/shares/fin");
        let full = Share {
            full_groups: vec![r"DOMAIN\Board".into()],
            rw_groups: vec![r"domain\board".into(), r"DOMAIN\Finance".into()],
            ..Default::default()
        };
        assert_eq!(ShareForm::edit(&full).rw, "DOMAIN\\Finance", "full control left out of write access");
    }

    #[test]
    fn restore_guards() {
        assert!(RestoreForm::new("dc", &[]).refused());
        let folders = vec!["/srv/shares/a".to_string(), "/srv/shares/b".to_string()];
        let mut d = RestoreForm::new("fileserver-data", &folders);
        assert!(!d.needs_code());
        assert_eq!(d.body(), json!({"confirm": "", "folders": folders, "target": "new"}));
        d.original = true;
        d.confirm = " abcdef12 ".into();
        assert!(d.needs_code());
        assert_eq!(d.body()["target"], "original");
        assert_eq!(d.body()["confirm"], "abcdef12");
        d.sub = "/srv/shares/a/Finance".into();
        assert_eq!(d.body()["folders"], json!(["/srv/shares/a/Finance"]));
        let c = RestoreForm::new("fileserver-config", &[]);
        assert!(c.needs_code());
        assert_eq!(c.body()["parts"], json!(["shares", "acls"]));
        assert!(RestoreForm::new("panel", &[]).needs_code() && RestoreForm::new("samba-files", &[]).needs_code());
    }

    #[test]
    fn sync_job_rules() {
        let mut base = new_sync_job(false);
        base.source.host = "old-fs".into();
        base.password_file = "/etc/x".into();
        let mut f = SyncForm::from_job(&base);
        f.name = "dados".into();
        f.id = "dados".into();
        f.mirror = true;
        assert_eq!(f.to_job(&base, "MIRROR", "").unwrap_err(), SyncFormError::Mirror);
        f.confirm = "mirror".into();
        let j = f.to_job(&base, "MIRROR", "").unwrap();
        assert_eq!((j.mode.as_str(), j.confirm.as_str(), j.enabled), ("mirror", "dados", false));
        // NTLM edit with no password typed: none sent, the stored file kept
        assert!(j.password.is_empty() && j.password_file == "/etc/x");
        let v = serde_json::to_value(&j).unwrap();
        assert!(v.get("password").is_none());
        // Kerberos: no password file
        base.source.auth = "kerberos".into();
        f.cron = "0 1 * * *".into();
        let k = f.to_job(&base, "MIRROR", "secret").unwrap();
        assert!(k.password.is_empty() && k.password_file.is_empty() && k.enabled);
        assert_eq!(k.exclude, ["Thumbs.db", "~$*", "*.tmp"]);
    }

    #[test]
    fn user_fields() {
        let u = User {
            name: " Ana ".into(),
            title: "CFO".into(),
            groups: vec!["Domain Users".into(), "Finance".into()],
            ..Default::default()
        };
        assert_eq!(UserFields::from_user(&u).body()["name"], "Ana");
        let t = UserFields::from_template(&u);
        assert!(t.0[0].is_empty() && t.0[5] == "CFO");
        assert_eq!(template_groups(&u), ["Finance"]);
    }
}
