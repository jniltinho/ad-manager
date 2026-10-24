//! The subset of the panel API's JSON the app uses (the same fields as the Fyne app's types).

use std::collections::HashMap;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

pub type Time = Option<DateTime<Utc>>;

/// A password typed in a form: wiped from memory when dropped, never printed (Debug shows `***`).
#[derive(Clone, Default)]
pub struct Secret(zeroize::Zeroizing<String>);

impl Secret {
    pub fn new(s: &str) -> Self {
        Self(zeroize::Zeroizing::new(s.to_string()))
    }

    pub fn expose(&self) -> &str {
        &self.0
    }

    pub fn is_empty(&self) -> bool {
        self.0.is_empty()
    }

    pub fn clear(&mut self) {
        *self = Self::default();
    }
}

impl PartialEq for Secret {
    fn eq(&self, other: &Self) -> bool {
        self.expose() == other.expose()
    }
}

impl std::fmt::Debug for Secret {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str("***")
    }
}

impl Serialize for Secret {
    fn serialize<S: serde::Serializer>(&self, s: S) -> Result<S::Ok, S::Error> {
        s.serialize_str(self.expose())
    }
}

impl<'de> Deserialize<'de> for Secret {
    fn deserialize<D: serde::Deserializer<'de>>(d: D) -> Result<Self, D::Error> {
        String::deserialize(d).map(|s| Self(zeroize::Zeroizing::new(s)))
    }
}

/// Go's zero time ("0001-01-01T00:00:00Z") and a missing value are both "unset".
/// A Go `null` (nil slice or map) decodes as the empty value.
pub fn nd<'de, D: serde::Deserializer<'de>, T: Default + Deserialize<'de>>(d: D) -> Result<T, D::Error> {
    Option::<T>::deserialize(d).map(Option::unwrap_or_default)
}

