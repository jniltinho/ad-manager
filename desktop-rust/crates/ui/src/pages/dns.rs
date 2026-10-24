//! The domain's DNS dialog (Computers › DNS) and the SMB sessions dialog of a server, as the web's DnsRecordsDialog and
//! SmbSessionsDialog. DNS: the zones (the DC locator zone `_msdcs.` read only), the records of a node with the names
//! below it to walk into, add an A, CNAME or PTR record and delete one (never the DCs', SOA, NS or SRV records).

use adm_core::api::{ApiError, q};
use adm_core::i18n::t;
use adm_core::tf;
use egui::{RichText, Ui};
use reqwest::Method;
use serde::Deserialize;
use serde_json::json;

use crate::app::Cx;
use crate::modal::{Dialog, Flow, footer};
use crate::widgets::{self, Kind};

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
struct Zone {
    name: String,
    reverse: bool,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
struct Record {
    name: String,
    #[serde(rename = "type")]
    kind: String,
    data: String,
    ttl: u32,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
struct Node {
    name: String,
    children: i64,
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
struct Records {
    records: Vec<Record>,
    nodes: Vec<Node>,
}

const TYPES: [&str; 3] = ["A", "CNAME", "PTR"];

/// The DCs' locator zone: never edited.
fn msdcs(zone: &str) -> bool {
    zone.to_lowercase().starts_with("_msdcs.")
}

/// A record that may be deleted here: A, CNAME or PTR, not the zone itself nor an underscore name, outside `_msdcs.`.
fn editable(zone: &str, r: &Record) -> bool {
    !msdcs(zone) && TYPES.contains(&r.kind.as_str()) && r.name != "@" && !r.name.split('.').any(|l| l.starts_with('_'))
}

pub fn open(cx: &mut Cx) {
    let id = cx.push(Dns::default());
    let api = cx.panel.clone();
    cx.run(async move { api.get::<Vec<Zone>>("/api/dns/zones").await }, move |app, zones| {
        let first = zones.iter().find(|z| !msdcs(&z.name)).or(zones.first()).map(|z| z.name.clone());
        if let Some(d) = app.cx.dialog_mut::<Dns>(id) {
            d.zones = zones;
            d.loading = false;
        }
        if let Some(z) = first {
            records(&mut app.cx, id, z, Vec::new());
        }
    });
}

/// Loads the records of `zone` at `path` (the last name, or the zone itself); only the last request fills the dialog.
fn records(cx: &mut Cx, id: u64, zone: String, path: Vec<String>) {
    let Some(d) = cx.dialog_mut::<Dns>(id) else { return };
    d.seq += 1;
    let seq = d.seq;
    if zone != d.zone {
        let reverse = d.zones.iter().any(|z| z.name == zone && z.reverse);
        d.kind = if reverse { 2 } else { 0 };
        d.name.clear();
        d.data.clear();
        d.ttl = "3600".into();
    }
    d.zone = zone.clone();
    d.path = path.clone();
    d.loading = true;
    d.error = None;
    let node = path.last().cloned().unwrap_or_else(|| "@".into());
    let api = cx.panel.clone();
    let url = format!("/api/dns/records?zone={}&node={}", q(&zone), q(&node));
    cx.spawn(async move { api.get::<Records>(&url).await }, move |app, r| {
        if let Some(d) = app.cx.dialog_mut::<Dns>(id)
            && d.seq == seq
        {
            d.loading = false;
            match r {
                Ok(r) => d.data_ = Some(r),
                Err(e) => d.error = Some(e.to_string()),
            }
        }
    });
}

#[derive(Default)]
struct Dns {
    zones: Vec<Zone>,
    zone: String,
    path: Vec<String>,
    data_: Option<Records>,
    loading: bool,
    error: Option<String>,
    seq: u64,
    kind: usize,
    name: String,
    data: String,
    ttl: String,
}

impl Dialog for Dns {
    fn title(&self) -> String {
        t("DNS of the domain").into()
    }

    fn width(&self) -> f32 {
        760.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        let me = cx.current_dialog();
        let muted = look.c("fg-muted");
        let mut go: Option<(String, Vec<String>)> = None;
        if let Some(e) = &self.error {
            ui.colored_label(look.c("err"), e);
        }
        if !self.zones.is_empty() {
            ui.horizontal_wrapped(|ui| {
                ui.spacing_mut().item_spacing.x = 8.0;
                let names: Vec<String> = self
                    .zones
                    .iter()
                    .map(|z| if z.reverse { format!("{} ({})", z.name, t("reverse")) } else { z.name.clone() })
                    .collect();
                let mut i = self.zones.iter().position(|z| z.name == self.zone).unwrap_or(0);
                if widgets::select(ui, "dns-zone", t("Zone"), &mut i, &names, 288.0)
                    && let Some(z) = self.zones.get(i)
                {
                    go = Some((z.name.clone(), Vec::new()));
                }
                // the path: the zone, then each name walked into
                ui.spacing_mut().item_spacing.x = 4.0;
                if widgets::button(ui, &look, "", &self.zone, Kind::Link).clicked() {
                    go = Some((self.zone.clone(), Vec::new()));
                }
                for (k, n) in self.path.iter().enumerate() {
                    ui.label(RichText::new("›").size(12.0));
                    if widgets::button(ui, &look, "", n, Kind::Link).clicked() {
                        go = Some((self.zone.clone(), self.path[..=k].to_vec()));
                    }
                }
            });
            ui.add_space(12.0);
        }
        let mut delete = None;
        if self.loading {
            ui.label(t("Loading…"));
        } else if let Some(r) = &self.data_ {
            let zone = self.zone.clone();
            let mut rows: Vec<[RichText; 5]> = r
                .records
                .iter()
                .map(|x| {
                    [
                        RichText::new(&x.name).monospace(),
                        RichText::new(&x.kind),
                        RichText::new(x.ttl.to_string()),
                        RichText::new(&x.data).monospace(),
                        RichText::new(""),
                    ]
                })
                .collect();
            let recs = r.records.clone();
            let n_rec = rows.len();
            for n in &r.nodes {
                rows.push([
                    RichText::new(format!("{} › {}", n.name, tf!("%d names below", n.children))).color(look.c("link")),
                    RichText::new(""),
                    RichText::new(""),
                    RichText::new(""),
                    RichText::new(""),
                ]);
            }
            let nodes = r.nodes.clone();
            let path = self.path.clone();
            let mut cell = |ui: &mut Ui, k: usize| {
                if k < n_rec {
                    if editable(&zone, &recs[k])
                        && widgets::button(ui, &look, "🗑", "", Kind::Link).on_hover_text(t("Delete")).clicked()
                    {
                        delete = Some(recs[k].clone());
                    }
                } else if let Some(n) = nodes.get(k - n_rec)
                    && ui
                        .interact(
                            ui.max_rect().with_min_x(ui.min_rect().left() - 2000.0),
                            ui.id().with(k),
                            egui::Sense::click(),
                        )
                        .on_hover_cursor(egui::CursorIcon::PointingHand)
                        .clicked()
                {
                    let mut p = path.clone();
                    p.push(n.name.clone());
                    go = Some((zone.clone(), p));
                }
            };
            widgets::mini_table(ui, &look, [t("Name"), t("Record type"), "TTL", t("Data"), ""], &rows, Some(&mut cell));
            if r.records.is_empty() && r.nodes.is_empty() {
                ui.label(RichText::new(t("No records.")).size(12.0).color(muted));
            }
            if !msdcs(&self.zone) {
                ui.add_space(12.0);
                ui.horizontal_wrapped(|ui| {
                    ui.spacing_mut().item_spacing.x = 8.0;
                    let names: Vec<String> = TYPES.iter().map(|s| (*s).to_string()).collect();
                    widgets::select(ui, "dns-type", t("Record type"), &mut self.kind, &names, 80.0);
                    ui.add(widgets::line(&mut self.name).hint_text(t("Name")).desired_width(128.0));
                    ui.label(RichText::new(format!(".{}", self.zone)).size(12.0).color(muted));
                    let hint =
                        if self.kind == 0 { "192.168.10.50".to_string() } else { format!("host.{}", cx.me.domain) };
                    ui.add(widgets::line(&mut self.data).hint_text(hint).desired_width(192.0));
                    ui.add(widgets::line(&mut self.ttl).desired_width(80.0)).on_hover_text("TTL");
                    let ready = !self.name.trim().is_empty() && !self.data.trim().is_empty();
                    if ui
                        .add_enabled_ui(ready, |ui| widgets::button(ui, &look, "", t("Add"), Kind::Primary))
                        .inner
                        .clicked()
                    {
                        let body = json!({
                            "zone": self.zone, "type": TYPES[self.kind.min(2)], "name": self.name.trim(),
                            "data": self.data.trim(), "ttl": self.ttl.trim().parse::<u32>().unwrap_or(3600),
                        });
                        let (zone, path, name) = (self.zone.clone(), self.path.clone(), self.name.trim().to_string());
                        let api = cx.panel.clone();
                        cx.run(
                            async move { api.act_with(Method::POST, "/api/dns/records", &body).await },
                            move |app, ()| {
                                app.cx.set_status(tf!("Record %s added.", name));
                                if let Some(d) = app.cx.dialog_mut::<Dns>(me) {
                                    d.name.clear();
                                    d.data.clear();
                                }
                                records(&mut app.cx, me, zone, path);
                            },
                        );
                    }
                });
            }
            ui.add_space(8.0);
            ui.label(
                RichText::new(t("With your account, on the domain controller of the panel. Records of the domain controllers, SOA, NS and SRV are not changed here."))
                    .size(11.0)
                    .color(muted),
            );
        }
        if let Some(r) = delete {
            let (zone, path) = (self.zone.clone(), self.path.clone());
            cx.confirm(
                t("Delete record"),
                tf!("Delete the record %s %s %s?", r.name, r.kind, r.data),
                t("Delete"),
                true,
                move |cx| {
                    let api = cx.panel.clone();
                    let url = format!(
                        "/api/dns/records?zone={}&name={}&type={}&data={}",
                        q(&zone),
                        q(&r.name),
                        q(&r.kind),
                        q(&r.data)
                    );
                    cx.run(async move { api.act(Method::DELETE, &url).await }, move |app, ()| {
                        app.cx.set_status(tf!("Record %s deleted.", r.name));
                        records(&mut app.cx, me, zone, path);
                    });
                },
            );
        }
        if let Some((z, p)) = go {
            records(cx, me, z, p);
        }
        let f = footer(ui, cx, None, t("Close"), false, true);
        if f.ok { Flow::Close } else { Flow::Keep }
    }
}

#[derive(Debug, Clone, Default, Deserialize)]
#[serde(default)]
struct Session {
    user: String,
    client: String,
    open_files: i64,
    active_seconds: i64,
    idle_seconds: i64,
}

/// "2 h 05 min", "3 min", "40 s", as the web's duration.
fn duration(s: i64) -> String {
    if s >= 3600 {
        format!("{} h {:02} min", s / 3600, (s % 3600) / 60)
    } else if s >= 60 {
        format!("{} min", s / 60)
    } else {
        format!("{s} s")
    }
}

/// Opens the SMB sessions of a server (read with the operator's account).
pub fn sessions(cx: &mut Cx, host: String) {
    let id = cx.push(Sessions { host: host.clone(), list: None, error: None });
    let api = cx.panel.clone();
    let url = format!("/api/computers/sessions?host={}", q(&host));
    // the reply is {"host", "sessions"}, as the web's dialog reads it
    #[derive(serde::Deserialize, Default)]
    #[serde(default)]
    struct Out {
        #[serde(deserialize_with = "adm_core::model::nd")]
        sessions: Vec<Session>,
    }
    cx.spawn(async move { api.get::<Out>(&url).await }, move |app, r: Result<Out, ApiError>| {
        if let Some(d) = app.cx.dialog_mut::<Sessions>(id) {
            match r {
                Ok(o) => d.list = Some(o.sessions),
                Err(e) => d.error = Some(e.to_string()),
            }
        }
    });
}

struct Sessions {
    host: String,
    list: Option<Vec<Session>>,
    error: Option<String>,
}

impl Dialog for Sessions {
    fn title(&self) -> String {
        tf!("SMB sessions on %s", self.host)
    }