pub fn is_set(t: &Time) -> bool {
    t.is_some_and(|d| d.timestamp() > 0)
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct User {
    pub dn: String,
    pub sam: String,
    pub sid: String,
    pub upn: String,
    pub name: String,
    pub given_name: String,
    pub surname: String,
    pub mail: String,
    pub phone: String,
    pub title: String,
    pub department: String,
    pub description: String,
    pub enabled: bool,
    pub locked: bool,
    pub protected: bool,
    #[serde(deserialize_with = "nd")]
    pub groups: Vec<String>,
    pub last_logon: Time,
    pub pwd_last_set: Time,
    pub created: Time,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Group {
    pub name: String,
    #[serde(rename = "type")]
    pub kind: String,
    pub scope: String,
    pub description: String,
    pub protected: bool,
    pub member_count: i64,
    pub members: Option<Vec<String>>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct OrgUnit {
    pub dn: String,
    pub name: String,
    pub description: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Computer {
    pub dn: String,
    pub name: String,
    pub sam: String,
    pub dns_name: String,
    pub os: String,
    pub os_version: String,
    pub enabled: bool,
    pub is_dc: bool,
    pub last_logon: Time,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Brand {
    pub lang: String,
    pub theme: String,
    pub flat: bool,
    pub logo: bool,
    pub company: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Health {
    pub domain: String,
    pub netbios: String,
    pub version: String,
    pub setup: bool,
    pub brand: Brand,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct MeSettings {
    pub lang: String,
    pub inactive_days: i64,
    pub password_length: i64,
    pub default_ou: String,
    /// Idle minutes before the session ends (the footer says it).
    pub session_minutes: i64,
    /// The API documentation is served (the API tab).
    pub swagger: bool,
    /// The company's support link (the footer's Support; the project's when empty).
    pub support_url: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Me {
    pub user: String,
    pub domain: String,
    pub netbios: String,
    pub version: String,
    /// The agent queue monitor is served (the Queues tab).
    pub asynqmon: bool,
    pub brand: Brand,
    pub settings: MeSettings,
}

/// A Group Policy Object and where it is linked (`GET /api/gpos`).
#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Gpo {
    pub guid: String,
    pub dn: String,
    pub name: String,
    pub user_enabled: bool,
    pub machine_enabled: bool,
    pub user_version: i64,
    pub machine_version: i64,
    #[serde(deserialize_with = "nd")]
    pub extensions: Vec<String>,
    #[serde(deserialize_with = "nd")]
    pub links: Vec<GpoLink>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct GpoLink {
    pub target: String,
    pub enforced: bool,
    pub disabled: bool,
    pub order: i64,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct GpoContainer {
    pub dn: String,
    pub block_inheritance: bool,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Gpos {
    #[serde(deserialize_with = "nd")]
    pub gpos: Vec<Gpo>,
    #[serde(deserialize_with = "nd")]
    pub containers: Vec<GpoContainer>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct PolicySummary {
    pub complexity: bool,
    pub min_length: i64,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Summary {
    #[serde(deserialize_with = "nd")]
    pub users: HashMap<String, i64>,
    pub groups: i64,
    pub computers: i64,
    pub policy: PolicySummary,
    /// The LDAP URL the panel talks to.
    pub server: String,
    /// The domain controllers (Home's panel).
    #[serde(deserialize_with = "nd")]
    pub dcs: Vec<Computer>,
    /// Computers per operating system (Home's panel).
    #[serde(deserialize_with = "nd")]
    pub os: HashMap<String, i64>,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
#[serde(default)]
pub struct Policy {
    pub complexity: bool,
    pub min_length: i64,
    pub history: i64,
    pub min_age_days: i64,
    pub max_age_days: i64,
    pub lockout_threshold: i64,
    pub lockout_minutes: i64,
    pub lockout_window_minutes: i64,
}

/// A file server registered in the panel (`GET /api/agents`), reached only through the panel.
#[derive(Debug, Clone, Default, Deserialize, PartialEq)]
#[serde(default)]
pub struct Agent {
    pub id: i64,
    pub name: String,
    pub url: String,
    /// The panel accepts the agent's self-signed certificate.
    pub insecure: bool,
}

impl Agent {
    pub fn host(&self) -> String {
        url::Url::parse(&self.url)
            .ok()
            .and_then(|u| u.host_str().map(str::to_string))
            .unwrap_or_else(|| self.url.clone())
    }

    /// An agent on the panel's own machine (registered by a loopback URL).
    pub fn local(&self) -> bool {
        let h = self.host();
        let h = h.trim_start_matches('[').trim_end_matches(']');
        h == "127.0.0.1" || h == "localhost" || h == "::1"
    }

    /// Where the agent answers as the web shows it: "This server" for the panel's own machine, else its host.
    pub fn place(&self) -> String {
        if self.local() { crate::i18n::t("This server").to_string() } else { self.host() }
    }

    /// Tells apart servers with look-alike names: where each agent answers.
    pub fn label(&self) -> String {
        if self.local() {
            format!("{} · {}", self.name, crate::i18n::t("This server"))
        } else {
            format!("{} · {}", self.name, self.host())
        }
    }
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Share {
    pub name: String,
    pub path: String,
    pub comment: String,
    #[serde(deserialize_with = "nd")]
    pub rw_groups: Vec<String>,
    #[serde(deserialize_with = "nd")]
    pub ro_groups: Vec<String>,
    #[serde(deserialize_with = "nd")]
    pub rw_users: Vec<String>,
    #[serde(deserialize_with = "nd")]
    pub ro_users: Vec<String>,
    #[serde(deserialize_with = "nd")]
    pub full_groups: Vec<String>,
    #[serde(deserialize_with = "nd")]
    pub full_users: Vec<String>,
    pub browseable: bool,
    pub recycle: bool,
    pub abe: bool,
    pub audit: bool,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Task {
    pub id: String,
    pub state: String,
    pub last_error: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct AuditEvent {
    pub time: String,
    pub actor: String,
    pub ip: String,
    pub action: String,
    pub target: String,
    pub result: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct SambaFile {
    pub name: String,
    pub size: i64,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct SambaEntry {
    pub time: String,
    pub level: i64,
    pub source: String,
    pub message: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct SambaFiles {
    pub dir: String,
    #[serde(deserialize_with = "nd")]
    pub files: Vec<SambaFile>,
    pub error: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct BackupManifest {
    pub id: String,
    pub kind: String,
    pub origin: String,
    pub by: String,
    pub full: bool,
    pub file: String,
    pub size: i64,
    pub sha256: String,
    pub started: Time,
    pub status: String,
    pub error: String,
    #[serde(deserialize_with = "nd")]
    pub log: Vec<String>,
    #[serde(deserialize_with = "nd")]
    pub folders: Vec<String>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct RestoreStatus {
    pub backup: String,
    pub kind: String,
    pub dest: String,
    pub started: Time,
    pub status: String,
    pub error: String,
    #[serde(deserialize_with = "nd")]
    pub log: Vec<String>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct BackupServer {
    pub id: i64,
    pub name: String,
    pub dc: bool,
    #[serde(deserialize_with = "nd")]
    pub kinds: Vec<String>,
    #[serde(deserialize_with = "nd")]
    pub roots: Vec<String>,
    #[serde(deserialize_with = "nd")]
    pub backups: Vec<BackupManifest>,
    pub error: String,
    pub restore: Option<RestoreStatus>,
    #[serde(deserialize_with = "nd")]
    pub upload_kinds: Vec<String>,
    pub upload_max_mib: i64,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
#[serde(default)]
pub struct Retention {
    pub chains: i64,
    pub days: i64,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
#[serde(default)]
pub struct BackupSchedule {
    pub id: String,
    pub kind: String,
    pub cron: String,
    #[serde(deserialize_with = "nd", skip_serializing_if = "Vec::is_empty")]
    pub folders: Vec<String>,
    #[serde(skip_serializing_if = "is_zero")]
    pub full_every_days: i64,
    pub retention: Retention,
    pub enabled: bool,
}

fn is_zero(n: &i64) -> bool {
    *n == 0
}

fn is_empty(s: &str) -> bool {
    s.is_empty()
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
#[serde(default)]
pub struct SyncSource {
    pub host: String,
    #[serde(skip_serializing_if = "is_zero")]
    pub port: i64,
    pub share: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub path: String,
    pub auth: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub user: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub domain: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq)]
#[serde(default)]
pub struct SyncJob {
    pub id: String,
    pub name: String,
    /// "" (a folder) or "server": every share of the source server, each into the share of the same name here.
    #[serde(skip_serializing_if = "String::is_empty")]
    pub kind: String,
    pub rev: i64,
    pub source: SyncSource,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub password_file: String,
    pub dest: String,
    pub mode: String,
    #[serde(deserialize_with = "nd", skip_serializing_if = "Vec::is_empty")]
    pub exclude: Vec<String>,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub bwlimit: String,
    #[serde(skip_serializing_if = "is_zero")]
    pub transfers: i64,
    pub max_delete: i64,
    pub max_delete_pct: i64,
    #[serde(skip_serializing_if = "std::ops::Not::not")]
    pub verify: bool,
    pub reinherit: bool,
    #[serde(skip_serializing_if = "is_zero")]
    pub keep_days: i64,
    #[serde(skip_serializing_if = "is_zero")]
    pub keep_gib: i64,
    #[serde(skip_serializing_if = "String::is_empty")]
    pub cron: String,
    pub enabled: bool,
    #[serde(rename = "approved_rev", skip_serializing_if = "is_zero")]
    pub approved: i64,
    #[serde(skip_serializing_if = "std::ops::Not::not")]
    pub password_set: bool,
    /// Typed in the form: kept encrypted by the panel and the agent.
    #[serde(skip_serializing_if = "Secret::is_empty")]
    pub password: Secret,
    #[serde(skip_serializing_if = "is_empty")]
    pub confirm: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default, rename_all = "PascalCase")]
pub struct SyncPlan {
    pub new: i64,
    pub changed: i64,
    pub delete: i64,
    pub same: i64,
    pub errors: i64,
    pub new_bytes: i64,
    pub changed_bytes: i64,
    pub delete_bytes: i64,
    #[serde(deserialize_with = "nd", rename = "sample")]
    pub sample: HashMap<String, Vec<String>>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct SyncProgress {
    pub transfers: i64,
    pub bytes: i64,
    /// What a running run does now: preparing, copying, acl or finalizing.
    pub phase: String,
    /// The files to copy (copying), and the items whose permissions were applied (acl).
    pub total: i64,
    pub acl_items: i64,
    /// The files rclone compared (nothing new planned yet).
    pub checks: i64,
    /// A server job's share being brought, and its place among them.
    pub share: String,
    pub share_index: i64,
    pub share_count: i64,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct SyncResult {
    pub plan: SyncPlan,
    /// The files this run put in its quarantine.
    #[serde(deserialize_with = "nd")]
    pub quarantined: Vec<String>,
    pub transfers: i64,
    pub bytes: i64,
    pub deleted: i64,
    pub checked: bool,
    pub content_verified: bool,
    pub seconds: f64,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct SyncRun {
    pub id: String,
    pub job: String,
    pub preview: bool,
    pub origin: String,
    pub started: Time,
    pub finished: Time,
    pub status: String,
    pub error: String,
    #[serde(deserialize_with = "nd")]
    pub notes: Vec<String>,
    pub quarantine: String,
    pub progress: SyncProgress,
    pub result: Option<SyncResult>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct SyncList {
    #[serde(deserialize_with = "nd")]
    pub jobs: Vec<SyncJob>,
    #[serde(deserialize_with = "nd")]
    pub runs: Vec<SyncRun>,
    #[serde(deserialize_with = "nd")]
    pub roots: Vec<String>,
    pub file_server: bool,
    pub kerberos: bool,
    pub machine: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct CheckStep {
    pub name: String,
    pub ok: bool,
    pub detail: String,
    pub hint: String,
    #[serde(deserialize_with = "nd")]
    pub items: Vec<String>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct Quarantine {
    pub name: String,
    pub job: String,
    pub run: String,
    pub dest: String,
    pub created: Time,
    pub files: i64,
    pub size: i64,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct DiskSpace {
    pub root: String,
    pub free: i64,
    pub total: i64,
    pub low: bool,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct QuarantineList {
    #[serde(deserialize_with = "nd")]
    pub quarantines: Vec<Quarantine>,
    #[serde(deserialize_with = "nd")]
    pub space: Vec<DiskSpace>,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
pub struct QuarantineFile {
    pub path: String,
    pub size: i64,
    pub in_dest: bool,
    pub modified: Time,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn decodes_go_json() {
        let u: User = serde_json::from_str(
            r#"{"sam":"ana","name":"Ana","enabled":true,"locked":false,"protected":false,"last_logon":"0001-01-01T00:00:00Z"}"#,
        )
        .unwrap();
        assert!(!is_set(&u.last_logon) && u.enabled);
        let p: SyncPlan = serde_json::from_str(r#"{"New":2,"NewBytes":10,"sample":{"new":["a"]}}"#).unwrap();
        assert_eq!((p.new, p.new_bytes, p.sample["new"].len()), (2, 10, 1));
        let j = SyncJob { id: "a".into(), mode: "copy".into(), ..Default::default() };
        let v = serde_json::to_value(&j).unwrap();
        assert!(v.get("password").is_none() && v.get("approved_rev").is_none() && v["max_delete"] == 0);
        let with = SyncJob { password: Secret::new("s3cret"), ..j };
        assert_eq!(serde_json::to_value(&with).unwrap()["password"], "s3cret");
        assert!(!format!("{with:?}").contains("s3cret"), "Debug never shows the password");
    }

    #[test]
    fn agent_labels() {
        let _g = crate::i18n::tests::english();
        let a = Agent { id: 1, name: "fs".into(), url: "https://127.0.0.1:8445".into(), ..Default::default() };
        assert!(a.local());
        let b = Agent { id: 2, name: "fs".into(), url: "https://fs01.domain.local:8445".into(), ..Default::default() };
        assert_eq!(b.label(), "fs · fs01.domain.local");
        assert!(!b.local());
    }
}