    fn width(&self) -> f32 {
        760.0
    }

    fn ui(&mut self, ui: &mut Ui, cx: &mut Cx) -> Flow {
        let look = cx.look.clone();
        let muted = look.c("fg-muted");
        match (&self.list, &self.error) {
            (_, Some(e)) => {
                ui.colored_label(look.c("err"), e);
            }
            (None, None) => {
                ui.label(t("Loading…"));
            }
            (Some(l), None) => {
                let rows: Vec<[RichText; 5]> = l
                    .iter()
                    .map(|x| {
                        [
                            crate::style::semibold(&x.user, 12.0),
                            RichText::new(&x.client).monospace(),
                            RichText::new(x.open_files.to_string()),
                            RichText::new(duration(x.active_seconds)),
                            RichText::new(duration(x.idle_seconds)),
                        ]
                    })
                    .collect();
                widgets::mini_table_aligned(
                    ui,
                    &look,
                    [t("User"), t("Client"), t("Open files"), t("Connected for"), t("Idle for")],
                    [false, false, true, true, true],
                    &rows,
                    None,
                );
                if l.is_empty() {
                    ui.label(RichText::new(t("Nobody connected.")).size(12.0).color(muted));
                }
            }
        }
        ui.add_space(8.0);
        ui.label(
            RichText::new(t("Read with your account: it needs to be an administrator of that server."))
                .size(11.0)
                .color(muted),
        );
        let f = footer(ui, cx, None, t("Close"), false, true);
        if f.ok { Flow::Close } else { Flow::Keep }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn editable_records_and_durations() {
        let r = |name: &str, kind: &str| Record { name: name.into(), kind: kind.into(), ..Default::default() };
        assert!(editable("domain.local", &r("pc1", "A")));
        assert!(!editable("domain.local", &r("@", "A")), "the zone itself");
        assert!(!editable("domain.local", &r("_ldap._tcp", "TYPE33")));
        assert!(!editable("_msdcs.domain.local", &r("dc1", "CNAME")), "the DC locator zone");
        assert_eq!(
            (duration(40), duration(180), duration(7500)),
            ("40 s".to_string(), "3 min".to_string(), "2 h 05 min".to_string())
        );
    }
}
